#!/usr/bin/env python3
"""Strict Phase 19 capacity evidence validation and repetition aggregation."""

from __future__ import annotations

import hashlib
import json
import math
import re
import statistics
from pathlib import Path


SCHEMA = "gopulse.phase19.capacity-evidence.v1"
PROFILE_SCHEMA = "gopulse.phase19.capacity-profile.v1"
REPORT_SCHEMA = "gopulse.phase19.load.v1"
RECIPE_SCHEMA = "gopulse.phase18.recipe.v1"
STAGES = ("rps-50", "rps-100", "rps-150", "rps-200")
STAGE_RPS = {name: int(name[4:]) for name in STAGES}
RECIPE_DIGEST = "sha256:0e61a5473f72d735ab322261e312290249f39997b649837fea32bfe2c947cf14"
ALLOWED_STOP_REASONS = {"oom", "ownership_lost", "unsafe_cleanup", "profile_hard_error"}
PROJECT_PATTERN = re.compile(r"^gopulse-p19-[0-9a-f]{12}$")
SHA256_PATTERN = re.compile(r"^sha256:[0-9a-f]{64}$")
REVISION_PATTERN = re.compile(r"^[0-9a-f]{40}$")
VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
METRICS = (
    "achieved_rps",
    "p50_ms",
    "p95_ms",
    "p99_ms",
    "max_ms",
    "explicit_reject_rate",
    "timeout_rate",
    "unexpected_error_rate",
    "max_schedule_lag_ms",
)

EXPECTED_ROUTES = {
    "GET /api/v1/posts": ("read", "GET", [200]),
    "GET /api/v1/posts/:postId": ("read", "GET", [200]),
    "GET /api/v1/posts/following": ("read", "GET", [200]),
    "GET /api/v1/posts/:postId/comments": ("read", "GET", [200]),
    "GET /api/v1/users/:username": ("read", "GET", [200]),
    "GET /api/v1/search/posts": ("search", "GET", [200]),
    "GET /api/v1/search/users": ("search", "GET", [200]),
    "GET /api/v1/notifications": ("notification_bookmark_read", "GET", [200]),
    "GET /api/v1/bookmarks": ("notification_bookmark_read", "GET", [200]),
    "GET /api/v1/users/me/following": ("notification_bookmark_read", "GET", [200]),
    "GET /api/v1/users/me/followers": ("notification_bookmark_read", "GET", [200]),
    "POST /api/v1/posts": ("content_write", "POST", [201]),
    "POST /api/v1/posts/:postId/comments": ("content_write", "POST", [201]),
    "PATCH /api/v1/posts/:postId": ("content_write", "PATCH", [200]),
    "DELETE /api/v1/posts/:postId": ("content_write", "DELETE", [204]),
    "PUT /api/v1/posts/:postId/like": ("interaction_write", "PUT", [204]),
    "DELETE /api/v1/posts/:postId/like": ("interaction_write", "DELETE", [204]),
    "PUT /api/v1/users/:userId/follow": ("interaction_write", "PUT", [204]),
    "DELETE /api/v1/users/:userId/follow": ("interaction_write", "DELETE", [204]),
    "PUT /api/v1/posts/:postId/bookmark": ("interaction_write", "PUT", [204]),
    "POST /api/v1/auth/login": ("identity_session", "POST", [200]),
    "GET /api/v1/users/me": ("identity_session", "GET", [200]),
}


def sha256_file(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def _require(condition, message):
    if not condition:
        raise ValueError(message)


def _object(value, message):
    _require(isinstance(value, dict), message)
    return value


def _exact_keys(value, required, message):
    _object(value, message)
    _require(set(value) == set(required), message + " has unexpected or missing fields")


def _number(value, message, minimum=0):
    _require(isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value) and value >= minimum, message)
    return float(value)


def _digest(value, message):
    _require(isinstance(value, str) and SHA256_PATTERN.fullmatch(value), message)
    return value


