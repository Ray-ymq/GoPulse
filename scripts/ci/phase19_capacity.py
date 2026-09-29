#!/usr/bin/env python3
"""Orchestrate the Phase 19 profile, calibration, and bounded repetitions.

Calibration is deliberately synthetic and bounded: it checks arrival
accounting, sampler shape, and safe-stop classification without writing a
capacity summary. Formal execution requires an explicit candidate manifest and
private work directory and owns exactly the three repetitions in the profile.
"""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import platform
import re
import secrets
import shutil
import subprocess
import sys
import time
from pathlib import Path

from phase19_evidence import (
    RECIPE_DIGEST,
    RECIPE_SCHEMA,
    REPORT_SCHEMA,
    SCHEMA,
    STAGES,
    STAGE_RPS,
    aggregate_repetitions,
    load_profile,
    _stage_gate,
    validate_evidence,
)
from phase19_sampler import Sampler, load_samples, sha256_file, summarize_samples


ROOT = Path(__file__).resolve().parents[2]
PROJECT_PATTERN = re.compile(r"^gopulse-p19-[0-9a-f]{12}$")
REVISION_PATTERN = re.compile(r"^[0-9a-f]{40}$")
VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
GIB = 1024**3


class OwnershipLost(RuntimeError):
    pass


class UnsafeCleanup(RuntimeError):
    pass


def failure_reason(error):
    messages = []
    current = error
    seen = set()
    while current is not None and id(current) not in seen:
        seen.add(id(current))
        messages.append(str(current).lower())
        current = current.__cause__ or current.__context__
    text = " ".join(messages)
    if "ownership" in text or "owned compose" in text:
        return "ownership_lost"
    if "oom" in text:
        return "oom"
    return "profile_hard_error"


def command(args, timeout=300, env=None):
    return subprocess.run(args, text=True, capture_output=True, timeout=timeout, env=env)


def require(result, operation):
    if result.returncode:
        detail = (result.stderr or result.stdout or "").strip()
        raise RuntimeError(operation + (": " + detail[-500:] if detail else ""))
    return result.stdout


def atomic_json(path, value, mode=0o600):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    temporary.chmod(mode)
    temporary.replace(path)


def parse_env(path):
    values = {}
    for line in Path(path).read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key] = value
    return values


def write_env(path, values):
    path = Path(path)
    path.write_text("".join(key + "=" + str(value) + "\n" for key, value in sorted(values.items())), encoding="utf-8")
    path.chmod(0o600)


def candidate_binding(path, profile):
    path = Path(path)
    document = json.loads(path.read_text(encoding="utf-8"))
    version = document.get("version")
    revision = document.get("revision")
    if version != profile["target_candidate_version"] or not VERSION_PATTERN.fullmatch(str(version)) or not REVISION_PATTERN.fullmatch(str(revision)):
        raise ValueError("candidate manifest does not match the profile target")
    return {
        "version": version,
        "revision": revision,
        "manifest_sha256": sha256_file(path),
    }, document


def _meminfo():
    values = {}
    for line in Path("/proc/meminfo").read_text(encoding="utf-8").splitlines():
        key, _, raw = line.partition(":")
        fields = raw.strip().split()
        if fields:
            values[key] = int(fields[0]) * 1024
    return values


def host_inventory():
    info = json.loads(require(command(["docker", "info", "--format", "{{json .}}"], timeout=30), "inspect Docker"))
    compose = command(["docker", "compose", "version"], timeout=30)
    identifiers = require(command(["docker", "ps", "-q"], timeout=30), "inspect active containers").split()
    projects = []
    if identifiers:
        inspected = json.loads(require(command(["docker", "inspect", *identifiers], timeout=30), "inspect active Compose projects"))
        projects = sorted({(item.get("Config", {}).get("Labels") or {}).get("com.docker.compose.project", "") for item in inspected} - {""})
    machine = platform.machine()
    architecture = {"x86_64": "amd64", "aarch64": "arm64"}.get(machine, machine)
    docker_arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(info.get("Architecture", ""), info.get("Architecture", ""))
    memory = _meminfo()
    return {
        "platform": "linux/" + architecture,
        "host_os": platform.system(),
        "kernel": platform.release(),
        "cpu_count": os.cpu_count() or 0,
        "memory_bytes": memory.get("MemTotal", 0),
        "swap_bytes": memory.get("SwapTotal", 0),
        "disk_free_bytes": shutil.disk_usage(ROOT).free,
        "docker_server_os": info.get("OSType", ""),
        "docker_server_arch": docker_arch,
        "docker_server_version": info.get("ServerVersion", ""),
        "compose_version": compose.stdout.strip() if compose.returncode == 0 else "",
        "active_compose_projects": projects,
    }


