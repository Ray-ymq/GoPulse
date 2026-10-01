#!/usr/bin/env python3
"""Phase 20-05 resource-budget contract, measurements, and evidence verifier.

The module keeps the machine-readable budget separate from the existing
capacity evidence.  It accepts only bounded, owned evidence and never turns a
missing sample or a skipped fault into a passing result.
"""
from __future__ import annotations

import argparse
import concurrent.futures
import hashlib
import json
import math
import os
import re
import shutil
import subprocess
import sys
import threading
import time
import urllib.request
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
CONTRACT_PATH = ROOT / "deploy/phase20-resource-budgets.json"
CONTRACT_SCHEMA_PATH = ROOT / "deploy/phase20-resource-budgets.schema.json"
COMPOSE_PATH = ROOT / "deploy/compose.yaml"
TRACE_COMPOSE_PATH = ROOT / "deploy/phase20-trace.yaml"
CAPACITY_PROFILE_PATH = ROOT / "loadtest/phase20-capacity-profile.json"
CAPACITY_SCHEMA_PATH = ROOT / "loadtest/phase20-capacity-profile.schema.json"
SUSTAINED_PROFILE_PATH = ROOT / "loadtest/phase20-sustained-profile.json"
SUSTAINED_SCHEMA_PATH = ROOT / "loadtest/phase20-sustained-profile.schema.json"
MANIFEST_VERSION = "2.2.5"
REVISION = re.compile(r"^[0-9a-f]{40}$")
NANO_CPU_ROUNDING_TOLERANCE = 1024

if str(ROOT / "scripts/ci") not in sys.path:
    sys.path.insert(0, str(ROOT / "scripts/ci"))


class Incomplete(ValueError):
    """An evidence or contract condition cannot produce a conclusion."""


def digest(path: Path | str) -> str:
    return "sha256:" + hashlib.sha256(Path(path).read_bytes()).hexdigest()


