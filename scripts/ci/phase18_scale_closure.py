#!/usr/bin/env python3
"""Run the fixed two-run Phase 18-05 scale, diagnosis and fault closure."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any, Iterable

ROOT = Path(__file__).resolve().parents[2]
TARGET_VERSION = "2.0.5"
EXPECTED_REPETITIONS = 2
BRANCH_RE = re.compile(r"^develop/2\.0\.5$")
PROJECT_RE = re.compile(r"^gopulse-p1805-[0-9a-f]{12}-r[12]$")
UNIT_NAMES = ("U1", "U2", "U3", "U4")
PROCESS_SERVICES = (
    "backend",
    "backend-2",
    "business-worker",
    "business-worker-2",
    "search-indexer",
    "search-indexer-2",
    "router",
    "router-2",
    "marshaller",
    "marshaller-2",
    "monitor",
    "redis-exporter",
)
BUSINESS_SERVICES = (
    "frontend",
    "backend",
    "backend-2",
    "business-worker",
    "business-worker-2",
    "search-indexer",
    "search-indexer-2",
)
OBSERVABILITY_SERVICES = (
    "frontend",
    "backend",
    "backend-2",
    "router",
    "router-2",
    "marshaller",
    "marshaller-2",
    "monitor",
)
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
    "GOPULSE_OBSERVABILITY_PASSWORD",
}
ALLOWED_FILES = (
    "deploy/runtime-contracts.json",
    "deploy/runtime-contracts.schema.json",
    "deploy/compose.yaml",
    "docs/runtime-contracts.md",
    "componentmetrics/catalog.go",
    "componentmetrics/config.go",
    "componentmetrics/cmd/catalog/main.go",
    "scripts/ci/verify_runtime_contracts.py",
    "scripts/ci/test_runtime_contracts.py",
    "scripts/verify-runtime-contracts.sh",
    "scripts/verify-phase18-scale-closure.sh",
    "scripts/ci/phase18_scale_closure.py",
    "scripts/ci/phase18_scale_evidence.py",
    "scripts/ci/test_phase18_scale_closure.py",
    "scripts/ci/test_phase18_scale_evidence.py",
    "scripts/verify-phase18-evidence.py",
    "dev/phases/Plan.md",
    "dev/phases/README.md",
    "dev/phases/Phase-18-高并发与可观测架构收敛.md",
    "dev/logs/Phase-18/Phase-18-05-合同单一来源独立诊断与完整矩阵收口.md",
    "README.md",
    "VERSION",
    ".env.example",
    "frontend/package.json",
    "frontend/package-lock.json",
    "admin-frontend/package.json",
    "admin-frontend/package-lock.json",
)
MATRIX_ORDER = (
    "normal-concurrency",
    "business-scale-up",
    "business-scale-down",
    "observability-scale-up",
    "observability-scale-down",
    "rabbitmq-short-fault",
    "kafka-short-fault",
    "business-search-es-fault",
    "observability-es-fault",
    "victoriametrics-fault",
    "single-instance-sigterm",
    "service-rebuild",
    "terminal-closure",
)
COMPONENT_METRICS_PATH = "/internal/v1/metrics"


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


def write_text_once(path: Path, value: str) -> None:
    if path.exists():
        raise ValueError(f"refusing to overwrite evidence: {path}")
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
        raise ValueError("Phase 18-05 formal mode requires exactly --repetitions 2")
    return value


def final_result(statuses: Iterable[str]) -> str:
    values = list(statuses)
    if values and all(status == "target_met" for status in values):
        return "target_met"
    if any(status == "execution_failed" for status in values):
        return "execution_failed"
    return "boundary_found"


def redact(text: str, values: dict[str, str] | None = None) -> str:
    if not values:
        return text
    for value in sorted({item for key, item in values.items() if key in SECRET_KEYS and item}, key=len, reverse=True):
        text = text.replace(value, "<redacted>")
    return text


def command_result(
    args: list[str], *, cwd: Path = ROOT, timeout: int = 300, env: dict[str, str] | None = None
) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(args, cwd=cwd, env=env, text=True, errors="replace", capture_output=True, timeout=timeout)
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
    safe_name = re.sub(r"[^A-Za-z0-9_.-]+", "_", name)
    started = time.monotonic()
    result = command_result(args, cwd=cwd, timeout=timeout, env=env)
    stdout = redact(result.stdout, redact_values)
    stderr = redact(result.stderr, redact_values)
    write_text_once(directory / f"{safe_name}.stdout.log", stdout)
    write_text_once(directory / f"{safe_name}.stderr.log", stderr)
    item: dict[str, Any] = {
        "name": safe_name,
        "command": args,
        "cwd": str(cwd.relative_to(ROOT)) if cwd.is_relative_to(ROOT) else str(cwd),
        "exit_code": result.returncode,
        "elapsed_seconds": round(time.monotonic() - started, 3),
        "stdout": f"{safe_name}.stdout.log",
        "stderr": f"{safe_name}.stderr.log",
    }
    if result.returncode != 0:
        item["failure"] = {
            "stage": safe_name,
            "exit_code": result.returncode,
            "stdout": item["stdout"],
            "stderr": item["stderr"],
        }
    return item


def parse_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#") or "=" not in line:
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


def changed_files() -> list[str]:
    result = command_result(["git", "diff", "--name-only", "origin/main...HEAD"])
    if result.returncode != 0:
        raise RuntimeError("could not resolve candidate file ledger")
    return sorted(path for path in result.stdout.splitlines() if path)


def working_tree_digest() -> str:
    status = command_result(["git", "status", "--porcelain=v1", "--untracked-files=all"])
    if status.returncode != 0:
        raise RuntimeError("could not inspect candidate worktree")
    if status.stdout.strip():
        raise ValueError("formal candidate must have a clean worktree")
    return sha256_bytes(b"clean-worktree")


def candidate_binding() -> dict[str, Any]:
    version = (ROOT / "VERSION").read_text(encoding="utf-8").strip()
    branch = command_result(["git", "branch", "--show-current"]).stdout.strip()
    revision = candidate_revision()
    if version != TARGET_VERSION:
        raise ValueError(f"Phase 18-05 requires VERSION={TARGET_VERSION}")
    if not BRANCH_RE.fullmatch(branch):
        raise ValueError("Phase 18-05 runner must execute on develop/2.0.5")
    files = changed_files()
    unexpected = sorted(set(files) - set(ALLOWED_FILES))
    if unexpected:
        raise ValueError("candidate changes outside Phase-18-05 file ledger: " + ", ".join(unexpected))
    manifest = {
        "version": version,
        "revision": revision,
        "files": files,
        "runtime_contract": sha256_file(ROOT / "deploy/runtime-contracts.json"),
        "compose": sha256_file(ROOT / "deploy/compose.yaml"),
    }
    return {
        "version": version,
        "revision": revision,
        "branch": branch,
        "working_tree_sha256": working_tree_digest(),
        "manifest_sha256": sha256_bytes(json.dumps(manifest, sort_keys=True).encode()),
        "runtime_contract_sha256": manifest["runtime_contract"],
        "runtime_schema_sha256": sha256_file(ROOT / "deploy/runtime-contracts.schema.json"),
        "compose_sha256": manifest["compose"],
        "runner_sha256": sha256_file(ROOT / "scripts/ci/phase18_scale_closure.py"),
        "evidence_validator_sha256": sha256_file(ROOT / "scripts/ci/phase18_scale_evidence.py"),
        "candidate_files": files,
        "file_ledger": [
            {"path": path, "status": "changed" if path in files else "not_needed"}
            for path in ALLOWED_FILES
        ],
        "condition": {
            "runner": "scripts/verify-phase18-scale-closure.sh --repetitions 2",
            "replicas": {
                "backend": 2,
                "business-worker": 2,
                "search-indexer": 2,
                "router": 2,
                "marshaller": 2,
                "monitor": 1,
            },
            "kafka_partitions": 4,
            "diagnostics": "direct-private-probe",
            "matrix_order": list(MATRIX_ORDER),
            "fault_domains": [
                "rabbitmq",
                "kafka",
                "elasticsearch",
                "observability-elasticsearch",
                "victoriametrics",
            ],
            "host_port_policy": "no-new-host-ports",
        },
    }


def validate_candidate_binding(path: Path, candidate: dict[str, Any]) -> dict[str, Any]:
    expected = {"schema": "gopulse.phase18.scale-closure-binding.v1", "candidate": candidate}
    if path.exists():
        actual = json.loads(path.read_text(encoding="utf-8"))
        if actual != expected:
            raise ValueError("candidate binding belongs to another candidate")
        return actual
    write_json_once(path, expected)
    return expected


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


def acceptance_token(binding: dict[str, Any], label: str) -> str:
    return hashlib.sha256(f"phase18-05-{binding['revision']}-{label}".encode()).hexdigest()[:12]


def candidate_environment(temp_root: Path, binding: dict[str, Any], run_number: int) -> tuple[Path, dict[str, str]]:
    values = parse_env(ROOT / ".env.example")
    token = acceptance_token(binding, f"compose-{run_number}")
    values.update(
        {
            "APP_ENV": "test",
            "PUBLISHED_HOST": "127.0.0.1",
            "FRONTEND_PORT": str(free_port()),
            "GOPULSE_VERSION": binding["version"],
            "GOPULSE_REVISION": binding["revision"],
            "GOPULSE_IMAGE_TAG": f"phase18-05-{binding['revision'][:12]}",
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
            "GOPULSE_OBSERVABILITY_ADMIN_USERNAME": f"admin_{token}",
            "GOPULSE_OBSERVABILITY_USER_USERNAME": f"user_{token}",
            "GOPULSE_OBSERVABILITY_PASSWORD": f"Acceptance-{token}-password",
            "GOPULSE_ACCEPTANCE_TOKEN": token,
            "KAFKA_OBSERVABILITY_PARTITIONS": "4",
            "ROUTER_KAFKA_MIN_PARTITIONS": "4",
            "MARSHALLER_KAFKA_MIN_PARTITIONS": "4",
            "BACKEND_ENDPOINTS": "backend,backend-2",
            "BUSINESS_WORKER_ENDPOINTS": "business-worker,business-worker-2",
            "SEARCH_INDEXER_ENDPOINTS": "search-indexer,search-indexer-2",
            "ROUTER_ENDPOINTS": "router,router-2",
            "MARSHALLER_ENDPOINTS": "marshaller,marshaller-2",
        }
    )
    path = temp_root / "compose.env"
    write_env(path, values)
    return path, values


def wait_healthy(
    project: str,
    env_file: Path,
    service: str,
    directory: Path,
    values: dict[str, str],
    label: str,
    timeout: int = 180,
) -> dict[str, Any]:
    deadline = time.monotonic() + timeout
    last = ""
    while time.monotonic() < deadline:
        listed = command_result(compose_command(project, env_file, "ps", "-q", service), timeout=30)
        container = listed.stdout.strip().splitlines()[0] if listed.stdout.strip() else ""
        if container:
            state = command_result(
                [
                    "docker",
                    "inspect",
                    "--format",
                    "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}",
                    container,
                ],
                timeout=30,
            )
            last = state.stdout.strip() or state.stderr.strip()
            if state.returncode == 0 and last == "healthy":
                return {"service": service, "status": "healthy", "container": container}
        time.sleep(2)
    write_text_once(directory / f"{label}-health.log", redact(last, values))
    return {"service": service, "status": "timeout", "last": f"{label}-health.log"}


def run_acceptance(
    directory: Path,
    project: str,
    env_file: Path,
    values: dict[str, str],
    name: str,
    scenario: str,
    spec: str,
) -> dict[str, Any]:
    scenario_token = hashlib.sha256(
        f"{values.get('GOPULSE_ACCEPTANCE_TOKEN', 'compose')}-{name}".encode()
    ).hexdigest()[:12]
    args = compose_command(
        project,
        env_file,
        "--profile",
        "acceptance",
        "run",
        "--rm",
        "--no-deps",
        "-e",
        f"GOPULSE_ACCEPTANCE_SCENARIO={scenario}",
        "-e",
        f"GOPULSE_ACCEPTANCE_TOKEN={scenario_token}",
        "acceptance",
        spec,
    )
    return record_command(directory, name, args, timeout=900, redact_values=values)


def capacity_metrics_path(component: dict[str, Any]) -> str:
    listeners = {listener["name"] for listener in component["listeners"]}
    return COMPONENT_METRICS_PATH if "metrics" in listeners else "/metrics"


def _diagnostic_services(component: dict[str, Any], selected: set[str] | None) -> list[tuple[int, str]]:
    return [
        (index, service)
        for index, service in enumerate(component["compose_services"])
        if selected is None or service in selected
    ]


def _contract() -> dict[str, Any]:
    return json.loads((ROOT / "deploy/runtime-contracts.json").read_text(encoding="utf-8"))


def direct_diagnostics(
    directory: Path,
    project: str,
    env_file: Path,
    values: dict[str, str],
    phase: str = "initial",
    services: set[str] | None = None,
) -> dict[str, Any]:
    contract = _contract()
    records: list[dict[str, Any]] = []
    for component in contract["components"]:
        listeners = {listener["name"]: listener["port"] for listener in component["listeners"]}
        probe_port = listeners[component["diagnostic"]["probe_listener"]]
        metrics_port = listeners.get("metrics")
        metrics_path = capacity_metrics_path(component)
        token_key = component["id"].upper().replace("-", "_") + "_METRICS_TOKEN"
        for index, service in _diagnostic_services(component, services):
            expected_identity = component["replica"]["instances"][index]
            body = [
                "set -eu",
                (
                    'identity="${GOPULSE_INSTANCE_ID:-' + expected_identity + '}"'
                    if component["replica"]["identity_source"] == "componentmetrics-fallback"
                    else 'identity="${GOPULSE_INSTANCE_ID:-}"'
                ),
                'printf "identity=%s\\n" "$identity"',
            ]
            for path in component["diagnostic"]["paths"]:
                body.append(f"wget --quiet --output-document=- http://127.0.0.1:{probe_port}{path} >/dev/null")
                body.append(f'printf "probe={path}\\n"')
            if metrics_port is not None:
                body.append(
                    f'wget --quiet --header "Authorization: Bearer ${token_key}" --output-document=- '
                    f"http://127.0.0.1:{metrics_port}{metrics_path} | grep -E 'gopulse_' >/dev/null"
                )
                body.append('printf "capacity=metrics\\n"')
            elif component["role"] == "plugin-exporter":
                body.append(
                    f"wget --quiet --output-document=- http://127.0.0.1:{probe_port}/metrics "
                    "| grep -E 'gopulse_' >/dev/null"
                )
                body.append('printf "capacity=metrics\\n"')
            body.append(f'test "$identity" = "{expected_identity}"')
            result = record_command(
                directory,
                f"diagnostic-{phase}-{component['id']}-{service}",
                compose_command(project, env_file, "exec", "-T", service, "sh", "-ec", "; ".join(body)),
                timeout=60,
                redact_values=values,
            )
            records.append(
                {
                    "process_id": component["process_id"],
                    "service": service,
                    "expected_identity": expected_identity,
                    "independent": component["diagnostic"]["independent"],
                    "probe_paths": component["diagnostic"]["paths"],
                    "capacity_signals": component["diagnostic"]["capacity_signals"],
                    "command": result,
                    "status": "target_met" if result["exit_code"] == 0 else "boundary_found",
                }
            )
        if not component["compose_services"] and services is None:
            records.append(
                {
                    "process_id": component["process_id"],
                    "service": None,
                    "expected_identity": component["replica"]["instances"][0],
                    "independent": component["diagnostic"]["independent"],
                    "probe_paths": component["diagnostic"]["paths"],
                    "capacity_signals": component["diagnostic"]["capacity_signals"],
                    "status": "not_started",
                    "reason": "managed plugin process is started by Monitor only when installed",
                }
            )
    checked = sum(item["status"] == "target_met" for item in records)
    missing = sum(item["status"] == "not_started" for item in records)
    failed = sum(item["status"] == "boundary_found" for item in records)
    return {"records": records, "checked": checked, "missing": missing, "failed": failed, "total": len(records)}


def _status_from_commands(commands: Iterable[dict[str, Any]], *, boundary_ok: bool = False) -> str:
    values = list(commands)
    if all(item.get("exit_code") == 0 for item in values) and boundary_ok:
        return "target_met"
    if any(item.get("exit_code") in (124, 127) for item in values):
        return "execution_failed"
    return "boundary_found"


def _record_action(
    matrix: dict[str, Any],
    directory: Path,
    project: str,
    env_file: Path,
    values: dict[str, str],
    name: str,
    *args: str,
    timeout: int = 900,
) -> dict[str, Any]:
    result = record_command(directory, name, compose_command(project, env_file, *args), timeout=timeout, redact_values=values)
    matrix.setdefault("commands", []).append(result)
    return result


def _append_acceptance(
    matrix: dict[str, Any],
    directory: Path,
    project: str,
    env_file: Path,
    values: dict[str, str],
    name: str,
    scenario: str,
    spec: str,
) -> dict[str, Any]:
    result = run_acceptance(directory, project, env_file, values, name, scenario, spec)
    matrix.setdefault("acceptance", []).append(result)
    return result


def bootstrap_admin(
    matrix: dict[str, Any],
    directory: Path,
    project: str,
    env_file: Path,
    values: dict[str, str],
) -> tuple[dict[str, Any], dict[str, Any] | None]:
    probe = _record_action(
        matrix,
        directory,
        project,
        env_file,
        values,
        "bootstrap-user-id",
        "exec",
        "-T",
        "-e",
        f"BOOTSTRAP_USERNAME={values['GOPULSE_OBSERVABILITY_ADMIN_USERNAME']}",
        "mysql",
        "sh",
        "-ec",
        'MYSQL_PWD="$MYSQL_PASSWORD" mysql --user="$MYSQL_USER" --batch --skip-column-names "$MYSQL_DATABASE" --execute "SELECT id FROM users WHERE username=\'${BOOTSTRAP_USERNAME}\'"',
        timeout=60,
    )
    probe_path = directory / f"{probe['name']}.stdout.log"
    user_id = probe_path.read_text(encoding="utf-8").strip() if probe_path.exists() else ""
    if probe["exit_code"] != 0 or not re.fullmatch(r"[1-9][0-9]*", user_id):
        return probe, None
    promoted = _record_action(
        matrix,
        directory,
        project,
        env_file,
        values,
        "bootstrap-admin-role",
        "--profile",
        "operations",
        "run",
        "--rm",
        "--no-deps",
        "admin-role",
        "bootstrap",
        "--user-id",
        user_id,
        timeout=120,
    )
    return probe, promoted


def run_matrix(run_dir: Path, binding: dict[str, Any], run_number: int) -> dict[str, Any]:
    directory = run_dir / "U3"
    directory.mkdir(parents=True, exist_ok=True)
    project = f"gopulse-p1805-{acceptance_token(binding, f'project-{run_number}')}-r{run_number}"
    if not PROJECT_RE.fullmatch(project):
        raise ValueError("unsafe acceptance project name")
    temp_root = Path(tempfile.mkdtemp(prefix=f"gopulse-p1805-{run_number}-"))
    env_file, values = candidate_environment(temp_root, binding, run_number)
    matrix: dict[str, Any] = {
        "run": run_number,
        "project": project,
        "env_sha256": sha256_file(env_file),
        "condition": binding["condition"],
        "order": list(MATRIX_ORDER),
        "steps": [],
        "commands": [],
        "acceptance": [],
        "diagnostics": [],
        "started": False,
        "status": "execution_failed",
        "failure_stage": None,
    }
    touched = False

    def step(name: str, status: str, **details: Any) -> None:
        matrix["steps"].append({"name": name, "status": status, **details})

    try:
        build = _record_action(
            matrix,
            directory,
            project,
            env_file,
            values,
            "matrix-build",
            "build",
            "backend",
            "business-worker",
            "search-indexer",
            "frontend",
            "router",
            "marshaller",
            "monitor",
            "redis-exporter",
            "acceptance",
            timeout=1800,
        )
        step("candidate-build", _status_from_commands([build], boundary_ok=build["exit_code"] == 0), command=build)
        touched = True
        if build["exit_code"] != 0:
            matrix["failure_stage"] = "candidate-build"
            return matrix
        up = _record_action(
            matrix,
            directory,
            project,
            env_file,
            values,
            "matrix-up",
            "up",
            "-d",
            "--wait",
            "--wait-timeout",
            "420",
            *OBSERVABILITY_SERVICES,
            *[service for service in BUSINESS_SERVICES if service not in OBSERVABILITY_SERVICES],
            "redis-exporter",
            timeout=1800,
        )
        matrix["started"] = up["exit_code"] == 0
        touched = True
        step("initial-start", _status_from_commands([up], boundary_ok=up["exit_code"] == 0), command=up)
        if up["exit_code"] != 0:
            matrix["failure_stage"] = "initial-start"
            return matrix

        health = [wait_healthy(project, env_file, service, directory, values, f"health-{service}") for service in PROCESS_SERVICES]
        matrix["health"] = health
        health_ok = all(item["status"] == "healthy" for item in health)
        step("initial-health", "target_met" if health_ok else "execution_failed", checks=health)
        if not health_ok:
            matrix["failure_stage"] = "initial-health"

        diagnostics = direct_diagnostics(directory, project, env_file, values, "initial")
        matrix["diagnostics"].append({"phase": "initial", **diagnostics})
        step("normal-concurrency", "target_met" if diagnostics["failed"] == 0 and diagnostics["missing"] == 0 else "boundary_found", diagnostics=diagnostics)
        normal_business = _append_acceptance(matrix, directory, project, env_file, values, "acceptance-normal-business", "business", "e2e/compose-business.spec.ts")
        setup = _append_acceptance(matrix, directory, project, env_file, values, "acceptance-observability-setup", "setup", "e2e/compose-observability.spec.ts")
        bootstrap_probe, bootstrap_promoted = bootstrap_admin(matrix, directory, project, env_file, values)
        normal_observability = _append_acceptance(matrix, directory, project, env_file, values, "acceptance-normal-observability", "admin", "e2e/compose-observability.spec.ts")
        setup_commands = [setup, bootstrap_probe, bootstrap_promoted] if bootstrap_promoted is not None else [setup, bootstrap_probe]
        setup_ok = all(item["exit_code"] == 0 for item in setup_commands) and bootstrap_promoted is not None
        step(
            "observability-setup",
            "target_met" if setup_ok else ("execution_failed" if any(item["exit_code"] in (124, 127) for item in setup_commands) else "boundary_found"),
            setup=setup,
            bootstrap_probe=bootstrap_probe,
            bootstrap_promoted=bootstrap_promoted,
        )
        acceptance_commands = [normal_business, setup, bootstrap_probe, normal_observability]
        if bootstrap_promoted is not None:
            acceptance_commands.append(bootstrap_promoted)
        if normal_business["exit_code"] != 0 or normal_observability["exit_code"] != 0:
            step("normal-acceptance", "execution_failed" if any(item["exit_code"] in (124, 127) for item in acceptance_commands) else "boundary_found", business=normal_business, observability=normal_observability)
        else:
            step("normal-acceptance", "target_met", business=normal_business, observability=normal_observability)

        business_up = _record_action(matrix, directory, project, env_file, values, "business-scale-up", "up", "-d", *BUSINESS_SERVICES)
        business_diag = direct_diagnostics(directory, project, env_file, values, "business-scale-up")
        matrix["diagnostics"].append({"phase": "business-scale-up", **business_diag})
        step("business-scale-up", "target_met" if business_up["exit_code"] == 0 and business_diag["failed"] == 0 else "boundary_found", command=business_up, diagnostics=business_diag)
        business_stop = _record_action(matrix, directory, project, env_file, values, "business-scale-down", "stop", "backend-2", "business-worker-2", "search-indexer-2", timeout=180)
        remaining_services = set(BUSINESS_SERVICES) - {"backend-2", "business-worker-2", "search-indexer-2"}
        remaining_diag = direct_diagnostics(directory, project, env_file, values, "business-scale-down", remaining_services)
        matrix["diagnostics"].append({"phase": "business-scale-down", **remaining_diag})
        business_start = _record_action(matrix, directory, project, env_file, values, "business-scale-down-recover", "start", "backend-2", "business-worker-2", "search-indexer-2", timeout=180)
        step("business-scale-down", "target_met" if business_stop["exit_code"] == 0 and business_start["exit_code"] == 0 and remaining_diag["failed"] == 0 and remaining_diag["missing"] == 0 else "boundary_found", stop=business_stop, start=business_start, diagnostics=remaining_diag)

        observability_up = _record_action(matrix, directory, project, env_file, values, "observability-scale-up", "up", "-d", *OBSERVABILITY_SERVICES)
        observability_diag = direct_diagnostics(directory, project, env_file, values, "observability-scale-up")
        matrix["diagnostics"].append({"phase": "observability-scale-up", **observability_diag})
        step("observability-scale-up", "target_met" if observability_up["exit_code"] == 0 and observability_diag["failed"] == 0 else "boundary_found", command=observability_up, diagnostics=observability_diag)
        observability_stop = _record_action(matrix, directory, project, env_file, values, "observability-scale-down", "stop", "router-2", "marshaller-2", timeout=180)
        observability_start = _record_action(matrix, directory, project, env_file, values, "observability-scale-down-recover", "start", "router-2", "marshaller-2", timeout=180)
        step("observability-scale-down", "target_met" if observability_stop["exit_code"] == 0 and observability_start["exit_code"] == 0 else "boundary_found", stop=observability_stop, start=observability_start)

        for name, service, scenario, spec in (
            ("rabbitmq-short-fault", "rabbitmq", "business", "e2e/compose-business.spec.ts"),
            ("kafka-short-fault", "kafka", "transport-down", "e2e/compose-observability.spec.ts"),
            ("business-search-es-fault", "elasticsearch", "business", "e2e/compose-business.spec.ts"),
            ("observability-es-fault", "observability-elasticsearch", "transport-down", "e2e/compose-observability.spec.ts"),
            ("victoriametrics-fault", "victoriametrics", "vm-down", "e2e/compose-observability.spec.ts"),
        ):
            stop = _record_action(matrix, directory, project, env_file, values, f"{name}-stop", "stop", service, timeout=180)
            acceptance = _append_acceptance(matrix, directory, project, env_file, values, f"acceptance-{name}", scenario, spec)
            start = _record_action(matrix, directory, project, env_file, values, f"{name}-start", "start", service, timeout=180)
            recovered = wait_healthy(project, env_file, service, directory, values, f"health-{name}", timeout=180)
            okay = stop["exit_code"] == 0 and start["exit_code"] == 0 and recovered["status"] == "healthy"
            step(name, "target_met" if okay and acceptance["exit_code"] == 0 else ("execution_failed" if any(item["exit_code"] in (124, 127) for item in (stop, acceptance, start)) else "boundary_found"), stop=stop, acceptance=acceptance, start=start, recovery=recovered)

        term = _record_action(matrix, directory, project, env_file, values, "single-instance-sigterm", "kill", "--signal", "SIGTERM", "backend-2", timeout=120)
        restart = _record_action(matrix, directory, project, env_file, values, "single-instance-sigterm-restart", "up", "-d", "--force-recreate", "--no-deps", "backend-2", timeout=180)
        recovery = wait_healthy(project, env_file, "backend-2", directory, values, "health-single-instance-sigterm", timeout=180)
        step("single-instance-sigterm", "target_met" if term["exit_code"] == 0 and restart["exit_code"] == 0 and recovery["status"] == "healthy" else "boundary_found", signal=term, restart=restart, recovery=recovery)

        rebuild = _record_action(matrix, directory, project, env_file, values, "service-rebuild", "up", "-d", "--build", "--force-recreate", "backend-2", "router-2", "marshaller-2", timeout=1800)
        rebuild_diag = direct_diagnostics(directory, project, env_file, values, "service-rebuild")
        matrix["diagnostics"].append({"phase": "service-rebuild", **rebuild_diag})
        step("service-rebuild", "target_met" if rebuild["exit_code"] == 0 and rebuild_diag["failed"] == 0 else "boundary_found", command=rebuild, diagnostics=rebuild_diag)

        closure_commands = [
            ("closure-compose-ps", ["ps", "--all"]),
            ("closure-outbox", ["exec", "-T", "mysql", "sh", "-ec", 'mysql --batch --skip-column-names --user="$MYSQL_USER" --password="$MYSQL_PASSWORD" "$MYSQL_DATABASE" -e "SELECT COUNT(*) FROM business_outbox WHERE status IN (\'pending\', \'leased\');"']),
            ("closure-rabbitmq", ["exec", "-T", "rabbitmq", "rabbitmqctl", "list_queues", "name", "messages", "messages_unacknowledged"]),
            ("closure-kafka-topic", ["exec", "-T", "kafka", "/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", "kafka:19092", "--describe", "--topic", "gopulse-observability-v1"]),
            ("closure-kafka-group", ["exec", "-T", "kafka", "/opt/kafka/bin/kafka-consumer-groups.sh", "--bootstrap-server", "kafka:19092", "--describe", "--group", "gopulse-marshaller-metrics-v1"]),
            ("closure-business-es", ["exec", "-T", "elasticsearch", "sh", "-ec", "curl --fail --silent http://127.0.0.1:9200/_cat/indices?format=json"]),
            ("closure-observability-es", ["exec", "-T", "observability-elasticsearch", "sh", "-ec", "curl --fail --silent http://127.0.0.1:9200/_cat/indices?format=json"]),
            ("closure-victoriametrics", ["exec", "-T", "victoriametrics", "wget", "--spider", "--quiet", "http://127.0.0.1:8428/health"]),
        ]
        closure = []
        for name, args in closure_commands:
            closure.append(_record_action(matrix, directory, project, env_file, values, name, *args, timeout=120))
        matrix["closure"] = closure
        terminal_diag = direct_diagnostics(directory, project, env_file, values, "terminal")
        matrix["diagnostics"].append({"phase": "terminal", **terminal_diag})
        step("terminal-closure", "target_met" if all(item["exit_code"] == 0 for item in closure) and terminal_diag["failed"] == 0 and terminal_diag["missing"] == 0 else "boundary_found", closure=closure, diagnostics=terminal_diag)
        failed_steps = [item for item in matrix["steps"] if item["status"] != "target_met"]
        execution_failure = any(
            item.get("status") == "execution_failed"
            or any(command.get("exit_code") in (124, 127) for command in item.get("commands", []) if isinstance(command, dict))
            for item in failed_steps
        )
        matrix["status"] = "execution_failed" if execution_failure else ("boundary_found" if failed_steps else "target_met")
        if failed_steps:
            matrix["failures"] = [item["name"] for item in failed_steps]
        matrix["numeric"] = {
            "elapsed_seconds": round(sum(float(item["elapsed_seconds"]) for item in matrix["commands"] + matrix["acceptance"]), 3),
            "diagnostic_total": sum(item["total"] for item in matrix["diagnostics"]),
            "diagnostic_checked": sum(item["checked"] for item in matrix["diagnostics"]),
        }
        return matrix
    except Exception as error:
        matrix["status"] = "execution_failed"
        matrix["failure_stage"] = matrix.get("failure_stage") or "runner"
        matrix["runner_error"] = str(error)
        write_text_once(directory / "runner-error.log", str(error) + "\n")
        return matrix
    finally:
        cleanup = record_command(
            directory,
            "cleanup",
            compose_command(project, env_file, "down", "--volumes", "--remove-orphans"),
            timeout=300,
            redact_values=values,
        )
        matrix["cleanup"] = cleanup
        if cleanup["exit_code"] != 0:
            matrix["status"] = "execution_failed"
            matrix["failure_stage"] = matrix.get("failure_stage") or "cleanup"
        write_json_once(directory / "matrix.json", matrix)
        shutil.rmtree(temp_root, ignore_errors=True)


def _unit_status(commands: list[dict[str, Any]]) -> str:
    if all(command["exit_code"] == 0 for command in commands):
        return "target_met"
    if any(command["exit_code"] in (124, 127) for command in commands):
        return "execution_failed"
    return "boundary_found"


def run_u1(run_dir: Path, binding: dict[str, Any]) -> dict[str, Any]:
    directory = run_dir / "U1"
    commands = [
        (
            "runtime-contract-verifier",
            [
                "python3",
                "scripts/ci/verify_runtime_contracts.py",
                "--contract",
                "deploy/runtime-contracts.json",
                "--compose",
                "deploy/compose.yaml",
                "--env",
                ".env.example",
                "--candidate",
                binding["version"],
            ],
        ),
        (
            "runtime-contract-negative-tests",
            ["python3", "-m", "unittest", "discover", "-s", "scripts/ci", "-p", "test_runtime_contracts.py"],
        ),
    ]
    results = [record_command(directory, name, args, timeout=300) for name, args in commands]
    status = _unit_status(results)
    return {"status": status, "passed": status == "target_met", "commands": results}


def run_u2(run_dir: Path) -> dict[str, Any]:
    directory = run_dir / "U2"
    results = [
        record_command(
            directory,
            "closure-self-tests",
            ["python3", "-m", "unittest", "discover", "-s", "scripts/ci", "-p", "test_phase18_scale_closure.py"],
            timeout=300,
        ),
        record_command(
            directory,
            "evidence-self-tests",
            ["python3", "-m", "unittest", "discover", "-s", "scripts/ci", "-p", "test_phase18_scale_evidence.py"],
            timeout=300,
        ),
    ]
    status = _unit_status(results)
    return {"status": status, "passed": status == "target_met", "commands": results}


def run_u4(run_dir: Path, binding: dict[str, Any]) -> dict[str, Any]:
    directory = run_dir / "U4"
    commands = [
        ("versions", ["python3", "scripts/ci/validate_versions.py"]),
        ("diff-check", ["git", "diff", "--check"]),
        ("branch", ["git", "branch", "--show-current"]),
    ]
    results = [record_command(directory, name, args, timeout=300) for name, args in commands]
    branch_ok = binding["branch"] == "develop/2.0.5"
    write_json_once(directory / "branch-version.json", {"branch": binding["branch"], "version": binding["version"], "passed": branch_ok and binding["version"] == TARGET_VERSION})
    status = _unit_status(results)
    if not branch_ok or binding["version"] != TARGET_VERSION:
        status = "boundary_found"
    return {"status": status, "passed": status == "target_met", "branch": branch_ok, "version": binding["version"], "commands": results}


def run_one(root: Path, binding: dict[str, Any], run_number: int) -> dict[str, Any]:
    run_dir = root / f"run-{run_number}"
    if run_dir.exists():
        raise ValueError(f"refusing to overwrite {run_dir}")
    run_dir.mkdir(mode=0o700)
    validate_candidate_binding(run_dir / "binding.json", binding)
    units: dict[str, Any] = {}
    for name, runner in (
        ("U1", lambda directory: run_u1(directory, binding)),
        ("U2", run_u2),
    ):
        try:
            units[name] = runner(run_dir)
        except Exception as error:
            write_text_once(run_dir / name / "runner-error.log", str(error) + "\n")
            units[name] = {"status": "execution_failed", "passed": False, "runner_error": str(error)}
    try:
        units["U3"] = run_matrix(run_dir, binding, run_number)
    except Exception as error:
        write_text_once(run_dir / "U3" / "runner-error.log", str(error) + "\n")
        units["U3"] = {"status": "execution_failed", "passed": False, "runner_error": str(error)}
    try:
        units["U4"] = run_u4(run_dir, binding)
    except Exception as error:
        write_text_once(run_dir / "U4" / "runner-error.log", str(error) + "\n")
        units["U4"] = {"status": "execution_failed", "passed": False, "runner_error": str(error)}
    statuses = [units[name].get("status", "execution_failed") for name in UNIT_NAMES]
    summary = {
        "schema": "gopulse.phase18.scale-closure-run.v1",
        "run": run_number,
        "candidate": binding,
        "units": units,
        "passed": {name: units[name].get("status") == "target_met" for name in UNIT_NAMES},
        "status": final_result(statuses),
    }
    write_json_once(run_dir / "summary.json", summary)
    return summary


def run_repetitions(root: Path, binding: dict[str, Any], repetitions: int) -> list[dict[str, Any]]:
    validate_repetitions(repetitions)
    return [run_one(root, binding, run_number) for run_number in (1, 2)]


def summarize(root: Path, binding: dict[str, Any], runs: list[dict[str, Any]]) -> dict[str, Any]:
    if len(runs) != EXPECTED_REPETITIONS or [run.get("run") for run in runs] != [1, 2]:
        raise ValueError("closure summary requires exactly run-1 and run-2")
    unit_pass_counts = {name: sum(run["passed"].get(name, False) for run in runs) for name in UNIT_NAMES}
    raw_numeric: dict[str, list[float]] = {}
    for run in runs:
        numeric = run.get("units", {}).get("U3", {}).get("numeric", {})
        for key, value in numeric.items():
            if isinstance(value, (int, float)) and not isinstance(value, bool):
                raw_numeric.setdefault(key, []).append(float(value))
    numeric_averages = {key: average(values) for key, values in raw_numeric.items() if len(values) == 2}
    cleanup = [
        {
            "run": run["run"],
            "exit_code": run.get("units", {}).get("U3", {}).get("cleanup", {}).get("exit_code"),
            "stdout": run.get("units", {}).get("U3", {}).get("cleanup", {}).get("stdout"),
            "stderr": run.get("units", {}).get("U3", {}).get("cleanup", {}).get("stderr"),
        }
        for run in runs
    ]
    summary = {
        "schema": "gopulse.phase18.scale-closure-summary.v1",
        "candidate": binding,
        "runs": 2,
        "run_statuses": [run["status"] for run in runs],
        "unit_pass_counts": unit_pass_counts,
        "deterministic_counts": {name: f"{unit_pass_counts[name]}/2" for name in UNIT_NAMES},
        "raw_numeric": raw_numeric,
        "numeric_averages": numeric_averages,
        "cleanup": cleanup,
        "failure_stages": [
            {
                "run": run["run"],
                "unit": name,
                "status": run["units"].get(name, {}).get("status"),
                "failure_stage": run["units"].get(name, {}).get("failure_stage")
                or run["units"].get(name, {}).get("runner_error"),
            }
            for run in runs
            for name in UNIT_NAMES
            if run["units"].get(name, {}).get("status") != "target_met"
        ],
        "file_ledger": binding["file_ledger"],
        "result": final_result(run["status"] for run in runs),
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
    evidence_root = ROOT / ".run" / f"phase18-scale-closure-{TARGET_VERSION}-{binding['revision'][:12]}-{secrets.token_hex(6)}"
    evidence_root.mkdir(parents=True, mode=0o700)
    validate_candidate_binding(evidence_root / "binding.json", binding)
    runs = run_repetitions(evidence_root, binding, args.repetitions)
    summary = summarize(evidence_root, binding, runs)
    print(json.dumps({"evidence": str(evidence_root.relative_to(ROOT)), "result": summary["result"], "runs": 2}, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"phase18 scale closure runner failed before evidence completion: {error}", file=sys.stderr)
        raise SystemExit(1)