def validate_profile(profile):
    """Validate the checked-in profile without relying on an optional package."""
    _exact_keys(
        profile,
        {"schema_version", "profile_id", "target_candidate_version", "host", "recipe", "workload", "stages", "repetitions", "gates", "stop_conditions", "sampling", "statistics"},
        "profile",
    )
    _require(profile["schema_version"] == PROFILE_SCHEMA, "profile schema is invalid")
    _require(isinstance(profile["profile_id"], str) and profile["profile_id"], "profile id is invalid")
    _require(VERSION_PATTERN.fullmatch(profile["target_candidate_version"]), "profile candidate version is invalid")
    host = profile["host"]
    _exact_keys(host, {"platform", "host_os", "kernel_contains", "cpu_count_min", "memory_bytes_min", "swap_bytes_min", "disk_free_bytes_min", "docker_server_os", "docker_server_arch", "compose_major"}, "profile host")
    _require(host["platform"] == "linux/amd64" and host["host_os"] == "Linux" and host["docker_server_os"] == "linux" and host["docker_server_arch"] == "amd64" and host["compose_major"] == 2, "profile host identity is invalid")
    for field in ("cpu_count_min", "memory_bytes_min", "swap_bytes_min", "disk_free_bytes_min"):
        _number(host[field], "profile host resource is invalid", 1)

    recipe = profile["recipe"]
    _exact_keys(recipe, {"schema_version", "seed", "counts", "id_ranges", "digest"}, "profile recipe")
    _require(recipe["schema_version"] == RECIPE_SCHEMA and recipe["seed"] == 18002005, "profile recipe identity is invalid")
    _require(recipe["digest"] == RECIPE_DIGEST, "profile recipe digest differs from deterministic recipe")
    counts = recipe["counts"]
    expected_counts = {"users": 5000, "posts": 50000, "comments": 100000, "post_likes": 200000, "user_follows": 200000, "post_bookmarks": 25000, "business_outbox": 550000, "notifications": 500000}
    _exact_keys(counts, expected_counts, "profile recipe counts")
    _require(counts == expected_counts, "profile recipe counts differ from contract")
    ranges = recipe["id_ranges"]
    expected_ranges = {"users": {"first": 1, "last": 5000}, "posts": {"first": 1, "last": 50000}, "comments": {"first": 1, "last": 100000}}
    _exact_keys(ranges, expected_ranges, "profile recipe id ranges")
    _require(ranges == expected_ranges, "profile recipe id ranges differ from contract")

    workload = profile["workload"]
    _exact_keys(workload, {"virtual_users", "request_timeout_seconds", "mix", "routes"}, "profile workload")
    _require(workload["virtual_users"] == 1024, "profile virtual user count is not frozen")
    _require(_number(workload["request_timeout_seconds"], "profile request timeout is invalid", 0.000001) <= 60, "profile request timeout is too large")
    expected_mix = [("read", 45), ("search", 10), ("notification_bookmark_read", 10), ("content_write", 15), ("interaction_write", 15), ("identity_session", 5)]
    _require([(item.get("category"), item.get("percent")) for item in workload["mix"]] == expected_mix, "profile route mix differs from contract")
    _require(isinstance(workload["routes"], list) and len(workload["routes"]) == len(EXPECTED_ROUTES), "profile route contract does not enumerate all routes")
    route_templates = set()
    for route in workload["routes"]:
        _exact_keys(route, {"category", "method", "template", "allowed_statuses"}, "profile route")
        _require(route["template"] not in route_templates, "profile route template is duplicated")
        route_templates.add(route["template"])
        _require(isinstance(route["allowed_statuses"], list) and route["allowed_statuses"], "profile route statuses are empty")
        _require(route["template"] in EXPECTED_ROUTES and (route["category"], route["method"], route["allowed_statuses"]) == EXPECTED_ROUTES[route["template"]], "profile route contract differs from workload semantics")
    _require(route_templates == set(EXPECTED_ROUTES), "profile route contract is missing a fixed route")

    stages = profile["stages"]
    _require(isinstance(stages, list) and len(stages) == 4, "profile must contain four stages")
    for item, name in zip(stages, STAGES):
        _exact_keys(item, {"name", "target_rps", "warmup_target_rps", "warmup_seconds", "measurement_seconds", "recovery_seconds"}, "profile stage")
        _require(item["name"] == name and item["target_rps"] == STAGE_RPS[name], "profile stage order or target drifted")
        _number(item["warmup_target_rps"], "profile warmup rate is invalid", 0.000001)
        _require(item["warmup_target_rps"] < item["target_rps"], "profile warmup rate must be below measurement rate")
        for field in ("warmup_seconds", "measurement_seconds", "recovery_seconds"):
            _require(_number(item[field], "profile stage window is invalid", 0.000001) <= 3600, "profile stage window is too large")
    _require(profile["repetitions"] == 3, "profile repetition count is not three")

    gates = profile["gates"]
    _exact_keys(gates, {"synchronous", "asynchronous", "observability"}, "profile gates")
    sync = gates["synchronous"]
    _exact_keys(sync, {"min_achieved_rps_ratio", "max_p95_ms", "max_p99_ms", "max_timeout_rate", "max_unexpected_error_rate", "max_explicit_reject_rate", "max_schedule_lag_ms"}, "profile synchronous gates")
    _require(0 < sync["min_achieved_rps_ratio"] <= 1, "profile achieved-rate gate is invalid")
    for field in ("max_p95_ms", "max_p99_ms", "max_schedule_lag_ms"):
        _number(sync[field], "profile synchronous gate is invalid", 0.000001)
    for field in ("max_timeout_rate", "max_unexpected_error_rate", "max_explicit_reject_rate"):
        _require(0 <= sync[field] <= 1, "profile synchronous error gate is invalid")
    async_gates = gates["asynchronous"]
    _exact_keys(async_gates, {"max_recovery_seconds", "max_outbox_pending", "max_rabbit_ready", "max_rabbit_unacked", "max_kafka_lag"}, "profile asynchronous gates")
    _number(async_gates["max_recovery_seconds"], "profile asynchronous recovery gate is invalid", 0.000001)
    for field in ("max_outbox_pending", "max_rabbit_ready", "max_rabbit_unacked", "max_kafka_lag"):
        _number(async_gates[field], "profile asynchronous queue gate is invalid", 0)
    obs = gates["observability"]
    _exact_keys(obs, {"max_recovery_seconds", "require_metric_progress", "require_log_progress", "require_event_progress"}, "profile observability gates")
    _number(obs["max_recovery_seconds"], "profile observability recovery gate is invalid", 0.000001)

    stops = profile["stop_conditions"]
    _exact_keys(stops, {"oom", "ownership_lost", "unsafe_cleanup", "profile_hard_error", "preserve_unexecuted_stages"}, "profile stop conditions")
    _require(all(isinstance(value, bool) and value for value in stops.values()), "profile must enable every safe stop condition")
    sampling = profile["sampling"]
    _exact_keys(sampling, {"interval_seconds", "required_signals"}, "profile sampling")
    _require(_number(sampling["interval_seconds"], "profile sampling interval is invalid", 0.000001) <= 60, "profile sampling interval is too large")
    _require(isinstance(sampling["required_signals"], list) and len(set(sampling["required_signals"])) == len(sampling["required_signals"]), "profile sampling signals are invalid")
    expected_signals = {"host_cpu", "host_rss", "load_cpu", "load_rss", "load_scheduler_lag", "sut_cpu", "sut_rss", "sut_saturation", "outbox", "rabbitmq", "kafka_lag"}
    _require(set(sampling["required_signals"]) == expected_signals, "profile sampling signal set differs from contract")
    statistics_contract = profile["statistics"]
    _exact_keys(statistics_contract, {"percentiles", "aggregations", "cv_definition", "retain_raw_repetitions", "do_not_merge_latency_samples"}, "profile statistics")
    _require(statistics_contract["percentiles"] == [0.5, 0.95, 0.99] and statistics_contract["aggregations"] == ["median", "min", "max", "cv"] and statistics_contract["cv_definition"] == "population_stddev_div_mean_percent" and statistics_contract["retain_raw_repetitions"] and statistics_contract["do_not_merge_latency_samples"], "profile statistics contract is invalid")
    return profile


