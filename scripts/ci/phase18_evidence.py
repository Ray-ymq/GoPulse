#!/usr/bin/env python3
"""Strict Phase 18 capacity evidence contract shared by runner and verifier."""
from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path

SCHEMA = "gopulse.phase18.capacity.v1"
SCALING_SCHEMA = "gopulse.phase18.scaling.v2"
QUALIFICATION_SCHEMA = "gopulse.phase18.qualification.v1"
BOTTLENECK_SCHEMA = "gopulse.phase18.bottleneck-diagnostic.v1"
LOAD_SCHEMA = "gopulse.phase18.load.v1"
RECIPE_SCHEMA = "gopulse.phase18.recipe.v1"
GIB = 1024 ** 3
MIB = 1024 ** 2
MIN_REFERENCE_DISK_BYTES = 80 * GIB
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
ROUND_EVIDENCE_KEYS = {
    "corpus_sha256", "load_source_commit", "load_binary_sha256",
    "raw_samples_sha256", "raw_samples_records", "load_binding_sha256",
}
SCALING_COMPONENTS = {
    "backend": ("backend_mixed", 3),
    "business-worker": ("business_worker_backlog", 2),
    "search-indexer": ("search_indexer_backlog", 2),
    "router": ("router_concurrent_publish", 2),
    "marshaller": ("marshaller_backlog", 2),
}
SCALING_MIN_RATIOS = {
    component: 1.3 for component in SCALING_COMPONENTS if component != "backend"
}
SCALING_COUNTER_SOURCES = {
    "backend": "load_report",
    "business-worker": "rabbit_ack",
    "search-indexer": "rabbit_ack",
    "router": "kafka_end_offset",
    "marshaller": "kafka_committed",
}
SCALING_EXECUTION_ORDER = [
    "backend:replacement", "business-worker:replacement",
    "search-indexer:replacement", "router:replacement", "marshaller:replacement",
    "ownership:runtime", "ownership:tests",
    "backend:single", "backend:multi",
    "business-worker:single", "business-worker:multi",
    "search-indexer:single", "search-indexer:multi",
    "router:single", "router:multi",
    "marshaller:single", "marshaller:multi",
]
SCALING_TEST_COMMANDS = {
    "outbox_ownership": (0,),
    "alert_ownership": (0,),
    "rabbit_ack_redelivery": (0,),
    "kafka_rebalance_fencing": (0,),
    "monitor_single_owner": (0,),
}
QUALIFICATION_PARSER_CASES = {
    "mysql": {
        "future_lease", "expired_lease_reclaim", "post_release_nulls",
        "empty_owner", "microsecond_precision", "missing_row",
    },
    "kafka": {
        "empty_group", "single_member", "dual_member_four_partitions",
        "rebalance", "unassigned_partition", "no_offsets",
    },
}


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
    if host.get("cpu_count") != 8 or host.get("memory_bytes", 0) < 12 * GIB or host.get("swap_total_bytes", 0) < 8 * GIB or host.get("disk_available_bytes", 0) < MIN_REFERENCE_DISK_BYTES:
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
        if set(item) != {
            "id", "project_sha256", "recipe_receipt", "load_report", "resources", "convergence", "evidence",
        } or item["id"] != index:
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
        evidence = item["evidence"]
        if not isinstance(evidence, dict) or set(evidence) != ROUND_EVIDENCE_KEYS:
            raise ValueError("capacity round load and raw-sample binding is incomplete")
        for key in ("corpus_sha256", "load_binary_sha256", "raw_samples_sha256", "load_binding_sha256"):
            if not DIGEST.fullmatch(evidence[key]):
                raise ValueError("capacity round evidence contains an invalid digest")
        if not REVISION.fullmatch(evidence["load_source_commit"]):
            raise ValueError("capacity round load source commit is invalid")
        if not isinstance(evidence["raw_samples_records"], int) or evidence["raw_samples_records"] < 1:
            raise ValueError("capacity round raw-sample receipt is incomplete")
        resources = item["resources"]
        if set(resources) != {
            "samples", "oom_killed", "restart_count", "max_swap_delta_bytes",
            "load_process_peak_rss_bytes", "peak_container_cpu_percent",
            "first_bottleneck",
        } or resources["samples"] < 1:
            raise ValueError("resource sampling evidence is incomplete")
        if evidence["raw_samples_records"] != resources["samples"]:
            raise ValueError("raw resource sample count differs from the resource summary")
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


def _require_digest(value, label: str) -> None:
    if not isinstance(value, str) or not DIGEST.fullmatch(value):
        raise ValueError(label + " is not a SHA-256 digest")


def _require_positive(value, label: str) -> float:
    number = _number(value)
    if number <= 0:
        raise ValueError(label + " must be positive")
    return number


