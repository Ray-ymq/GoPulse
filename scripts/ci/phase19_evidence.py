#!/usr/bin/env python3
"""Strict Phase 19 capacity evidence validation.

The verifier treats progress JSONL, resource JSONL, and stage-scoped recovery
receipts as the source of truth. Summary fields are accepted only after those
raw files are re-read and their digests, counts, bindings, and derived gates
are recomputed.
"""

from __future__ import annotations

import hashlib
import json
import math
import re
import statistics
from datetime import datetime
from pathlib import Path


SCHEMA = "gopulse.phase19.capacity-evidence.v1"
PROFILE_SCHEMA = "gopulse.phase19.capacity-profile.v1"
REPORT_SCHEMA = "gopulse.phase19.load.v1"
PROGRESS_SCHEMA = "gopulse.phase19.progress.v1"
RESOURCE_SCHEMA = "gopulse.phase19.resources.v1"
RECIPE_SCHEMA = "gopulse.phase18.recipe.v1"
STAGES = ("rps-50", "rps-100", "rps-150", "rps-200")
STAGE_RPS = {name: int(name[4:]) for name in STAGES}
RECIPE_DIGEST = "sha256:0e61a5473f72d735ab322261e312290249f39997b649837fea32bfe2c947cf14"
ALLOWED_STOP_REASONS = {"oom", "ownership_lost", "unsafe_cleanup", "profile_hard_error"}
PROJECT_PATTERN = re.compile(r"^gopulse-p19-[0-9a-f]{12}$")
SHA256_PATTERN = re.compile(r"^sha256:[0-9a-f]{64}$")
REVISION_PATTERN = re.compile(r"^[0-9a-f]{40}$")
VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
COMPOSE_VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
METRICS = (
    "achieved_rps", "p50_ms", "p95_ms", "p99_ms", "max_ms",
    "explicit_reject_rate", "timeout_rate", "unexpected_error_rate",
    "max_schedule_lag_ms",
)
EXPECTED_COMPONENTS = (
    "mysql", "redis", "rabbitmq", "elasticsearch", "observability-elasticsearch",
    "kafka", "victoriametrics", "backend", "backend-2", "business-worker",
    "business-worker-2", "search-indexer", "search-indexer-2", "router", "router-2",
    "marshaller", "marshaller-2", "monitor", "frontend", "admin-frontend",
)
EXPECTED_SIGNALS = {
    "host_cpu", "host_rss", "load_cpu", "load_rss", "load_scheduler_lag",
    "sut_cpu", "sut_rss", "sut_saturation", "outbox", "rabbitmq", "kafka_lag",
}
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


def _version(value, message):
    _require(isinstance(value, str) and COMPOSE_VERSION_PATTERN.fullmatch(value), message)
    return tuple(int(part) for part in value.split("."))


def _timestamp(value, message):
    _require(isinstance(value, str), message)
    try:
        datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ValueError(message) from error
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
    _exact_keys(host, {"platform", "host_os", "kernel_contains", "cpu_count_min", "memory_bytes_min", "swap_bytes_min", "disk_free_bytes_min", "docker_server_os", "docker_server_arch", "compose_min_version"}, "profile host")
    _require(host["platform"] == "linux/amd64" and host["host_os"] == "Linux" and host["docker_server_os"] == "linux" and host["docker_server_arch"] == "amd64", "profile host identity is invalid")
    _version(host["compose_min_version"], "profile Compose minimum version is invalid")
    for field in ("cpu_count_min", "memory_bytes_min", "swap_bytes_min", "disk_free_bytes_min"):
        _number(host[field], "profile host resource is invalid", 1)

    recipe = profile["recipe"]
    _exact_keys(recipe, {"schema_version", "seed", "counts", "id_ranges", "digest"}, "profile recipe")
    _require(recipe["schema_version"] == RECIPE_SCHEMA and recipe["seed"] == 18002005, "profile recipe identity is invalid")
    _require(recipe["digest"] == RECIPE_DIGEST, "profile recipe digest differs from deterministic recipe")
    expected_counts = {"users": 5000, "posts": 50000, "comments": 100000, "post_likes": 200000, "user_follows": 200000, "post_bookmarks": 25000, "business_outbox": 550000, "notifications": 500000}
    _exact_keys(recipe["counts"], expected_counts, "profile recipe counts")
    _require(recipe["counts"] == expected_counts, "profile recipe counts differ from contract")
    expected_ranges = {"users": {"first": 1, "last": 5000}, "posts": {"first": 1, "last": 50000}, "comments": {"first": 1, "last": 100000}}
    _exact_keys(recipe["id_ranges"], expected_ranges, "profile recipe id ranges")
    _require(recipe["id_ranges"] == expected_ranges, "profile recipe id ranges differ from contract")

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
    _exact_keys(sampling, {"interval_seconds", "required_signals", "required_components"}, "profile sampling")
    _require(_number(sampling["interval_seconds"], "profile sampling interval is invalid", 0.000001) <= 60, "profile sampling interval is too large")
    _require(set(sampling["required_signals"]) == EXPECTED_SIGNALS and len(sampling["required_signals"]) == len(EXPECTED_SIGNALS), "profile sampling signal set differs from contract")
    _require(tuple(sampling["required_components"]) == EXPECTED_COMPONENTS, "profile sampling component set differs from contract")
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


