#!/usr/bin/env python3
"""Strict Phase 18 capacity evidence contract shared by runner and verifier."""
from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path

SCHEMA = "gopulse.phase18.capacity.v1"
LOAD_SCHEMA = "gopulse.phase18.load.v1"
RECIPE_SCHEMA = "gopulse.phase18.recipe.v1"
GIB = 1024 ** 3
MIB = 1024 ** 2
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
REVISION = re.compile(r"^[0-9a-f]{40}$")
SENSITIVE = re.compile(r"(?i)(password|secret|token|cookie|authorization|mysql://|amqp://)[\"\s:=]+[^\s,}\]]{8,}")
EXPECTED_COUNTS = {
    "users": 5000, "posts": 50000, "comments": 100000,
    "post_likes": 200000, "user_follows": 200000,
    "post_bookmarks": 25000, "business_outbox": 550000,
    "notifications": 500000,
}
EXPECTED_RANGES = {
    "users": {"first": 1, "last": 5000},
    "posts": {"first": 1, "last": 50000},
    "comments": {"first": 1, "last": 100000},
}
CATEGORIES = {
    "read", "search", "notification_bookmark_read", "content_write",
    "interaction_write", "identity_session",
}
PHASES = {"warmup", "steady", "burst"}
BOTTLENECK_KEYS = {"component", "reason_code", "fact"}


def sha(path: Path) -> str:
    return "sha256:" + hashlib.sha256(Path(path).read_bytes()).hexdigest()


def atomic(path: Path, value: dict) -> None:
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n")
    temporary.chmod(0o600)
    temporary.replace(path)


def _scan(value, key: str = "") -> str:
    values = []
    if isinstance(value, dict):
        for child_key, child in value.items():
            values.append(_scan(child, str(child_key)))
    elif isinstance(value, list):
        for child in value:
            values.append(_scan(child, key))
    elif isinstance(value, str) and not ("${" in value and "}" in value):
        values.append(key + ": " + value)
    return "\n".join(values)


def secret_scan(document: dict) -> None:
    if SENSITIVE.search(_scan(document)):
        raise ValueError("capacity evidence contains a sensitive value")


def _number(value) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError("capacity evidence contains a non-number")
    return float(value)


def _phase(report: dict, name: str) -> dict:
    matches = [phase for phase in report.get("phases", []) if phase.get("name") == name]
    if len(matches) != 1:
        raise ValueError("load report must contain exactly one " + name + " phase")
    return matches[0]


def _validate_counts(value: dict, label: str) -> None:
    required = {"requests", "succeeded", "explicit_rejects", "timeouts", "errors"}
    if not isinstance(value, dict) or set(value) != required:
        raise ValueError(label + " has invalid counters")
    for key in required:
        if not isinstance(value[key], int) or value[key] < 0:
            raise ValueError(label + " has invalid counter values")
    classified = value["succeeded"] + value["explicit_rejects"] + value["timeouts"] + value["errors"]
    if classified != value["requests"]:
        raise ValueError(label + " counters do not classify every request")


def _validate_bottleneck(value: dict) -> None:
    if not isinstance(value, dict) or set(value) != BOTTLENECK_KEYS:
        raise ValueError("resource bottleneck evidence is incomplete")
    for key in BOTTLENECK_KEYS:
        if not isinstance(value[key], str) or not value[key].strip():
            raise ValueError("resource bottleneck evidence has an invalid field")


def _validate_store_counts(value: dict, label: str) -> None:
    if not isinstance(value, dict) or set(value) != {"metrics", "logs", "events"}:
        raise ValueError(label + " marshaller store counts are incomplete")
    if any(not isinstance(value[key], int) or value[key] < 0 for key in value):
        raise ValueError(label + " marshaller store counts are invalid")