def load_profile(path):
    path = Path(path)
    document = json.loads(path.read_text(encoding="utf-8"))
    validate_profile(document)
    return document, sha256_file(path)


def _outcomes(window):
    outcomes = _object(window.get("outcomes"), "measurement outcomes are missing")
    required = {"requests", "succeeded", "explicit_rejects", "rejected_429", "rejected_503", "timeouts", "transport_errors", "unexpected_errors"}
    _exact_keys(outcomes, required, "measurement outcomes")
    _require(all(isinstance(outcomes[key], int) and outcomes[key] >= 0 for key in required), "measurement outcome count is invalid")
    _require(outcomes["requests"] == sum(outcomes[key] for key in ("succeeded", "explicit_rejects", "timeouts", "transport_errors", "unexpected_errors")), "measurement outcomes do not add to requests")
    _require(outcomes["explicit_rejects"] == outcomes["rejected_429"] + outcomes["rejected_503"], "429/503 rejection classification is inconsistent")
    return outcomes


def _window(report, name, target_rps, duration_seconds=None):
    window = report if isinstance(report, dict) and report.get("name") == name else _object(report.get(name), "load report window is missing")
    required = {"name", "target_rps", "duration_seconds", "scheduled_slots", "dropped_slots", "max_schedule_lag_ms", "completed_requests", "achieved_rps", "outcomes", "statuses", "latency"}
    _exact_keys(window, required, "load report window")
    _require(window["name"] == name, "load report window name is inconsistent")
    _number(window["duration_seconds"], "load report window duration is invalid", 0.000001)
    if duration_seconds is not None:
        _require(window["duration_seconds"] == duration_seconds, "load report window duration differs from profile")
    for field in ("target_rps", "scheduled_slots", "dropped_slots", "max_schedule_lag_ms", "completed_requests", "achieved_rps"):
        _number(window[field], "load report window value is invalid", 0)
    _require(window["scheduled_slots"] == window["dropped_slots"] + window["completed_requests"], "scheduled, dropped, and completed slot accounting differs")
    outcomes = _outcomes(window)
    _require(window["completed_requests"] == outcomes["requests"], "window completion count differs from outcomes")
    statuses = _object(window["statuses"], "window statuses are missing")
    _require(all(isinstance(value, int) and value >= 0 for value in statuses.values()), "window status counts are invalid")
    _require(sum(statuses.values()) == outcomes["requests"], "window status counts differ from outcomes")
    _require(statuses.get("429", 0) == outcomes["rejected_429"] and statuses.get("503", 0) == outcomes["rejected_503"], "window rejection status counts differ from outcomes")
    latency = _object(window["latency"], "window latency is missing")
    _exact_keys(latency, {"p50_ms", "p95_ms", "p99_ms", "max_ms"}, "window latency")
    for value in latency.values():
        _number(value, "window latency value is invalid", 0)
    _require(window["target_rps"] == target_rps, "window target rate is inconsistent")
    expected_achieved = window["completed_requests"] / window["duration_seconds"]
    _require(math.isclose(window["achieved_rps"], expected_achieved, rel_tol=1e-9, abs_tol=1e-9), "window achieved rate differs from completion count")
    return window


