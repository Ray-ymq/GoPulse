#!/usr/bin/env python3
"""Run GoPulse's isolated integration and browser test lifecycles.

The development, observation, and stop lifecycles moved to the native
devtools/cmd/devenv helper in Phase 23-03. This module keeps the isolated test
lifecycles, which own their own Compose project, ports, and private state.
"""

from __future__ import annotations

import argparse
from contextlib import contextmanager
from http.cookiejar import CookieJar
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import signal
import socket
import subprocess
import sys
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Iterable, Mapping, Sequence
from urllib.error import HTTPError, URLError
from urllib.request import HTTPCookieProcessor, ProxyHandler, Request, build_opener


SEMVER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
ENV_KEY = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
KNOWN_CONFIG_KEYS = {
    "APP_ENV", "AUTH_COOKIE_NAME", "AUTH_COOKIE_SECURE", "AUTH_JWT_SECRET", "AUTH_JWT_TTL",
    "ALERT_EVALUATION_ENABLED", "BACKEND_SERVICE_ROLE", "BACKEND_HTTP_MAX_CONCURRENCY",
    "BACKEND_METRICS_TOKEN", "BACKEND_VICTORIAMETRICS_PASSWORD", "BACKEND_VICTORIAMETRICS_QUERY_TIMEOUT",
    "BACKEND_VICTORIAMETRICS_URL", "BACKEND_VICTORIAMETRICS_USERNAME", "BUSINESS_WORKER_METRICS_TOKEN",
    "ADMIN_FRONTEND_PORT", "ELASTICSEARCH_PORT", "ELASTICSEARCH_REQUEST_TIMEOUT", "ELASTICSEARCH_URL", "FRONTEND_PORT",
    "GOPULSE_RUNTIME_MODE", "GOPULSE_TRACE_ENABLED", "GOPULSE_TRACE_ENDPOINT", "GOPULSE_TRACE_SERVICE_NAME",
    "GOPULSE_TRACE_SAMPLE_RATIO", "GOPULSE_TRACE_QUEUE_CAPACITY", "GOPULSE_TRACE_BATCH_SIZE",
    "GOPULSE_TRACE_BATCH_TIMEOUT", "GOPULSE_TRACE_EXPORT_TIMEOUT", "GOPULSE_TRACE_SHUTDOWN_TIMEOUT",
    "GOPULSE_VERSION", "GOPULSE_IMAGE_TAG", "GOPULSE_INSTANCE_ID", "GOPULSE_REPLICA_COUNT",
    "GOPULSE_UPDATE_VERSION", "HTTP_HOST", "HTTP_PORT", "KAFKA_OBSERVABILITY_PARTITIONS", "KAFKA_PORT",
    "LOG_MONITOR_INGEST_TOKEN", "LOG_MONITOR_URL", "LOG_SHIP_REQUEST_TIMEOUT", "LOG_SHIP_QUEUE_CAPACITY",
    "LOG_SHIP_RETRY_MIN", "LOG_SHIP_RETRY_MAX", "LOG_SHIP_SHUTDOWN_TIMEOUT", "MARSHALLER_API_TOKEN",
    "MARSHALLER_ENDPOINTS", "MARSHALLER_ELASTICSEARCH_URL", "MARSHALLER_ELASTICSEARCH_TIMEOUT",
    "MARSHALLER_EVENT_INDEX_PREFIX", "MARSHALLER_EVENT_RETENTION_DAYS", "MARSHALLER_EVENT_TEMPLATE",
    "MARSHALLER_FUTURE_SKEW", "MARSHALLER_HTTP_HOST", "MARSHALLER_HTTP_PORT", "MARSHALLER_KAFKA_BROKERS",
    "MARSHALLER_KAFKA_COMMIT_TIMEOUT", "MARSHALLER_KAFKA_GROUP", "MARSHALLER_KAFKA_MIN_PARTITIONS",
    "MARSHALLER_KAFKA_TOPIC", "MARSHALLER_LOG_INDEX_PREFIX", "MARSHALLER_LOG_RETENTION_DAYS",
    "MARSHALLER_LOG_TEMPLATE", "MARSHALLER_MAX_IN_FLIGHT", "MARSHALLER_MAX_RETRYING", "MARSHALLER_METRICS_TOKEN",
    "MARSHALLER_READINESS_TIMEOUT", "MARSHALLER_RETRY_MIN", "MARSHALLER_RETRY_MAX", "MARSHALLER_RETENTION_BATCH_INDICES",
    "MARSHALLER_RETENTION_CATCHUP_DEADLINE", "MARSHALLER_RETENTION_CYCLE", "MARSHALLER_RETENTION_MAX_RETRIES",
    "MARSHALLER_RETENTION_REQUEST_TIMEOUT", "MARSHALLER_RETENTION_RETRY_MAX", "MARSHALLER_RETENTION_RETRY_MIN",
    "MARSHALLER_RETENTION_ROUND_TIMEOUT", "MARSHALLER_SHUTDOWN_TIMEOUT", "MARSHALLER_VM_PASSWORD",
    "MARSHALLER_VM_TIMEOUT", "MARSHALLER_VM_URL", "MARSHALLER_VM_USERNAME", "MONITOR_API_TOKEN",
    "MONITOR_EVENT_MAX_BYTES", "MONITOR_EVENT_QUEUE_CAPACITY", "MONITOR_EVENT_RETRY_MAX", "MONITOR_EVENT_RETRY_MIN",
    "MONITOR_EVENT_SHUTDOWN_TIMEOUT", "MONITOR_HTTP_HOST", "MONITOR_HTTP_PORT", "MONITOR_LOG_FUTURE_SKEW",
    "MONITOR_LOG_MAX_BYTES", "MONITOR_METRICS_TOKEN", "MONITOR_PLUGIN_STARTUP_TIMEOUT", "MONITOR_PLUGIN_STOP_TIMEOUT",
    "MONITOR_PUBLISH_TIMEOUT", "MONITOR_REQUEST_TIMEOUT", "MONITOR_ROUTER_TOKEN", "MONITOR_ROUTER_URL",
    "MONITOR_ROUTER_URLS", "MONITOR_SCRAPE_INTERVAL", "MONITOR_SCRAPE_TIMEOUT", "MONITOR_SHUTDOWN_TIMEOUT",
    "MYSQL_CONN_MAX_LIFETIME", "MYSQL_DATABASE", "MYSQL_HOST", "MYSQL_MAX_IDLE_CONNS", "MYSQL_MAX_OPEN_CONNS",
    "MYSQL_PASSWORD", "MYSQL_PORT", "MYSQL_ROOT_PASSWORD", "MYSQL_USER", "OBSERVABILITY_ELASTICSEARCH_PORT",
    "OBSERVABILITY_ELASTICSEARCH_URL", "OUTBOX_CLAIM_BATCH", "OUTBOX_CLEANUP_BATCH", "OUTBOX_CLEANUP_INTERVAL",
    "OUTBOX_LEASE_DURATION", "OUTBOX_PUBLISHED_RETENTION", "OUTBOX_PUBLISH_TIMEOUT", "OUTBOX_RETRY_DELAY",
    "OUTBOX_POLL_INTERVAL", "PLATFORM_API_HTTP_MAX_CONCURRENCY", "PLATFORM_API_MYSQL_MAX_OPEN_CONNS",
    "RABBITMQ_MANAGEMENT_PORT", "RABBITMQ_PASSWORD", "RABBITMQ_PORT", "RABBITMQ_URL", "RABBITMQ_USER",
    "REDIS_DB", "REDIS_EXPORTER_HTTP_HOST", "REDIS_EXPORTER_HTTP_PORT", "REDIS_EXPORTER_SCRAPE_TIMEOUT",
    "REDIS_EXPORTER_SHUTDOWN_TIMEOUT", "REDIS_HOST", "REDIS_OPERATION_TIMEOUT", "REDIS_PASSWORD", "REDIS_PORT",
    "REDIS_POST_DETAIL_TTL", "REDIS_METRICS_TOKEN", "ROUTER_API_TOKEN", "ROUTER_ENDPOINTS", "ROUTER_HTTP_HOST",
    "ROUTER_HTTP_PORT", "ROUTER_KAFKA_BROKERS", "ROUTER_KAFKA_MAX_BUFFERED_BYTES", "ROUTER_KAFKA_MAX_BUFFERED_RECORDS",
    "ROUTER_KAFKA_MIN_PARTITIONS", "ROUTER_KAFKA_PRODUCE_TIMEOUT", "ROUTER_KAFKA_TOPIC", "ROUTER_METRICS_TOKEN",
    "ROUTER_REQUEST_TIMEOUT", "ROUTER_SHUTDOWN_TIMEOUT", "SEARCH_INDEXER_ENDPOINTS", "SEARCH_INDEXER_METRICS_TOKEN",
    "SEARCH_INDEXER_MAX_RETRIES", "SEARCH_INDEXER_PREFETCH", "SEARCH_INDEXER_PUBLISH_TIMEOUT",
    "SEARCH_INDEXER_RECONNECT_MAX", "SEARCH_INDEXER_RECONNECT_MIN", "SEARCH_INDEXER_RETRY_DELAY",
    "SEARCH_INDEXER_SHUTDOWN_TIMEOUT", "SEARCH_REINDEX_BATCH", "VICTORIAMETRICS_PASSWORD", "VICTORIAMETRICS_PORT",
    "VICTORIAMETRICS_USERNAME",
}