def validate_load_report(report: dict) -> None:
    required = {
        "schema_version", "seed", "started_at", "finished_at", "steady_target_rps",
        "burst_target_rps", "virtual_users", "phases", "routes", "total", "load_process",
    }
    if not isinstance(report, dict) or set(report) != required or report.get("schema_version") != LOAD_SCHEMA:
        raise ValueError("invalid Phase 18 load report shape")
    if report.get("seed") != 18002005 or report.get("virtual_users", 0) < 1:
        raise ValueError("load report has invalid seed or virtual users")
    phases = report.get("phases")
    if not isinstance(phases, list) or len(phases) != 3 or {phase.get("name") for phase in phases} != PHASES:
        raise ValueError("load report must contain warmup, steady, and burst")
    for phase in phases:
        if set(phase) != {
            "name", "target_rps", "duration_seconds", "scheduled_slots", "dropped_slots",
            "max_schedule_lag_ms", "completed_requests", "counts", "latency_by_category", "counts_by_category",
        }:
            raise ValueError("invalid phase report shape")
        if _number(phase["target_rps"]) <= 0 or _number(phase["duration_seconds"]) <= 0:
            raise ValueError("phase target and duration must be positive")
        if phase["scheduled_slots"] < phase["completed_requests"]:
            raise ValueError("completed requests exceed scheduled slots")
        if phase["dropped_slots"] > phase["scheduled_slots"]:
            raise ValueError("dropped slots exceed scheduled slots")
        if phase["completed_requests"] != phase["scheduled_slots"] - phase["dropped_slots"]:
            raise ValueError("phase completed, scheduled, and dropped slots are inconsistent")
        if not isinstance(phase["max_schedule_lag_ms"], (int, float)) or phase["max_schedule_lag_ms"] < 0:
            raise ValueError("invalid scheduling lag")
        _validate_counts(phase["counts"], "phase")
        if phase["counts"]["requests"] != phase["completed_requests"]:
            raise ValueError("phase completed count differs from counters")
        if set(phase["latency_by_category"]) != CATEGORIES or set(phase["counts_by_category"]) != CATEGORIES:
            raise ValueError("phase category catalog is incomplete")
        for category in CATEGORIES:
            _validate_counts(phase["counts_by_category"][category], "category")
            latency = phase["latency_by_category"][category]
            if set(latency) != {"p50_ms", "p95_ms", "p99_ms", "max_ms"}:
                raise ValueError("invalid category latency")
            ordered = [_number(latency[key]) for key in ("p50_ms", "p95_ms", "p99_ms", "max_ms")]
            if any(value < 0 for value in ordered) or ordered != sorted(ordered):
                raise ValueError("category latency is not ordered")
        category_requests = sum(item["requests"] for item in phase["counts_by_category"].values())
        if category_requests != phase["completed_requests"]:
            raise ValueError("category counters do not classify every request")
    routes = report.get("routes")
    if not isinstance(routes, dict) or not routes:
        raise ValueError("load report routes are missing")
    for key, route in routes.items():
        if set(route) != {"category", "method", "template", "counts", "statuses", "latency"}:
            raise ValueError("invalid route report shape")
        if route["category"] not in CATEGORIES or route["template"] != key:
            raise ValueError("invalid route identity")
        _validate_counts(route["counts"], "route")
        if not isinstance(route["statuses"], dict) or any(not isinstance(value, int) or value < 0 for value in route["statuses"].values()):
            raise ValueError("invalid route status counters")
        if "timeout" in route["statuses"] and route["statuses"]["timeout"] != route["counts"]["timeouts"]:
            raise ValueError("route timeout status differs from counters")
        if sum(route["statuses"].values()) != route["counts"]["requests"]:
            raise ValueError("route status counters do not classify every request")
    _validate_counts(report["total"], "total")
    if report["total"]["requests"] != sum(phase["completed_requests"] for phase in phases):
        raise ValueError("total counters differ from phases")
    if report["total"]["requests"] != sum(route["counts"]["requests"] for route in routes.values()):
        raise ValueError("route counters differ from total")
    process = report["load_process"]
    if set(process) != {"rss_bytes", "goroutines", "heap_alloc_bytes"} or any(not isinstance(value, int) or value < 0 for value in process.values()):
        raise ValueError("invalid load process statistics")


def _rate(round_value: dict) -> float:
    phase = _phase(round_value["load_report"], "steady")
    return phase["completed_requests"] / phase["duration_seconds"]