def _report(document, profile, candidate, recipe, repeat_number):
    required = {"schema_version", "profile", "candidate", "recipe", "repeat", "execution_status", "started_at", "finished_at", "stages", "total", "load_process"}
    _object(document, "load report must be an object")
    _require(required <= set(document) <= required | {"resources", "stop"}, "load report has unexpected or missing fields")
    # Optional resources/stop fields are accepted by the report schema; the
    # evidence verifier gets their presence from the round-level receipts.
    _require(document.get("schema_version") == REPORT_SCHEMA, "load report schema is invalid")
    _require(document["execution_status"] in {"complete", "incomplete"}, "load report execution status is invalid")
    _require(document["profile"] == {"id": profile["profile_id"], "sha256": candidate["profile_sha256"]}, "load report profile binding differs")
    _require(document["candidate"] == candidate["binding"], "load report candidate binding differs")
    _require(document["recipe"] == {"schema_version": RECIPE_SCHEMA, "seed": profile["recipe"]["seed"], "digest": profile["recipe"]["digest"]}, "load report recipe binding differs")
    _require(document["repeat"] == {"number": repeat_number, "total": 3}, "load report repetition binding differs")
    stages = document["stages"]
    _require(isinstance(stages, list) and len(stages) == 4, "load report must retain all four stages")
    stage_total = {field: 0 for field in ("requests", "succeeded", "explicit_rejects", "rejected_429", "rejected_503", "timeouts", "transport_errors", "unexpected_errors")}
    for stage, name in zip(stages, STAGES):
        _exact_keys(stage, {"name", "target_rps", "status", "warmup", "measurement", "recovery"}, "load report stage")
        _require(stage["name"] == name and stage["target_rps"] == STAGE_RPS[name] and stage["status"] in {"complete", "not_executed"}, "load report stage binding is invalid")
        profile_stage = profile["stages"][STAGES.index(name)]
        warmup = _window(stage["warmup"], "warmup", profile_stage["warmup_target_rps"], profile_stage["warmup_seconds"])
        measurement = _window(stage["measurement"], "measurement", STAGE_RPS[name], profile_stage["measurement_seconds"])
        recovery = _window(stage["recovery"], "recovery", 0, profile_stage["recovery_seconds"])
        _require(recovery["scheduled_slots"] == 0 and recovery["completed_requests"] == 0 and not recovery["statuses"], "recovery window contains load")
        for window in (warmup, measurement):
            for field in stage_total:
                stage_total[field] += window["outcomes"][field]
    total = _object(document["total"], "load report total is missing")
    _outcomes({"outcomes": total})
    _require(total == stage_total, "load report total differs from stage outcomes")
    if document["execution_status"] == "complete":
        _require(all(stage["status"] == "complete" for stage in stages), "complete load report contains an unexecuted stage")
    else:
        for stage in stages:
            if stage["status"] == "not_executed":
                _require(all(window["completed_requests"] == 0 and window["scheduled_slots"] == 0 for window in (stage["warmup"], stage["measurement"], stage["recovery"])), "unexecuted stage contains fabricated requests")
    load_process = _object(document["load_process"], "load process stats are missing")
    _exact_keys(load_process, {"rss_bytes", "goroutines", "heap_alloc_bytes"}, "load process stats")
    _require(all(isinstance(value, int) and value >= 0 for value in load_process.values()), "load process stats are invalid")
    return document