def _validate_scaling_measurement(value: dict, label: str, replicas: int,
                                  counter_source: str) -> float:
    required = {
        "replicas", "elapsed_seconds", "processed", "counter_before", "counter_after",
        "counter_source", "input_sha256", "configuration_sha256", "report_sha256",
        "raw_samples_sha256",
    }
    if not isinstance(value, dict) or set(value) != required:
        raise ValueError(label + " scaling measurement is incomplete")
    if value["replicas"] != replicas:
        raise ValueError(label + " replica count differs from the managed topology")
    if value["counter_source"] != counter_source:
        raise ValueError(label + " external counter source differs")
    elapsed = _require_positive(value["elapsed_seconds"], label + " elapsed time")
    for key in ("processed", "counter_before", "counter_after"):
        if not isinstance(value[key], int) or value[key] < 0:
            raise ValueError(label + " contains an invalid counter")
    if value["counter_after"] - value["counter_before"] != value["processed"]:
        raise ValueError(label + " processed count is not the observed counter delta")
    if value["processed"] < 1:
        raise ValueError(label + " did not complete measurable work")
    for key in ("input_sha256", "configuration_sha256", "report_sha256", "raw_samples_sha256"):
        _require_digest(value[key], label + " " + key)
    return value["processed"] / elapsed


def _validate_scaling_pairs(value: dict) -> dict[str, float]:
    if not isinstance(value, dict) or set(value) != set(SCALING_COMPONENTS):
        raise ValueError("scaling pairs do not cover every managed component")
    ratios = {}
    for component, (operation, replicas) in SCALING_COMPONENTS.items():
        item = value[component]
        expected_fields = {
            "operation", "acceptance", "threshold", "single", "multi", "ratio",
            "rates_per_second",
        }
        if component == "backend":
            expected_fields.add("measurement_mode")
        if not isinstance(item, dict) or set(item) != expected_fields:
            raise ValueError(component + " scaling pair is incomplete")
        if component == "backend" and item["measurement_mode"] != "closed_loop":
            raise ValueError("Backend scaling pair requires uncapped closed-loop load")
        threshold = SCALING_MIN_RATIOS.get(component)
        expected_acceptance = "characterization" if threshold is None else "minimum_ratio"
        if item["operation"] != operation or item["acceptance"] != expected_acceptance:
            raise ValueError(component + " scaling operation or acceptance differs")
        if threshold is None:
            if item["threshold"] is not None:
                raise ValueError(component + " characterization must not declare a threshold")
        elif _number(item["threshold"]) != threshold:
            raise ValueError(component + " scaling threshold differs")
        source = SCALING_COUNTER_SOURCES[component]
        single = item["single"]
        multi = item["multi"]
        single_rate = _validate_scaling_measurement(single, component + " single", 1, source)
        multi_rate = _validate_scaling_measurement(
            multi, component + " multi", replicas, source
        )
        for key in ("input_sha256", "configuration_sha256", "counter_source"):
            if single[key] != multi[key]:
                raise ValueError(component + " pair did not use the same " + key)
        ratio = multi_rate / single_rate
        rates = item["rates_per_second"]
        if not isinstance(rates, dict) or set(rates) != {"single", "multi"}:
            raise ValueError(component + " lacks recomputed rates")
        if abs(_number(rates["single"]) - single_rate) > 1e-9 or abs(_number(rates["multi"]) - multi_rate) > 1e-9:
            raise ValueError(component + " reported rates were not recomputed from raw counters")
        if abs(_number(item["ratio"]) - ratio) > 1e-9:
            raise ValueError(component + " reported ratio was not recomputed from raw counters")
        if threshold is not None and ratio < threshold:
            raise ValueError(component + " did not reach the %.1fx scaling threshold" % threshold)
        ratios[component] = ratio
    return ratios


def _validate_replacement(value: dict, component: str, replicas: int) -> None:
    required = {
        "instance", "accepted", "completed", "lost", "duplicate_side_effects",
        "counter_source", "external_counter_before", "external_counter_during_removal",
        "external_counter_after", "instance_counters_before",
        "instance_counters_during_removal", "instance_counters_after",
        "survivor_progress", "stopped_seconds", "observations_sha256",
    }
    if not isinstance(value, dict) or set(value) != required:
        raise ValueError(component + " replacement evidence is incomplete")
    if not isinstance(value["instance"], str) or not value["instance"].endswith(("-2", "-3")):
        raise ValueError(component + " replacement instance is invalid")
    if value["counter_source"] != SCALING_COUNTER_SOURCES[component]:
        raise ValueError(component + " replacement counter source differs")
    for key in (
        "accepted", "completed", "lost", "duplicate_side_effects",
        "external_counter_before", "external_counter_during_removal",
        "external_counter_after", "survivor_progress",
    ):
        if not isinstance(value[key], int) or value[key] < 0:
            raise ValueError(component + " replacement contains an invalid counter")
    stopped = _require_positive(value["stopped_seconds"], component + " stopped duration")
    if value["accepted"] < 1 or value["completed"] != value["accepted"]:
        raise ValueError(component + " replacement did not complete every accepted item")
    if value["lost"] != 0 or value["duplicate_side_effects"] != 0:
        raise ValueError(component + " replacement lost or duplicated accepted work")
    if not value["external_counter_before"] <= value["external_counter_during_removal"] <= value["external_counter_after"]:
        raise ValueError(component + " replacement external counters are not monotonic")
    if value["external_counter_after"] - value["external_counter_before"] != value["completed"]:
        raise ValueError(component + " replacement external delta differs from completed work")
    phases = {}
    for key in ("instance_counters_before", "instance_counters_during_removal", "instance_counters_after"):
        counters = value[key]
        if not isinstance(counters, dict) or len(counters) != replicas:
            raise ValueError(component + " replacement instance counters are incomplete")
        if value["instance"] not in counters:
            raise ValueError(component + " replacement instance has no counter evidence")
        for instance, count in counters.items():
            if not isinstance(instance, str) or not _valid_instance(instance):
                raise ValueError(component + " replacement has an invalid instance label")
            if not isinstance(count, int) or count < 0:
                raise ValueError(component + " replacement counter is invalid")
        phases[key] = counters
    removed = value["instance"]
    if phases["instance_counters_during_removal"][removed] != phases["instance_counters_before"][removed]:
        raise ValueError(component + " removed instance advanced after ownership was removed")
    survivors = sorted(set(phases["instance_counters_before"]) - {removed})
    for instance in survivors:
        if phases["instance_counters_during_removal"][instance] <= phases["instance_counters_before"][instance]:
            raise ValueError(component + " surviving replica did not advance while ownership moved")
        if phases["instance_counters_after"][instance] < phases["instance_counters_during_removal"][instance]:
            raise ValueError(component + " surviving replica counter regressed after restart")
    progress = sum(
        phases["instance_counters_during_removal"][instance] - phases["instance_counters_before"][instance]
        for instance in survivors
    )
    if progress != value["survivor_progress"] or progress < 1:
        raise ValueError(component + " survivor progress was not observed")
    if stopped <= 0:
        raise ValueError(component + " replacement stop duration is invalid")
    _require_digest(value["observations_sha256"], component + " replacement observations")