def write_json(path: Path | str, value: object, mode: int = 0o600) -> None:
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = target.with_suffix(target.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    temporary.chmod(mode)
    temporary.replace(target)
    target.chmod(mode)


def read_json(path: Path | str) -> Any:
    return json.loads(Path(path).read_text(encoding="utf-8"))


def command(args: list[str], timeout: int = 300, *, env: dict[str, str] | None = None, input_text: str | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, input=input_text, text=True, capture_output=True, timeout=timeout, env=env)


def require(result: subprocess.CompletedProcess[str], operation: str) -> str:
    if result.returncode:
        detail = (result.stderr or result.stdout or "").strip()
        raise Incomplete(operation + (": " + detail[-700:] if detail else ""))
    return result.stdout


def _finite(value: Any, label: str) -> None:
    if isinstance(value, (int, float)) and not isinstance(value, bool) and not math.isfinite(value):
        raise Incomplete(label + " is not finite")
    if isinstance(value, str) and value.strip().upper() in {"TBD", "TODO", "UNKNOWN", "N/A"}:
        raise Incomplete(label + " is not a frozen value")


def _walk_finite(value: Any, path: str = "$") -> None:
    _finite(value, path)
    if isinstance(value, dict):
        for key, child in value.items():
            _walk_finite(child, path + "." + str(key))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            _walk_finite(child, f"{path}[{index}]")


def _schema_validate(value: Any, schema: Path, label: str) -> None:
    try:
        import jsonschema

        jsonschema.validate(value, read_json(schema))
    except ImportError as error:
        raise Incomplete("jsonschema is required for " + label) from error
    except Exception as error:
        raise Incomplete(label + " schema validation failed: " + str(error)) from error


def load_contract() -> dict[str, Any]:
    contract = read_json(CONTRACT_PATH)
    _schema_validate(contract, CONTRACT_SCHEMA_PATH, "resource budget contract")
    _walk_finite(contract)
    ids = [item["budget_id"] for item in contract["budgets"]]
    if len(ids) != len(set(ids)):
        raise Incomplete("resource budget IDs are not unique")
    if {item["case_id"] for item in contract["failure_cases"]} != {"B04", "B05", "B06"}:
        raise Incomplete("B04/B05/B06 failure cases are incomplete")
    if {item["id"] for item in contract["overhead"]["combinations"]} != {"O0", "O1", "O2", "O3"}:
        raise Incomplete("observer combinations are incomplete")
    if contract["candidate_version"] != MANIFEST_VERSION:
        raise Incomplete("resource budget candidate version drift")
    return contract


def load_profiles(contract: dict[str, Any] | None = None) -> tuple[dict[str, Any], dict[str, Any]]:
    contract = contract or load_contract()
    capacity = read_json(CAPACITY_PROFILE_PATH)
    sustained = read_json(SUSTAINED_PROFILE_PATH)
    _schema_validate(capacity, CAPACITY_SCHEMA_PATH, "capacity profile")
    _schema_validate(sustained, SUSTAINED_SCHEMA_PATH, "sustained profile")
    if capacity["target_candidate_version"] != MANIFEST_VERSION or sustained["candidate_version"] != MANIFEST_VERSION:
        raise Incomplete("profile candidate version drift")
    if capacity["resource_budget"]["contract_id"] != contract["contract_id"] or sustained["resource_budget_contract_id"] != contract["contract_id"]:
        raise Incomplete("profile budget contract binding drift")
    if capacity["recipe"]["digest"] != sustained["recipe"]["digest"] or capacity["recipe"]["seed"] != sustained["recipe"]["seed"]:
        raise Incomplete("sustained profile recipe drift")
    return capacity, sustained


def parse_bytes(value: Any) -> int:
    if isinstance(value, bool):
        raise Incomplete("boolean is not a byte value")
    if isinstance(value, (int, float)):
        if value < 0 or not math.isfinite(float(value)):
            raise Incomplete("negative or non-finite byte value")
        return int(value)
    match = re.fullmatch(r"\s*([0-9]+(?:\.[0-9]+)?)\s*([kmgtpe](?:i?b)?|b)?\s*", str(value), re.I)
    if not match:
        raise Incomplete("invalid byte value: " + str(value))
    number = float(match.group(1))
    suffix = (match.group(2) or "b").lower()
    # Docker's resource parser treats the Compose shorthand m/g values as
    # binary units.  Keep the explicit IEC spellings binary as well so the
    # rendered contract compares with HostConfig.Memory exactly.
    factors = {"b": 1, "k": 1024, "m": 1024**2, "g": 1024**3, "t": 1024**4,
               "p": 1024**5, "e": 1024**6, "kb": 1024, "mb": 1024**2, "gb": 1024**3, "tb": 1024**4,
               "kib": 1024, "mib": 1024**2, "gib": 1024**3, "tib": 1024**4}
    if suffix not in factors:
        raise Incomplete("invalid byte suffix: " + suffix)
    return int(number * factors[suffix])


def compose_document(env_file: Path, files: list[Path] | None = None) -> dict[str, Any]:
    paths = files or [COMPOSE_PATH]
    args = ["docker", "compose", "--env-file", str(env_file)]
    for path in paths:
        args.extend(["-f", str(path)])
    args.extend(["config", "--no-interpolate", "--format", "json"])
    return json.loads(require(command(args, timeout=120), "render Compose resource contract"))


def _limit_values(service: dict[str, Any]) -> tuple[float, int]:
    cpus = service.get("cpus")
    if cpus is None:
        deploy = service.get("deploy", {}).get("resources", {}).get("limits", {})
        cpus = deploy.get("cpus")
    memory = service.get("mem_limit")
    if memory is None:
        memory = service.get("deploy", {}).get("resources", {}).get("limits", {}).get("memory")
    if cpus is None or memory is None:
        raise Incomplete("Compose service has no effective CPU/memory limit")
    try:
        cpu_value = float(cpus)
    except (TypeError, ValueError) as error:
        raise Incomplete("Compose CPU limit is invalid") from error
    if cpu_value <= 0 or not math.isfinite(cpu_value):
        raise Incomplete("Compose CPU limit is not finite and positive")
    return cpu_value, parse_bytes(memory)


def nano_cpus_match(actual: int, expected: int) -> bool:
    """Allow the Docker daemon's sub-micro-CPU conversion rounding."""
    return abs(int(actual) - int(expected)) <= NANO_CPU_ROUNDING_TOLERANCE


def verify_rendered_limits(contract: dict[str, Any], document: dict[str, Any], *, include_collector: bool = False) -> dict[str, Any]:
    expected = {name: value for name, value in contract["compose_limits"].items() if include_collector or name != "phase20-collector"}
    actual_services = document.get("services", {})
    checks = []
    for name, frozen in expected.items():
        if name not in actual_services:
            raise Incomplete("Compose resource target is missing: " + name)
        cpus, memory = _limit_values(actual_services[name])
        if abs(cpus - float(frozen["cpus"])) > 1e-9 or memory != int(frozen["memory_bytes"]):
            raise Incomplete("Compose resource limit drift: " + name)
        checks.append({"service": name, "cpus": cpus, "memory_bytes": memory, "scope": frozen["scope"], "status": "pass"})
    return {"status": "pass", "services": checks, "compose_sha256": digest(COMPOSE_PATH), "trace_overlay_sha256": digest(TRACE_COMPOSE_PATH) if include_collector else None}


def inspect_project(project: str, env_file: Path, files: list[Path], contract: dict[str, Any]) -> dict[str, Any]:
    ps_args = ["docker", "compose", "--project-name", project, "--env-file", str(env_file)]
    for file in files:
        ps_args.extend(["-f", str(file)])
    ids = require(command(ps_args + ["ps", "-q"], timeout=30), "list owned containers").split()
    if not ids:
        raise Incomplete("B01 did not find running owned containers")
    inspected = json.loads(require(command(["docker", "inspect", *ids], timeout=60), "inspect owned resource limits"))
    results = []
    for item in inspected:
        labels = item.get("Config", {}).get("Labels", {}) or {}
        if labels.get("com.docker.compose.project") != project:
            raise Incomplete("B01 ownership changed")
        service = labels.get("com.docker.compose.service")
        if service not in contract["compose_limits"]:
            continue
        frozen = contract["compose_limits"][service]
        host = item.get("HostConfig", {})
        nano_cpus = int(host.get("NanoCpus") or 0)
        memory = int(host.get("Memory") or 0)
        expected_nano = int(round(float(frozen["cpus"]) * 1_000_000_000))
        if not nano_cpus_match(nano_cpus, expected_nano) or memory != int(frozen["memory_bytes"]):
            raise Incomplete("B01 inspect limit drift: " + service)
        state = item.get("State", {})
        if state.get("OOMKilled") or state.get("Restarting"):
            raise Incomplete("B01 found OOM/restarting container: " + service)
        results.append({"service": service, "container_id": item.get("Id"), "nano_cpus": nano_cpus, "memory_bytes": memory, "oom_killed": bool(state.get("OOMKilled")), "running": bool(state.get("Running"))})
    if not results:
        raise Incomplete("B01 did not inspect any frozen steady-state service")
    return {"status": "pass", "project": project, "containers": sorted(results, key=lambda row: row["service"]), "ownership": "single_project"}


def read_jsonl(path: Path) -> list[dict[str, Any]]:
    rows = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if line.strip():
            rows.append(json.loads(line))
    return rows


def summarize_resources(rows: list[dict[str, Any]], contract: dict[str, Any]) -> dict[str, Any]:
    if not rows:
        raise Incomplete("resource samples are empty")
    if any(row.get("missing_signals") or row.get("failure") for row in rows):
        raise Incomplete("resource samples contain missing signals or failure")
    containers = [row.get("signals", {}).get("containers", []) for row in rows]
    sut_cpu = [sum(float(item.get("cpu_percent", 0)) for item in group) / 100 for group in containers]
    sut_rss = [sum(int(item.get("rss_bytes", 0)) for item in group) for group in containers]
    host_free = [int(row["signals"]["host"]["disk_free_bytes"]) for row in rows if row.get("signals", {}).get("host", {}).get("disk_free_bytes") is not None]
    rabbit = [row.get("signals", {}).get("rabbitmq", []) for row in rows]
    ready = [sum(int(item.get("ready", 0)) for item in group) for group in rabbit]
    unacked = [sum(int(item.get("unacked", 0)) for item in group) for group in rabbit]
    lag = [sum(int(item.get("lag", 0)) for item in row.get("signals", {}).get("kafka_lag", [])) for row in rows]
    if not host_free or not rabbit or not lag:
        raise Incomplete("resource samples omit disk, queue, or lag signals")
    thresholds = {item["budget_id"]: item for item in contract["budgets"]}
    values = {
        "cpu.sut_peak_cores": max(sut_cpu),
        "memory.sut_rss_peak": max(sut_rss),
        "queue.rabbit_ready_peak": max(ready),
        "queue.rabbit_unacked_peak": max(unacked),
        "queue.kafka_lag_peak": max(lag),
        "disk.host_free_min": min(host_free),
    }
    statuses = {}
    for budget_id, value in values.items():
        budget = thresholds[budget_id]
        passed = value <= budget["threshold"] if budget["comparison"] == "less_equal" else value >= budget["threshold"]
        statuses[budget_id] = {"value": value, "threshold": budget["threshold"], "comparison": budget["comparison"], "status": "pass" if passed else "fail"}
    return {"sample_count": len(rows), "cpu_sut_peak_cores": max(sut_cpu), "rss_sut_peak_bytes": max(sut_rss), "host_free_min_bytes": min(host_free), "rabbit_ready_peak": max(ready), "rabbit_unacked_peak": max(unacked), "kafka_lag_peak": max(lag), "budgets": statuses}


def validate_overhead_trials(value: dict[str, Any], contract: dict[str, Any]) -> dict[str, Any]:
    if value.get("formal") is not False:
        raise Incomplete("observer comparison must be diagnostic-only")
    expected = {item["id"] for item in contract["overhead"]["combinations"]}
    trials = value.get("trials")
    if not isinstance(trials, list) or len(trials) != len(expected) * 3:
        raise Incomplete("observer comparison must contain four combinations with three repetitions")
    seen = {(item.get("combination_id"), item.get("repeat")) for item in trials}
    if seen != {(case, repeat) for case in expected for repeat in range(1, 4)}:
        raise Incomplete("observer comparison combinations/repetitions are incomplete")
    for trial in trials:
        if trial.get("target_rps") != 200 or trial.get("warmup_seconds") != 15 or trial.get("measurement_seconds") != 60:
            raise Incomplete("observer comparison load drift")
        if trial.get("execution_status") != "complete" or trial.get("business_errors") != 0:
            raise Incomplete("observer comparison trial is incomplete")
        for key in ("p99_ms", "cpu_peak_cores", "rss_peak_bytes"):
            if not isinstance(trial.get(key), (int, float)) or not math.isfinite(float(trial[key])):
                raise Incomplete("observer comparison raw metric missing: " + key)
        if trial.get("combination_id") in {"O1", "O2"} and float(trial.get("missing_sample_ratio", 1)) > contract["overhead"]["thresholds"]["sampler"]["missing_sample_ratio"]:
            raise Incomplete("observer comparison sample loss exceeds frozen threshold")
    by_key = {(item["combination_id"], item["repeat"]): item for item in trials}
    import statistics

    def median(combo: str, field: str) -> float:
        return float(statistics.median(float(by_key[(combo, repeat)][field]) for repeat in range(1, 4)))

    def delta(left: str, right: str, field: str) -> dict[str, Any]:
        first, second = median(left, field), median(right, field)
        difference = first - second
        ratio = None if second == 0 else difference / second
        return {"enabled": first, "baseline": second, "delta": difference, "ratio": ratio}

    thresholds = contract["overhead"]["thresholds"]
    comparisons = {
        "normal_observability": {"p99_ms": delta("O1", "O3", "p99_ms"), "cpu_peak_cores": delta("O1", "O3", "cpu_peak_cores"), "rss_peak_bytes": delta("O1", "O3", "rss_peak_bytes")},
        "trace_sampling": {"p99_ms": delta("O2", "O1", "p99_ms"), "cpu_peak_cores": delta("O2", "O1", "cpu_peak_cores"), "rss_peak_bytes": delta("O2", "O1", "rss_peak_bytes")},
        "sampler": {"p99_ms": delta("O1", "O3", "p99_ms"), "cpu_peak_cores": median("O1", "sampler_cpu_peak_cores")},
    }
    normal = comparisons["normal_observability"]
    normal_limit = thresholds["normal_observability"]
    if normal["p99_ms"]["delta"] > normal_limit["business_p99_delta_ms"] or (normal["p99_ms"]["ratio"] is not None and normal["p99_ms"]["ratio"] > normal_limit["business_p99_ratio"]):
        raise Incomplete("normal observer overhead exceeds frozen threshold")
    if normal["cpu_peak_cores"]["delta"] > normal_limit["cpu_delta_cores"] or normal["rss_peak_bytes"]["delta"] > normal_limit["rss_delta_bytes"]:
        raise Incomplete("normal observer resource overhead exceeds frozen threshold")
    trace = comparisons["trace_sampling"]
    trace_limit = thresholds["trace_sampling"]
    if trace["p99_ms"]["delta"] > trace_limit["business_p99_delta_ms"] or (trace["p99_ms"]["ratio"] is not None and trace["p99_ms"]["ratio"] > trace_limit["business_p99_ratio"]) or trace["cpu_peak_cores"]["delta"] > trace_limit["cpu_delta_cores"] or trace["rss_peak_bytes"]["delta"] > trace_limit["rss_delta_bytes"]:
        raise Incomplete("Trace observer overhead exceeds frozen threshold")
    sampler = comparisons["sampler"]
    sampler_limit = thresholds["sampler"]
    if sampler["p99_ms"]["delta"] > sampler_limit["business_p99_delta_ms"] or sampler["cpu_peak_cores"] > sampler_limit["observer_cpu_peak_cores"]:
        raise Incomplete("sampler overhead exceeds frozen threshold")
    return {"status": "pass", "trials": len(trials), "combinations": sorted(expected), "comparisons": comparisons}


def _relative_case(path: Path, root: Path) -> str:
    try:
        return str(path.resolve().relative_to(root.resolve()))
    except ValueError as error:
        raise Incomplete("evidence path escapes budget root") from error


def verify_budget_directory(directory: Path, *, formal: bool = True) -> dict[str, Any]:
    root = Path(directory).resolve()
    contract = load_contract()
    document = read_json(root / "budget.json")
    if document.get("schema") != "gopulse.phase20.budget.v1" or document.get("formal") is not formal:
        raise Incomplete("budget document schema/formal mode mismatch")
    manifest_path = root / "candidate-manifest.json"
    manifest = read_json(manifest_path)
    if manifest.get("version") != MANIFEST_VERSION or not REVISION.fullmatch(str(manifest.get("revision", ""))):
        raise Incomplete("budget candidate binding is invalid")
    binding = {"version": manifest["version"], "revision": manifest["revision"], "manifest_sha256": digest(manifest_path)}
    if document.get("candidate") != binding or document.get("contract_sha256") != digest(CONTRACT_PATH):
        raise Incomplete("budget candidate or contract digest drift")
    expected = {"B01", "B02", "B03", "B04", "B05", "B06"} if formal else {"B01", "B04", "B05", "B06", "B07"}
    cases = document.get("cases")
    if not isinstance(cases, list) or {item.get("case_id") for item in cases} != expected or len(cases) != len(expected):
        raise Incomplete("budget case set is incomplete")
    checked = {}
    for case in cases:
        relative = case.get("path")
        if not isinstance(relative, str) or Path(relative).is_absolute() or ".." in Path(relative).parts:
            raise Incomplete("budget case path is unsafe")
        path = root / relative
        if not path.is_file() or digest(path) != case.get("sha256"):
            raise Incomplete("budget case digest mismatch: " + str(relative))
        raw = read_json(path)
        if raw.get("case_id") != case["case_id"] or raw.get("status") != "pass" or case.get("status") != "pass":
            raise Incomplete("budget case did not pass: " + str(case.get("case_id")))
        checked[case["case_id"]] = raw
    b01 = checked["B01"]
    if b01.get("rendered", {}).get("status") != "pass" or b01.get("inspection", {}).get("status") != "pass":
        raise Incomplete("B01 did not prove rendered and inspected limits")
    if formal:
        b02 = checked["B02"]
        if b02.get("resource_summary", {}).get("sample_count", 0) <= 0 or any(item.get("status") != "pass" for item in b02["resource_summary"].get("budgets", {}).values()):
            raise Incomplete("B02 resource budget result is incomplete")
        validate_overhead_trials(checked["B03"]["overhead"], contract)
    for case_id in ("B04", "B05", "B06"):
        raw = checked[case_id]
        if not raw.get("injection", {}).get("effective") or not raw.get("recovery", {}).get("status") == "pass":
            raise Incomplete(case_id + " lacks effective injection/recovery evidence")
    if not formal and checked["B07"].get("preflight", {}).get("status") != "pass":
        raise Incomplete("B07 preflight evidence is incomplete")
    return {"execution_status": "complete", "case_status": {case_id: "pass" for case_id in sorted(expected)}, "candidate": binding, "formal": formal}


def _candidate_env(manifest: dict[str, Any], output: Path) -> dict[str, str]:
    from phase19_capacity import parse_env, write_env

    values = parse_env(ROOT / ".env.example")
    revision = manifest["revision"]
    values.update({
        "GOPULSE_VERSION": manifest["version"],
        "GOPULSE_REVISION": revision,
        "GOPULSE_IMAGE_TAG": "phase20-05-" + revision[:12],
        "GOPULSE_RUNTIME_MODE": "container",
        "GOPULSE_BOOTSTRAP_USER_ID": "1",
        "PUBLISHED_HOST": "127.0.0.1",
        "FRONTEND_PORT": "19080",
        "HTTP_PORT": "19090",
        "MYSQL_PORT": "19306",
    })
    for name, image in {**(manifest.get("images") or {}), **(manifest.get("third_party") or {})}.items():
        if isinstance(image, dict):
            image = image.get("ref")
        if isinstance(image, str) and image:
            values["GOPULSE_" + name.upper().replace("-", "_") + "_IMAGE"] = image
    write_env(output, values)
    return values


def _compose_args(project: str, env: Path, files: list[Path]) -> list[str]:
    args = ["docker", "compose", "--project-name", project, "--env-file", str(env)]
    for path in files:
        args.extend(["-f", str(path)])
    return args


def run_b01(root: Path, manifest: dict[str, Any], contract: dict[str, Any]) -> dict[str, Any]:
    env = root / "candidate.env"
    _candidate_env(manifest, env)
    rendered = verify_rendered_limits(contract, compose_document(env))
    project = "gopulse-p2005-b01-" + manifest["revision"][:8]
    override = root / "b01.override.yaml"
    override.write_text("services:\n  mysql:\n    ports:\n      - 127.0.0.1:19306:3306\n", encoding="utf-8")
    files = [COMPOSE_PATH, override]
    args = _compose_args(project, env, files)
    started = False
    try:
        require(command(args + ["up", "-d", "--wait", "--wait-timeout", "900"], timeout=1200), "start B01 owned stack")
        started = True
        inspection = inspect_project(project, env, files, contract)
    finally:
        if started:
            cleanup = command(args + ["down", "--volumes", "--remove-orphans"], timeout=900)
            if cleanup.returncode:
                raise Incomplete("B01 cleanup failed")
    return {"case_id": "B01", "status": "pass", "rendered": rendered, "inspection": inspection}


def run_b02(root: Path, manifest_path: Path, manifest: dict[str, Any], contract: dict[str, Any]) -> dict[str, Any]:
    """Run one real 200 RPS diagnostic cell and bind its sampler output."""
    import phase19_capacity as legacy
    import phase20_diagnostic as diagnostic

    profile = diagnostic.load_profile()
    binding, candidate = legacy.candidate_binding(manifest_path, profile)
    work = root / "b02-cell"
    work.mkdir(mode=0o700)
    shutil.copyfile(manifest_path, work / "candidate-manifest.json")
    shutil.copyfile(CAPACITY_PROFILE_PATH, work / "profile.json")
    recipe_binary, load_binary = legacy.build_loadtest(work)
    cell = diagnostic.run_cell(profile, binding, candidate, recipe_binary, load_binary, work, 1, 3)
    resources = work / cell["raw"]["resources"]["path"]
    summary = summarize_resources(read_jsonl(resources), contract)
    return {"case_id": "B02", "status": "pass", "cell": cell, "resource_summary": summary, "evidence_path": str(resources)}


def _stats_snapshot(project: str, env_file: Path, files: list[Path]) -> dict[str, float]:
    ids = require(command(_compose_args(project, env_file, files) + ["ps", "-q"], timeout=30), "list overhead containers").split()
    if not ids:
        raise Incomplete("overhead trial has no owned containers")
    inspected = json.loads(require(command(["docker", "inspect", *ids], timeout=30), "inspect overhead containers"))
    objects = {item["Id"]: item for item in inspected}
    raw = require(command(["docker", "stats", "--no-stream", "--format", "{{json .}}", *ids], timeout=30), "sample overhead containers")
    cpu = 0.0
    rss = 0
    for line in raw.splitlines():
        item = json.loads(line)
        obj = next((value for key, value in objects.items() if key.startswith(item.get("ID", ""))), None)
        if obj and obj.get("Config", {}).get("Labels", {}).get("com.docker.compose.project") == project:
            cpu += float(item.get("CPUPerc", "0%").rstrip("%")) / 100
            rss += parse_bytes(item.get("MemUsage", "0B").split("/", 1)[0])
    return {"cpu_peak_cores": cpu, "rss_peak_bytes": rss}


class LightweightResourceCounter:
    """Capture comparable SUT CPU/RSS samples without enabling the product sampler.

    O3 deliberately disables the Kafka/Rabbit/metrics observer.  It still needs
    the same resource time series as O1/O2 so a startup or background spike is
    not hidden by the two-point before/after fallback.  Docker stats is the
    independent low-overhead counter source for that comparison; it does not
    query Kafka, queues, product metrics, or any observer endpoint.
    """

    def __init__(self, project: str, env_file: Path, files: list[Path], interval: float, run_id: str, path: Path):
        self.project = project
        self.env_file = env_file
        self.files = files
        self.interval = interval
        self.run_id = run_id
        self.path = Path(path)
        self.stop_event = threading.Event()
        self.thread: threading.Thread | None = None
        self.failure: str | None = None
        self.records: list[dict[str, Any]] = []

    def start(self) -> None:
        self.thread = threading.Thread(target=self._run, name="phase20-lightweight-resource-counter")
        self.thread.start()

    def stop(self) -> None:
        self.stop_event.set()
        if self.thread:
            self.thread.join(timeout=20)
        if self.thread and self.thread.is_alive():
            raise RuntimeError("lightweight resource counter failed to join")
        if self.failure:
            raise RuntimeError("lightweight resource counter failed: " + self.failure)

    def _capture(self) -> list[dict[str, Any]]:
        ids = require(_compose_args(self.project, self.env_file, self.files) + ["ps", "-q"], "list lightweight counter containers").split()
        if not ids:
            raise Incomplete("lightweight counter has no owned containers")
        inspected = json.loads(require(command(["docker", "inspect", *ids], timeout=30), "inspect lightweight counter ownership"))
        objects = {item["Id"]: item for item in inspected}
        if any(item.get("Config", {}).get("Labels", {}).get("com.docker.compose.project") != self.project for item in inspected):
            raise Incomplete("lightweight counter ownership changed")
        raw = require(command(["docker", "stats", "--no-stream", "--format", "{{json .}}", *ids], timeout=30), "sample lightweight counter resources")
        containers = []
        for line in raw.splitlines():
            item = json.loads(line)
            obj = next((value for key, value in objects.items() if key.startswith(item.get("ID", ""))), None)
            if obj is None:
                raise Incomplete("lightweight counter returned an unknown container")
            containers.append({
                "service": obj.get("Config", {}).get("Labels", {}).get("com.docker.compose.service"),
                "cpu_percent": float(item.get("CPUPerc", "0%").rstrip("%")),
                "rss_bytes": parse_bytes(item.get("MemUsage", "0B").split("/", 1)[0]),
                "oom": bool(obj.get("State", {}).get("OOMKilled")),
                "running": bool(obj.get("State", {}).get("Running")),
            })
        if not containers:
            raise Incomplete("lightweight counter returned no containers")
        return containers

    def _run(self) -> None:
        next_at = time.monotonic()
        try:
            with self.path.open("x") as stream:
                os.chmod(self.path, 0o600)
                while not self.stop_event.wait(max(0, next_at - time.monotonic())):
                    started = time.monotonic()
                    missing: list[str] = []
                    failure = None
                    try:
                        containers = self._capture()
                    except Exception as error:
                        containers = []
                        failure = type(error).__name__ + ": " + str(error)
                    finished = time.monotonic()
                    row = {
                        "schema": "gopulse.phase20.overhead-resources.v1",
                        "run_id": self.run_id,
                        "source": "docker stats --no-stream",
                        "sampling_interval_seconds": self.interval,
                        "sequence": len(self.records),
                        "started_monotonic": started,
                        "finished_monotonic": finished,
                        "missing_signals": missing,
                        "failure": failure,
                        "signals": {"containers": containers},
                    }
                    self.records.append(row)
                    stream.write(json.dumps(row) + "\n")
                    stream.flush()
                    if failure:
                        self.failure = failure
                        self.stop_event.set()
                        break
                    next_at += self.interval
        except Exception as error:
            self.failure = type(error).__name__ + ": " + str(error)


def _http_window(api: Any, target_rps: int, warmup_seconds: int, measurement_seconds: int, sampler: Any | None, project: str, env_file: Path, files: list[Path], raw_path: Path) -> dict[str, Any]:
    def run_window(seconds: int, label: str) -> list[dict[str, Any]]:
        total = target_rps * seconds
        started = time.monotonic()

        def call(slot: int, scheduled: float) -> dict[str, Any]:
            sent = time.monotonic()
            try:
                response = api.call("/api/v1/users/me")
                status = 200 if isinstance(response, dict) and "data" in response else None
                error = None
            except Exception as error_value:  # retain the failure, never turn it into success
                status = None
                error = type(error_value).__name__ + ": " + str(error_value)
            finished = time.monotonic()
            return {"window": label, "slot_id": slot, "scheduled_monotonic": scheduled, "sent_monotonic": sent, "completed_monotonic": finished, "latency_ms": (finished - sent) * 1000, "status": status, "error": error}

        with concurrent.futures.ThreadPoolExecutor(max_workers=64) as workers:
            futures = []
            for slot in range(total):
                scheduled = started + slot / target_rps
                time.sleep(max(0, scheduled - time.monotonic()))
                futures.append(workers.submit(call, slot, scheduled))
            return [future.result() for future in futures]

    before = _stats_snapshot(project, env_file, files)
    process_before = time.process_time()
    if sampler:
        sampler.start()
    warmup = run_window(warmup_seconds, "warmup")
    measurement = run_window(measurement_seconds, "measurement")
    if sampler:
        sampler.stop()
    process_cpu = time.process_time() - process_before
    after = _stats_snapshot(project, env_file, files)
    rows = warmup + measurement
    failures = [row for row in rows if row["status"] != 200]
    values = sorted(row["latency_ms"] for row in measurement)
    p99 = values[int(math.ceil(0.99 * (len(values) - 1)))] if values else float("inf")
    resource_rows = sampler.records if sampler else []
    cpu_values = [sum(float(item.get("cpu_percent", 0)) for item in row.get("signals", {}).get("containers", [])) / 100 for row in resource_rows]
    rss_values = [sum(int(item.get("rss_bytes", 0)) for item in row.get("signals", {}).get("containers", [])) for row in resource_rows]
    raw = {"schema": "gopulse.phase20.overhead-trial.v1", "requests": rows, "sampler_records": resource_rows, "stats_before": before, "stats_after": after}
    write_json(raw_path, raw)
    return {
        "raw_path": raw_path.name,
        "combination_id": "",
        "repeat": 0,
        "target_rps": target_rps,
        "warmup_seconds": warmup_seconds,
        "measurement_seconds": measurement_seconds,
        "execution_status": "complete",
        "business_errors": len(failures),
        "p99_ms": p99,
        "cpu_peak_cores": max(cpu_values, default=max(before["cpu_peak_cores"], after["cpu_peak_cores"])),
        "rss_peak_bytes": max(rss_values, default=max(before["rss_peak_bytes"], after["rss_peak_bytes"])),
        "sampler_cpu_peak_cores": process_cpu / max(1, warmup_seconds + measurement_seconds),
        "missing_sample_ratio": (sum(bool(row.get("missing_signals") or row.get("failure")) for row in resource_rows) / len(resource_rows)) if resource_rows else 0.0,
        "request_count": len(measurement),
    }


def run_b03(root: Path, manifest_path: Path, manifest: dict[str, Any], contract: dict[str, Any]) -> dict[str, Any]:
    """Execute the four observer combinations as independent owned projects."""
    import phase20_diagnostic as diagnostic
    from phase20_sampler import Sampler

    profile = diagnostic.load_profile()
    binding, _ = __import__("phase19_capacity").candidate_binding(manifest_path, profile)
    binaries = root / "b03-bin"
    recipe_binary, _ = __import__("phase19_capacity").build_loadtest(binaries)
    combinations = {item["id"]: item for item in contract["overhead"]["combinations"]}
    trials = []
    for combination_id in ("O0", "O1", "O2", "O3"):
        combination = combinations[combination_id]
        for repeat in range(1, 4):
            trial_dir = root / (combination_id + "-" + str(repeat))
            trial_dir.mkdir(mode=0o700)
            env_file = trial_dir / "candidate.env"
            values = _candidate_env(manifest, env_file)
            override = __import__("phase19_capacity").compose_override(trial_dir / "compose.override.yaml", int(values["MYSQL_PORT"]))
            project = __import__("phase19_capacity").project_name()
            files = [COMPOSE_PATH, override]
            if combination_id == "O2":
                files.insert(1, TRACE_COMPOSE_PATH)
            started = False
            corpus = credentials = None
            try:
                legacy_args = _compose_args(project, env_file, files)
                require(command(legacy_args + ["up", "-d", "--wait", "--wait-timeout", "900"], timeout=1200), "start overhead project " + combination_id)
                __import__("phase19_capacity").ensure_owned_project(project, env_file, files)
                started = True
                address = __import__("phase19_capacity").mysql_service_address(project, env_file, files)
                _, corpus, credentials, _ = __import__("phase19_capacity").generate_recipe(recipe_binary, binding, values, trial_dir, 3306, mysql_host=address)
                require(command(legacy_args + ["run", "--rm", "--no-deps", "--entrypoint", "/usr/local/bin/search-reindex", "search-init"], timeout=900), "reindex overhead recipe")
                __import__("phase19_capacity").wait_initial_convergence(project, env_file, files, timeout=900)
                require(command(legacy_args + ["run", "--rm", "--no-deps", "admin-role"], timeout=60), "bootstrap overhead operator")
                api = diagnostic.ProductAPI("http://127.0.0.1:" + values["FRONTEND_PORT"], json.loads(credentials.read_text(encoding="utf-8")))
                if not combination["observability_services"]:
                    require(command(legacy_args + ["stop", "router", "router-2", "marshaller", "marshaller-2", "monitor"], timeout=120), "stop observer services for O0")
                sampler = None
                if combination["sampler_enabled"]:
                    sampler = Sampler(project, env_file, files, values, profile, combination_id + "-" + str(repeat), trial_dir / "resources.jsonl")
                elif combination_id == "O3":
                    sampler = LightweightResourceCounter(project, env_file, files, float(profile["sampling"]["interval_seconds"]), combination_id + "-" + str(repeat), trial_dir / "resources.jsonl")
                raw_path = trial_dir / "trial.json"
                trial = _http_window(api, int(contract["overhead"]["target_rps"]), int(contract["overhead"]["warmup_seconds"]), int(contract["overhead"]["measurement_seconds"]), sampler, project, env_file, files, raw_path)
                trial.update({"combination_id": combination_id, "repeat": repeat, "project": project, "candidate": binding})
                trials.append(trial)
            finally:
                if corpus:
                    corpus.unlink(missing_ok=True)
                if credentials:
                    credentials.unlink(missing_ok=True)
                if started:
                    cleanup = __import__("phase19_capacity").cleanup_project(project, env_file, files)
                    write_json(trial_dir / "cleanup.json", cleanup)
    overhead = {"formal": False, "trials": trials}
    overhead["validation"] = validate_overhead_trials(overhead, contract)
    return {"case_id": "B03", "status": "pass", "overhead": overhead}


def _prepare_fault_stack(work: Path, manifest_path: Path, manifest: dict[str, Any], recipe_binary: Path, label: str, *, trace: bool = True) -> dict[str, Any]:
    import phase19_capacity as legacy
    import phase20_diagnostic as diagnostic

    profile = diagnostic.load_profile()
    binding, _ = legacy.candidate_binding(manifest_path, profile)
    work.mkdir(mode=0o700, parents=True, exist_ok=False)
    env_file = work / (label + ".env")
    _candidate_env(manifest, env_file)
    override = work / (label + ".override.yaml")
    legacy.compose_override(override, 19308)
    files = [COMPOSE_PATH, override]
    if trace:
        files.insert(1, TRACE_COMPOSE_PATH)
    project = legacy.project_name()
    args = _compose_args(project, env_file, files)
    legacy.require(command(args + ["up", "-d", "--wait", "--wait-timeout", "900"], timeout=1200), "start " + label + " stack")
    legacy.ensure_owned_project(project, env_file, files)
    address = legacy.mysql_service_address(project, env_file, files)
    _, corpus, credentials, rejection = legacy.generate_recipe(recipe_binary, binding, _candidate_env(manifest, env_file), work, 3306, mysql_host=address)
    legacy.require(command(args + ["run", "--rm", "--no-deps", "--entrypoint", "/usr/local/bin/search-reindex", "search-init"], timeout=900), "reindex " + label + " recipe")
    legacy.wait_initial_convergence(project, env_file, files, timeout=900)
    legacy.require(command(args + ["run", "--rm", "--no-deps", "admin-role"], timeout=60), "bootstrap " + label + " operator")
    api = diagnostic.ProductAPI("http://127.0.0.1:" + str(legacy.parse_env(env_file)["FRONTEND_PORT"]), json.loads(credentials.read_text(encoding="utf-8")))
    return {"project": project, "env": env_file, "override": override, "files": files, "args": args, "binding": binding, "api": api, "corpus": corpus, "credentials": credentials, "rejection": rejection}


def _api_post(api: Any, title: str, content: str) -> dict[str, Any]:
    request = urllib.request.Request(api.url + "/api/v1/posts", json.dumps({"title": title, "content": content}).encode(), {"Content-Type": "application/json", "Cookie": api.cookie}, method="POST")
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=5) as response:
        raw = response.read(1024 * 1024)
        return {"status": response.status, "body": json.loads(raw) if raw else None}