def _rate(outcomes, field):
    return outcomes[field] / outcomes["requests"] if outcomes["requests"] else 1.0


def _measurement_values(report, name):
    stage = next(item for item in report["stages"] if item["name"] == name)
    measurement = stage["measurement"]
    outcomes = measurement["outcomes"]
    latency = measurement["latency"]
    return {
        "achieved_rps": measurement["achieved_rps"],
        "p50_ms": latency["p50_ms"],
        "p95_ms": latency["p95_ms"],
        "p99_ms": latency["p99_ms"],
        "max_ms": latency["max_ms"],
        "explicit_reject_rate": _rate(outcomes, "explicit_rejects"),
        "timeout_rate": _rate(outcomes, "timeouts"),
        "unexpected_error_rate": _rate(outcomes, "unexpected_errors"),
        "max_schedule_lag_ms": measurement["max_schedule_lag_ms"],
    }


def aggregate_values(values):
    _require(len(values) == 3, "capacity aggregation requires exactly three raw repetitions")
    mean = statistics.fmean(values)
    cv = statistics.pstdev(values) / mean * 100 if mean else 0.0
    return {"median": statistics.median(values), "min": min(values), "max": max(values), "cv": cv}


def aggregate_repetitions(rounds):
    _require(len(rounds) == 3, "capacity aggregation requires three repetitions")
    result = []
    for name in STAGES:
        raw = {metric: [_measurement_values(item["load_report"], name)[metric] for item in rounds] for metric in METRICS}
        result.append({"stage": name, "target_rps": STAGE_RPS[name], "raw": raw, "aggregates": {metric: aggregate_values(values) for metric, values in raw.items()}})
    return result