class DevelopmentError(RuntimeError):
    """A bounded, user-actionable local lifecycle failure."""


@dataclass(frozen=True)
class Workspace:
    root: Path
    identity: str

    @property
    def private_root(self) -> Path:
        return self.root / ".run" / "local" / self.identity

    @property
    def state_path(self) -> Path:
        return self.private_root / "state.json"

    @property
    def integration_state_path(self) -> Path:
        return self.private_root / "integration-state.json"

    @property
    def integration_lock_path(self) -> Path:
        return self.private_root / "integration.lock"

    @property
    def project_dev(self) -> str:
        return f"gopulse-{self.identity}-dev"

    @property
    def project_test(self) -> str:
        # A Monitor image owns a compile-time release catalog. Keep test
        # volumes candidate-scoped so a newer image cannot inherit a plugin
        # volume whose active release is not registered in that image.
        version = "current"
        version_path = self.root / "VERSION"
        if version_path.is_file():
            candidate = version_path.read_text(encoding="utf-8").strip()
            if SEMVER.fullmatch(candidate):
                version = candidate.replace(".", "")
        return f"gopulse-{self.identity}-test-{version}"


def workspace_for(root: Path) -> Workspace:
    resolved = root.resolve()
    identity = hashlib.sha256(str(resolved).encode("utf-8")).hexdigest()[:12]
    return Workspace(resolved, identity)


def now() -> str:
    return datetime.now(timezone.utc).isoformat()


def parse_dotenv(path: Path) -> dict[str, str]:
    """Parse the small dotenv contract used by the project, without expansion."""
    values: dict[str, str] = {}
    for number, raw_line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[7:].lstrip()
        key, separator, raw_value = line.partition("=")
        if not separator or not ENV_KEY.fullmatch(key.strip()):
            raise DevelopmentError(f"invalid environment entry in {path}:{number}")
        value = raw_value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "'\"":
            value = value[1:-1]
        values[key.strip()] = value
    return values


def environment_file(root: Path, explicit: str | None, environ: Mapping[str, str]) -> Path | None:
    candidate = explicit or environ.get("GOPULSE_ENV_FILE")
    if candidate:
        path = Path(candidate).expanduser()
        if not path.is_absolute():
            path = (root / path).resolve()
        if not path.is_file():
            raise DevelopmentError(f"environment file does not exist: {path}")
        return path
    default = root / ".env"
    return default if default.is_file() else None