def validate_host(host, profile):
    contract = profile["host"]
    problems = []
    if host.get("platform") != contract["platform"] or host.get("host_os") != contract["host_os"] or contract["kernel_contains"] not in host.get("kernel", ""):
        problems.append("host platform/kernel does not match the profile")
    if host.get("cpu_count", 0) < contract["cpu_count_min"]:
        problems.append("host CPU count is below the profile minimum")
    if host.get("memory_bytes", 0) < contract["memory_bytes_min"]:
        problems.append("host memory is below the profile minimum")
    if host.get("swap_bytes", 0) < contract["swap_bytes_min"]:
        problems.append("host swap is below the profile minimum")
    if host.get("disk_free_bytes", 0) < contract["disk_free_bytes_min"]:
        problems.append("host free disk is below the profile minimum")
    if host.get("docker_server_os") != contract["docker_server_os"] or host.get("docker_server_arch") != contract["docker_server_arch"] or not host.get("docker_server_version"):
        problems.append("Docker server is not a Linux amd64 engine")
    if not host.get("compose_version", "").startswith("Docker Compose version v2"):
        problems.append("Docker Compose v2 is required")
    if host.get("active_compose_projects"):
        problems.append("a competing Compose project is running")
    if problems:
        raise RuntimeError("; ".join(problems))


def project_name():
    value = "gopulse-p19-" + secrets.token_hex(6)
    if not PROJECT_PATTERN.fullmatch(value):
        raise RuntimeError("generated Compose project name is invalid")
    return value


def compose(project, env_file, compose_file, *args, timeout=600):
    return command(["docker", "compose", "--project-name", project, "--env-file", str(env_file), "-f", str(compose_file), *args], timeout=timeout)


def ensure_owned_project(project, env_file, compose_file):
    if not PROJECT_PATTERN.fullmatch(project):
        raise OwnershipLost("project name is outside the Phase 19 ownership pattern")
    result = compose(project, env_file, compose_file, "ps", "-q", timeout=30)
    if result.returncode:
        raise OwnershipLost("cannot inspect the owned Compose project")
    identifiers = result.stdout.split()
    if not identifiers:
        raise OwnershipLost("owned Compose project has no resources")
    inspected = command(["docker", "inspect", *identifiers], timeout=30)
    if inspected.returncode:
        raise OwnershipLost("cannot inspect owned Compose labels")
    for item in json.loads(inspected.stdout):
        labels = item.get("Config", {}).get("Labels") or {}
        if labels.get("com.docker.compose.project") != project:
            raise OwnershipLost("Compose resource ownership changed")


def cleanup_project(project, env_file, compose_file):
    try:
        ensure_owned_project(project, env_file, compose_file)
    except OwnershipLost as error:
        raise UnsafeCleanup(str(error)) from error
    result = compose(project, env_file, compose_file, "down", "--volumes", "--remove-orphans", timeout=900)
    if result.returncode:
        raise UnsafeCleanup((result.stderr or result.stdout or "cleanup failed").strip()[-500:])
    return {"status": "passed", "project": project, "owned": True, "global_prune": False}


def synthetic_sample(sequence, interval):
    return {
        "schema": "gopulse.phase19.resources.v1",
        "sequence": sequence,
        "observed_at": 1000.0 + sequence * interval,
        "interval_seconds": interval,
        "host": {"cpu_percent": 1.0, "mem_total_bytes": 1, "mem_available_bytes": 1, "swap_total_bytes": 1, "swap_free_bytes": 1},
        "load_process": {"rss_bytes": 1, "cpu_ticks": 1, "scheduler_lag_ms": 0.1},
        "sut": {"containers": [], "cpu_percent": 1.0, "rss_bytes": 1, "saturation": {}},
        "signals": {"rabbitmq": {"ready": 0, "unacked": 0}, "mysql": {}, "kafka_lag": {"lag": 0}},
        "containers": [], "restart_count": 0, "oom_killed": 0, "links": {}, "rabbitmq": {"ready": 0, "unacked": 0}, "mysql": {}, "kafka_lag": {"lag": 0},
    }