def _stage_gate(profile, round_value, stage_index):
    name = STAGES[stage_index]
    report_stage = round_value["load_report"]["stages"][stage_index]
    if report_stage["status"] != "complete":
        return False
    measurement = report_stage["measurement"]
    values = _measurement_values(round_value["load_report"], name)
    sync = profile["gates"]["synchronous"]
    passed = (
        values["achieved_rps"] >= STAGE_RPS[name] * sync["min_achieved_rps_ratio"]
        and values["p95_ms"] <= sync["max_p95_ms"]
        and values["p99_ms"] <= sync["max_p99_ms"]
        and values["timeout_rate"] <= sync["max_timeout_rate"]
        and values["unexpected_error_rate"] <= sync["max_unexpected_error_rate"]
        and values["explicit_reject_rate"] <= sync["max_explicit_reject_rate"]
        and values["max_schedule_lag_ms"] <= sync["max_schedule_lag_ms"]
        and measurement["dropped_slots"] == 0
    )
    asynchronous = round_value["asynchronous"][stage_index]
    async_gate = profile["gates"]["asynchronous"]
    passed = passed and all(
        asynchronous[field] <= async_gate[key]
        for field, key in (
            ("recovery_seconds", "max_recovery_seconds"),
            ("outbox_pending", "max_outbox_pending"),
            ("rabbit_ready", "max_rabbit_ready"),
            ("rabbit_unacked", "max_rabbit_unacked"),
            ("kafka_lag", "max_kafka_lag"),
        )
    )
    observability = round_value["observability"][stage_index]
    obs_gate = profile["gates"]["observability"]
    passed = passed and observability["recovery_seconds"] <= obs_gate["max_recovery_seconds"]
    for field, required in (("metric_progress", obs_gate["require_metric_progress"]), ("log_progress", obs_gate["require_log_progress"]), ("event_progress", obs_gate["require_event_progress"])):
        if required:
            passed = passed and observability[field] is True
    return bool(passed)


def _validate_resources(directory, reference):
    _exact_keys(reference, {"raw_path", "summary_path", "sha256", "records"}, "resource reference")
    raw = _safe_relative(directory, reference["raw_path"])
    summary = _safe_relative(directory, reference["summary_path"])
    _require(raw.is_file() and summary.is_file(), "resource evidence file is missing")
    _require(sha256_file(raw) == reference["sha256"], "raw resource evidence digest differs")
    raw_records = [line for line in raw.read_text(encoding="utf-8").splitlines() if line.strip()]
    _require(len(raw_records) == reference["records"] and len(raw_records) > 0, "raw resource record count differs")
    for line in raw_records:
        value = json.loads(line)
        _require(value.get("schema") == "gopulse.phase19.resources.v1", "raw resource schema differs")
    summary_document = json.loads(summary.read_text(encoding="utf-8"))
    _require(summary_document.get("schema") == "gopulse.phase19.resources.v1" and summary_document.get("summary", {}).get("samples") == len(raw_records), "resource summary differs from raw evidence")


def _safe_relative(directory, value):
    _require(isinstance(value, str) and value and not Path(value).is_absolute(), "evidence path must be relative")
    path = (Path(directory) / value).resolve()
    root = Path(directory).resolve()
    _require(path == root or root in path.parents, "evidence path escapes its directory")
    return path


def _validate_cleanup(cleanup, project, incomplete=False):
    _exact_keys(cleanup, {"status", "project", "owned", "global_prune"}, "cleanup receipt")
    _require(cleanup["project"] == project and PROJECT_PATTERN.fullmatch(project) and isinstance(cleanup["owned"], bool) and cleanup["global_prune"] is False, "cleanup ownership is invalid")
    if not incomplete:
        _require(cleanup["owned"] is True, "complete cleanup is not owned")
    _require(cleanup["status"] in ({"passed", "failed"} if incomplete else {"passed"}), "cleanup status is invalid")