def compose_environment(root: Path, mode: str, explicit_env: str | None = None, environ: Mapping[str, str] | None = None) -> dict[str, str]:
    """Merge checked-in defaults, an optional caller file, and caller variables."""
    caller = dict(os.environ if environ is None else environ)
    defaults = parse_dotenv(root / ".env.example")
    values = dict(defaults)
    source = environment_file(root, explicit_env, caller)
    caller_keys: set[str] = set()
    if source:
        file_values = parse_dotenv(source)
        values.update(file_values)
        caller_keys.update(file_values)
    for key in KNOWN_CONFIG_KEYS:
        if key in caller:
            values[key] = caller[key]
            caller_keys.add(key)

    def default(key: str, value: str) -> None:
        if key not in caller_keys:
            values[key] = value

    def defaults_only(mapping: Mapping[str, str]) -> None:
        for key, value in mapping.items():
            default(key, value)

    if mode == "dev":
        defaults_only({
            "APP_ENV": "development", "GOPULSE_RUNTIME_MODE": "host", "BACKEND_SERVICE_ROLE": "combined",
            "HTTP_PORT": "8080", "FRONTEND_PORT": "5173", "ADMIN_FRONTEND_PORT": "5174", "ALERT_EVALUATION_ENABLED": "false",
            "GOPULSE_TRACE_ENABLED": "false", "LOG_MONITOR_URL": "", "LOG_MONITOR_INGEST_TOKEN": "",
            "BACKEND_ENDPOINTS": "backend", "BUSINESS_WORKER_ENDPOINTS": "business-worker",
            "SEARCH_INDEXER_ENDPOINTS": "search-indexer",
        })
    elif mode == "observe":
        defaults_only({
            "APP_ENV": "development", "GOPULSE_RUNTIME_MODE": "host", "BACKEND_SERVICE_ROLE": "combined",
            "HTTP_PORT": "8080", "FRONTEND_PORT": "5173", "ADMIN_FRONTEND_PORT": "5174", "ALERT_EVALUATION_ENABLED": "false",
            "GOPULSE_TRACE_ENABLED": "false", "BACKEND_ENDPOINTS": "backend",
            "BUSINESS_WORKER_ENDPOINTS": "business-worker", "SEARCH_INDEXER_ENDPOINTS": "search-indexer",
            "ROUTER_ENDPOINTS": "router", "MARSHALLER_ENDPOINTS": "marshaller",
            "LOG_MONITOR_URL": "http://127.0.0.1:9090", "MONITOR_URL": "http://127.0.0.1:9090",
            "MONITOR_ROUTER_URL": "http://127.0.0.1:9091", "MONITOR_ROUTER_URLS": "http://127.0.0.1:9091",
        })
    elif mode == "test":
        # Test dependencies must never follow a developer's .env values. This
        # is the isolation boundary consumed by the later integration batch.
        values.update({
            "APP_ENV": "test", "GOPULSE_RUNTIME_MODE": "host", "BACKEND_SERVICE_ROLE": "combined",
            "HTTP_HOST": "127.0.0.1", "MYSQL_HOST": "127.0.0.1", "REDIS_HOST": "127.0.0.1",
            "HTTP_PORT": "18080", "FRONTEND_PORT": "15173", "ADMIN_FRONTEND_PORT": "15174", "ALERT_EVALUATION_ENABLED": "false",
            "GOPULSE_TRACE_ENABLED": "false", "MYSQL_PORT": "23306", "MYSQL_DATABASE": "gopulse_integration",
            "MYSQL_USER": "gopulse_integration", "MYSQL_PASSWORD": "integration-mysql",
            "MYSQL_ROOT_PASSWORD": "integration-root", "REDIS_PORT": "26379", "REDIS_PASSWORD": "integration-redis",
            "REDIS_DB": "15", "RABBITMQ_PORT": "25672", "RABBITMQ_MANAGEMENT_PORT": "25673",
            "RABBITMQ_USER": "gopulse_integration", "RABBITMQ_PASSWORD": "integration-rabbitmq",
            "RABBITMQ_URL": "amqp://gopulse_integration:integration-rabbitmq@127.0.0.1:25672/",
            "ELASTICSEARCH_PORT": "29200", "ELASTICSEARCH_URL": "http://127.0.0.1:29200",
            "OBSERVABILITY_ELASTICSEARCH_PORT": "29201", "OBSERVABILITY_ELASTICSEARCH_URL": "http://127.0.0.1:29201",
            "KAFKA_PORT": "19092", "VICTORIAMETRICS_PORT": "18428", "BACKEND_VICTORIAMETRICS_URL": "http://127.0.0.1:18428",
            "VICTORIAMETRICS_USERNAME": "gopulse-test-marshaller", "VICTORIAMETRICS_PASSWORD": "test-victoria-metrics-password-32-bytes",
            "BACKEND_VICTORIAMETRICS_USERNAME": "gopulse-test-marshaller", "BACKEND_VICTORIAMETRICS_PASSWORD": "test-victoria-metrics-password-32-bytes",
            "MARSHALLER_VM_USERNAME": "gopulse-test-marshaller", "MARSHALLER_VM_PASSWORD": "test-victoria-metrics-password-32-bytes",
            "MARSHALLER_VM_URL": "http://127.0.0.1:18428", "MARSHALLER_ELASTICSEARCH_URL": "http://127.0.0.1:29201",
            "MONITOR_HTTP_PORT": "19090", "MONITOR_URL": "http://127.0.0.1:19090",
            "MONITOR_BOOTSTRAP_PACKAGE": "/opt/gopulse/packages/gopulse-redis-exporter.tar.gz",
            "ROUTER_HTTP_PORT": "19091", "MARSHALLER_HTTP_PORT": "19093", "FRONTEND_PORT": "15173", "ADMIN_FRONTEND_PORT": "15174",
            "ROUTER_KAFKA_BROKERS": "127.0.0.1:19092", "MARSHALLER_KAFKA_BROKERS": "127.0.0.1:19092",
            "ROUTER_API_TOKEN": "test-router-api-token-32-bytes-0123456789", "ROUTER_METRICS_TOKEN": "test-router-metrics-token-32-bytes-0123456789",
            "MARSHALLER_API_TOKEN": "test-marshaller-api-token-32-bytes-0123456789", "MARSHALLER_METRICS_TOKEN": "test-marshaller-metrics-token-32-bytes-0123456789",
            "MONITOR_API_TOKEN": "test-monitor-api-token-32-bytes-0123456789", "MONITOR_METRICS_TOKEN": "test-monitor-metrics-token-32-bytes-0123456789",
            "MONITOR_ROUTER_TOKEN": "test-router-api-token-32-bytes-0123456789", "REDIS_EXPORTER_HTTP_PORT": "19121",
            "LOG_MONITOR_URL": "http://127.0.0.1:19090", "LOG_MONITOR_INGEST_TOKEN": "test-log-ingest-token-32-bytes-0123456789",
            "MONITOR_ROUTER_URL": "http://127.0.0.1:19091", "MONITOR_ROUTER_URLS": "http://127.0.0.1:19091",
            "REDIS_POST_DETAIL_TTL": "5m", "AUTH_JWT_SECRET": "test-integration-jwt-secret-32-bytes-0123456789",
            "AUTH_COOKIE_NAME": "gopulse_test_session", "AUTH_COOKIE_SECURE": "false",
            "BACKEND_ENDPOINTS": "backend", "BUSINESS_WORKER_ENDPOINTS": "business-worker",
            "SEARCH_INDEXER_ENDPOINTS": "search-indexer", "ROUTER_ENDPOINTS": "router", "MARSHALLER_ENDPOINTS": "marshaller",
        })
    else:
        raise DevelopmentError(f"unknown environment mode: {mode}")
    return values