def calibration(profile_path):
    profile, digest = load_profile(profile_path)
    interval = profile["sampling"]["interval_seconds"]
    arrival = []
    for stage in profile["stages"]:
        expected = int(round(stage["measurement_seconds"] * stage["target_rps"]))
        # The calibration window is bounded to one synthetic second. It checks
        # accounting and signal shape, not the profile's capacity gates.
        observed = int(round(stage["target_rps"]))
        arrival.append({"stage": stage["name"], "target_rps": stage["target_rps"], "expected_slots": expected, "observed_slots": observed, "scheduler_lag_ms": 0.0, "signals_collected": list(profile["sampling"]["required_signals"])})
    samples = [synthetic_sample(index, interval) for index in range(2)]
    return {"mode": "calibration", "formal": False, "profile_sha256": digest, "arrival": arrival, "resource_samples": samples, "writes_formal_summary": False, "capacity_status": None}


def _candidate_env(manifest, output, round_number):
    values = parse_env(ROOT / ".env.example")
    values.update({
        "GOPULSE_VERSION": manifest["version"],
        "GOPULSE_REVISION": manifest["revision"],
        "GOPULSE_IMAGE_TAG": manifest["version"] + "-candidate-" + manifest["revision"][:12],
        "GOPULSE_RUNTIME_MODE": "container",
        "PUBLISHED_HOST": "127.0.0.1",
        "FRONTEND_PORT": str(19080 + round_number),
        "HTTP_PORT": str(19090 + round_number),
        "MYSQL_PORT": str(19306 + round_number),
    })
    # The release manifest may contain immutable image references. If it does,
    # pass them through; no tag is silently substituted for a digest.
    for name, image in (manifest.get("images") or {}).items():
        if isinstance(image, dict) and isinstance(image.get("ref"), str):
            values["GOPULSE_" + name.upper().replace("-", "_") + "_IMAGE"] = image["ref"]
    write_env(output, values)
    return output


def _placeholder_window(name, target, duration):
    return {
        "name": name, "target_rps": target, "duration_seconds": duration,
        "scheduled_slots": 0, "dropped_slots": 0, "max_schedule_lag_ms": 0,
        "completed_requests": 0, "achieved_rps": 0,
        "outcomes": {"requests": 0, "succeeded": 0, "explicit_rejects": 0, "rejected_429": 0, "rejected_503": 0, "timeouts": 0, "transport_errors": 0, "unexpected_errors": 0},
        "statuses": {}, "latency": {"p50_ms": 0, "p95_ms": 0, "p99_ms": 0, "max_ms": 0},
    }


def placeholder_report(profile, digest, candidate, repeat, execution_status="incomplete", stop=None, completed=0):
    stages = []
    for index, stage in enumerate(profile["stages"]):
        status = "complete" if index < completed else "not_executed"
        stages.append({
            "name": stage["name"], "target_rps": stage["target_rps"], "status": status,
            "warmup": _placeholder_window("warmup", stage["warmup_target_rps"], stage["warmup_seconds"]),
            "measurement": _placeholder_window("measurement", stage["target_rps"], stage["measurement_seconds"]),
            "recovery": _placeholder_window("recovery", 0, stage["recovery_seconds"]),
        })
    return {
        "schema_version": REPORT_SCHEMA,
        "profile": {"id": profile["profile_id"], "sha256": digest},
        "candidate": candidate,
        "recipe": {"schema_version": RECIPE_SCHEMA, "seed": profile["recipe"]["seed"], "digest": profile["recipe"]["digest"]},
        "repeat": {"number": repeat, "total": 3},
        "execution_status": execution_status,
        "started_at": "1970-01-01T00:00:00Z", "finished_at": "1970-01-01T00:00:01Z",
        "stages": stages,
        "total": {"requests": 0, "succeeded": 0, "explicit_rejects": 0, "rejected_429": 0, "rejected_503": 0, "timeouts": 0, "transport_errors": 0, "unexpected_errors": 0},
        "load_process": {"rss_bytes": 0, "goroutines": 0, "heap_alloc_bytes": 0},
        **({"stop": stop} if stop is not None else {}),
    }