def _latency_vector(round_value: dict) -> tuple[float, float]:
    phase = _phase(round_value["load_report"], "steady")
    p95 = max(phase["latency_by_category"][category]["p95_ms"] for category in CATEGORIES)
    p99 = max(phase["latency_by_category"][category]["p99_ms"] for category in CATEGORIES)
    return p95, p99


def _deviation(values: list[float]) -> float:
    mean = sum(values) / len(values)
    if mean <= 0:
        raise ValueError("repeatability metric has a non-positive mean")
    return (max(values) - min(values)) / mean * 100


def repeatability(rounds: list[dict]) -> dict:
    rates = [_rate(item) for item in rounds]
    p95 = [_latency_vector(item)[0] for item in rounds]
    p99 = [_latency_vector(item)[1] for item in rounds]
    return {
        "rps_percent": _deviation(rates),
        "p95_percent": _deviation(p95),
        "p99_percent": _deviation(p99),
        "rates_rps": rates,
    }


def evaluate_slo(rounds: list[dict]) -> dict:
    checks = {}
    for index, item in enumerate(rounds, 1):
        report = item["load_report"]
        steady = _phase(report, "steady")
        burst = _phase(report, "burst")
        stable_total = steady["counts"]["requests"]
        stable_failures = steady["counts"]["timeouts"] + steady["counts"]["errors"]
        checks[f"round_{index}_unexpected_error_rate"] = stable_failures / max(stable_total, 1) <= .01
        read = steady["latency_by_category"]["read"]
        checks[f"round_{index}_read_latency"] = read["p95_ms"] <= 500 and read["p99_ms"] <= 1500
        for category in ("content_write", "interaction_write"):
            latency = steady["latency_by_category"][category]
            checks[f"round_{index}_{category}_latency"] = latency["p95_ms"] <= 800 and latency["p99_ms"] <= 2000
        burst_total = burst["counts"]["requests"]
        checks[f"round_{index}_burst_explicit_reject_rate"] = burst["counts"]["explicit_rejects"] / max(burst_total, 1) <= .05
        checks[f"round_{index}_burst_unexpected_rate"] = (burst["counts"]["timeouts"] + burst["counts"]["errors"]) / max(burst_total, 1) <= .01
        resources = item["resources"]
        checks[f"round_{index}_no_oom_or_restart"] = resources["oom_killed"] == 0 and resources["restart_count"] == 0
        checks[f"round_{index}_swap_delta"] = resources["max_swap_delta_bytes"] <= 256 * MIB
        checks[f"round_{index}_load_generator_not_bottleneck"] = (
            resources["load_process_peak_rss_bytes"] <= 2 * GIB
            and all(
                phase["dropped_slots"] / max(phase["scheduled_slots"], 1) <= .001
                and phase["max_schedule_lag_ms"] <= 50
                for phase in report["phases"]
            )
        )
        convergence = item["convergence"]
        converged = convergence.get("converged") is True
        checks[f"round_{index}_search_notification_convergence"] = converged and convergence["search_seconds"] <= 30 and convergence["notification_seconds"] <= 30
        checks[f"round_{index}_observability_convergence"] = converged and convergence["metrics_logs_events_seconds"] <= 60
        checks[f"round_{index}_recovery"] = converged and convergence["recovery_seconds"] <= 600
    return {"status": "passed" if all(checks.values()) else "failed", "checks": checks}