def write_private_env(workspace: Workspace, mode: str, values: Mapping[str, str]) -> Path:
    workspace.private_root.mkdir(parents=True, exist_ok=True)
    path = workspace.private_root / f"{mode}.env"
    content = "".join(f"{key}={values[key]}\n" for key in sorted(values) if ENV_KEY.fullmatch(key))
    path.write_text(content, encoding="utf-8")
    path.chmod(0o600)
    return path


def digest_paths(root: Path, paths: Iterable[Path]) -> str:
    digest = hashlib.sha256()
    files: list[Path] = []
    for item in paths:
        path = item if item.is_absolute() else root / item
        if path.is_file():
            files.append(path)
        elif path.is_dir():
            files.extend(file for file in path.rglob("*") if file.is_file() and ".git" not in file.parts)
    for path in sorted(set(files), key=lambda value: str(value.relative_to(root))):
        relative = path.relative_to(root).as_posix().encode("utf-8")
        digest.update(len(relative).to_bytes(8, "big"))
        digest.update(relative)
        content = path.read_bytes()
        digest.update(len(content).to_bytes(8, "big"))
        digest.update(content)
    return digest.hexdigest()


def source_digest(root: Path, observe: bool = False) -> str:
    paths: list[Path] = [root / "backend", root / "componentmetrics"]
    if observe:
        paths.extend([root / "router", root / "marshaller"])
    return digest_paths(root, paths)


def e2e_source_digest(root: Path, observe: bool = False) -> str:
    paths: list[Path] = [
        root / "frontend" / "src", root / "frontend" / "e2e", root / "frontend" / "package.json",
        root / "frontend" / "package-lock.json", root / "frontend" / "playwright.config.ts",
        root / "frontend" / "vite.config.ts", root / "frontend" / "vite.config.test.ts",
    ]
    if observe:
        paths.extend([
            root / "admin-frontend" / "src", root / "admin-frontend" / "package.json",
            root / "admin-frontend" / "package-lock.json", root / "admin-frontend" / "vite.config.ts",
            root / "admin-frontend" / "vite.config.test.ts",
        ])
    return digest_paths(root, paths)


def monitor_input_digest(root: Path) -> str:
    return digest_paths(root, [
        root / "VERSION", root / "componentmetrics", root / "monitor", root / "exporters",
        root / "deploy" / "docker" / "observability.Dockerfile", root / "scripts" / "package-redis-exporter.sh",
        root / "deploy" / "plugins",
    ])


def process_birth_identity(pid: int) -> str | None:
    try:
        raw = (Path("/proc") / str(pid) / "stat").read_text(encoding="utf-8")
    except (FileNotFoundError, PermissionError, OSError):
        return None
    closing = raw.rfind(")")
    if closing < 0:
        return None
    fields = raw[closing + 2 :].split()
    if len(fields) <= 19:
        return None
    if fields[0] == "Z":
        return None
    return fields[19]


def process_owned(record: Mapping[str, object]) -> bool:
    try:
        pid = int(record["pid"])
    except (KeyError, TypeError, ValueError):
        return False
    return process_birth_identity(pid) == str(record.get("birth", ""))


