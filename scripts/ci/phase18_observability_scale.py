#!/usr/bin/env python3
"""Run the fixed two-round Phase 18-04 observability-scale acceptance."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
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
TARGET_VERSION = "2.0.4"
VERSION_RE = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
BRANCH_RE = re.compile(r"^develop/2\.0\.4$")
PROJECT_RE = re.compile(r"^gopulse-p1804-[0-9a-f]{12}-r[12]$")
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
    "GOPULSE_OBSERVABILITY_PASSWORD",
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
        raise ValueError("Phase 18-04 formal mode requires exactly --repetitions 2")
    return value


def validate_candidate_binding(path: Path, candidate: dict[str, Any]) -> dict[str, Any]:
    expected = {"schema": "gopulse.phase18.observability-scale-binding.v1", "candidate": candidate}
    if path.exists():
        actual = json.loads(path.read_text(encoding="utf-8"))
        if actual != expected:
            raise ValueError("candidate binding belongs to another candidate")
        return actual
    write_json_once(path, expected)
    return expected


def validate_es_separation(business_url: str, observability_url: str) -> None:
    business = business_url.rstrip("/")
    observability = observability_url.rstrip("/")
    if not business or not observability or business == observability:
        raise ValueError("business and observability Elasticsearch endpoints must be distinct")


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
    for value in sorted(
        {item for key, item in values.items() if key in SECRET_KEYS and item},
        key=len,
        reverse=True,
    ):
        text = text.replace(value, "<redacted>")
    return text


def command_result(
    args: list[str], *, cwd: Path = ROOT, timeout: int = 300, env: dict[str, str] | None = None
) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(
            args,
            cwd=cwd,
            env=env,
            text=True,
            errors="replace",
            capture_output=True,
            timeout=timeout,
        )
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
    started_at = time.monotonic()
    result = command_result(args, cwd=cwd, timeout=timeout, env=env)
    stdout = redact(result.stdout, redact_values)
    stderr = redact(result.stderr, redact_values)
    write_text_once(directory / f"{safe_name}.stdout.log", stdout)
    write_text_once(directory / f"{safe_name}.stderr.log", stderr)
    return {
        "name": safe_name,
        "command": args,
        "cwd": str(cwd.relative_to(ROOT)) if cwd.is_relative_to(ROOT) else str(cwd),
        "exit_code": result.returncode,
        "elapsed_seconds": round(time.monotonic() - started_at, 3),
        "stdout": f"{safe_name}.stdout.log",
        "stderr": f"{safe_name}.stderr.log",
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
    untracked: list[str] = []
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
    if version != TARGET_VERSION or not VERSION_RE.fullmatch(version):
        raise ValueError(f"Phase 18-04 requires VERSION={TARGET_VERSION}")
    if not BRANCH_RE.fullmatch(branch):
        raise ValueError("Phase 18-04 runner must execute on develop/2.0.4")
    validate_es_separation("http://elasticsearch:9200", "http://observability-elasticsearch:9200")
    return {
        "version": version,
        "revision": revision,
        "branch": branch,
        "working_tree_sha256": working_tree_digest(),
        "runtime_contract_sha256": sha256_file(ROOT / "deploy/runtime-contracts.json"),
        "compose_sha256": sha256_file(ROOT / "deploy/compose.yaml"),
        "condition": {
            "runner": "scripts/verify-phase18-observability-scale.sh --repetitions 2",
            "replicas": {"router": 2, "marshaller": 2},
            "kafka_partitions": 4,
            "elasticsearch": {
                "business": "http://elasticsearch:9200",
                "observability": "http://observability-elasticsearch:9200",
            },
            "faults": [
                "router_replica",
                "marshaller_rebalance",
                "kafka",
                "elasticsearch",
                "observability-elasticsearch",
                "victoriametrics",
            ],
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


def acceptance_token(binding: dict[str, Any], label: str) -> str:
    revision = binding["revision"]
    return hashlib.sha256(f"phase18-04-{revision}-{label}".encode()).hexdigest()[:12]


def candidate_environment(temp_root: Path, binding: dict[str, Any]) -> tuple[Path, dict[str, str]]:
    values = parse_env(ROOT / ".env.example")
    token = acceptance_token(binding, "compose")
    values.update(
        {
            "APP_ENV": "test",
            "PUBLISHED_HOST": "127.0.0.1",
            "FRONTEND_PORT": str(free_port()),
            "GOPULSE_VERSION": binding["version"],
            "GOPULSE_REVISION": binding["revision"],
            "GOPULSE_IMAGE_TAG": f"phase18-04-{binding['revision'][:12]}",
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
    evidence_name: str,
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
    write_text_once(directory / f"{evidence_name}-health.log", redact(last, values))
    return {"service": service, "status": "timeout", "last": f"{evidence_name}-health.log"}


def run_acceptance(
    directory: Path,
    project: str,
    env_file: Path,
    values: dict[str, str],
    name: str,
    scenario: str,
) -> dict[str, Any]:
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
        "acceptance",
        "e2e/compose-observability.spec.ts",
    )
    return record_command(directory, name, args, timeout=240, redact_values=values)


def bootstrap_admin(
    directory: Path,
    project: str,
    env_file: Path,
    values: dict[str, str],
) -> tuple[dict[str, Any], dict[str, Any] | None]:
    probe = record_command(
        directory,
        "bootstrap-user-id",
        compose_command(
            project,
            env_file,
            "exec",
            "-T",
            "-e",
            f"BOOTSTRAP_USERNAME={values['GOPULSE_OBSERVABILITY_ADMIN_USERNAME']}",
            "mysql",
            "sh",
            "-ec",
            'MYSQL_PWD="$MYSQL_PASSWORD" mysql --user="$MYSQL_USER" --batch --skip-column-names "$MYSQL_DATABASE" --execute "SELECT id FROM users WHERE username=\'${BOOTSTRAP_USERNAME}\'"',
        ),
        timeout=60,
        redact_values=values,
    )
    probe_path = directory / f"{probe['name']}.stdout.log"
    user_id = probe_path.read_text(encoding="utf-8").strip() if probe_path.exists() else ""
    if probe["exit_code"] != 0 or not re.fullmatch(r"[1-9][0-9]*", user_id):
        return probe, None
    promoted = record_command(
        directory,
        "bootstrap-admin-role",
        compose_command(
            project,
            env_file,
            "--profile",
            "operations",
            "run",
            "--rm",
            "--no-deps",
            "admin-role",
            "bootstrap",
            "--user-id",
            user_id,
        ),
        timeout=120,
        redact_values=values,
    )
    return probe, promoted


def matrix_scenarios() -> list[tuple[str, str | None, str]]:
    return [
        ("normal", None, "admin"),
        ("router_replica_failover", "router-2", "transport-down"),
        ("marshaller_rebalance", "marshaller-2", "transport-down"),
        ("kafka_short_fault", "kafka", "transport-down"),
        ("business_search_es_fault", "elasticsearch", "transport-down"),
        ("observability_es_fault", "observability-elasticsearch", "transport-down"),
        ("victoriametrics_fault", "victoriametrics", "vm-down"),
        ("recovery", None, "admin"),
    ]


def run_matrix(run_dir: Path, binding: dict[str, Any], run_number: int) -> dict[str, Any]:
    directory = run_dir / "U3"
    directory.mkdir(parents=True, exist_ok=True)
    project = f"gopulse-p1804-{acceptance_token(binding, f'project-{run_number}')}-r{run_number}"
    if not PROJECT_RE.fullmatch(project):
        raise ValueError("unsafe acceptance project name")
    temp_root = Path(tempfile.mkdtemp(prefix=f"gopulse-p1804-{run_number}-"))
    env_file, values = candidate_environment(temp_root, binding)
    matrix: dict[str, Any] = {
        "project": project,
        "env_sha256": sha256_file(env_file),
        "scenarios": [],
        "health": [],
        "timings": [],
        "started": False,
        "status": "execution_failed",
    }
    sequence = 0

    def compose(*args: str, timeout: int = 1800) -> dict[str, Any]:
        nonlocal sequence
        sequence += 1
        result = record_command(
            directory,
            f"compose-{sequence:02d}",
            compose_command(project, env_file, *args),
            timeout=timeout,
            redact_values=values,
        )
        matrix["timings"].append({"name": result["name"], "elapsed_seconds": result["elapsed_seconds"]})
        return result

    try:
        build = compose(
            "build",
            "backend",
            "backend-2",
            "frontend",
            "admin-frontend",
            "router",
            "router-2",
            "marshaller",
            "marshaller-2",
            "monitor",
            "acceptance",
            timeout=1800,
        )
        matrix["build"] = build
        if build["exit_code"] != 0:
            matrix["reason"] = "observability image build failed"
            return matrix

        matrix["started"] = True
        up = compose(
            "up",
            "-d",
            "--wait",
            "--wait-timeout",
            "420",
            "frontend",
            "backend",
            "backend-2",
            "router",
            "router-2",
            "marshaller",
            "marshaller-2",
            "monitor",
            timeout=1800,
        )
        matrix["up"] = up
        if up["exit_code"] != 0:
            matrix["reason"] = "dual Router/Marshaller Compose startup failed"
            return matrix

        services = (
            "elasticsearch",
            "observability-elasticsearch",
            "victoriametrics",
            "router",
            "router-2",
            "marshaller",
            "marshaller-2",
            "monitor",
            "backend",
            "backend-2",
            "frontend",
        )
        for service in services:
            health = wait_healthy(project, env_file, service, directory, values, f"health-{service}")
            matrix["health"].append(health)
        if not all(item["status"] == "healthy" for item in matrix["health"]):
            matrix["reason"] = "one or more replica or storage health checks did not converge"
            return matrix

        setup = run_acceptance(directory, project, env_file, values, "acceptance-setup", "setup")
        matrix["scenarios"].append({"name": "setup", "acceptance": setup})
        if setup["exit_code"] != 0:
            matrix["reason"] = "observability setup acceptance failed"
            matrix["status"] = "execution_failed" if setup["exit_code"] in (124, 127) else "boundary_found"
            return matrix
        probe, promoted = bootstrap_admin(directory, project, env_file, values)
        matrix["admin_bootstrap"] = promoted
        if promoted is None or promoted["exit_code"] != 0:
            matrix["reason"] = "administrator role bootstrap failed"
            code = promoted["exit_code"] if promoted is not None else probe["exit_code"]
            matrix["status"] = "execution_failed" if code in (124, 127) else "boundary_found"
            return matrix

        failures: list[str] = []
        execution_failure = False
        for name, stopped_service, scenario in matrix_scenarios():
            item: dict[str, Any] = {"name": name}
            if stopped_service is not None:
                stop = compose("stop", stopped_service, timeout=120)
                item["stop"] = stop
                if stop["exit_code"] != 0:
                    failures.append(f"{name}: could not stop {stopped_service}")
                    execution_failure = True
            acceptance = run_acceptance(directory, project, env_file, values, f"acceptance-{name}", scenario)
            item["acceptance"] = acceptance
            matrix["timings"].append({"name": acceptance["name"], "elapsed_seconds": acceptance["elapsed_seconds"]})
            if acceptance["exit_code"] != 0:
                failures.append(f"{name}: acceptance returned {acceptance['exit_code']}")
                execution_failure = execution_failure or acceptance["exit_code"] in (124, 127)
            if stopped_service is not None:
                start = compose("start", stopped_service, timeout=120)
                item["start"] = start
                if start["exit_code"] != 0:
                    failures.append(f"{name}: could not start {stopped_service}")
                    execution_failure = True
                health = wait_healthy(
                    project,
                    env_file,
                    stopped_service,
                    directory,
                    values,
                    f"health-{name}-{stopped_service}",
                )
                item["recovery_health"] = health
                if health["status"] != "healthy":
                    failures.append(f"{name}: {stopped_service} did not recover")
                    execution_failure = True
            matrix["scenarios"].append(item)

        closure_commands = [
            ("closure-compose-ps", compose("ps", "--all", timeout=60)),
            (
                "closure-kafka-topic",
                compose(
                    "exec",
                    "-T",
                    "kafka",
                    "/opt/kafka/bin/kafka-topics.sh",
                    "--bootstrap-server",
                    "kafka:19092",
                    "--describe",
                    "--topic",
                    "gopulse-observability-v1",
                    timeout=60,
                ),
            ),
            (
                "closure-kafka-group",
                compose(
                    "exec",
                    "-T",
                    "kafka",
                    "/opt/kafka/bin/kafka-consumer-groups.sh",
                    "--bootstrap-server",
                    "kafka:19092",
                    "--describe",
                    "--group",
                    "gopulse-marshaller-metrics-v1",
                    timeout=60,
                ),
            ),
            (
                "closure-business-es",
                compose(
                    "exec",
                    "-T",
                    "elasticsearch",
                    "sh",
                    "-ec",
                    "curl --fail --silent 'http://127.0.0.1:9200/_cat/indices?format=json'",
                    timeout=60,
                ),
            ),
            (
                "closure-observability-es",
                compose(
                    "exec",
                    "-T",
                    "observability-elasticsearch",
                    "sh",
                    "-ec",
                    "curl --fail --silent 'http://127.0.0.1:9200/_cat/indices?format=json'",
                    timeout=60,
                ),
            ),
        ]
        matrix["closure"] = {name: result for name, result in closure_commands}
        for name, result in closure_commands:
            if result["exit_code"] != 0:
                failures.append(f"{name}: command returned {result['exit_code']}")
                execution_failure = True
        metrics = []
        for service, token_name in (
            ("router", "ROUTER_METRICS_TOKEN"),
            ("router-2", "ROUTER_METRICS_TOKEN"),
            ("marshaller", "MARSHALLER_METRICS_TOKEN"),
            ("marshaller-2", "MARSHALLER_METRICS_TOKEN"),
        ):
            probe = compose(
                "exec",
                "-T",
                service,
                "sh",
                "-ec",
                f'wget --quiet --header "Authorization: Bearer $${token_name}" --output-document=- http://127.0.0.1:1910' + ("5" if service.startswith("router") else "6") + "/metrics | grep -E " + '"(instance|partition|buffer|target_blocked|backpressure)"',
                timeout=60,
            )
            metrics.append({"service": service, "probe": probe})
            if probe["exit_code"] != 0:
                failures.append(f"{service}: replica metrics probe returned {probe['exit_code']}")
                execution_failure = execution_failure or probe["exit_code"] in (124, 127)
        matrix["replica_metrics"] = metrics
        matrix["status"] = "execution_failed" if execution_failure else ("boundary_found" if failures else "target_met")
        if failures:
            matrix["failures"] = failures
        return matrix
    except Exception as error:
        matrix["status"] = "execution_failed"
        matrix["reason"] = str(error)
        write_text_once(directory / "runner-error.log", str(error) + "\n")
        return matrix
    finally:
        if matrix["started"]:
            cleanup = compose("down", "--volumes", "--remove-orphans", timeout=300)
            matrix["cleanup"] = cleanup
            if cleanup["exit_code"] != 0 and matrix.get("status") == "target_met":
                matrix["status"] = "execution_failed"
        write_json_once(directory / "matrix.json", matrix)
        shutil.rmtree(temp_root, ignore_errors=True)


def run_u1(run_dir: Path) -> dict[str, Any]:
    directory = run_dir / "U1"
    commands = [
        ("componentmetrics-go-test", ROOT / "componentmetrics", ["go", "test", "-count=1", "./..."]),
        ("router-go-test", ROOT / "router", ["go", "test", "-count=1", "./..."]),
        ("marshaller-go-test", ROOT / "marshaller", ["go", "test", "-count=1", "./..."]),
        ("monitor-go-test", ROOT / "monitor", ["go", "test", "-count=1", "./..."]),
        (
            "backend-go-test",
            ROOT / "backend",
            ["go", "test", "-count=1", "./internal/config", "./internal/platform", "./internal/search", "./cmd/search-reindex"],
        ),
    ]
    results = [record_command(directory, name, args, cwd=cwd, timeout=900) for name, cwd, args in commands]
    if all(item["exit_code"] == 0 for item in results):
        status = "target_met"
    elif any(item["exit_code"] in (124, 127) for item in results):
        status = "execution_failed"
    else:
        status = "boundary_found"
    return {"commands": results, "passed": status == "target_met", "status": status}


def run_u2(run_dir: Path) -> dict[str, Any]:
    directory = run_dir / "U2"
    result = record_command(
        directory,
        "runner-self-test",
        ["python3", "-m", "unittest", "discover", "-s", "scripts/ci", "-p", "test_phase18_observability_scale.py"],
        timeout=300,
    )
    status = "target_met" if result["exit_code"] == 0 else ("execution_failed" if result["exit_code"] in (124, 127) else "boundary_found")
    return {"commands": [result], "passed": status == "target_met", "status": status}


def run_u4(run_dir: Path, binding: dict[str, Any]) -> dict[str, Any]:
    directory = run_dir / "U4"
    commands = [
        (
            "runtime-contracts",
            [
                "python3",
                "scripts/ci/verify_runtime_contracts.py",
                "--contract",
                "deploy/runtime-contracts.json",
                "--compose",
                "deploy/compose.yaml",
                "--env",
                ".env.example",
            ],
        ),
        ("versions", ["python3", "scripts/ci/validate_versions.py"]),
        ("diff-check", ["git", "diff", "--check"]),
    ]
    results = [record_command(directory, name, args, timeout=900) for name, args in commands]
    branch_ok = binding["branch"] == "develop/2.0.4"
    version_ok = binding["version"] == TARGET_VERSION
    write_json_once(directory / "branch-version.json", {"branch": binding["branch"], "version": binding["version"], "passed": branch_ok and version_ok})
    if branch_ok and version_ok and all(item["exit_code"] == 0 for item in results):
        status = "target_met"
    elif any(item["exit_code"] in (124, 127) for item in results):
        status = "execution_failed"
    else:
        status = "boundary_found"
    return {"commands": results, "branch": branch_ok, "version": version_ok, "passed": status == "target_met", "status": status}


def run_one(root: Path, binding: dict[str, Any], run_number: int) -> dict[str, Any]:
    run_dir = root / f"run-{run_number}"
    if run_dir.exists():
        raise ValueError(f"refusing to overwrite {run_dir}")
    run_dir.mkdir(mode=0o700)
    write_json_once(run_dir / "binding.json", binding)
    units: dict[str, Any] = {}
    for name, runner in (("U1", run_u1), ("U2", run_u2)):
        try:
            units[name] = runner(run_dir)
        except Exception as error:
            write_text_once(run_dir / name / "runner-error.log", str(error) + "\n")
            units[name] = {"status": "execution_failed", "passed": False, "reason": str(error)}
    try:
        units["U3"] = run_matrix(run_dir, binding, run_number)
    except Exception as error:
        write_text_once(run_dir / "U3" / "runner-error.log", str(error) + "\n")
        units["U3"] = {"status": "execution_failed", "passed": False, "reason": str(error)}
    try:
        units["U4"] = run_u4(run_dir, binding)
    except Exception as error:
        write_text_once(run_dir / "U4" / "runner-error.log", str(error) + "\n")
        units["U4"] = {"status": "execution_failed", "passed": False, "reason": str(error)}
    passed = {name: bool(units[name].get("passed", units[name].get("status") == "target_met")) for name in UNIT_NAMES}
    status = final_result(units[name].get("status", "execution_failed") for name in UNIT_NAMES)
    summary = {"run": run_number, "units": units, "passed": passed, "status": status}
    write_json_once(run_dir / "summary.json", summary)
    return summary


def summarize(root: Path, binding: dict[str, Any], runs: list[dict[str, Any]]) -> dict[str, Any]:
    pass_counts = {name: sum(1 for run in runs if run["passed"].get(name)) for name in UNIT_NAMES}
    raw_timings: dict[str, list[float]] = {}
    for run in runs:
        for item in run["units"].get("U3", {}).get("timings", []):
            raw_timings.setdefault(item["name"], []).append(float(item["elapsed_seconds"]))
    averages = {name: average(values) for name, values in raw_timings.items() if values}
    summary = {
        "schema": "gopulse.phase18.observability-scale-summary.v1",
        "candidate": binding,
        "runs": 2,
        "unit_pass_counts": pass_counts,
        "raw_timings_seconds": raw_timings,
        "averages_seconds": averages,
        "deterministic_counts": {"target_met_runs": sum(run["status"] == "target_met" for run in runs), "runs": 2},
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
    evidence_root = ROOT / ".run" / f"phase18-observability-scale-{binding['version']}-{binding['revision'][:12]}-{secrets.token_hex(6)}"
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
        print(f"phase18 observability scale runner failed before evidence completion: {error}", file=sys.stderr)
        raise SystemExit(1)