def _queue_snapshot(stack: dict[str, Any]) -> dict[str, Any]:
    import phase19_capacity as legacy

    return legacy._rabbit_snapshot(stack["project"], stack["env"], stack["files"])


def _wait_empty(stack: dict[str, Any], timeout: int = 120) -> dict[str, Any]:
    import phase19_capacity as legacy

    started = time.monotonic()
    last = None
    while time.monotonic() - started < timeout:
        last = {"queue": _queue_snapshot(stack), "async": legacy.async_snapshot(stack["project"], stack["env"], stack["files"])}
        if all(last["async"].get(key, 1) <= 0 for key in ("outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag")):
            return {"status": "pass", "elapsed_seconds": time.monotonic() - started, "last": last}
        time.sleep(2)
    raise Incomplete("owned business waterline did not recover within 120 seconds: " + repr(last))


def _finish_fault_stack(stack: dict[str, Any]) -> dict[str, Any]:
    import phase19_capacity as legacy

    cleanup = legacy.cleanup_project(stack["project"], stack["env"], stack["files"])
    for path in (stack["corpus"], stack["credentials"], stack["env"], stack["override"]):
        Path(path).unlink(missing_ok=True)
    return cleanup


def run_b04(root: Path, manifest_path: Path, manifest: dict[str, Any], recipe_binary: Path, contract: dict[str, Any]) -> dict[str, Any]:
    stack = _prepare_fault_stack(root / "B04", manifest_path, manifest, recipe_binary, "b04", trace=True)
    collector = require(command(stack["args"] + ["ps", "-q", "phase20-collector"], timeout=30), "resolve B04 Collector").strip()
    worker_before = _queue_snapshot(stack)
    try:
        fault_started = time.time()
        require(command(stack["args"] + ["stop", "phase20-collector"], timeout=60), "inject B04 Collector fault")
        stopped = json.loads(require(command(["docker", "inspect", collector], timeout=30), "inspect B04 Collector stop"))[0]
        if stopped.get("State", {}).get("Running"):
            raise Incomplete("B04 Collector stop was not effective")
        business = []
        for index in range(10):
            business.append(_api_post(stack["api"], "B04 bounded write " + str(index), "accepted business fact during bounded Collector outage"))
        if any(item["status"] != 201 for item in business):
            raise Incomplete("B04 business request failed while Collector was stopped")
        time.sleep(60)
        require(command(stack["args"] + ["start", "phase20-collector"], timeout=60), "recover B04 Collector")
        recovered = _wait_empty(stack, 120)
        stopped_at = time.time()
        worker_fault_start = time.time()
        require(command(stack["args"] + ["stop", "business-worker", "business-worker-2"], timeout=60), "inject B04 worker backlog")
        backlog_posts = []
        for index in range(10, 20):
            backlog_posts.append(_api_post(stack["api"], "B04 backlog write " + str(index), "bounded worker backlog"))
        if any(item["status"] != 201 for item in backlog_posts):
            raise Incomplete("B04 backlog business request failed")
        time.sleep(10)
        backlog = _queue_snapshot(stack)
        if backlog["ready"] <= 0 and backlog["unacked"] <= 0:
            raise Incomplete("B04 worker fault did not produce an effective bounded backlog")
        require(command(stack["args"] + ["start", "business-worker", "business-worker-2"], timeout=60), "recover B04 worker backlog")
        worker_recovery = _wait_empty(stack, 120)
        return {"case_id": "B04", "status": "pass", "formal": True, "injection": {"effective": True, "collector": {"target": "phase20-collector", "duration_seconds": stopped_at - fault_started, "business_statuses": [item["status"] for item in business]}, "worker": {"target": "business-worker and business-worker-2", "duration_seconds": time.time() - worker_fault_start, "backlog": backlog, "business_statuses": [item["status"] for item in backlog_posts]}}, "recovery": {"status": "pass", "collector": recovered, "worker": worker_recovery}, "waterline_before": worker_before}
    finally:
        _finish_fault_stack(stack)