def _valid_instance(instance: str) -> bool:
    return bool(re.fullmatch(r"[a-z0-9][a-z0-9-]{0,62}", instance or "")) and "--" not in instance


def validate_scaling(document: dict, manifest: Path | None = None) -> dict:
    if not isinstance(document, dict) or document.get("schema") != SCALING_SCHEMA or document.get("complete") is not True or document.get("execution_status") != "complete":
        raise ValueError("Phase 18 scaling evidence is not complete")
    candidate = document.get("candidate", {})
    if candidate.get("version") != "2.0.2" or not REVISION.fullmatch(candidate.get("revision", "")):
        raise ValueError("invalid Phase 18 scaling candidate")
    for key in ("manifest_sha256", "bundle_sha256"):
        _require_digest(candidate.get(key), "candidate " + key)
    if manifest is not None and sha(manifest) != candidate["manifest_sha256"]:
        raise ValueError("scaling evidence belongs to another release manifest")
    if not candidate.get("image_digests") or any(not DIGEST.fullmatch(str(value)) for value in candidate["image_digests"].values()):
        raise ValueError("candidate image digests are incomplete")

    host = document.get("host", {})
    if host.get("platform") != "linux/amd64" or host.get("host_os") != "Linux" or "WSL2" not in host.get("kernel", ""):
        raise ValueError("scaling evidence requires the reference WSL2 Linux amd64 host")
    if host.get("cpu_count") != 8 or host.get("memory_bytes", 0) < 12 * GIB or host.get("swap_total_bytes", 0) < 8 * GIB or host.get("disk_available_bytes", 0) < MIN_REFERENCE_DISK_BYTES:
        raise ValueError("reference host resource contract is not satisfied")
    if host.get("docker_server_os") != "linux" or host.get("docker_server_arch") != "amd64" or not host.get("docker_server_version") or not host.get("compose_version"):
        raise ValueError("reference host Docker or Compose contract is not satisfied")
    active = host.get("active_compose_projects")
    if not isinstance(active, list) or any(not isinstance(value, str) or not value for value in active) or active:
        raise ValueError("reference host has a competing Compose project")

    topology = document.get("topology", {})
    if set(topology) != {"replicas", "aliases", "edge_bindings"}:
        raise ValueError("scaling topology evidence is incomplete")
    if topology["replicas"] != {component: replicas for component, (_, replicas) in SCALING_COMPONENTS.items()}:
        raise ValueError("scaling topology differs from the allocated replica counts")
    aliases = topology["aliases"]
    if not isinstance(aliases, dict) or set(aliases) != {"backend", "router"}:
        raise ValueError("scaling topology aliases are incomplete")
    if aliases["backend"] != ["backend-local", "backend-2", "backend-3"] or aliases["router"] != ["router-local", "router-2"]:
        raise ValueError("scaling topology aliases do not include every replica")
    bindings = topology["edge_bindings"]
    if not isinstance(bindings, list) or len(bindings) != 1 or set(bindings[0]) != {"service", "host_ip", "host_port"}:
        raise ValueError("product topology must expose exactly one edge binding")
    if bindings[0]["service"] != "frontend" or bindings[0]["host_ip"] != "127.0.0.1" or not isinstance(bindings[0]["host_port"], int) or not 1 <= bindings[0]["host_port"] <= 65535:
        raise ValueError("product edge binding is invalid")

    inputs = document.get("inputs", {})
    if not isinstance(inputs, dict) or set(inputs) != {
        "corpus_sha256", "snapshot_sha256", "load_source_commit", "load_binary_sha256",
        "router_load_binary_sha256", "recipe_receipt_sha256", "execution_order",
    }:
        raise ValueError("scaling common inputs are incomplete")
    for key in ("corpus_sha256", "snapshot_sha256", "load_binary_sha256",
                "router_load_binary_sha256", "recipe_receipt_sha256"):
        _require_digest(inputs[key], "scaling input " + key)
    if (not REVISION.fullmatch(inputs["load_source_commit"])
            or inputs["load_source_commit"] != candidate["revision"]):
        raise ValueError("scaling load source commit differs from the candidate")
    if inputs["execution_order"] != SCALING_EXECUTION_ORDER:
        raise ValueError("scaling execution order or paired side binding differs")
    _validate_scaling_pairs(document.get("pairs"))

    replacements = document.get("replacements")
    if not isinstance(replacements, dict) or set(replacements) != set(SCALING_COMPONENTS):
        raise ValueError("replacement matrix does not cover every managed component")
    for component, (_, replicas) in SCALING_COMPONENTS.items():
        _validate_replacement(replacements[component], component, replicas)

    edge = document.get("edge", {})
    edge_keys = {
        "requests", "successful", "instance_counts", "removed_instance",
        "counts_after_removal", "counts_after_restart", "successful_during_removal",
        "successful_after_restart", "observations_sha256",
    }
    if not isinstance(edge, dict) or set(edge) != edge_keys:
        raise ValueError("edge distribution evidence is incomplete")
    for key in ("requests", "successful", "successful_during_removal", "successful_after_restart"):
        if not isinstance(edge[key], int) or edge[key] < 0:
            raise ValueError("edge distribution contains an invalid counter")
    if edge["successful"] != edge["requests"] or edge["successful"] < 1:
        raise ValueError("edge request success count is invalid")
    expected_instances = {"backend-local", "backend-2", "backend-3"}
    for key in ("instance_counts", "counts_after_removal", "counts_after_restart"):
        counters = edge[key]
        if not isinstance(counters, dict) or set(counters) != expected_instances:
            raise ValueError("edge backend instance counters are incomplete")
        if any(not isinstance(value, int) or value < 0 for value in counters.values()):
            raise ValueError("edge backend instance counter is invalid")
    if sum(edge["instance_counts"].values()) < edge["successful"] or any(value < 1 for value in edge["instance_counts"].values()):
        raise ValueError("not every Backend replica served the edge workload")
    removed = edge["removed_instance"]
    if removed not in expected_instances - {"backend-local"}:
        raise ValueError("edge removed Backend instance is invalid")
    if edge["successful_during_removal"] < 1 or edge["successful_after_restart"] < 1:
        raise ValueError("edge did not remain available during Backend replacement")
    if edge["counts_after_removal"][removed] != edge["instance_counts"][removed]:
        raise ValueError("removed Backend continued serving after it stopped")
    survivors = expected_instances - {removed}
    if not all(edge["counts_after_removal"][item] > edge["instance_counts"][item] for item in survivors):
        raise ValueError("not every surviving Backend served during replacement")
    _require_digest(edge["observations_sha256"], "edge observations")

    ownership = document.get("ownership", {})
    if not isinstance(ownership, dict) or set(ownership) != {"outbox_lease", "alert_lease", "rabbit_ack_redelivery", "kafka_rebalance_fencing", "monitor_single_owner"}:
        raise ValueError("scaling ownership evidence is incomplete")
    outbox = ownership["outbox_lease"]
    if not isinstance(outbox, dict) or set(outbox) != {"future_owner", "future_lease_until", "blocked_observation", "reclaimed_observation"}:
        raise ValueError("Outbox lease evidence is incomplete")
    for label in ("blocked_observation", "reclaimed_observation"):
        observation = outbox[label]
        if not isinstance(observation, dict) or set(observation) != {"owner", "lease_until", "status", "updated_at", "counter_before", "counter_after"}:
            raise ValueError("Outbox lease observation is incomplete")
        for key in ("counter_before", "counter_after"):
            if not isinstance(observation[key], int) or observation[key] < 0:
                raise ValueError("Outbox lease counter is invalid")
    if (outbox["blocked_observation"]["owner"] != outbox["future_owner"]
            or outbox["blocked_observation"]["lease_until"] != outbox["future_lease_until"]
            or outbox["blocked_observation"]["status"] != "leased"):
        raise ValueError("Outbox future lease was not preserved")
    if outbox["blocked_observation"]["counter_after"] != outbox["blocked_observation"]["counter_before"]:
        raise ValueError("Outbox expired-owner lease reached publication")
    if outbox["reclaimed_observation"]["counter_after"] - outbox["reclaimed_observation"]["counter_before"] != 1:
        raise ValueError("Outbox expired lease was not reclaimed exactly once")
    alert = ownership["alert_lease"]
    if not isinstance(alert, dict) or set(alert) != {"future_owner", "future_lease_until", "blocked_observation", "reclaimed_observation"}:
        raise ValueError("alert lease evidence is incomplete")
    for label in ("blocked_observation", "reclaimed_observation"):
        observation = alert[label]
        if not isinstance(observation, dict) or set(observation) != {
            "owner", "lease_until", "status", "last_evaluated_at",
        }:
            raise ValueError("alert lease observation is incomplete")
    if alert["blocked_observation"]["owner"] != alert["future_owner"] or alert["blocked_observation"]["status"] != "leased":
        raise ValueError("alert future lease was not preserved")
    if alert["blocked_observation"]["last_evaluated_at"]:
        raise ValueError("alert future lease was evaluated before expiry")
    if alert["reclaimed_observation"]["status"] != "applied" or not alert["reclaimed_observation"]["last_evaluated_at"]:
        raise ValueError("alert expired lease was not reclaimed")
    rabbit = ownership["rabbit_ack_redelivery"]
    rabbit_keys = {
        "published", "acknowledged", "redelivery_attempts", "duplicate_side_effects",
        "final_ready", "final_unacknowledged", "observations_sha256",
    }
    if not isinstance(rabbit, dict) or set(rabbit) != rabbit_keys:
        raise ValueError("Rabbit ack/redelivery evidence is incomplete")
    for key in rabbit_keys - {"observations_sha256"}:
        if not isinstance(rabbit[key], int) or rabbit[key] < 0:
            raise ValueError("Rabbit ack/redelivery contains an invalid counter")
    if (rabbit["acknowledged"] != rabbit["published"] or rabbit["redelivery_attempts"] < 1
            or rabbit["duplicate_side_effects"] != 0 or rabbit["final_ready"] != 0
            or rabbit["final_unacknowledged"] != 0):
        raise ValueError("Rabbit ack/redelivery gate failed")
    _require_digest(rabbit["observations_sha256"], "Rabbit ack/redelivery observations")
    kafka = ownership["kafka_rebalance_fencing"]
    kafka_keys = {
        "messages", "committed", "duplicate_commits", "old_owner_commits_after_revoke",
        "final_lag", "members_before", "members_during_removal", "members_after_restart",
        "observations_sha256",
    }
    if not isinstance(kafka, dict) or set(kafka) != kafka_keys:
        raise ValueError("Kafka rebalance/fencing evidence is incomplete")
    for key in ("messages", "committed", "duplicate_commits", "old_owner_commits_after_revoke", "final_lag"):
        if not isinstance(kafka[key], int) or kafka[key] < 0:
            raise ValueError("Kafka rebalance/fencing contains an invalid counter")
    for key in ("members_before", "members_during_removal", "members_after_restart"):
        if (not isinstance(kafka[key], list) or not kafka[key]
                or any(not isinstance(value, str) or not value for value in kafka[key])):
            raise ValueError("Kafka rebalance member evidence is incomplete")
    if (kafka["committed"] != kafka["messages"] or kafka["duplicate_commits"] != 0
            or kafka["old_owner_commits_after_revoke"] != 0 or kafka["final_lag"] != 0
            or len(kafka["members_before"]) < 2 or len(kafka["members_after_restart"]) < 2):
        raise ValueError("Kafka rebalance/fencing gate failed")
    _require_digest(kafka["observations_sha256"], "Kafka rebalance/fencing observations")
    monitor = ownership["monitor_single_owner"]
    if not isinstance(monitor, dict) or set(monitor) != {"second_exit_code", "registry_sha256_before", "registry_sha256_after", "process_record_sha256_before", "process_record_sha256_after"}:
        raise ValueError("Monitor single-owner evidence is incomplete")
    if not isinstance(monitor["second_exit_code"], int) or monitor["second_exit_code"] == 0:
        raise ValueError("second Monitor instance did not fail")
    for key in ("registry_sha256_before", "registry_sha256_after", "process_record_sha256_before", "process_record_sha256_after"):
        _require_digest(monitor[key], "Monitor " + key)
    if monitor["registry_sha256_before"] != monitor["registry_sha256_after"] or monitor["process_record_sha256_before"] != monitor["process_record_sha256_after"]:
        raise ValueError("second Monitor instance mutated plugin ownership state")

    tests = document.get("tests")
    if not isinstance(tests, dict) or set(tests) != set(SCALING_TEST_COMMANDS):
        raise ValueError("scaling ownership test evidence is incomplete")
    for name, item in tests.items():
        if not isinstance(item, dict) or set(item) != {"command", "exit_code", "output_sha256"} or not isinstance(item["command"], str) or not item["command"]:
            raise ValueError(name + " scaling test evidence is invalid")
        if item["exit_code"] not in SCALING_TEST_COMMANDS[name]:
            raise ValueError(name + " scaling test did not pass")
        _require_digest(item["output_sha256"], name + " scaling test output")

    cleanup = document.get("cleanup")
    if not isinstance(cleanup, dict) or set(cleanup) != {"resource_inventory_before_sha256", "resource_inventory_after_sha256", "owned_projects_remaining", "label_cleanup"}:
        raise ValueError("scaling cleanup evidence is incomplete")
    _require_digest(cleanup["resource_inventory_before_sha256"], "cleanup inventory before")
    _require_digest(cleanup["resource_inventory_after_sha256"], "cleanup inventory after")
    if cleanup["resource_inventory_before_sha256"] != cleanup["resource_inventory_after_sha256"] or cleanup["owned_projects_remaining"] != 0 or cleanup["label_cleanup"] != "passed":
        raise ValueError("scaling cleanup gate failed")
    if document.get("secret_scan") != "passed":
        raise ValueError("scaling secret scan did not pass")
    secret_scan(document)
    return document