def save_json(path: Path, document: Mapping[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    path.chmod(0o600)


def load_state(workspace: Workspace) -> dict[str, object] | None:
    if not workspace.state_path.is_file():
        return None
    try:
        state = json.loads(workspace.state_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise DevelopmentError(f"cannot read private lifecycle state: {exc}") from exc
    if state.get("workspace_root") != str(workspace.root) or state.get("workspace_id") != workspace.identity:
        raise DevelopmentError("private lifecycle state belongs to another workspace")
    return state


def run_command(command: Sequence[str], *, cwd: Path, env: Mapping[str, str] | None = None, label: str, timeout: float | None = None) -> None:
    try:
        completed = subprocess.run(list(command), cwd=cwd, env=dict(env) if env is not None else None, check=False, timeout=timeout)
    except OSError as exc:
        raise DevelopmentError(f"{label} could not start: {exc}") from exc
    except subprocess.TimeoutExpired as exc:
        raise DevelopmentError(f"{label} exceeded its {int(timeout or 0)}s bound") from exc
    if completed.returncode != 0:
        raise DevelopmentError(f"{label} failed with exit code {completed.returncode}")


def compose_command(project: str, env_file: Path, files: Sequence[Path], profiles: Sequence[str], args: Sequence[str]) -> list[str]:
    command = ["docker", "compose", "--project-name", project, "--env-file", str(env_file)]
    for profile in profiles:
        command.extend(["--profile", profile])
    for compose_file in files:
        command.extend(["--file", str(compose_file)])
    command.extend(args)
    return command


def compose_up(workspace: Workspace, env_file: Path, values: Mapping[str, str], observe: bool, project: str | None = None) -> tuple[str, list[Path]]:
    project = project or workspace.project_dev
    files = [workspace.root / "deploy" / "compose.local.yaml"]
    if observe:
        files.append(workspace.root / "deploy" / "compose.local-linux.yaml")
    env = dict(os.environ)
    env.update(values)
    if observe:
        tag = monitor_image_tag(workspace.root)
        env["GOPULSE_MONITOR_IMAGE"] = tag
        if platform.system() != "Linux":
            raise DevelopmentError("dev-observe requires Linux for the trusted host-network Monitor image")
        try:
            subprocess.run(["docker", "image", "inspect", tag], cwd=workspace.root, env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except (OSError, subprocess.CalledProcessError) as exc:
            raise DevelopmentError(f"Monitor image {tag} is not prepared; run make monitor-image first") from exc
    try:
        run_command(
            compose_command(project, env_file, files, ["observe"] if observe else [], ["up", "--detach", "--wait", "--wait-timeout", "420"]),
            cwd=workspace.root,
            env=env,
            label=f"Compose dependency startup for {project}",
        )
    except DevelopmentError:
        failed_state = {
            "workspace_root": str(workspace.root), "project": project, "env_file": str(env_file),
            "compose_files": [str(path) for path in files], "observe": observe,
            "monitor_image": env.get("GOPULSE_MONITOR_IMAGE", ""),
        }
        compose_down(workspace, failed_state)
        raise
    return project, files


def compose_down(workspace: Workspace, state: Mapping[str, object]) -> None:
    project = str(state.get("project", ""))
    env_file = Path(str(state.get("env_file", "")))
    raw_files = state.get("compose_files", [])
    files = [Path(str(item)) for item in raw_files] if isinstance(raw_files, list) else []
    if not project or not env_file.is_file() or not files:
        return
    env = dict(os.environ)
    if state.get("monitor_image"):
        env["GOPULSE_MONITOR_IMAGE"] = str(state["monitor_image"])
    try:
        run_command(
            compose_command(project, env_file, files, ["observe"] if state.get("observe") else [], ["down", "--remove-orphans"]),
            cwd=Path(str(state["workspace_root"])), env=env, label=f"Compose dependency stop for {project}",
        )
    except DevelopmentError as exc:
        print(f"[gopulse] warning: {exc}", file=sys.stderr)


def monitor_image_tag(root: Path) -> str:
    return f"gopulse/monitor:local-{monitor_input_digest(root)[:16]}"


def check_ports(ports: Iterable[int], owned_records: Iterable[Mapping[str, object]] = ()) -> None:
    owned_pids = {int(record["pid"]) for record in owned_records if process_owned(record)}
    for port in sorted(set(ports)):
        if any(_pid_has_port(pid, port) for pid in owned_pids):
            continue
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        try:
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            sock.bind(("127.0.0.1", port))
        except OSError as exc:
            raise DevelopmentError(f"port {port} is already in use by an unowned process") from exc
        finally:
            sock.close()


def _pid_has_port(pid: int, port: int) -> bool:
    # A live owned process is accepted during idempotent repeated startup. The
    # process birth token has already been checked; a port-specific inspection
    # would require privileged /proc parsing and is unnecessary here.
    return process_birth_identity(pid) is not None and port > 0


def wait_http(url: str, *, timeout: float, token: str | None = None, label: str, process_record: Mapping[str, object] | None = None) -> None:
    deadline = time.monotonic() + timeout
    opener = build_opener(ProxyHandler({}))
    last_error = "no response"
    while time.monotonic() < deadline:
        if process_record is not None and not process_owned(process_record):
            raise DevelopmentError(f"{label} exited before readiness; inspect its private log")
        request = Request(url, method="GET")
        if token:
            request.add_header("Authorization", f"Bearer {token}")
        try:
            with opener.open(request, timeout=2) as response:
                if 200 <= response.status < 300:
                    return
                last_error = f"HTTP {response.status}"
        except HTTPError as exc:
            last_error = f"HTTP {exc.code}"
        except (URLError, TimeoutError, OSError) as exc:
            last_error = str(exc)
        time.sleep(0.25)
    raise DevelopmentError(f"{label} did not become ready within {int(timeout)}s ({last_error})")


def ensure_npm_dependencies(root: Path, directory: str) -> None:
    package_root = root / directory
    if (package_root / "node_modules").is_dir():
        return
    run_command(["npm", "ci", "--no-audit", "--no-fund"], cwd=package_root, label=f"npm dependency installation for {directory}")


def build_binary(workspace: Workspace, env: Mapping[str, str], name: str, package: str, digest: str) -> Path:
    binary = workspace.private_root / "bin" / name
    metadata_path = workspace.private_root / "bin" / f"{name}.json"
    previous: dict[str, object] = {}
    if metadata_path.is_file():
        try:
            previous = json.loads(metadata_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            previous = {}
    if binary.is_file() and previous.get("digest") == digest:
        return binary
    binary.parent.mkdir(parents=True, exist_ok=True)
    run_command(["go", "build", "-trimpath", "-o", str(binary), package], cwd=workspace.root / "backend", env=env, label=f"build {name}")
    save_json(metadata_path, {"name": name, "digest": digest, "command": ["go", "build", "-trimpath", "-o", str(binary), package]})
    return binary


def spawn_process(workspace: Workspace, state: dict[str, object], name: str, command: Sequence[str], env: Mapping[str, str], cwd: Path, state_path: Path | None = None) -> None:
    log_path = workspace.private_root / "logs" / f"{name}.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    log_handle = log_path.open("ab")
    try:
        process = subprocess.Popen(
            list(command), cwd=cwd, env=dict(env), stdin=subprocess.DEVNULL,
            stdout=log_handle, stderr=subprocess.STDOUT, start_new_session=True,
        )
    except OSError as exc:
        log_handle.close()
        raise DevelopmentError(f"start {name} failed: {exc}") from exc
    log_handle.close()
    record = {
        "pid": process.pid, "birth": process_birth_identity(process.pid), "command": list(command),
        "cwd": str(cwd), "log": str(log_path), "started_at": now(),
    }
    if not record["birth"]:
        terminate_process(record)
        raise DevelopmentError(f"could not record process identity for {name}")
    processes = state.setdefault("processes", {})
    assert isinstance(processes, dict)
    processes[name] = record
    save_json(state_path or workspace.state_path, state)


def terminate_process(record: Mapping[str, object]) -> None:
    if not process_owned(record):
        return
    pid = int(record["pid"])
    try:
        os.killpg(pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline and process_owned(record):
        time.sleep(0.1)
    if process_owned(record):
        try:
            os.killpg(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass


def application_environment(root: Path, mode: str, explicit_env: str | None, overrides: Mapping[str, str] | None = None) -> tuple[Workspace, dict[str, str], Path]:
    workspace = workspace_for(root)
    values = compose_environment(root, mode, explicit_env)
    if overrides:
        values.update(overrides)
    env_file = write_private_env(workspace, mode, values)
    process_env = dict(os.environ)
    process_env.update(values)
    return workspace, process_env, env_file


def port_argument(raw: str) -> str:
    if not raw.isdigit() or not 1 <= int(raw) <= 65535:
        raise argparse.ArgumentTypeError("port must be an integer from 1 to 65535")
    return str(int(raw))


@contextmanager
def integration_lock(workspace: Workspace):
    workspace.private_root.mkdir(parents=True, exist_ok=True)
    handle = workspace.integration_lock_path.open("a+")
    try:
        try:
            fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise DevelopmentError("another integration or browser check owns the test environment lock") from exc
        yield
    finally:
        try:
            fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
        finally:
            handle.close()


def e2e_ports(values: Mapping[str, str], observe: bool) -> list[int]:
    ports = [int(values[key]) for key in ("MYSQL_PORT", "REDIS_PORT", "RABBITMQ_PORT", "RABBITMQ_MANAGEMENT_PORT", "ELASTICSEARCH_PORT")]
    if observe:
        ports.extend(int(values[key]) for key in ("KAFKA_PORT", "VICTORIAMETRICS_PORT", "OBSERVABILITY_ELASTICSEARCH_PORT", "MONITOR_HTTP_PORT", "ROUTER_HTTP_PORT", "MARSHALLER_HTTP_PORT", "REDIS_EXPORTER_HTTP_PORT"))
        ports.extend([int(values["HTTP_PORT"]), 19101, 19102, 19103, 19105, 19106])
    ports.extend([int(values["HTTP_PORT"]), int(values["FRONTEND_PORT"]), 19101, 19102, 19103])
    if observe:
        ports.append(int(values["ADMIN_FRONTEND_PORT"]))
    return ports


def register_test_account(root: Path, values: Mapping[str, str], username: str, password: str) -> None:
    registration = json.dumps({"username": username, "password": password}, separators=(",", ":"))
    command = [
        "curl", "--silent", "--show-error", "--max-time", "20", "-H", "Content-Type: application/json",
        "-d", registration, "-o", os.devnull, "-w", "%{http_code}",
        f"http://127.0.0.1:{values['HTTP_PORT']}/api/v1/auth/register",
    ]
    try:
        registered = subprocess.run(command, cwd=root, env=dict(values), stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise DevelopmentError(f"test account registration for {username} could not complete") from exc
    if registered.returncode != 0 or registered.stdout.strip() not in {"201", "409"}:
        raise DevelopmentError(f"test account registration for {username} failed")


def browser_api_json(values: Mapping[str, str], path: str, method: str, payload: Mapping[str, object], cookies: CookieJar, label: str) -> dict[str, object]:
    body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    request = Request(
        f"http://127.0.0.1:{values['HTTP_PORT']}{path}",
        data=body,
        headers={"Content-Type": "application/json"},
        method=method,
    )
    opener = build_opener(ProxyHandler({}), HTTPCookieProcessor(cookies))
    try:
        with opener.open(request, timeout=20) as response:
            if not 200 <= response.status < 300:
                raise DevelopmentError(f"{label} returned HTTP {response.status}")
            decoded = json.loads(response.read().decode("utf-8"))
    except DevelopmentError:
        raise
    except (OSError, ValueError, HTTPError, URLError) as exc:
        raise DevelopmentError(f"{label} failed") from exc
    if not isinstance(decoded, dict):
        raise DevelopmentError(f"{label} returned an invalid JSON object")
    return decoded


def test_account_id(values: Mapping[str, str], username: str, password: str) -> int:
    cookies = CookieJar()
    browser_api_json(values, "/api/v1/auth/login", "POST", {"username": username, "password": password}, cookies, f"test account login for {username}")
    response = browser_api_json(values, "/api/v1/users/me", "GET", {}, cookies, f"test account identity for {username}")
    data = response.get("data")
    if not isinstance(data, dict) or not isinstance(data.get("id"), int) or data["id"] <= 0:
        raise DevelopmentError(f"test account identity for {username} returned an invalid user")
    return int(data["id"])


def promote_test_account_via_api(values: Mapping[str, str], admin_username: str, target_username: str, password: str) -> None:
    target_id = test_account_id(values, target_username, password)
    cookies = CookieJar()
    browser_api_json(values, "/api/v1/auth/login", "POST", {"username": admin_username, "password": password}, cookies, "test browser administrator login")
    response = browser_api_json(values, f"/api/v1/admin/users/{target_id}/role", "PUT", {"role": "super_admin"}, cookies, "test browser demotion-role setup")
    data = response.get("data")
    if not isinstance(data, dict) or not isinstance(data.get("user"), dict) or data["user"].get("role") != "super_admin":
        raise DevelopmentError("test browser demotion-role setup returned an invalid role")


def start_e2e_test_processes(root: Path, workspace: Workspace, state: dict[str, object], values: dict[str, str], observe: bool) -> None:
    """Start the source services and Vite processes used by a browser scope."""
    one_shot_env = dict(values)
    one_shot_env["LOG_MONITOR_URL"] = ""
    one_shot_env["LOG_MONITOR_INGEST_TOKEN"] = ""
    run_command(["go", "run", "./cmd/migrate", "up"], cwd=root / "backend", env=one_shot_env, label="test database migration")
    run_command(["go", "run", "./cmd/search-reindex", "--if-missing"], cwd=root / "backend", env=one_shot_env, label="test search reindex")

    binary_digest = source_digest(root, observe=False)
    backend = build_binary(workspace, values, "e2e-backend", "./cmd/server", binary_digest)
    if not observe:
        worker = build_binary(workspace, values, "e2e-business-worker", "./cmd/business-worker", binary_digest)
        indexer = build_binary(workspace, values, "e2e-search-indexer", "./cmd/search-indexer", binary_digest)

    if observe:
        router_digest = source_digest(root, observe=True)
        router = workspace.private_root / "bin" / "e2e-router"
        marshaller = workspace.private_root / "bin" / "e2e-marshaller"
        for name, directory, package, output in [
            ("e2e-router", root / "router", "./cmd/router", router),
            ("e2e-marshaller", root / "marshaller", "./cmd/marshaller", marshaller),
        ]:
            metadata = workspace.private_root / "bin" / f"{name}.json"
            previous = json.loads(metadata.read_text(encoding="utf-8")) if metadata.is_file() else {}
            if not output.is_file() or previous.get("digest") != router_digest:
                run_command(["go", "build", "-trimpath", "-o", str(output), package], cwd=directory, env=values, label=f"build {name}")
                save_json(metadata, {"name": name, "digest": router_digest, "command": ["go", "build", "-trimpath", "-o", str(output), package]})
        router_env = dict(values)
        router_env["GOPULSE_INSTANCE_ID"] = "router-e2e"
        marshaller_env = dict(values)
        marshaller_env["GOPULSE_INSTANCE_ID"] = "marshaller-e2e"
        spawn_process(workspace, state, "router", [str(router)], router_env, root / "router", workspace.integration_state_path)
        wait_http(f"http://127.0.0.1:{values['ROUTER_HTTP_PORT']}/ready", timeout=180, token=values["ROUTER_API_TOKEN"], label="test Router", process_record=state["processes"]["router"])
        spawn_process(workspace, state, "marshaller", [str(marshaller)], marshaller_env, root / "marshaller", workspace.integration_state_path)
        wait_http(f"http://127.0.0.1:{values['MARSHALLER_HTTP_PORT']}/ready", timeout=180, token=values["MARSHALLER_API_TOKEN"], label="test Marshaller", process_record=state["processes"]["marshaller"])

    backend_env = dict(values)
    backend_env["GOPULSE_INSTANCE_ID"] = "backend-e2e"
    spawn_process(workspace, state, "backend", [str(backend)], backend_env, root / "backend", workspace.integration_state_path)
    wait_http(f"http://127.0.0.1:{values['HTTP_PORT']}/ready", timeout=180, label="test Backend", process_record=state["processes"]["backend"])

    if not observe:
        worker_env = dict(values)
        worker_env["GOPULSE_INSTANCE_ID"] = "business-worker-e2e"
        spawn_process(workspace, state, "business-worker", [str(worker)], worker_env, root / "backend", workspace.integration_state_path)
        wait_http("http://127.0.0.1:19102/ready", timeout=180, label="test Business Worker", process_record=state["processes"]["business-worker"])
        indexer_env = dict(values)
        indexer_env["GOPULSE_INSTANCE_ID"] = "search-indexer-e2e"
        spawn_process(workspace, state, "search-indexer", [str(indexer)], indexer_env, root / "backend", workspace.integration_state_path)
        wait_http("http://127.0.0.1:19103/ready", timeout=180, label="test Search Indexer", process_record=state["processes"]["search-indexer"])
    else:
        wait_http(f"http://127.0.0.1:{values['MONITOR_HTTP_PORT']}/ready", timeout=180, token=values["MONITOR_API_TOKEN"], label="test Monitor")

    ensure_npm_dependencies(root, "frontend")
    frontend_env = dict(values)
    spawn_process(workspace, state, "frontend", ["npm", "run", "dev", "--", "--host", "127.0.0.1", "--port", values["FRONTEND_PORT"]], frontend_env, root / "frontend", workspace.integration_state_path)
    wait_http(f"http://127.0.0.1:{values['FRONTEND_PORT']}/", timeout=180, label="test user Vite", process_record=state["processes"]["frontend"])
    if observe:
        ensure_npm_dependencies(root, "admin-frontend")
        admin_env = dict(values)
        admin_env["FRONTEND_PORT"] = values["ADMIN_FRONTEND_PORT"]
        spawn_process(workspace, state, "admin-frontend", ["npm", "run", "dev", "--", "--host", "127.0.0.1", "--port", values["ADMIN_FRONTEND_PORT"]], admin_env, root / "admin-frontend", workspace.integration_state_path)
        wait_http(f"http://127.0.0.1:{values['ADMIN_FRONTEND_PORT']}/admin/", timeout=180, label="test admin Vite", process_record=state["processes"]["admin-frontend"])

    if observe:
        admin_username = f"observe_admin_{workspace.identity}"
        user_username = f"observe_user_{workspace.identity}"
        demotion_username = f"observe_demote_{workspace.identity}"
        password = "observe-browser-password-32-bytes-0123456789"
        values.update({
            "OBSERVE_ADMIN_USERNAME": admin_username,
            "OBSERVE_USER_USERNAME": user_username,
            "OBSERVE_DEMOTION_USERNAME": demotion_username,
            "OBSERVE_ADMIN_PASSWORD": password,
        })
        for username in (admin_username, user_username, demotion_username):
            register_test_account(root, values, username, password)
        admin_role = build_binary(workspace, values, "e2e-admin-role", "./cmd/admin-role", binary_digest)
        run_command([str(admin_role), "promote", "--username", admin_username], cwd=root / "backend", env=values, label="test browser admin-role bootstrap")
        promote_test_account_via_api(values, admin_username, demotion_username, password)


def cleanup_observe_admin(root: Path, workspace: Workspace, state: Mapping[str, object], values: Mapping[str, str]) -> None:
    """Remove only deterministic accounts created by the observability test."""
    usernames = [values.get("OBSERVE_ADMIN_USERNAME", "")]
    for key in ("OBSERVE_USER_USERNAME", "OBSERVE_DEMOTION_USERNAME"):
        if values.get(key):
            usernames.append(values[key])
    usernames = [username for username in usernames if username]
    if not usernames:
        return
    for username in usernames:
        if not re.fullmatch(r"observe_(?:admin|user|demote)_[0-9a-f]+", username):
            raise DevelopmentError("refusing to clean an unexpected observability test username")
    escaped = [username.replace("\\", "\\\\").replace("'", "\\'") for username in usernames]
    quoted = ",".join(f"'{username}'" for username in escaped)
    sql = " ".join([
        f"DELETE n FROM notifications AS n LEFT JOIN users AS recipient ON recipient.id=n.recipient_id LEFT JOIN users AS actor ON actor.id=n.actor_id LEFT JOIN posts AS post ON post.id=n.post_id LEFT JOIN users AS post_author ON post_author.id=post.author_id LEFT JOIN comments AS comment ON comment.id=n.comment_id LEFT JOIN users AS comment_author ON comment_author.id=comment.author_id WHERE recipient.username IN ({quoted}) OR actor.username IN ({quoted}) OR post_author.username IN ({quoted}) OR comment_author.username IN ({quoted});",
        f"DELETE b FROM post_bookmarks AS b LEFT JOIN users AS bookmark_user ON bookmark_user.id=b.user_id LEFT JOIN posts AS post ON post.id=b.post_id LEFT JOIN users AS post_author ON post_author.id=post.author_id WHERE bookmark_user.username IN ({quoted}) OR post_author.username IN ({quoted});",
        f"DELETE l FROM post_likes AS l LEFT JOIN users AS liker ON liker.id=l.user_id LEFT JOIN posts AS post ON post.id=l.post_id LEFT JOIN users AS post_author ON post_author.id=post.author_id WHERE liker.username IN ({quoted}) OR post_author.username IN ({quoted});",
        f"DELETE comment FROM comments AS comment LEFT JOIN users AS comment_author ON comment_author.id=comment.author_id LEFT JOIN posts AS post ON post.id=comment.post_id LEFT JOIN users AS post_author ON post_author.id=post.author_id WHERE comment_author.username IN ({quoted}) OR post_author.username IN ({quoted});",
        f"DELETE post FROM posts AS post INNER JOIN users AS post_author ON post_author.id=post.author_id WHERE post_author.username IN ({quoted});",
        f"DELETE FROM bootstrap_super_admin WHERE user_id IN (SELECT id FROM users WHERE username IN ({quoted}));",
        f"DELETE FROM users WHERE username IN ({quoted});",
    ])
    env_file = Path(str(state.get("env_file", "")))
    project = str(state.get("project", ""))
    raw_files = state.get("compose_files", [])
    files = [Path(str(item)) for item in raw_files] if isinstance(raw_files, list) else []
    if not project or not env_file.is_file() or not files:
        raise DevelopmentError("cannot clean observability test administrator without Compose state")
    command = compose_command(
        project, env_file, files, ["observe"],
        [
            "exec", "-T", "mysql", "mysql", "-uroot",
            f"-p{values['MYSQL_ROOT_PASSWORD']}", values["MYSQL_DATABASE"], "-e", sql,
        ],
    )
    run_command(command, cwd=root, env=dict(os.environ), label="observability test administrator cleanup")


def run_e2e(root: Path, scope: str, overrides: Mapping[str, str]) -> None:
    if scope not in {"business", "observe"}:
        raise DevelopmentError(f"unknown e2e SCOPE={scope!r}; expected business or observe")
    observe = scope == "observe"
    workspace, values, env_file = application_environment(root, "test", None, overrides)
    lifecycle = load_state(workspace)
    if lifecycle and lifecycle.get("status") == "running":
        raise DevelopmentError("cannot start browser checks while this workspace owns a development lifecycle; run make stop first")
    with integration_lock(workspace):
        check_ports(e2e_ports(values, observe))
        project, compose_files = compose_up(workspace, env_file, values, observe, project=workspace.project_test)
        source = e2e_source_digest(root, observe=observe)
        state: dict[str, object] = {
            "schema": 1, "status": "running", "workspace_root": str(workspace.root), "workspace_id": workspace.identity,
            "branch": subprocess.run(["git", "branch", "--show-current"], cwd=root, check=True, capture_output=True, text=True).stdout.strip(),
            "revision": subprocess.run(["git", "rev-parse", "HEAD"], cwd=root, check=True, capture_output=True, text=True).stdout.strip(),
            "mode": "e2e", "scope": scope, "observe": observe, "project": project,
            "compose_files": [str(path) for path in compose_files], "env_file": str(env_file), "source_digest": source,
            "compose_digest": digest_paths(root, compose_files), "monitor_image": monitor_image_tag(root) if observe else "",
            "started_at": now(), "processes": {}, "logs": {}, "browser_traces": str(root / "test-results"),
        }
        save_json(workspace.integration_state_path, state)
        try:
            start_e2e_test_processes(root, workspace, state, values, observe)
            browser_env = dict(os.environ)
            browser_env.update(values)
            browser_env["GOPULSE_BASE_URL"] = f"http://127.0.0.1:{values['FRONTEND_PORT']}"
            browser_env["GOPULSE_ACCEPTANCE_TOKEN"] = f"{workspace.identity}{int(time.time())}"
            if observe:
                browser_env.update({
                    "GOPULSE_ADMIN_USERNAME": values["OBSERVE_ADMIN_USERNAME"],
                    "GOPULSE_USER_USERNAME": values["OBSERVE_USER_USERNAME"],
                    "GOPULSE_DEMOTION_USERNAME": values["OBSERVE_DEMOTION_USERNAME"],
                    "GOPULSE_ACCEPTANCE_PASSWORD": values["OBSERVE_ADMIN_PASSWORD"],
                    "GOPULSE_OBSERVABILITY_ADMIN_USERNAME": values["OBSERVE_ADMIN_USERNAME"],
                    "GOPULSE_OBSERVABILITY_USER_USERNAME": values["OBSERVE_USER_USERNAME"],
                    "GOPULSE_OBSERVABILITY_PASSWORD": values["OBSERVE_ADMIN_PASSWORD"],
                    "GOPULSE_REDIS_PASSWORD": values["REDIS_PASSWORD"],
                })
                admin_command = ["npm", "exec", "--", "playwright", "test", "e2e/admin-frontend.spec.ts", "--grep", "same-origin paths|ordinary user|database demotion"]
                observe_command = ["npm", "exec", "--", "playwright", "test", "e2e/compose-observability.spec.ts"]
                state["browser_commands"] = [admin_command, observe_command]
                run_command(admin_command, cwd=root / "frontend", env=browser_env, label="native admin browser checks", timeout=600)
                observe_env = dict(browser_env)
                observe_env["GOPULSE_ACCEPTANCE_SCENARIO"] = "admin"
                run_command(observe_command, cwd=root / "frontend", env=observe_env, label="native observability browser checks", timeout=600)
            else:
                browser_command = ["npm", "exec", "--", "playwright", "test", "e2e/business.spec.ts", "e2e/compose-business.spec.ts", "--grep-invert", "search-rebuild|search-live"]
                state["browser_commands"] = [browser_command]
                browser_env["GOPULSE_ACCEPTANCE_SCENARIO"] = "business"
                run_command(browser_command, cwd=root / "frontend", env=browser_env, label="native business browser checks", timeout=600)
            state["status"] = "passed"
            print(f"[gopulse] e2e scope={scope} passed for test project {project}")
        except BaseException:
            state["status"] = "failed"
            raise
        finally:
            for record in list(state.get("processes", {}).values()):
                if isinstance(record, dict):
                    terminate_process(record)
            try:
                if observe:
                    cleanup_observe_admin(root, workspace, state, values)
            except BaseException:
                state["status"] = "failed"
                raise
            finally:
                compose_down(workspace, state)
                state["stopped_at"] = now()
                save_json(workspace.integration_state_path, state)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    # The development, observation, stop, and integration lifecycles now run in
    # devtools/cmd/devenv; this module keeps the isolated browser lifecycle.
    e2e = subparsers.add_parser("e2e")
    e2e.add_argument("--scope", default="business")
    e2e.add_argument("--http-port", type=port_argument)
    e2e.add_argument("--frontend-port", type=port_argument)
    e2e.add_argument("--admin-frontend-port", type=port_argument)
    e2e.set_defaults(handler=lambda args: run_e2e(Path.cwd(), args.scope, {
        key: value for key, value in {
            "HTTP_PORT": args.http_port,
            "FRONTEND_PORT": args.frontend_port,
            "ADMIN_FRONTEND_PORT": args.admin_frontend_port,
        }.items() if value is not None
    }))
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        args.handler(args)
    except DevelopmentError as exc:
        print(f"[gopulse] ERROR: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("[gopulse] interrupted", file=sys.stderr)
        return 130
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
