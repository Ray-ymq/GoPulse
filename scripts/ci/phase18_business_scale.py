#!/usr/bin/env python3
"""Run the fixed two-round Phase 18-03 business-scale acceptance."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import secrets
import socket
import subprocess
import sys
import time
from pathlib import Path
from typing import Any, Iterable

ROOT = Path(__file__).resolve().parents[2]
VERSION_RE = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
BRANCH_RE = re.compile(r"^develop/2\.0\.3$")
PROJECT_RE = re.compile(r"^gopulse-p1803-[0-9a-f]{12}-r[12]$")
EXPECTED_REPETITIONS = 2
UNIT_NAMES = ("U1", "U2", "U3", "U4")
SECRET_KEYS = {
    "MYSQL_PASSWORD",
    "MYSQL_ROOT_PASSWORD",
    "REDIS_PASSWORD",
    "RABBITMQ_PASSWORD",
    "AUTH_JWT_SECRET",
    "MONITOR_API_TOKEN",
    "BACKEND_METRICS_TOKEN",
    "BUSINESS_WORKER_METRICS_TOKEN",
    "SEARCH_INDEXER_METRICS_TOKEN",
    "MONITOR_METRICS_TOKEN",
    "ROUTER_METRICS_TOKEN",
    "MARSHALLER_METRICS_TOKEN",
    "LOG_MONITOR_INGEST_TOKEN",
    "ROUTER_API_TOKEN",
    "MARSHALLER_API_TOKEN",
    "VICTORIAMETRICS_PASSWORD",
    "BACKEND_VICTORIAMETRICS_PASSWORD",
}


def sha256_bytes(value: bytes) -> str:
    return "sha256:" + hashlib.sha256(value).hexdigest()


def sha256_file(path: Path) -> str:
    return sha256_bytes(path.read_bytes())


def write_json_once(path: Path, value: Any) -> None:
    if path.exists():
        raise ValueError(f"refusing to overwrite evidence: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    path.chmod(0o600)


def write_text(path: Path, value: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(value, encoding="utf-8")
    path.chmod(0o600)


def average(values: Iterable[float | int]) -> float:
    numbers = [float(value) for value in values]
    if not numbers:
        raise ValueError("cannot average an empty sequence")
    return round(sum(numbers) / len(numbers), 3)


def validate_repetitions(value: int) -> int:
    if value != EXPECTED_REPETITIONS:
        raise ValueError("Phase 18-03 formal mode requires exactly --repetitions 2")
    return value


def validate_candidate_binding(path: Path, candidate: dict[str, Any]) -> dict[str, Any]:
    expected = {"schema": "gopulse.phase18.business-scale-binding.v1", "candidate": candidate}
    if path.exists():
        actual = json.loads(path.read_text(encoding="utf-8"))
        if actual != expected:
            raise ValueError("candidate binding belongs to another candidate")
        return actual
    write_json_once(path, expected)
    return expected


def redact(text: str, values: dict[str, str] | None = None) -> str:
    redactions = set()
    if values:
        redactions.update(value for key, value in values.items() if key in SECRET_KEYS and value)
    for value in sorted(redactions, key=len, reverse=True):
        text = text.replace(value, "<redacted>")
    return text


def command_result(
    args: list[str], *, cwd: Path = ROOT, timeout: int = 300, env: dict[str, str] | None = None
) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(args, cwd=cwd, env=env, text=True, capture_output=True, timeout=timeout)
    except subprocess.TimeoutExpired as error:
        stdout = error.stdout if isinstance(error.stdout, str) else ""
        stderr = error.stderr if isinstance(error.stderr, str) else ""
        return subprocess.CompletedProcess(args, 124, stdout, stderr + "\ncommand timed out")
    except OSError as error:
        return subprocess.CompletedProcess(args, 127, "", str(error))


def record_command(
    directory: Path,
    name: str,
    args: list[str],
    *,
    cwd: Path = ROOT,
    timeout: int = 300,
    env: dict[str, str] | None = None,
    redact_values: dict[str, str] | None = None,
) -> dict[str, Any]:
    started_at = time.monotonic()
    result = command_result(args, cwd=cwd, timeout=timeout, env=env)
    stdout = redact(result.stdout, redact_values)
    stderr = redact(result.stderr, redact_values)
    write_text(directory / f"{name}.stdout.log", stdout)
    write_text(directory / f"{name}.stderr.log", stderr)
    return {
        "command": args,
        "cwd": str(cwd.relative_to(ROOT)) if cwd.is_relative_to(ROOT) else str(cwd),
        "exit_code": result.returncode,
        "elapsed_seconds": round(time.monotonic() - started_at, 3),
        "stdout": f"{name}.stdout.log",
        "stderr": f"{name}.stderr.log",
    }


def parse_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key] = value
    return values


def write_env(path: Path, values: dict[str, str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("".join(f"{key}={values[key]}\n" for key in sorted(values)), encoding="utf-8")
    path.chmod(0o600)


def free_port() -> int:
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])
    finally:
        sock.close()


def candidate_revision() -> str:
    result = command_result(["git", "rev-parse", "HEAD"])
    revision = result.stdout.strip()
    if result.returncode != 0 or not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise RuntimeError("could not resolve candidate revision")
    return revision


def working_tree_digest() -> str:
    status = command_result(["git", "status", "--porcelain=v1", "--untracked-files=all"]).stdout
    tracked = command_result(["git", "diff", "HEAD", "--binary"]).stdout
    untracked = []
    for line in status.splitlines():
        if line.startswith("?? "):
            path = ROOT / line[3:]
            if path.is_file():
                untracked.append(line[3:] + "\0" + path.read_bytes().decode("utf-8", errors="replace"))
    return sha256_bytes((tracked + "\n" + "\n".join(untracked)).encode("utf-8"))


def candidate_binding() -> dict[str, Any]:
    version = (ROOT / "VERSION").read_text(encoding="utf-8").strip()
    revision = candidate_revision()
    branch = command_result(["git", "branch", "--show-current"]).stdout.strip()
    if not VERSION_RE.fullmatch(version):
        raise ValueError("VERSION must use major.minor.patch")
    if not BRANCH_RE.fullmatch(branch):
        raise ValueError("Phase 18-03 runner must execute on develop/2.0.3")
    return {
        "version": version,
        "revision": revision,
        "branch": branch,
        "working_tree_sha256": working_tree_digest(),
        "runtime_contract_sha256": sha256_file(ROOT / "deploy/runtime-contracts.json"),
        "compose_sha256": sha256_file(ROOT / "deploy/compose.yaml"),
        "condition": {
            "runner": "scripts/verify-phase18-business-scale.sh --repetitions 2",
            "replicas": {"backend": 2, "business-worker": 2, "search-indexer": 2},
            "faults": ["backend", "business-worker", "search-indexer", "rabbitmq", "elasticsearch"],
        },
    }


def compose_command(project: str, env_file: Path, *args: str) -> list[str]:
    return [
        "docker",
        "compose",
        "--project-name",
        project,
        "--env-file",
        str(env_file),
        "--file",
        str(ROOT / "deploy/compose.yaml"),
        *args,
    ]


def acceptance_token(run_number: int, label: str) -> str:
    digest = hashlib.sha256(f"phase18-03-{run_number}-{label}".encode()).hexdigest()
    return digest[:12]


def candidate_environment(run_dir: Path, binding: dict[str, Any], run_number: int) -> tuple[Path, dict[str, str]]:
    values = parse_env(ROOT / ".env.example")
    token = acceptance_token(run_number, "compose")
    values.update(
        {
            "APP_ENV": "test",
            "PUBLISHED_HOST": "127.0.0.1",
            "FRONTEND_PORT": str(free_port()),
            "GOPULSE_VERSION": binding["version"],
            "GOPULSE_REVISION": binding["revision"],
            "GOPULSE_IMAGE_TAG": f"phase18-03-{binding['revision'][:12]}-{run_number}",
            "GOPULSE_UPDATE_VERSION": "1.11.7",
            "MYSQL_DATABASE": f"gopulse_{token}",
            "MYSQL_USER": f"user_{token}",
            "MYSQL_PASSWORD": f"mysql-{token}-0123456789abcdef0123456789abcdef",
            "MYSQL_ROOT_PASSWORD": f"root-{token}-0123456789abcdef0123456789abcdef",
            "REDIS_PASSWORD": f"redis-{token}-0123456789abcdef0123456789abcdef",
            "RABBITMQ_USER": f"rabbit_{token}",
            "RABBITMQ_PASSWORD": f"rabbit-{token}-0123456789abcdef0123456789abcdef",
            "AUTH_JWT_SECRET": f"jwt-{token}-0123456789abcdef0123456789abcdef",
            "AUTH_COOKIE_NAME": f"gopulse_{token}",
            "MONITOR_API_TOKEN": f"monitor-{token}-0123456789abcdef0123456789abcdef",
            "BACKEND_METRICS_TOKEN": f"backend-metrics-{token}-0123456789abcdef0123456789abcdef",
            "BUSINESS_WORKER_METRICS_TOKEN": f"worker-metrics-{token}-0123456789abcdef0123456789abcdef",
            "SEARCH_INDEXER_METRICS_TOKEN": f"indexer-metrics-{token}-0123456789abcdef0123456789abcdef",
            "MONITOR_METRICS_TOKEN": f"monitor-metrics-{token}-0123456789abcdef0123456789abcdef",
            "ROUTER_METRICS_TOKEN": f"router-metrics-{token}-0123456789abcdef0123456789abcdef",
            "MARSHALLER_METRICS_TOKEN": f"marshaller-metrics-{token}-0123456789abcdef0123456789abcdef",
            "LOG_MONITOR_INGEST_TOKEN": f"logs-{token}-0123456789abcdef0123456789abcdef",
            "ROUTER_API_TOKEN": f"router-{token}-0123456789abcdef0123456789abcdef",
            "MARSHALLER_API_TOKEN": f"marshaller-{token}-0123456789abcdef0123456789abcdef",
            "VICTORIAMETRICS_USERNAME": f"vm_{token}",
            "VICTORIAMETRICS_PASSWORD": f"vm-{token}-0123456789abcdef0123456789abcdef",
            "BACKEND_VICTORIAMETRICS_USERNAME": f"vm_{token}",
            "BACKEND_VICTORIAMETRICS_PASSWORD": f"backend-vm-{token}-0123456789abcdef0123456789abcdef",
        }
    )
    path = run_dir / "compose.env"
    write_env(path, values)
    return path, values


def wait_healthy(project: str, env_file: Path, service: str, directory: Path, values: dict[str, str], timeout: int = 180) -> dict[str, Any]:
    deadline = time.monotonic() + timeout
    last = ""
    while time.monotonic() < deadline:
        result = command_result(compose_command(project, env_file, "ps", "-q", service), timeout=30)
        container = result.stdout.strip().splitlines()[0] if result.stdout.strip() else ""
        if container:
            state = command_result(["docker", "inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}", container], timeout=30)
            last = state.stdout.strip() or state.stderr.strip()
            if state.returncode == 0 and last == "healthy":
                return {"service": service, "status": "healthy", "container": container}
        time.sleep(2)
    write_text(directory / f"{service}-health.log", redact(last, values))
    return {"service": service, "status": "timeout", "last": f"{service}-health.log"}


def acceptance(
    directory: Path,
    project: str,
    env_file: Path,
    values: dict[str, str],
    label: str,
    scenario: str,
    token_label: str | None = None,
    timeout: int = 180,
) -> dict[str, Any]:
    token = acceptance_token(int(directory.parent.name.split("-")[-1]), token_label or label)
    args = compose_command(
        project,
        env_file,
        "--profile",
        "acceptance",
        "run",
        "--rm",
        "--no-deps",
        "-e",
        f"GOPULSE_ACCEPTANCE_TOKEN={token}",
        "-e",
        f"GOPULSE_ACCEPTANCE_SCENARIO={scenario}",
        "acceptance",
        "e2e/compose-business.spec.ts",
    )
    return record_command(directory, label, args, timeout=timeout, redact_values=values)


def closure_snapshot(
    directory: Path, project: str, env_file: Path, values: dict[str, str]
) -> dict[str, Any]:
    mysql = compose_command(
        project,
        env_file,
        "exec",
        "-T",
        "mysql",
        "sh",
        "-c",
        "mysql --batch --skip-column-names --user=\"$MYSQL_USER\" --password=\"$MYSQL_PASSWORD\" \"$MYSQL_DATABASE\" -e \"SELECT COUNT(*) FROM business_outbox WHERE status IN ('pending','leased'); SELECT COUNT(*) FROM notifications;\"",
    )
    search = compose_command(
        project,
        env_file,
        "exec",
        "-T",
        "elasticsearch",
        "sh",
        "-c",
        "curl --fail --silent http://127.0.0.1:9200/_alias/gopulse-post-search-v1",
    )
    rabbit = compose_command(
        project,
        env_file,
        "exec",
        "-T",
        "rabbitmq",
        "rabbitmqctl",
        "list_queues",
        "name",
        "messages",
        "messages_unacknowledged",
    )
    records = {}
    for label, args in (("closure-mysql", mysql), ("closure-search", search), ("closure-rabbit", rabbit)):
        result = command_result(args, timeout=60)
        records[label] = {"exit_code": result.returncode, "stdout": redact(result.stdout, values), "stderr": redact(result.stderr, values)}
        write_text(directory / f"{label}.stdout.log", records[label]["stdout"])
        write_text(directory / f"{label}.stderr.log", records[label]["stderr"])
    mysql_lines = records["closure-mysql"]["stdout"].splitlines()
    pending = mysql_lines[0].strip() if len(mysql_lines) > 0 else "unknown"
    notifications = mysql_lines[1].strip() if len(mysql_lines) > 1 else "unknown"
    search_value = records["closure-search"]["stdout"].strip()
    return {
        "outbox_pending_or_leased": pending,
        "notifications": notifications,
        "search_alias": bool(search_value),
        "rabbit_snapshot_recorded": records["closure-rabbit"]["exit_code"] == 0,
        "commands": {key: {"exit_code": value["exit_code"]} for key, value in records.items()},
        "target_met": pending == "0" and notifications.isdigit() and int(notifications) > 0 and bool(search_value),
    }


def matrix_scenarios() -> list[tuple[str, str, str | None, str | None, bool]]:
    """Return the fixed U3 matrix and its per-fault account setup requirement."""
    return [
        ("normal", "business", None, None, False),
        ("backend_failover", "business", "backend-2", None, False),
        ("worker_failover", "worker-seed", "business-worker-2", "worker-recovery", True),
        ("worker_recovery", "worker-verify", None, "worker-recovery", False),
        ("indexer_failover", "indexer-seed", "search-indexer-2", "indexer-recovery", True),
        ("indexer_recovery", "indexer-verify", None, "indexer-recovery", False),
        ("rabbit_fault_seed", "worker-seed", "rabbitmq", "rabbit-recovery", True),
        ("rabbit_recovery", "worker-verify", None, "rabbit-recovery", False),
        ("elasticsearch_fault_seed", "indexer-seed", "elasticsearch", "elasticsearch-recovery", True),
        ("elasticsearch_recovery", "indexer-verify", None, "elasticsearch-recovery", False),
    ]


def run_matrix(run_dir: Path, binding: dict[str, Any], run_number: int) -> dict[str, Any]:
    directory = run_dir / "U3"
    directory.mkdir(parents=True, exist_ok=True)
    project = f"gopulse-p1803-{acceptance_token(run_number, 'project')}-r{run_number}"
    if not PROJECT_RE.fullmatch(project):
        raise ValueError("unsafe acceptance project name")
    env_file, values = candidate_environment(directory, binding, run_number)
    matrix: dict[str, Any] = {"project": project, "env_file": env_file.name, "scenarios": [], "started": False}
    compose = lambda *args: record_command(directory, f"compose-{len(list(directory.glob('compose-*.stdout.log')))+1}", compose_command(project, env_file, *args), timeout=1800, redact_values=values)
    started = False
    try:
        build = compose("--profile", "acceptance", "build", "backend", "business-worker", "search-indexer", "admin-frontend", "frontend", "acceptance")
        matrix["build"] = build
        if build["exit_code"] != 0:
            matrix.update(status="execution_failed", reason="image build failed")
            return matrix
        up = compose(
            "up",
            "-d",
            "--wait",
            "--wait-timeout",
            "300",
            "frontend",
            "backend",
            "backend-2",
            "business-worker",
            "business-worker-2",
            "search-indexer",
            "search-indexer-2",
        )
        matrix["up"] = up
        started = up["exit_code"] == 0
        matrix["started"] = started
        if not started:
            matrix.update(status="execution_failed", reason="multi-replica Compose startup failed")
            return matrix
        for service in ("backend", "backend-2", "business-worker", "business-worker-2", "search-indexer", "search-indexer-2"):
            matrix.setdefault("health", []).append(wait_healthy(project, env_file, service, directory, values))
        if not all(item["status"] == "healthy" for item in matrix["health"]):
            matrix.update(status="execution_failed", reason="replica health did not converge")
            return matrix
        for label, scenario, stopped, token_label, initialize_accounts in matrix_scenarios():
            if initialize_accounts:
                setup = acceptance(
                    directory,
                    project,
                    env_file,
                    values,
                    f"{label}_account_init",
                    "business",
                    token_label,
                )
                matrix["scenarios"].append({"name": f"{label}_account_init", "acceptance": setup})
                if setup["exit_code"] != 0:
                    matrix.update(status="boundary_found", reason=f"acceptance account initialization failed: {label}")
                    return matrix
            if stopped:
                stop = compose("stop", stopped)
                matrix["scenarios"].append({"name": label + "_stop", "action": stop})
                if stop["exit_code"] != 0:
                    matrix.update(status="execution_failed", reason=f"could not stop {stopped}")
                    return matrix
            result = acceptance(directory, project, env_file, values, label, scenario, token_label)
            matrix["scenarios"].append({"name": label, "acceptance": result})
            if stopped:
                start = compose("start", stopped)
                matrix["scenarios"].append({"name": label + "_start", "action": start})
                if start["exit_code"] != 0:
                    matrix.update(status="execution_failed", reason=f"could not start {stopped}")
                    return matrix
                if wait_healthy(project, env_file, stopped, directory, values)["status"] != "healthy":
                    matrix.update(status="execution_failed", reason=f"{stopped} did not recover")
                    return matrix
            if result["exit_code"] != 0:
                matrix.update(status="boundary_found", reason=f"acceptance scenario failed: {label}")
                return matrix
        matrix["closure"] = closure_snapshot(directory, project, env_file, values)
        status = "target_met" if matrix["closure"]["target_met"] else "boundary_found"
        matrix["status"] = status
        return matrix
    finally:
        if started:
            cleanup = compose("down", "--volumes", "--remove-orphans")
            matrix["cleanup"] = cleanup
            if cleanup["exit_code"] != 0 and matrix.get("status") == "target_met":
                matrix["status"] = "execution_failed"


def run_u1(run_dir: Path) -> dict[str, Any]:
    directory = run_dir / "U1"
    commands = [
        ("backend-go-test", ROOT / "backend", ["go", "test", "-count=1", "./internal/config", "./internal/platform", "./internal/outbox", "./internal/alert", "./internal/worker", "./cmd/server"]),
        ("componentmetrics-go-test", ROOT / "componentmetrics", ["go", "test", "-count=1", "./..."]),
        ("collector-go-test", ROOT / "monitor", ["go", "test", "-count=1", "./internal/metrics/collector"]),
        ("backend-logquery-go-test", ROOT / "backend", ["go", "test", "-count=1", "./internal/logquery"]),
        ("marshaller-logs-go-test", ROOT / "marshaller", ["go", "test", "-count=1", "./internal/logs"]),
        ("marshaller-elasticsearch-go-test", ROOT / "marshaller", ["go", "test", "-count=1", "./internal/elasticsearch"]),
    ]
    results = [record_command(directory, name, args, cwd=cwd, timeout=600) for name, cwd, args in commands]
    return {"commands": results, "passed": all(item["exit_code"] == 0 for item in results)}


def run_u2(run_dir: Path) -> dict[str, Any]:
    directory = run_dir / "U2"
    result = record_command(
        directory,
        "runner-self-test",
        ["python3", "-m", "unittest", "discover", "-s", "scripts/ci", "-p", "test_phase18_business_scale.py"],
        timeout=300,
    )
    return {"commands": [result], "passed": result["exit_code"] == 0}


def run_u4(run_dir: Path, binding: dict[str, Any]) -> dict[str, Any]:
    directory = run_dir / "U4"
    commands = [
        ("runtime-contracts", ["python3", "scripts/ci/verify_runtime_contracts.py", "--contract", "deploy/runtime-contracts.json", "--compose", "deploy/compose.yaml", "--env", ".env.example"]),
        ("versions", ["python3", "scripts/ci/validate_versions.py"]),
        ("diff-check", ["git", "diff", "--check"]),
    ]
    results = [record_command(directory, name, args, timeout=600) for name, args in commands]
    branch_ok = binding["branch"] == "develop/2.0.3"
    write_json_once(directory / "branch.json", {"branch": binding["branch"], "expected": "develop/2.0.3", "passed": branch_ok})
    return {"commands": results, "branch": branch_ok, "passed": branch_ok and all(item["exit_code"] == 0 for item in results)}


def run_one(root: Path, binding: dict[str, Any], run_number: int) -> dict[str, Any]:
    run_dir = root / f"run-{run_number}"
    if run_dir.exists():
        raise ValueError(f"refusing to overwrite {run_dir}")
    run_dir.mkdir(mode=0o700)
    write_json_once(run_dir / "binding.json", binding)
    units: dict[str, Any] = {}
    units["U1"] = run_u1(run_dir)
    units["U2"] = run_u2(run_dir)
    try:
        units["U3"] = run_matrix(run_dir, binding, run_number)
    except Exception as error:  # evidence must retain the failure and allow run-2
        write_text(run_dir / "U3" / "runner-error.log", str(error) + "\n")
        units["U3"] = {"status": "execution_failed", "reason": str(error)}
    units["U4"] = run_u4(run_dir, binding)
    passed = {name: bool(units[name].get("passed", units[name].get("status") == "target_met")) for name in UNIT_NAMES}
    status = "target_met" if all(passed.values()) else "boundary_found"
    if units["U3"].get("status") == "execution_failed":
        status = "execution_failed"
    summary = {"run": run_number, "units": units, "passed": passed, "status": status}
    write_json_once(run_dir / "summary.json", summary)
    return summary


def summarize(root: Path, binding: dict[str, Any], runs: list[dict[str, Any]]) -> dict[str, Any]:
    pass_counts = {name: sum(1 for run in runs if run["passed"].get(name)) for name in UNIT_NAMES}
    u3_statuses = [run["units"]["U3"].get("status") for run in runs]
    durations: dict[str, list[float]] = {}
    for run in runs:
        scenarios = run["units"]["U3"].get("scenarios", [])
        for scenario in scenarios:
            result = scenario.get("acceptance") or scenario.get("action")
            if result and isinstance(result.get("exit_code"), int):
                durations.setdefault(scenario["name"], []).append(float(result.get("elapsed_seconds", 0)))
    averages = {name: average(values) for name, values in durations.items() if values}
    if all(run["status"] == "target_met" for run in runs):
        result = "target_met"
    elif any(status == "execution_failed" for status in u3_statuses):
        result = "execution_failed"
    else:
        result = "boundary_found"
    summary = {
        "schema": "gopulse.phase18.business-scale-summary.v1",
        "candidate": binding,
        "runs": 2,
        "unit_pass_counts": pass_counts,
        "u3_statuses": u3_statuses,
        "averages": averages,
        "result": result,
        "run_summaries": [f"run-{run['run']}/summary.json" for run in runs],
    }
    write_json_once(root / "summary.json", summary)
    return summary


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repetitions", type=int, required=True)
    args = parser.parse_args(argv)
    validate_repetitions(args.repetitions)
    binding = candidate_binding()
    evidence_root = ROOT / ".run" / f"phase18-business-scale-{binding['version']}-{binding['revision'][:12]}-{secrets.token_hex(6)}"
    evidence_root.mkdir(parents=True, mode=0o700)
    validate_candidate_binding(evidence_root / "binding.json", binding)
    runs = [run_one(evidence_root, binding, number) for number in (1, 2)]
    summary = summarize(evidence_root, binding, runs)
    print(json.dumps({"evidence": str(evidence_root.relative_to(ROOT)), "result": summary["result"], "runs": 2}))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"phase18 business scale runner failed before evidence completion: {error}", file=sys.stderr)
        raise SystemExit(1)