def _qualification_attachment(value: dict, label: str, work: Path | None) -> None:
    if (not isinstance(value, dict) or not {"path", "sha256", "bytes"}.issubset(value)
            or set(value) - {"path", "sha256", "bytes", "finding"}):
        raise ValueError(label + " attachment reference is incomplete")
    path = value["path"]
    if (not isinstance(path, str) or not path or path.startswith("/")
            or ".." in Path(path).parts or "\\" in path):
        raise ValueError(label + " attachment path is unsafe")
    _require_digest(value["sha256"], label + " attachment")
    if not isinstance(value["bytes"], int) or value["bytes"] < 1:
        raise ValueError(label + " attachment size is invalid")
    if work is not None:
        source = work / path
        if not source.is_file() or source.stat().st_size != value["bytes"] or sha(source) != value["sha256"]:
            raise ValueError(label + " attachment is missing or has changed")


def validate_bottleneck_diagnostic(document: dict, manifest: Path | None = None) -> dict:
    if not isinstance(document, dict) or document.get("schema") != BOTTLENECK_SCHEMA:
        raise ValueError("invalid Phase 18 bottleneck diagnostic schema")
    candidate = document.get("candidate")
    if not isinstance(candidate, dict) or candidate.get("version") != "2.0.2" or not REVISION.fullmatch(candidate.get("revision", "")):
        raise ValueError("bottleneck diagnostic candidate is invalid")
    for key in ("manifest_sha256", "bundle_sha256"):
        _require_digest(candidate.get(key), "bottleneck candidate " + key)
    if manifest is not None and sha(manifest) != candidate["manifest_sha256"]:
        raise ValueError("bottleneck diagnostic belongs to another release manifest")
    if not candidate.get("image_digests") or any(not DIGEST.fullmatch(str(value)) for value in candidate["image_digests"].values()):
        raise ValueError("bottleneck candidate image digests are incomplete")
    if not candidate.get("plugin_digests") or any(not DIGEST.fullmatch(str(value)) for value in candidate["plugin_digests"].values()):
        raise ValueError("bottleneck candidate plugin digests are incomplete")
    if document.get("finding") not in {"component", "shared_dependency", "load_generator", "inconclusive"}:
        raise ValueError("bottleneck diagnostic finding is invalid")
    if not isinstance(document.get("reason_code"), str) or not document["reason_code"]:
        raise ValueError("bottleneck diagnostic reason is missing")
    evidence = document.get("evidence")
    if not isinstance(evidence, list) or not evidence:
        raise ValueError("bottleneck diagnostic has no supporting evidence")
    for index, attachment in enumerate(evidence):
        _require_digest(attachment.get("sha256"), "bottleneck evidence %d" % index)
        if not isinstance(attachment.get("source"), str) or not attachment["source"]:
            raise ValueError("bottleneck evidence source is missing")
    if not isinstance(document.get("summary"), str) or not document["summary"]:
        raise ValueError("bottleneck diagnostic summary is missing")
    return document