def validate_capacity(document: dict, manifest: Path | None = None) -> dict:
    if document.get("schema") != SCHEMA or document.get("complete") is not True or document.get("execution_status") != "complete":
        raise ValueError("Phase 18 capacity evidence is not complete")
    candidate = document.get("candidate", {})
    if candidate.get("version") != "2.0.1" or not REVISION.fullmatch(candidate.get("revision", "")):
        raise ValueError("invalid Phase 18 candidate")
    for key in ("manifest_sha256", "bundle_sha256"):
        if not DIGEST.fullmatch(candidate.get(key, "")):
            raise ValueError("invalid candidate digest: " + key)
    if manifest is not None and sha(manifest) != candidate["manifest_sha256"]:
        raise ValueError("capacity evidence belongs to another release manifest")
    if not candidate.get("image_digests") or any(not DIGEST.fullmatch(value) for value in candidate["image_digests"].values()):
        raise ValueError("candidate image digests are incomplete")
    host = document.get("host", {})
    if host.get("platform") != "linux/amd64" or host.get("host_os") != "Linux" or "WSL2" not in host.get("kernel", ""):
        raise ValueError("capacity evidence requires the reference WSL2 Linux amd64 host")
    if host.get("cpu_count") != 8 or host.get("memory_bytes", 0) < 12 * GIB or host.get("swap_total_bytes", 0) < 8 * GIB or host.get("disk_available_bytes", 0) < 100 * GIB:
        raise ValueError("reference host resource contract is not satisfied")
    if host.get("docker_server_os") != "linux" or host.get("docker_server_arch") != "amd64" or not host.get("docker_server_version") or not host.get("compose_version"):
        raise ValueError("reference host Docker or Compose contract is not satisfied")
    projects = host.get("active_compose_projects")
    if not isinstance(projects, list) or any(not isinstance(value, str) or not value for value in projects) or projects:
        raise ValueError("reference host has a competing Compose project")
    recipe = document.get("recipe", {})
    if recipe.get("schema_version") != RECIPE_SCHEMA or recipe.get("seed") != 18002005 or recipe.get("counts") != EXPECTED_COUNTS or recipe.get("id_ranges") != EXPECTED_RANGES:
        raise ValueError("deterministic recipe contract differs")
    if not DIGEST.fullmatch(recipe.get("digest", "")) or recipe.get("same_seed_repeat") is not True or recipe.get("nonempty_rejection") is not True:
        raise ValueError("recipe repeatability or safety evidence is missing")
    rounds = document.get("rounds")
    if not isinstance(rounds, list) or len(rounds) != 3:
        raise ValueError("exactly three capacity rounds are required")
    projects = set()
    for index, item in enumerate(rounds, 1):
        if set(item) != {"id", "project_sha256", "recipe_receipt", "load_report", "resources", "convergence"} or item["id"] != index:
            raise ValueError("invalid capacity round shape")
        if not re.fullmatch(r"[0-9a-f]{64}", item["project_sha256"]) or item["project_sha256"] in projects:
            raise ValueError("capacity projects must be isolated and unique")
        projects.add(item["project_sha256"])
        receipt = item["recipe_receipt"]
        if receipt.get("schema_version") != RECIPE_SCHEMA or receipt.get("digest") != recipe["digest"] or receipt.get("counts") != recipe["counts"]:
            raise ValueError("round recipe differs from the common recipe")
        if receipt.get("candidate", {}).get("revision") != candidate["revision"] or receipt.get("candidate", {}).get("version") != candidate["version"]:
            raise ValueError("round recipe is not bound to the candidate")
        validate_load_report(item["load_report"])
        if item["load_report"]["virtual_users"] < 1024:
            raise ValueError("capacity rounds require the fixed virtual user pool")
        resources = item["resources"]
        if set(resources) != {
            "samples", "oom_killed", "restart_count", "max_swap_delta_bytes",
            "load_process_peak_rss_bytes", "peak_container_cpu_percent",
            "first_bottleneck",
        } or resources["samples"] < 1:
            raise ValueError("resource sampling evidence is incomplete")
        for key in ("samples", "oom_killed", "restart_count", "max_swap_delta_bytes", "load_process_peak_rss_bytes"):
            if not isinstance(resources[key], int) or resources[key] < 0:
                raise ValueError("resource sampling contains an invalid counter")
        if not isinstance(resources["peak_container_cpu_percent"], (int, float)) or resources["peak_container_cpu_percent"] < 0:
            raise ValueError("resource sampling contains an invalid CPU peak")
        _validate_bottleneck(resources["first_bottleneck"])
        convergence = item["convergence"]
        if set(convergence) != {
            "converged",
            "outbox_pending", "rabbit_ready", "rabbit_unacked", "search_count", "mysql_posts", "notifications", "kafka_lag",
            "logs_count_before", "logs_count_after", "events_count_before", "events_count_after",
            "marshaller_store_before", "marshaller_store_after",
            "search_seconds", "notification_seconds", "metrics_logs_events_seconds", "recovery_seconds",
        }:
            raise ValueError("convergence evidence is incomplete")
        if not isinstance(convergence["converged"], bool):
            raise ValueError("convergence state is invalid")
        for key in (
            "outbox_pending", "rabbit_ready", "rabbit_unacked", "search_count", "mysql_posts", "notifications", "kafka_lag",
            "logs_count_before", "logs_count_after", "events_count_before", "events_count_after",
        ):
            if not isinstance(convergence[key], int) or convergence[key] < 0:
                raise ValueError("convergence evidence contains an invalid count")
        _validate_store_counts(convergence["marshaller_store_before"], "before")
        _validate_store_counts(convergence["marshaller_store_after"], "after")
        if convergence["mysql_posts"] < 1 or convergence["notifications"] < EXPECTED_COUNTS["notifications"]:
            raise ValueError("accepted business facts were lost")
        if convergence["converged"]:
            if convergence["outbox_pending"] != 0 or convergence["rabbit_ready"] != 0 or convergence["rabbit_unacked"] != 0 or convergence["kafka_lag"] != 0:
                raise ValueError("converged round did not report empty queues")
            if convergence["search_count"] != convergence["mysql_posts"]:
                raise ValueError("converged round did not report matching search projection")
            if convergence["logs_count_after"] <= convergence["logs_count_before"] or convergence["events_count_after"] <= convergence["events_count_before"]:
                raise ValueError("converged round did not produce searchable logs and events")
            for kind in ("metrics", "logs", "events"):
                if convergence["marshaller_store_after"][kind] <= convergence["marshaller_store_before"][kind]:
                    raise ValueError("converged round did not record " + kind + " storage progress")
        else:
            if (convergence["logs_count_after"] < convergence["logs_count_before"]
                    or convergence["events_count_after"] < convergence["events_count_before"]):
                raise ValueError("non-converged round recorded observability regression")
            for kind in ("metrics", "logs", "events"):
                if convergence["marshaller_store_after"][kind] < convergence["marshaller_store_before"][kind]:
                    raise ValueError("non-converged round recorded " + kind + " storage regression")
            incomplete = (
                convergence["outbox_pending"] != 0
                or convergence["rabbit_ready"] != 0
                or convergence["rabbit_unacked"] != 0
                or convergence["kafka_lag"] != 0
                or convergence["search_count"] != convergence["mysql_posts"]
                or convergence["notifications"] < EXPECTED_COUNTS["notifications"]
                or convergence["logs_count_after"] <= convergence["logs_count_before"]
                or convergence["events_count_after"] <= convergence["events_count_before"]
                or any(convergence["marshaller_store_after"][kind] <= convergence["marshaller_store_before"][kind]
                       for kind in ("metrics", "logs", "events"))
            )
            if not incomplete:
                raise ValueError("non-converged round has no incomplete convergence condition")
        for key in ("search_seconds", "notification_seconds", "metrics_logs_events_seconds", "recovery_seconds"):
            if _number(convergence[key]) < 0:
                raise ValueError("convergence timing is invalid")
    computed = repeatability(rounds)
    reported = document.get("repeatability", {})
    for key in ("rps_percent", "p95_percent", "p99_percent"):
        if abs(_number(reported.get(key)) - computed[key]) > 1e-6:
            raise ValueError("repeatability report was not recomputed from rounds")
    if computed["rps_percent"] > 5 or computed["p95_percent"] > 10 or computed["p99_percent"] > 10:
        raise ValueError("three-round repeatability gate failed")
    slo = evaluate_slo(rounds)
    if document.get("slo", {}).get("status") != slo["status"] or document.get("slo", {}).get("checks") != slo["checks"]:
        raise ValueError("SLO evaluation was not recomputed from rounds")
    if slo["status"] == "failed":
        _validate_bottleneck(document.get("slo", {}).get("first_bottleneck", {}))
    if document.get("cleanup") != "passed" or document.get("secret_scan") != "passed":
        raise ValueError("cleanup or secret scan did not pass")
    secret_scan(document)
    return document