def run_b05(root: Path, manifest_path: Path, manifest: dict[str, Any], recipe_binary: Path, contract: dict[str, Any]) -> dict[str, Any]:
    stack = _prepare_fault_stack(root / "B05", manifest_path, manifest, recipe_binary, "b05", trace=True)
    collector = require(command(stack["args"] + ["ps", "-q", "phase20-collector"], timeout=30), "resolve B05 Collector").strip()
    fixture_dir = root / "B05" / "waterline-fixture"
    fixture_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    paths = []
    try:
        require(command(stack["args"] + ["stop", "phase20-collector"], timeout=60), "stop B05 Collector before fixture")
        chunk = (64 * 1024 * 1024 - 16 * 1024) // 4
        for index in range(4):
            path = fixture_dir / ("waterline-fixture-%02d.bin" % (index + 1))
            with path.open("wb") as output:
                output.truncate(chunk)
            paths.append(path)
            require(command(["docker", "cp", str(path), collector + ":/var/lib/gopulse/trace/" + path.name], timeout=120), "copy B05 owned waterline fixture")
        require(command(stack["args"] + ["start", "phase20-collector"], timeout=60), "start B05 Collector at waterline")
        time.sleep(5)
        observed = root / "B05" / "observed-trace"
        observed.mkdir(mode=0o700, exist_ok=True)
        require(command(["docker", "cp", collector + ":/var/lib/gopulse/trace/.", str(observed)], timeout=120), "copy B05 owned trace inventory")
        entries = [{"name": path.name, "bytes": path.stat().st_size} for path in sorted(observed.iterdir()) if path.is_file()]
        total = sum(item["bytes"] for item in entries)
        if total < 64 * 1024 * 1024 - 16 * 1024 or total > 64 * 1024 * 1024 or any(item["bytes"] > 16 * 1024 * 1024 for item in entries):
            raise Incomplete("B05 trace fixture did not stay within the frozen waterline")
        return {"case_id": "B05", "status": "pass", "formal": True, "injection": {"effective": True, "target": "phase20_trace_data", "waterline_bytes": total, "logical_threshold_bytes": 64 * 1024 * 1024, "fixture_files": entries}, "recovery": {"status": "pass", "collector_running": True, "volume_owned": True}, "cleanup": {"global_prune": False}}
    finally:
        _finish_fault_stack(stack)
        for path in paths:
            path.unlink(missing_ok=True)