def validate_qualification(document: dict, manifest: Path | None = None,
                           work: Path | None = None) -> dict:
    if (not isinstance(document, dict) or document.get("schema") != QUALIFICATION_SCHEMA
            or document.get("status") != "qualified"):
        raise ValueError("Phase 18 qualification is not qualified")
    candidate = document.get("candidate")
    if not isinstance(candidate, dict) or candidate.get("version") != "2.0.2" or not REVISION.fullmatch(candidate.get("revision", "")):
        raise ValueError("Phase 18 qualification candidate is invalid")
    for key in ("manifest_sha256", "bundle_sha256"):
        _require_digest(candidate.get(key), "qualification candidate " + key)
    if manifest is not None and sha(manifest) != candidate["manifest_sha256"]:
        raise ValueError("qualification belongs to another release manifest")
    if not candidate.get("image_digests") or any(not DIGEST.fullmatch(str(value)) for value in candidate["image_digests"].values()):
        raise ValueError("qualification candidate image digests are incomplete")
    if not candidate.get("plugin_digests") or any(not DIGEST.fullmatch(str(value)) for value in candidate["plugin_digests"].values()):
        raise ValueError("qualification candidate plugin digests are incomplete")
    if not isinstance(document.get("host"), dict) or document["host"].get("platform") != "linux/amd64":
        raise ValueError("qualification host evidence is incomplete")
    if not isinstance(document.get("started_at"), (int, float)) or not isinstance(document.get("finished_at"), (int, float)) or document["finished_at"] <= document["started_at"]:
        raise ValueError("qualification timing bounds are invalid")
    inputs = document.get("inputs")
    if not isinstance(inputs, dict) or set(inputs) != {
        "snapshot_sha256", "corpus_sha256", "recipe_receipt_sha256",
        "recipe_binary_sha256", "load_binary_sha256", "router_publisher_binary_sha256",
        "workload_recipe_sha256", "fixed_backlog", "execution_order",
    }:
        raise ValueError("qualification candidate inputs are incomplete")
    for key in ("snapshot_sha256", "corpus_sha256", "recipe_receipt_sha256",
                "recipe_binary_sha256", "load_binary_sha256", "router_publisher_binary_sha256"):
        _require_digest(inputs.get(key), "qualification input " + key)
    _require_digest(inputs.get("workload_recipe_sha256"), "qualification workload recipe")
    barrier = inputs.get("fixed_backlog")
    if (not isinstance(barrier, dict) or barrier.get("rabbit_messages", 0) < 1
            or barrier.get("prefetch", 0) < 1 or not isinstance(barrier.get("release_barrier"), str)):
        raise ValueError("qualification release barrier parameters are incomplete")
    if not isinstance(inputs.get("execution_order"), list) or not inputs["execution_order"]:
        raise ValueError("qualification execution order is missing")
    script_digests = document.get("script_digests")
    if not isinstance(script_digests, dict) or not script_digests:
        raise ValueError("qualification verifier/probe digests are missing")
    for name, value in script_digests.items():
        if not isinstance(name, str) or not name or not DIGEST.fullmatch(str(value)):
            raise ValueError("qualification verifier/probe digest is invalid")

    fixtures = document.get("parser_fixtures")
    if not isinstance(fixtures, dict) or set(fixtures) != {"mysql", "kafka", "live_mysql", "live_kafka"}:
        raise ValueError("qualification parser evidence is incomplete")
    for parser in ("mysql", "kafka"):
        items = fixtures[parser]
        if not isinstance(items, dict) or set(items) != QUALIFICATION_PARSER_CASES[parser]:
            raise ValueError(parser + " parser fixture coverage is incomplete")
        if any(value != "passed" for value in items.values()):
            raise ValueError(parser + " parser fixture failed")
    for key in ("live_mysql", "live_kafka"):
        if not isinstance(fixtures[key], dict) or fixtures[key].get("status") != "passed":
            raise ValueError("live " + key.removeprefix("live_") + " observer did not qualify")
        _require_digest(fixtures[key].get("observations_sha256"), key + " observations")

    rabbit_pairs = document.get("paired_workloads")
    if not isinstance(rabbit_pairs, dict) or set(rabbit_pairs) != {"business-worker", "search-indexer"}:
        raise ValueError("qualification RabbitMQ paired workloads are incomplete")
    for component, pair in rabbit_pairs.items():
        if not isinstance(pair, dict) or pair.get("status") != "passed":
            raise ValueError(component + " paired workload did not qualify")
        if pair.get("single_input_sha256") != pair.get("multi_input_sha256"):
            raise ValueError(component + " single/multi input differs")
        _require_digest(pair.get("single_input_sha256"), component + " fixed input")
        for side in ("single", "multi"):
            item = pair.get(side)
            if not isinstance(item, dict):
                raise ValueError(component + " " + side + " timing evidence is missing")
            if _require_positive(item.get("cold_start_seconds"), component + " cold start") <= 0:
                raise ValueError(component + " cold start duration is invalid")
            if _require_positive(item.get("measurement_seconds"), component + " measurement") <= 0:
                raise ValueError(component + " measurement duration is invalid")
            if not isinstance(item.get("samples"), int) or item["samples"] < 2:
                raise ValueError(component + " paired timeline has too few samples")
            for key in ("observations_sha256", "resource_samples_sha256"):
                _require_digest(item.get(key), component + " " + side + " " + key)

    marshaller = document.get("marshaller")
    if not isinstance(marshaller, dict) or set(marshaller) != {"single", "multi", "fixed_messages", "calibration"}:
        raise ValueError("Marshaller measurement evidence is incomplete")
    if not isinstance(marshaller["fixed_messages"], int) or marshaller["fixed_messages"] < 1:
        raise ValueError("Marshaller qualification message count is invalid")
    _require_digest(marshaller["calibration"].get("observations_sha256"), "Marshaller calibration")
    for side, members in (("single", 1), ("multi", 2)):
        item = marshaller[side]
        if not isinstance(item, dict) or item.get("status") != "passed":
            raise ValueError("Marshaller " + side + " measurement did not qualify")
        if item.get("fixed_messages") != marshaller["fixed_messages"]:
            raise ValueError("Marshaller measurement sides used different fixed input")
        if not isinstance(item.get("partition_count"), int) or not isinstance(item.get("member_count"), int):
            raise ValueError("Marshaller member or partition count is invalid")
        if _require_positive(item.get("measurement_seconds"), "Marshaller " + side + " window") < 30:
            raise ValueError("Marshaller " + side + " window is shorter than 30 seconds")
        if not isinstance(item.get("measurement_samples"), int) or item["measurement_samples"] < 10:
            raise ValueError("Marshaller " + side + " has fewer than 10 samples")
        if item.get("member_count") != members or item.get("partition_count") < 4:
            raise ValueError("Marshaller " + side + " member/partition assignment is incomplete")
        if side == "multi" and item.get("members_owning_partitions") != 2:
            raise ValueError("both Marshaller members did not receive partitions")
        for key in ("observations_sha256", "resource_samples_sha256"):
            _require_digest(item.get(key), "Marshaller " + side + " " + key)

    router = document.get("router")
    if not isinstance(router, dict) or set(router) != {"single_staircase", "multi_staircase", "kafka_producer_ceiling"}:
        raise ValueError("Router ceiling evidence is incomplete")
    for key in ("single_staircase", "multi_staircase"):
        staircase = router[key]
        if not isinstance(staircase, dict) or staircase.get("status") != "passed":
            raise ValueError("Router " + key + " did not qualify")
        steps = staircase.get("steps")
        if not isinstance(steps, list) or [item.get("concurrency") for item in steps] != [1, 8, 16, 32, 64]:
            raise ValueError("Router concurrency staircase is incomplete")
        for step in steps:
            if _require_positive(step.get("accepted"), "Router staircase accepted count") < 1 or _require_positive(step.get("elapsed_seconds"), "Router staircase duration") <= 0:
                raise ValueError("Router staircase step has invalid counters")
        _require_digest(staircase.get("observations_sha256"), "Router staircase observations")
        _require_digest(staircase.get("resource_samples_sha256"), "Router staircase resources")
    ceiling = router["kafka_producer_ceiling"]
    if not isinstance(ceiling, dict) or ceiling.get("status") not in {"observed", "inconclusive"}:
        raise ValueError("direct Kafka producer ceiling status is invalid")
    if ceiling["status"] == "observed":
        if _require_positive(ceiling.get("records_per_second"), "Kafka producer rate") <= 0:
            raise ValueError("direct Kafka producer rate is invalid")
        _require_digest(ceiling.get("observations_sha256"), "direct Kafka producer observations")
    else:
        if not isinstance(ceiling.get("reason_code"), str) or not ceiling["reason_code"]:
            raise ValueError("inconclusive Kafka producer ceiling lacks a reason code")

    replacements = document.get("replacements")
    required_components = {"backend", "business-worker", "search-indexer", "router", "marshaller"}
    if not isinstance(replacements, dict) or set(replacements) != required_components:
        raise ValueError("qualification replacement coverage is incomplete")
    for component, item in replacements.items():
        if not isinstance(item, dict) or item.get("status") != "passed":
            raise ValueError(component + " replacement did not qualify")
        for key in ("target_progress_before", "survivor_progress_during", "replacement_progress_after", "work_retained_samples"):
            if not isinstance(item.get(key), int) or item[key] < 1:
                raise ValueError(component + " replacement lacks live-work evidence")
        for key in ("observations_sha256", "resource_samples_sha256"):
            _require_digest(item.get(key), component + " replacement " + key)

    ownership = document.get("ownership")
    ownership_keys = {
        "status", "observations_sha256", "outbox_future_lease", "outbox_expired_reclaim",
        "alert_future_lease", "alert_expired_reclaim", "monitor_single_owner",
    }
    if not isinstance(ownership, dict) or set(ownership) != ownership_keys or ownership.get("status") != "passed":
        raise ValueError("runtime ownership fixture evidence is incomplete")
    _require_digest(ownership.get("observations_sha256"), "runtime ownership observations")
    if (ownership["outbox_future_lease"].get("owner") != "phase18-foreign"
            or ownership["outbox_future_lease"].get("status") != "leased"
            or ownership["outbox_expired_reclaim"].get("status") != "published"
            or ownership["outbox_expired_reclaim"].get("published") != 1):
        raise ValueError("Outbox future/expired lease fixture did not qualify")
    if (ownership["alert_future_lease"].get("owner") != "phase18-foreign"
            or ownership["alert_future_lease"].get("last_evaluated_at")
            or ownership["alert_expired_reclaim"].get("status") != "applied"):
        raise ValueError("alert future/expired lease fixture did not qualify")
    monitor = ownership["monitor_single_owner"]
    if (not isinstance(monitor, dict) or monitor.get("second_exit_code", 0) == 0
            or monitor.get("registry_sha256_before") != monitor.get("registry_sha256_after")
            or monitor.get("process_record_sha256_before") != monitor.get("process_record_sha256_after")):
        raise ValueError("Monitor single-owner fixture did not preserve state")

    resources = document.get("resources")
    if not isinstance(resources, dict) or resources.get("status") != "passed":
        raise ValueError("host/container/dependency resource sampling is incomplete")
    attachments = resources.get("attachments")
    if not isinstance(attachments, list) or not attachments:
        raise ValueError("qualification has no raw resource samples")
    for index, item in enumerate(attachments):
        _qualification_attachment(item, "resource sample %d" % index, work)
    if resources.get("containers_observed") is not True or resources.get("host_observed") is not True:
        raise ValueError("host and per-container resource sampling are required")
    for dependency in ("mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics"):
        if resources.get("dependencies", {}).get(dependency) != "observed":
            raise ValueError("direct dependency sample is missing for " + dependency)

    diagnostic = document.get("bottleneck_diagnostic")
    if not isinstance(diagnostic, dict) or set(diagnostic) != {"path", "sha256", "bytes", "finding"} or diagnostic.get("finding") not in {"component", "shared_dependency", "load_generator", "inconclusive"}:
        raise ValueError("bottleneck diagnostic summary is missing")
    _qualification_attachment(diagnostic, "bottleneck diagnostic", work)
    if work is not None:
        diagnostic_document = json.loads((work / diagnostic["path"]).read_text())
        validate_bottleneck_diagnostic(diagnostic_document, manifest)
        if diagnostic_document.get("candidate") != candidate:
            raise ValueError("bottleneck diagnostic belongs to another candidate revision")
        if diagnostic_document["finding"] != diagnostic["finding"]:
            raise ValueError("bottleneck diagnostic category changed")
    attachments = document.get("attachments")
    if not isinstance(attachments, list) or not attachments:
        raise ValueError("qualification raw attachment inventory is empty")
    for index, item in enumerate(attachments):
        _qualification_attachment(item, "qualification attachment %d" % index, work)
    if not isinstance(document.get("cleanup"), dict) or document["cleanup"].get("status") != "passed" or document["cleanup"].get("owned_projects_remaining") != 0:
        raise ValueError("qualification Compose cleanup failed")
    for key in ("resource_inventory_before_sha256", "resource_inventory_after_sha256"):
        _require_digest(document["cleanup"].get(key), "qualification cleanup " + key)
    if document.get("secret_scan") != "passed":
        raise ValueError("qualification secret/path scan failed")
    return document