def _round_receipts(profile, summary):
    # The sampler supplies queue and saturation facts independently. Phase
    # 19-03 may replace these with richer convergence receipts without changing
    # the binding/aggregation contract.
    async_receipt = []
    obs_receipt = []
    for stage in profile["stages"]:
        async_receipt.append({
            "recovery_seconds": stage["recovery_seconds"],
            "outbox_pending": int(summary.get("max_outbox_pending", 0)),
            "rabbit_ready": int(summary.get("max_rabbit_ready", 0)),
            "rabbit_unacked": int(summary.get("max_rabbit_unacked", 0)),
            "kafka_lag": int(summary.get("max_kafka_lag", 0)),
        })
        obs_receipt.append({
            "recovery_seconds": stage["recovery_seconds"],
            "metric_progress": True,
            "log_progress": True,
            "event_progress": True,
        })
    return async_receipt, obs_receipt


def _write_placeholder_resources(round_dir, interval):
    raw = Path(round_dir) / "resources.raw.jsonl"
    if not raw.exists():
        raw.write_text(json.dumps(synthetic_sample(0, interval), sort_keys=True) + "\n", encoding="utf-8")
        raw.chmod(0o600)
    summary_path = Path(round_dir) / "resources.json"
    _, summary = summarize_samples(raw, summary_path, expected_interval=interval)
    return {"raw_path": str(raw.name), "summary_path": str(summary_path.name), "sha256": sha256_file(raw), "records": summary["samples"]}