def _load_progress(path):
    records = []
    for line_number, line in enumerate(Path(path).read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        try:
            value = json.loads(line)
        except json.JSONDecodeError as error:
            raise ValueError("invalid capacity progress at line %d" % line_number) from error
        _object(value, "capacity progress record is invalid")
        _require(value.get("schema_version") == PROGRESS_SCHEMA, "capacity progress schema is invalid")
        _require(value.get("sequence") == len(records), "capacity progress sequence is not contiguous")
        _require(value.get("event") in {"run_started", "window_started", "window_finished", "recovery_started", "recovery_finished", "run_finished"}, "capacity progress event is invalid")
        _require(value.get("status") in {"started", "complete", "incomplete"}, "capacity progress status is invalid")
        _timestamp(value.get("at"), "capacity progress timestamp is invalid")
        if records:
            _require(value["at"] >= records[-1]["at"], "capacity progress timestamp regressed")
        records.append(value)
    _require(records, "capacity progress is empty")
    return records


def _validate_progress(directory, reference, report):
    _exact_keys(reference, {"path", "sha256", "records"}, "capacity progress reference")
    path = _safe_relative(directory, reference["path"])
    _require(path.is_file(), "capacity progress file is missing")
    _require(sha256_file(path) == reference["sha256"], "capacity progress digest differs")
    records = _load_progress(path)
    _require(len(records) == reference["records"], "capacity progress record count differs")
    _require(len(records) >= 2 and records[0]["event"] == "run_started" and records[-1]["event"] == "run_finished", "capacity progress boundaries are incomplete")
    _require(records[-1]["status"] == report["execution_status"], "capacity progress state differs from load report")
    if report["execution_status"] == "complete":
        expected = [("run_started", "", "")]
        for stage in STAGES:
            expected.extend([
                ("window_started", stage, "warmup"), ("window_finished", stage, "warmup"),
                ("window_started", stage, "measurement"), ("window_finished", stage, "measurement"),
                ("recovery_started", stage, "recovery"), ("recovery_finished", stage, "recovery"),
            ])
        expected.append(("run_finished", "", ""))
        actual = [(item["event"], item.get("stage", ""), item.get("window", "")) for item in records]
        _require(actual == expected, "capacity progress does not retain every stage boundary")
        for item in records:
            if item["event"] != "window_finished":
                continue
            stage_index = STAGES.index(item["stage"])
            report_window = report["stages"][stage_index][item["window"]]
            _require(item.get("scheduled_slots") == report_window["scheduled_slots"] and item.get("dropped_slots", 0) == report_window["dropped_slots"], "capacity progress window accounting differs")
    return records


def _report(document, profile, candidate, recipe, repeat_number, directory=None):
    required = {"schema_version", "profile", "candidate", "recipe", "repeat", "execution_status", "started_at", "finished_at", "stages", "total", "load_process", "progress"}
    _object(document, "load report must be an object")
    _require(required <= set(document) <= required | {"resources", "stop"}, "load report has unexpected or missing fields")
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
    if directory is not None:
        _validate_progress(directory, document["progress"], document)
    return document


def _rate(outcomes, field):
    return outcomes[field] / outcomes["requests"] if outcomes["requests"] else 1.0


def _measurement_values(report, name):
    stage = next(item for item in report["stages"] if item["name"] == name)
    measurement = stage["measurement"]
    outcomes = measurement["outcomes"]
    latency = measurement["latency"]
    return {
        "achieved_rps": measurement["achieved_rps"], "p50_ms": latency["p50_ms"], "p95_ms": latency["p95_ms"], "p99_ms": latency["p99_ms"], "max_ms": latency["max_ms"],
        "explicit_reject_rate": _rate(outcomes, "explicit_rejects"), "timeout_rate": _rate(outcomes, "timeouts"), "unexpected_error_rate": _rate(outcomes, "unexpected_errors"), "max_schedule_lag_ms": measurement["max_schedule_lag_ms"],
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
    passed = values["achieved_rps"] >= STAGE_RPS[name] * sync["min_achieved_rps_ratio"] and values["p95_ms"] <= sync["max_p95_ms"] and values["p99_ms"] <= sync["max_p99_ms"] and values["timeout_rate"] <= sync["max_timeout_rate"] and values["unexpected_error_rate"] <= sync["max_unexpected_error_rate"] and values["explicit_reject_rate"] <= sync["max_explicit_reject_rate"] and values["max_schedule_lag_ms"] <= sync["max_schedule_lag_ms"] and measurement["dropped_slots"] == 0
    asynchronous = round_value["asynchronous"][stage_index]
    async_gate = profile["gates"]["asynchronous"]
    passed = passed and asynchronous["terminal"]["outbox_pending"] <= async_gate["max_outbox_pending"] and asynchronous["terminal"]["rabbit_ready"] <= async_gate["max_rabbit_ready"] and asynchronous["terminal"]["rabbit_unacked"] <= async_gate["max_rabbit_unacked"] and asynchronous["terminal"]["kafka_lag"] <= async_gate["max_kafka_lag"] and asynchronous["recovery_seconds"] <= async_gate["max_recovery_seconds"]
    observability = round_value["observability"][stage_index]
    obs_gate = profile["gates"]["observability"]
    passed = passed and observability["recovery_seconds"] <= obs_gate["max_recovery_seconds"]
    if obs_gate["require_metric_progress"]:
        passed = passed and observability["terminal"]["metric_count"] > observability["baseline"]["metric_count"]
    if obs_gate["require_log_progress"]:
        passed = passed and observability["terminal"]["logs_count"] > observability["baseline"]["logs_count"]
    if obs_gate["require_event_progress"]:
        passed = passed and observability["terminal"]["events_count"] > observability["baseline"]["events_count"]
    return bool(passed)


def _safe_relative(directory, value):
    _require(isinstance(value, str) and value and not Path(value).is_absolute(), "evidence path must be relative")
    path = (Path(directory) / value).resolve()
    root = Path(directory).resolve()
    _require(path == root or root in path.parents, "evidence path escapes its directory")
    return path


def _validate_resources(directory, reference, profile):
    _exact_keys(reference, {"raw_path", "summary_path", "sha256", "records", "required_components"}, "resource reference")
    _require(reference["required_components"] == list(profile["sampling"]["required_components"]), "resource component contract differs")
    raw = _safe_relative(directory, reference["raw_path"])
    summary = _safe_relative(directory, reference["summary_path"])
    _require(raw.is_file() and summary.is_file(), "resource evidence file is missing")
    _require(sha256_file(raw) == reference["sha256"], "raw resource evidence digest differs")
    raw_records = [json.loads(line) for line in raw.read_text(encoding="utf-8").splitlines() if line.strip()]
    _require(len(raw_records) == reference["records"] and len(raw_records) > 0, "raw resource record count differs")
    active_resource_record = False
    for record in raw_records:
        _require(record.get("schema") == RESOURCE_SCHEMA, "raw resource schema differs")
        missing_signals = record.get("missing_signals", [])
        if record.get("sample_kind") == "initial":
            _require(set(missing_signals) <= {"load_cpu", "load_rss", "load_scheduler_lag"}, "raw initial resource signal is missing")
        else:
            _require(missing_signals == [], "raw resource signal is missing")
            active_resource_record = True
        components = record.get("component_resources")
        _require(isinstance(components, dict) and set(profile["sampling"]["required_components"]) <= set(components), "raw resource component set is incomplete")
        for value in components.values():
            _exact_keys(value, {"cpu_percent", "rss_bytes", "running", "restart_count", "oom_killed"}, "component resource sample")
    summary_document = json.loads(summary.read_text(encoding="utf-8"))
    _require(summary_document.get("schema") == RESOURCE_SCHEMA, "resource summary schema differs")
    summary_value = summary_document.get("summary")
    _object(summary_value, "resource summary is missing")
    _require(active_resource_record and summary_value.get("samples") == len(raw_records) and summary_value.get("required_components") == list(profile["sampling"]["required_components"]) and summary_value.get("missing_signals") == [], "resource summary differs from raw evidence")


def _validate_raw_receipt(directory, value, schema, fields):
    _exact_keys(value, {"stage", "raw_path", "sha256", "records", "baseline", "terminal", "recovery_seconds"}, "stage recovery receipt")
    _require(value["stage"] in STAGES, "stage recovery receipt has an invalid stage")
    raw = _safe_relative(directory, value["raw_path"])
    _require(raw.is_file(), "stage recovery raw evidence is missing")
    _require(sha256_file(raw) == value["sha256"], "stage recovery digest differs")
    records = [json.loads(line) for line in raw.read_text(encoding="utf-8").splitlines() if line.strip()]
    _require(len(records) == value["records"] and len(records) >= 2, "stage recovery raw evidence is incomplete")
    previous = None
    for record in records:
        _exact_keys(record, {"schema", "stage", "observed_at", *fields}, "stage recovery raw record")
        _require(record["schema"] == schema and record["stage"] == value["stage"], "stage recovery raw binding differs")
        _number(record["observed_at"], "stage recovery timestamp is invalid")
        if previous is not None:
            _require(record["observed_at"] >= previous, "stage recovery timestamps regressed")
        previous = record["observed_at"]
    _require(value["baseline"] == {key: records[0][key] for key in ("observed_at", *fields)}, "stage recovery baseline was not recomputed")
    _require(value["terminal"] == {key: records[-1][key] for key in ("observed_at", *fields)}, "stage recovery terminal was not recomputed")
    expected_seconds = records[-1]["observed_at"] - records[0]["observed_at"]
    _require(math.isclose(value["recovery_seconds"], expected_seconds, rel_tol=1e-6, abs_tol=0.01), "stage recovery duration was not recomputed")


def _validate_cleanup(cleanup, project, incomplete=False):
    _exact_keys(cleanup, {"status", "project", "owned", "global_prune"}, "cleanup receipt")
    _require(cleanup["project"] == project and PROJECT_PATTERN.fullmatch(project) and isinstance(cleanup["owned"], bool) and cleanup["global_prune"] is False, "cleanup ownership is invalid")
    if not incomplete:
        _require(cleanup["owned"] is True and cleanup["status"] == "passed", "complete cleanup is not owned and passed")
    else:
        _require(cleanup["status"] in {"passed", "failed"}, "incomplete cleanup status is invalid")


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


def _validate_bindings(directory, bindings):
    _exact_keys(bindings, {"compose", "runtime_contract", "recipe_descriptor", "runner"}, "acceptance bindings")
    for key in ("compose", "runtime_contract"):
        value = bindings[key]
        _exact_keys(value, {"path", "sha256"}, key + " binding")
        _digest(value["sha256"], key + " digest is invalid")
        path = _safe_relative(directory, value["path"])
        _require(path.is_file() and sha256_file(path) == value["sha256"], key + " binding differs from raw file")
    recipe = bindings["recipe_descriptor"]
    _exact_keys(recipe, {"schema_version", "seed", "digest"}, "recipe descriptor binding")
    _require(recipe == {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "digest": RECIPE_DIGEST}, "recipe descriptor binding differs")
    runner = bindings["runner"]
    _exact_keys(runner, {"source_commit", "binary_sha256"}, "runner binding")
    _require(REVISION_PATTERN.fullmatch(runner["source_commit"]), "runner source commit is invalid")
    _digest(runner["binary_sha256"], "runner binary digest is invalid")


def validate_evidence(document, directory=None):
    """Validate evidence in place and recompute all derived aggregation fields."""
    _object(document, "capacity evidence must be an object")
    root_required = {"schema", "execution_status", "capability_status", "profile", "candidate", "recipe", "bindings", "rounds", "cleanup", "capability"}
    root_allowed = root_required | {"aggregates", "host"}
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
    _require(directory is not None, "strict evidence validation requires its raw evidence directory")
    profile_path = _safe_relative(directory, profile_ref["path"])
    profile, actual_profile_digest = load_profile(profile_path)
    _require(actual_profile_digest == profile_ref["sha256"] and profile["profile_id"] == profile_ref["id"], "profile file binding differs")
    _validate_bindings(directory, document["bindings"])
    _require(candidate["version"] == profile["target_candidate_version"], "candidate version differs from profile target")

    rounds = document.get("rounds")
    _require(isinstance(rounds, list) and len(rounds) == 3, "evidence must retain exactly three repetition records")
    normalized_rounds = []
    seen_endpoints = set()
    for index, round_value in enumerate(rounds, 1):
        _object(round_value, "repetition record is invalid")
        _exact_keys(round_value, {"number", "project", "endpoint", "execution_status", "recipe", "load_report", "resources", "asynchronous", "observability", "cleanup", "stop"}, "repetition record")
        _require(round_value["number"] == index and round_value["execution_status"] in {"complete", "incomplete"}, "repetition order or state is invalid")
        _require(PROJECT_PATTERN.fullmatch(round_value["project"]), "repetition Compose project is invalid")
        endpoint = round_value["endpoint"]
        _exact_keys(endpoint, {"base_url", "port"}, "repetition endpoint")
        _require(isinstance(endpoint["port"], int) and 1 <= endpoint["port"] <= 65535 and endpoint["base_url"] == "http://127.0.0.1:%d" % endpoint["port"], "repetition endpoint is invalid")
        _require(endpoint["port"] not in seen_endpoints, "repetition endpoint was reused")
        seen_endpoints.add(endpoint["port"])
        recipe_value = round_value["recipe"]
        _exact_keys(recipe_value, {"receipt", "nonempty_rejection"}, "repetition recipe evidence")
        receipt = recipe_value["receipt"]
        _object(receipt, "recipe receipt is missing")
        _require(receipt.get("schema_version") == RECIPE_SCHEMA and receipt.get("seed") == 18002005 and receipt.get("digest") == RECIPE_DIGEST and receipt.get("candidate") == candidate, "recipe receipt is not bound to the candidate")
        _exact_keys(recipe_value["nonempty_rejection"], {"exit_code", "target_unchanged"}, "non-empty recipe rejection")
        _require(recipe_value["nonempty_rejection"] == {"exit_code": 3, "target_unchanged": True}, "non-empty recipe target was not safely rejected")
        load_report = round_value["load_report"]
        if isinstance(load_report, str):
            load_report = json.loads(_safe_relative(directory, load_report).read_text(encoding="utf-8"))
        _report(load_report, profile, {"profile_sha256": profile_ref["sha256"], "binding": candidate}, recipe, index, directory)
        _require(round_value["execution_status"] == load_report["execution_status"], "repetition state differs from load report")
        _validate_resources(directory, round_value["resources"], profile)
        for kind, schema, fields in (
            ("asynchronous", "gopulse.phase19.async-recovery.v1", ("outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag")),
            ("observability", "gopulse.phase19.observability-recovery.v1", ("metric_count", "logs_count", "events_count", "marshaller_store_counts")),
        ):
            values = round_value[kind]
            _require(isinstance(values, list) and len(values) == 4, kind + " stage receipts are incomplete")
            for stage_index, value in enumerate(values):
                _require(value.get("stage") == STAGES[stage_index], kind + " stage binding differs")
                _validate_raw_receipt(directory, value, schema, fields)
                _number(value["recovery_seconds"], kind + " recovery duration is invalid", 0)
                if kind == "observability":
                    _exact_keys(value["terminal"]["marshaller_store_counts"], {"metrics", "logs", "events"}, "marshaller store counts")
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
        _require("aggregates" not in document, "incomplete evidence cannot publish capacity aggregates")
    else:
        aggregates = aggregate_repetitions(normalized_rounds)
        _require(document.get("aggregates") == aggregates, "capacity aggregates were not recomputed from the three raw repetitions")
        stage_gates = [{"stage": name, "passed": all(_stage_gate(profile, round_value, index) for round_value in normalized_rounds)} for index, name in enumerate(STAGES)]
        expected_status = "target_met" if all(item["passed"] for item in stage_gates) else "boundary_found"
        _require(document.get("capability", {}).get("stage_gates") == stage_gates, "capacity gate result was not recomputed")
        _require(document["capability_status"] == expected_status, "capacity status does not match the frozen gates")
    _require(document["cleanup"] == normalized_rounds[-1]["cleanup"], "root cleanup receipt differs from the final repetition")
    _validate_cleanup(document["cleanup"], normalized_rounds[-1]["project"], incomplete=incomplete)
    _sensitive_scan(document)
    return document