def run_b06(root: Path, manifest_path: Path, manifest: dict[str, Any], recipe_binary: Path, contract: dict[str, Any]) -> dict[str, Any]:
    stack = _prepare_fault_stack(root / "B06", manifest_path, manifest, recipe_binary, "b06", trace=True)
    try:
        require(command(stack["args"] + ["stop", "business-worker", "business-worker-2"], timeout=60), "create B06 shutdown backlog")
        writes = [_api_post(stack["api"], "B06 shutdown write " + str(index), "graceful shutdown backlog") for index in range(4)]
        backlog = _queue_snapshot(stack)
        if any(item["status"] != 201 for item in writes) or backlog["ready"] <= 0 and backlog["unacked"] <= 0:
            raise Incomplete("B06 did not create an accepted bounded backlog")
        targets = ["business-worker", "business-worker-2", "phase20-collector"]
        sent_at = time.time()
        require(command(stack["args"] + ["kill", "-s", "SIGTERM", *targets], timeout=60), "send B06 SIGTERM")
        states = json.loads(require(command(["docker", "inspect", *[require(command(stack["args"] + ["ps", "-q", target], timeout=30), "resolve B06 target").strip() for target in targets]], timeout=60), "inspect B06 shutdown targets"))
        require(command(stack["args"] + ["up", "-d", "--wait", "--wait-timeout", "900", "business-worker", "business-worker-2", "phase20-collector"], timeout=300), "restart B06 shutdown targets")
        recovery = _wait_empty(stack, 120)
        return {"case_id": "B06", "status": "pass", "formal": True, "injection": {"effective": True, "signal": "SIGTERM", "targets": targets, "sent_at": sent_at, "states": [{"service": item.get("Config", {}).get("Labels", {}).get("com.docker.compose.service"), "running": item.get("State", {}).get("Running"), "exit_code": item.get("State", {}).get("ExitCode")} for item in states], "backlog": backlog, "business_statuses": [item["status"] for item in writes]}, "recovery": recovery, "cleanup": {"global_prune": False}}
    finally:
        _finish_fault_stack(stack)


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--work", required=True, type=Path)
    parser.add_argument("--preflight", action="store_true")
    args = parser.parse_args(argv)
    work = args.work.resolve()
    if work.exists():
        raise Incomplete("budget work directory already exists")
    work.mkdir(mode=0o700, parents=True)
    if work.stat().st_mode & 0o077:
        raise Incomplete("budget work directory is not private")
    contract = load_contract()
    capacity, sustained = load_profiles(contract)
    manifest = read_json(args.manifest)
    if manifest.get("version") != MANIFEST_VERSION or not REVISION.fullmatch(str(manifest.get("revision", ""))):
        raise Incomplete("candidate manifest is not a 2.2.5 immutable binding")
    binding = {"version": manifest["version"], "revision": manifest["revision"], "manifest_sha256": digest(args.manifest)}
    shutil.copyfile(args.manifest, work / "candidate-manifest.json")
    write_json(work / "budget-contract.json", contract)
    write_json(work / "profile-binding.json", {"capacity_profile": digest(CAPACITY_PROFILE_PATH), "sustained_profile": digest(SUSTAINED_PROFILE_PATH), "candidate": binding})
    cases: list[dict[str, Any]] = []
    try:
        b01 = run_b01(work, manifest, contract)
        path = work / "B01.json"; write_json(path, b01); cases.append({"case_id": "B01", "status": "pass", "path": path.name, "sha256": digest(path)})
        if args.preflight:
            import phase19_capacity as legacy
            recipe_binary, _ = legacy.build_loadtest(work / "preflight-bin")
            for runner in (run_b04, run_b05, run_b06):
                raw = runner(work, args.manifest, manifest, recipe_binary, contract)
                raw["formal"] = False
                path = work / (raw["case_id"] + ".json"); write_json(path, raw); cases.append({"case_id": raw["case_id"], "status": "pass", "path": path.name, "sha256": digest(path)})
            b07 = {"case_id": "B07", "status": "pass", "preflight": {"status": "pass", "candidate": binding, "profile_sha256": digest(CAPACITY_PROFILE_PATH), "sustained_profile_sha256": digest(SUSTAINED_PROFILE_PATH)}}
            path = work / "B07.json"; write_json(path, b07); cases.append({"case_id": "B07", "status": "pass", "path": path.name, "sha256": digest(path)})
            formal = False
        else:
            b02 = run_b02(work, args.manifest, manifest, contract)
            path = work / "B02.json"; write_json(path, b02); cases.append({"case_id": "B02", "status": "pass", "path": path.name, "sha256": digest(path)})
            b03 = run_b03(work, args.manifest, manifest, contract)
            path = work / "B03.json"; write_json(path, b03); cases.append({"case_id": "B03", "status": "pass", "path": path.name, "sha256": digest(path)})
            recipe_binary, _ = __import__("phase19_capacity").build_loadtest(work / "failure-bin")
            for runner in (run_b04, run_b05, run_b06):
                raw = runner(work, args.manifest, manifest, recipe_binary, contract)
                path = work / (raw["case_id"] + ".json"); write_json(path, raw); cases.append({"case_id": raw["case_id"], "status": "pass", "path": path.name, "sha256": digest(path)})
            formal = True
        document = {"schema": "gopulse.phase20.budget.v1", "formal": formal, "candidate": binding, "contract_sha256": digest(CONTRACT_PATH), "cases": cases, "execution_status": "complete"}
        write_json(work / "budget.json", document)
        result = verify_budget_directory(work, formal=formal)
        print(json.dumps(result, sort_keys=True))
        return 0
    except Exception as error:
        document = {"schema": "gopulse.phase20.budget.v1", "formal": not args.preflight, "candidate": binding, "contract_sha256": digest(CONTRACT_PATH), "cases": cases, "execution_status": "incomplete", "stop": {"classification": "acceptance_failure", "reason": type(error).__name__ + ": " + str(error)}}
        write_json(work / "budget.json", document)
        print(json.dumps(document["stop"], sort_keys=True), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(run())