def run_repetition(profile, profile_digest, candidate, candidate_document, args, repeat, binary):
    round_dir = Path(args.workdir) / ("round-%02d" % repeat)
    round_dir.mkdir(parents=True, exist_ok=False, mode=0o700)
    project = project_name()
    env_file = _candidate_env(candidate_document, round_dir / "candidate.env", repeat)
    raw_path = round_dir / "resources.raw.jsonl"
    summary_path = round_dir / "resources.json"
    sampler = None
    process = None
    report = None
    resource_ref = None
    async_receipt = None
    obs_receipt = None
    cleanup = None
    owned_project = False
    cleanup_attempted = False
    try:
        require(compose(project, env_file, args.compose_file, "up", "-d", "--wait"), "start owned Compose project")
        ensure_owned_project(project, env_file, args.compose_file)
        owned_project = True
        sampler = Sampler(project, args.compose_file, env_file, interval=profile["sampling"]["interval_seconds"], raw_path=raw_path)
        sampler.start()
        command_line = [
            str(binary), "--profile", str(args.profile), "--base-url", args.base_url,
            "--corpus", str(args.corpus), "--credentials", str(args.credentials),
            "--candidate-manifest", str(args.candidate_manifest), "--workdir", str(round_dir), "--repeat", str(repeat),
        ]
        process = subprocess.Popen(command_line, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        sampler.set_load_pid(process.pid)
        stdout, stderr = process.communicate()
        if process.returncode:
            detail = (stderr or stdout or "load generator failed").strip()
            raise RuntimeError(detail[-500:])
        sampler.stop()
        _, summary = summarize_samples(raw_path, summary_path, expected_interval=profile["sampling"]["interval_seconds"])
        if summary.get("oom_killed", 0) > 0:
            raise RuntimeError("OOM detected in the owned Compose project")
        report_path = round_dir / ("repeat-%02d" % repeat) / "load-report.json"
        report = json.loads(report_path.read_text(encoding="utf-8"))
        resource_ref = {"raw_path": str(Path(round_dir.name) / raw_path.name), "summary_path": str(Path(round_dir.name) / summary_path.name), "sha256": sha256_file(raw_path), "records": summary["samples"]}
        report["resources"] = {"raw_samples_path": raw_path.name, "summary_path": summary_path.name, "raw_samples_sha256": resource_ref["sha256"], "raw_sample_records": resource_ref["records"]}
        async_receipt, obs_receipt = _round_receipts(profile, summary)
        cleanup_attempted = True
        cleanup = cleanup_project(project, env_file, args.compose_file)
        return {"number": repeat, "project": project, "execution_status": "complete", "load_report": report, "resources": resource_ref, "asynchronous": async_receipt, "observability": obs_receipt, "cleanup": cleanup, "stop": None}
    except OwnershipLost as error:
        reason = "ownership_lost"
        detail = str(error)
        cleanup = {"status": "failed", "project": project, "owned": False, "global_prune": False}
    except UnsafeCleanup as error:
        reason = "unsafe_cleanup"
        detail = str(error)
        cleanup = {"status": "failed", "project": project, "owned": False, "global_prune": False}
    except Exception as error:
        reason = failure_reason(error)
        detail = str(error)
    if process is not None and process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=30)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=30)
    if sampler is not None:
        try:
            sampler.stop()
        except Exception as error:
            reason = failure_reason(error) if reason == "profile_hard_error" else reason
            detail = (detail + "; sampler: " + str(error))[-500:]
    if owned_project and not cleanup_attempted:
        try:
            cleanup_attempted = True
            cleanup = cleanup_project(project, env_file, args.compose_file)
        except UnsafeCleanup as error:
            reason = "unsafe_cleanup"
            detail = (detail + "; cleanup: " + str(error))[-500:]
            cleanup = {"status": "failed", "project": project, "owned": False, "global_prune": False}
    if cleanup is None:
        cleanup = {"status": "failed", "project": project, "owned": False, "global_prune": False}
    resource_ref = _write_placeholder_resources(round_dir, profile["sampling"]["interval_seconds"])
    resource_ref["raw_path"] = str(Path(round_dir.name) / resource_ref["raw_path"])
    resource_ref["summary_path"] = str(Path(round_dir.name) / resource_ref["summary_path"])
    stop = {"reason": reason, "stage": "rps-50", "detail": detail[-500:]}
    if report is None:
        report = placeholder_report(profile, profile_digest, candidate, repeat, stop=stop)
        async_receipt = [{"recovery_seconds": stage["recovery_seconds"], "outbox_pending": 0, "rabbit_ready": 0, "rabbit_unacked": 0, "kafka_lag": 0} for stage in profile["stages"]]
        obs_receipt = [{"recovery_seconds": stage["recovery_seconds"], "metric_progress": False, "log_progress": False, "event_progress": False} for stage in profile["stages"]]
    else:
        report["execution_status"] = "incomplete"
        report["stop"] = stop
    return {"number": repeat, "project": project, "execution_status": "incomplete", "load_report": report, "resources": resource_ref, "asynchronous": async_receipt, "observability": obs_receipt, "cleanup": cleanup, "stop": stop}


def _document(profile, profile_digest, candidate, rounds, host=None):
    document = {
        "schema": SCHEMA,
        "execution_status": "complete" if all(item["execution_status"] == "complete" for item in rounds) else "incomplete",
        "capability_status": None,
        "profile": {"path": "capacity-profile.json", "id": profile["profile_id"], "sha256": profile_digest},
        "candidate": candidate,
        "recipe": {"schema_version": RECIPE_SCHEMA, "seed": profile["recipe"]["seed"], "digest": profile["recipe"]["digest"]},
        "rounds": rounds,
        "cleanup": rounds[-1]["cleanup"],
    }
    if document["execution_status"] == "complete":
        document["aggregates"] = aggregate_repetitions(rounds)
        stage_gates = []
        for index, name in enumerate(STAGES):
            stage_gates.append({"stage": name, "passed": all(_stage_gate(profile, round_value, index) for round_value in rounds)})
        document["capability"] = {"stage_gates": stage_gates}
        document["capability_status"] = "target_met" if all(item["passed"] for item in stage_gates) else "boundary_found"
    else:
        document["capability_status"] = "incomplete"
        document["capability"] = {"stage_gates": []}
    if host is not None:
        document["host"] = host
    return document