def _sensitive_scan(value, path="evidence"):
    if isinstance(value, dict):
        for key, child in value.items():
            if re.search(r"(?i)(password|secret|token|cookie|authorization|dsn)", str(key)):
                raise ValueError("sensitive field in " + path + "." + str(key))
            _sensitive_scan(child, path + "." + str(key))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _sensitive_scan(child, path + "[" + str(index) + "]")
    elif isinstance(value, str) and re.search(r"(?i)(password|secret|token|authorization|cookie|dsn)\s*[=:]", value):
        raise ValueError("sensitive value in " + path)


def validate_evidence(document, directory=None):
    """Validate evidence in place and recompute all derived aggregation fields."""
    _object(document, "capacity evidence must be an object")
    root_required = {"schema", "execution_status", "capability_status", "profile", "candidate", "recipe", "rounds", "cleanup", "capability"}
    root_allowed = root_required | {"aggregates", "host", "profile_document"}
    _require(root_required <= set(document) <= root_allowed, "capacity evidence has unexpected or missing fields")
    _require(document.get("schema") == SCHEMA, "capacity evidence schema is invalid")
    _require(document.get("execution_status") in {"complete", "incomplete"}, "execution status is invalid")
    incomplete = document["execution_status"] == "incomplete"
    _exact_keys(document["capability"], {"stage_gates"}, "capacity capability result")
    _require(document.get("capability_status") == ("incomplete" if incomplete else document.get("capability_status")), "incomplete evidence cannot carry a capacity conclusion")
    if not incomplete:
        _require(document.get("capability_status") in {"target_met", "boundary_found"}, "complete evidence has no capacity status")
    profile_ref = _object(document.get("profile"), "profile binding is missing")
    _exact_keys(profile_ref, {"path", "id", "sha256"}, "profile binding")
    _digest(profile_ref["sha256"], "profile digest is invalid")
    candidate = _object(document.get("candidate"), "candidate binding is missing")
    _exact_keys(candidate, {"version", "revision", "manifest_sha256"}, "candidate binding")
    _require(VERSION_PATTERN.fullmatch(candidate["version"]) and REVISION_PATTERN.fullmatch(candidate["revision"]), "candidate identity is invalid")
    _digest(candidate["manifest_sha256"], "candidate manifest digest is invalid")
    recipe = _object(document.get("recipe"), "recipe binding is missing")
    _exact_keys(recipe, {"schema_version", "seed", "digest"}, "recipe binding")
    _require(recipe == {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "digest": RECIPE_DIGEST}, "recipe binding differs from profile")
    if directory is not None:
        profile_path = _safe_relative(directory, profile_ref["path"])
        profile, actual_profile_digest = load_profile(profile_path)
        _require(actual_profile_digest == profile_ref["sha256"] and profile["profile_id"] == profile_ref["id"], "profile file binding differs")
    else:
        profile = document.get("profile_document")
        _require(profile is not None, "profile document is required for in-memory validation")
        validate_profile(profile)
        _require(profile_ref["id"] == profile["profile_id"], "profile id differs")
    _require(candidate["version"] == profile["target_candidate_version"], "candidate version differs from profile target")

    rounds = document.get("rounds")
    _require(isinstance(rounds, list) and len(rounds) == 3, "evidence must retain exactly three repetition records")
    normalized_rounds = []
    for index, round_value in enumerate(rounds, 1):
        _object(round_value, "repetition record is invalid")
        _exact_keys(round_value, {"number", "project", "execution_status", "load_report", "resources", "asynchronous", "observability", "cleanup", "stop"}, "repetition record")
        _require(round_value["number"] == index and round_value["execution_status"] in {"complete", "incomplete"}, "repetition order or state is invalid")
        _require(PROJECT_PATTERN.fullmatch(round_value["project"]), "repetition Compose project is invalid")
        load_report = round_value["load_report"]
        if isinstance(load_report, str) and directory is not None:
            load_report = json.loads(_safe_relative(directory, load_report).read_text(encoding="utf-8"))
            round_value["load_report"] = load_report
        _report(load_report, profile, {"profile_sha256": profile_ref["sha256"], "binding": candidate}, recipe, index)
        _require(round_value["execution_status"] == load_report["execution_status"], "repetition state differs from load report")
        _require(isinstance(round_value["asynchronous"], list) and len(round_value["asynchronous"]) == 4, "asynchronous stage receipts are incomplete")
        _require(isinstance(round_value["observability"], list) and len(round_value["observability"]) == 4, "observability stage receipts are incomplete")
        for stage_index, stage_value in enumerate(round_value["asynchronous"]):
            _exact_keys(stage_value, {"recovery_seconds", "outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag"}, "asynchronous receipt")
            _require(stage_value["recovery_seconds"] == profile["stages"][stage_index]["recovery_seconds"], "asynchronous recovery window differs from profile")
            for value in stage_value.values():
                _number(value, "asynchronous receipt value is invalid", 0)
        for stage_index, stage_value in enumerate(round_value["observability"]):
            _exact_keys(stage_value, {"recovery_seconds", "metric_progress", "log_progress", "event_progress"}, "observability receipt")
            _require(stage_value["recovery_seconds"] == profile["stages"][stage_index]["recovery_seconds"], "observability recovery window differs from profile")
            _number(stage_value["recovery_seconds"], "observability recovery value is invalid", 0)
            _require(all(isinstance(stage_value[field], bool) for field in ("metric_progress", "log_progress", "event_progress")), "observability progress is invalid")
        if directory is not None:
            _validate_resources(directory, round_value["resources"])
        _validate_cleanup(round_value["cleanup"], round_value["project"], incomplete=round_value["execution_status"] == "incomplete")
        stop = round_value["stop"]
        if round_value["execution_status"] == "incomplete":
            _object(stop, "incomplete repetition has no safe-stop reason")
            _require(stop.get("reason") in ALLOWED_STOP_REASONS, "incomplete repetition has an unsafe stop reason")
            completed_stages = [stage for stage in load_report["stages"] if stage["status"] == "complete"]
            if len(completed_stages) == 4:
                _require(stop.get("reason") == "unsafe_cleanup" and round_value["cleanup"]["status"] == "failed", "incomplete repetition did not preserve an unexecuted stage")
        else:
            _require(stop is None, "complete repetition has a stop reason")
        normalized_rounds.append({**round_value, "load_report": load_report})

    _require(incomplete == any(item["execution_status"] == "incomplete" for item in normalized_rounds), "evidence execution status differs from repetition states")
    first_candidate = normalized_rounds[0]["load_report"]["candidate"]
    for round_value in normalized_rounds[1:]:
        _require(round_value["load_report"]["candidate"] == first_candidate, "candidate binding drifted between repetitions")
        _require(round_value["load_report"]["profile"] == normalized_rounds[0]["load_report"]["profile"], "profile binding drifted between repetitions")
        _require(round_value["load_report"]["recipe"] == normalized_rounds[0]["load_report"]["recipe"], "recipe binding drifted between repetitions")

    if incomplete:
        _require("aggregates" not in document or all(item["stage"] in STAGES for item in document["aggregates"]), "incomplete evidence contains invalid aggregate stages")
    else:
        aggregates = aggregate_repetitions(normalized_rounds)
        _require(document.get("aggregates") == aggregates, "capacity aggregates were not recomputed from the three raw repetitions")
        stage_gates = []
        for stage_index, name in enumerate(STAGES):
            passed = all(_stage_gate(profile, round_value, stage_index) for round_value in normalized_rounds)
            stage_gates.append({"stage": name, "passed": passed})
        expected_status = "target_met" if all(item["passed"] for item in stage_gates) else "boundary_found"
        _require(document.get("capability", {}).get("stage_gates") == stage_gates, "capacity gate result was not recomputed")
        _require(document["capability_status"] == expected_status, "capacity status does not match the frozen gates")
    _require(document["cleanup"] == normalized_rounds[-1]["cleanup"], "root cleanup receipt differs from the final repetition")
    _validate_cleanup(document["cleanup"], normalized_rounds[-1]["project"], incomplete=incomplete)
    _sensitive_scan(document)
    return document