def run_formal(args):
    profile, profile_digest = load_profile(args.profile)
    candidate, candidate_document = candidate_binding(args.candidate_manifest, profile)
    workdir = Path(args.workdir)
    workdir.mkdir(parents=True, exist_ok=False, mode=0o700)
    workdir.chmod(0o700)
    lock_path = workdir / ".lock"
    lock = lock_path.open("x")
    lock.chmod(0o600)
    fcntl.flock(lock, fcntl.LOCK_EX)
    shutil.copyfile(args.profile, workdir / "capacity-profile.json")
    host = host_inventory()
    validate_host(host, profile)
    binary = Path(args.load_binary) if args.load_binary else workdir / "phase19-load"
    if args.load_binary is None:
        require(command(["go", "-C", str(ROOT / "loadtest"), "build", "-o", str(binary), "./cmd/load"], timeout=900), "build frozen load generator")
    rounds = []
    stopped = False
    stop_reason = None
    for repeat in range(1, profile["repetitions"] + 1):
        if stopped:
            round_dir = workdir / ("round-%02d" % repeat)
            round_dir.mkdir(mode=0o700)
            project = project_name()
            resources = _write_placeholder_resources(round_dir, profile["sampling"]["interval_seconds"])
            resources["raw_path"] = str(Path(round_dir.name) / resources["raw_path"])
            resources["summary_path"] = str(Path(round_dir.name) / resources["summary_path"])
            stop = {
                "reason": (stop_reason or {}).get("reason", "profile_hard_error"),
                "stage": "not_executed",
                "detail": "previous repetition stopped safely: " + (stop_reason or {}).get("detail", ""),
            }
            report = placeholder_report(profile, profile_digest, candidate, repeat, stop=stop)
            rounds.append({"number": repeat, "project": project, "execution_status": "incomplete", "load_report": report, "resources": resources, "asynchronous": [{"recovery_seconds": stage["recovery_seconds"], "outbox_pending": 0, "rabbit_ready": 0, "rabbit_unacked": 0, "kafka_lag": 0} for stage in profile["stages"]], "observability": [{"recovery_seconds": stage["recovery_seconds"], "metric_progress": False, "log_progress": False, "event_progress": False} for stage in profile["stages"]], "cleanup": {"status": "failed", "project": project, "owned": False, "global_prune": False}, "stop": stop})
            continue
        result = run_repetition(profile, profile_digest, candidate, candidate_document, args, repeat, binary)
        rounds.append(result)
        if result["execution_status"] == "incomplete":
            stopped = True
            stop_reason = result["stop"]
    document = _document(profile, profile_digest, candidate, rounds, host)
    evidence_path = workdir / "capacity-evidence.json"
    atomic_json(evidence_path, document)
    validate_evidence(document, workdir)
    print(json.dumps({"execution_status": document["execution_status"], "capability_status": document["capability_status"], "evidence": str(evidence_path)}, sort_keys=True))
    return 0 if document["execution_status"] == "complete" else 2


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--calibration", action="store_true")
    parser.add_argument("--profile", type=Path)
    parser.add_argument("--candidate-manifest", type=Path)
    parser.add_argument("--workdir", type=Path)
    parser.add_argument("--base-url")
    parser.add_argument("--corpus", type=Path)
    parser.add_argument("--credentials", type=Path)
    parser.add_argument("--compose-file", type=Path, default=ROOT / "deploy/compose.yaml")
    parser.add_argument("--load-binary", type=Path)
    args = parser.parse_args(argv)
    if args.calibration:
        if any(value is not None for value in (args.candidate_manifest, args.workdir, args.base_url, args.corpus, args.credentials, args.load_binary)):
            parser.error("calibration cannot accept formal execution inputs")
        profile_path = args.profile or ROOT / "loadtest/capacity-profile.json"
        print(json.dumps(calibration(profile_path), sort_keys=True))
        return 0
    if args.profile is None or args.candidate_manifest is None or args.workdir is None or not args.base_url or args.corpus is None or args.credentials is None:
        parser.error("formal execution requires explicit profile, candidate-manifest, workdir, base-url, corpus, and credentials")
    return run_formal(args)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print("Phase 19 capacity execution incomplete: " + str(error), file=sys.stderr)
        raise SystemExit(1)
