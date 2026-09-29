#!/usr/bin/env python3
"""Run the Phase 19 acceptance path with private, owned inputs.

The formal command deliberately exposes only ``--manifest`` and ``--work``.
The checked-in profile, Compose file, runtime contract, recipe descriptor, and
all load parameters are resolved by this module. ``--preflight`` uses a
private, short in-memory profile and never writes formal capacity evidence.
"""

from __future__ import annotations

import argparse
import copy
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
import threading
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
    validate_evidence,
)
from phase19_sampler import Sampler, load_samples, metric_sum, sha256_file, summarize_samples


ROOT = Path(__file__).resolve().parents[2]
PROFILE_PATH = ROOT / "loadtest/capacity-profile.json"
COMPOSE_PATH = ROOT / "deploy/compose.yaml"
RUNTIME_CONTRACT_PATH = ROOT / "deploy/runtime-contracts.json"
PROJECT_PATTERN = re.compile(r"^gopulse-p19-[0-9a-f]{12}$")
REVISION_PATTERN = re.compile(r"^[0-9a-f]{40}$")
VERSION_PATTERN = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
COMPOSE_VERSION_PATTERN = re.compile(r"(?:v|version\s+v)?([0-9]+)\.([0-9]+)(?:\.([0-9]+))?", re.IGNORECASE)
GIB = 1024**3
EXPECTED_COUNTS = {
    "users": 5000, "posts": 50000, "comments": 100000,
    "post_likes": 200000, "user_follows": 200000,
    "post_bookmarks": 25000, "business_outbox": 550000,
    "notifications": 500000,
}


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
    if "cleanup" in text:
        return "unsafe_cleanup"
    return "profile_hard_error"


def command(args, timeout=300, env=None):
    return subprocess.run(args, text=True, capture_output=True, timeout=timeout, env=env)


def require(result, operation):
    if result.returncode:
        detail = (result.stderr or result.stdout or "").strip()
        raise RuntimeError(operation + (": " + detail[-700:] if detail else ""))
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


def write_secret(path, value):
    path = Path(path)
    path.write_text(str(value) + "\n", encoding="utf-8")
    path.chmod(0o600)
    return path


def parse_compose_version(value):
    match = COMPOSE_VERSION_PATTERN.search(value or "")
    if not match:
        raise ValueError("Docker Compose version is not parseable")
    return tuple(int(part or 0) for part in match.groups())


def candidate_binding(path, profile):
    path = Path(path)
    document = json.loads(path.read_text(encoding="utf-8"))
    version = document.get("version")
    revision = document.get("revision")
    if version != profile["target_candidate_version"] or not VERSION_PATTERN.fullmatch(str(version)) or not REVISION_PATTERN.fullmatch(str(revision)):
        raise ValueError("candidate manifest does not match the profile target")
    return {"version": version, "revision": revision, "manifest_sha256": sha256_file(path)}, document


def repository_commit():
    value = require(command(["git", "-C", str(ROOT), "rev-parse", "HEAD"], timeout=30), "resolve repository revision").strip()
    if not REVISION_PATTERN.fullmatch(value):
        raise RuntimeError("repository revision is invalid")
    return value


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
    compose = command(["docker", "compose", "version", "--format", "json"], timeout=30)
    compose_text = compose.stdout.strip() if compose.returncode == 0 else command(["docker", "compose", "version"], timeout=30).stdout.strip()
    compose_version = ""
    try:
        compose_document = json.loads(compose_text)
        compose_version = str(compose_document.get("version", ""))
    except json.JSONDecodeError:
        compose_version = compose_text
    identifiers = require(command(["docker", "ps", "-q"], timeout=30), "inspect active containers").split()
    projects = []
    if identifiers:
        inspected = json.loads(require(command(["docker", "inspect", *identifiers], timeout=30), "inspect active Compose projects"))
        projects = sorted({(item.get("Config", {}).get("Labels") or {}).get("com.docker.compose.project", "") for item in inspected} - {""})
    machine = platform.machine()
    architecture = {"x86_64": "amd64", "aarch64": "arm64"}.get(machine, machine)
    docker_arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(info.get("Architecture", ""), info.get("Architecture", ""))
    return {
        "platform": "linux/" + architecture,
        "host_os": platform.system(),
        "kernel": platform.release(),
        "cpu_count": os.cpu_count() or 0,
        "memory_bytes": _meminfo().get("MemTotal", 0),
        "swap_bytes": _meminfo().get("SwapTotal", 0),
        "disk_free_bytes": shutil.disk_usage(ROOT).free,
        "docker_server_os": info.get("OSType", ""),
        "docker_server_arch": docker_arch,
        "docker_server_version": info.get("ServerVersion", ""),
        "compose_version": compose_version,
        "compose_version_parsed": list(parse_compose_version(compose_version)),
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
    try:
        actual = tuple(host.get("compose_version_parsed") or parse_compose_version(host.get("compose_version", "")))
        minimum = tuple(int(part) for part in contract["compose_min_version"].split("."))
        if actual < minimum:
            problems.append("Docker Compose is below the supported minimum version")
    except (ValueError, TypeError):
        problems.append("Docker Compose version is missing or unparseable")
    if host.get("active_compose_projects"):
        problems.append("a competing Compose project is running")
    if problems:
        raise RuntimeError("; ".join(problems))


def resource_inventory():
    result = {}
    for key, args in (("containers", ["docker", "ps", "-aq"]), ("volumes", ["docker", "volume", "ls", "-q"]), ("networks", ["docker", "network", "ls", "-q"])):
        value = command(args, timeout=30)
        if value.returncode:
            raise RuntimeError("inspect Docker resource inventory")
        result[key] = sorted(value.stdout.split())
    return result


def project_name():
    value = "gopulse-p19-" + secrets.token_hex(6)
    if not PROJECT_PATTERN.fullmatch(value):
        raise RuntimeError("generated Compose project name is invalid")
    return value


def _compose_files(compose_file):
    if isinstance(compose_file, (list, tuple)):
        return [Path(item) for item in compose_file]
    return [Path(compose_file)]


def compose(project, env_file, compose_file, *args, timeout=600):
    command_line = ["docker", "compose", "--project-name", project, "--env-file", str(env_file)]
    for path in _compose_files(compose_file):
        command_line.extend(["-f", str(path)])
    command_line.extend(args)
    return command(command_line, timeout=timeout)


def ensure_owned_project(project, env_file, compose_file):
    if not PROJECT_PATTERN.fullmatch(project):
        raise OwnershipLost("project name is outside the Phase 19 ownership pattern")
    result = compose(project, env_file, compose_file, "ps", "-aq", timeout=30)
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
    if not PROJECT_PATTERN.fullmatch(project):
        raise UnsafeCleanup("project name is outside the Phase 19 ownership pattern")
    inspected = compose(project, env_file, compose_file, "ps", "-aq", timeout=30)
    if inspected.returncode:
        raise UnsafeCleanup("cannot inspect Compose resources before cleanup")
    identifiers = inspected.stdout.split()
    if identifiers:
        try:
            ensure_owned_project(project, env_file, compose_file)
        except OwnershipLost as error:
            raise UnsafeCleanup(str(error)) from error
    result = compose(project, env_file, compose_file, "down", "--volumes", "--remove-orphans", timeout=900)
    if result.returncode:
        raise UnsafeCleanup((result.stderr or result.stdout or "cleanup failed").strip()[-700:])
    return {"status": "passed", "project": project, "owned": bool(identifiers), "global_prune": False}


def round_env_file(round_dir):
    round_dir = Path(round_dir)
    baseline = round_dir / "baseline.env"
    return baseline if baseline.is_file() else round_dir / "candidate.env"


def synthetic_sample(sequence, interval):
    return {
        "schema": "gopulse.phase19.resources.v1", "sequence": sequence,
        "observed_at": 1000.0 + sequence * interval, "interval_seconds": interval,
        "host": {"cpu_percent": 1.0, "mem_total_bytes": 1, "mem_available_bytes": 1, "swap_total_bytes": 1, "swap_free_bytes": 1},
        "load_process": {"rss_bytes": 1, "cpu_ticks": 1, "scheduler_lag_ms": 0.1},
        "sut": {"containers": [], "cpu_percent": 1.0, "rss_bytes": 1, "saturation": {}},
        "signals": {"rabbitmq": {"ready": 0, "unacked": 0}, "mysql": {}, "kafka_lag": {"lag": 0}},
        "containers": [], "component_resources": {}, "missing_components": [], "missing_signals": [],
        "restart_count": 0, "oom_killed": 0, "links": {}, "rabbitmq": {"ready": 0, "unacked": 0}, "mysql": {}, "kafka_lag": {"lag": 0},
    }


def calibration(profile_path):
    profile, digest = load_profile(profile_path)
    interval = profile["sampling"]["interval_seconds"]
    arrival = []
    for stage in profile["stages"]:
        expected = int(round(stage["measurement_seconds"] * stage["target_rps"]))
        arrival.append({"stage": stage["name"], "target_rps": stage["target_rps"], "expected_slots": expected, "observed_slots": int(round(stage["target_rps"])), "scheduler_lag_ms": 0.0, "signals_collected": list(profile["sampling"]["required_signals"])})
    samples = [synthetic_sample(index, interval) for index in range(2)]
    return {"mode": "calibration", "formal": False, "profile_sha256": digest, "arrival": arrival, "resource_samples": samples, "writes_formal_summary": False, "capacity_status": None}


def _candidate_env(candidate_document, output, round_number, preflight=False):
    values = parse_env(ROOT / ".env.example")
    revision = candidate_document["revision"]
    values.update({
        "GOPULSE_VERSION": candidate_document["version"],
        "GOPULSE_REVISION": revision,
        "GOPULSE_IMAGE_TAG": ("phase19-preflight-" if preflight else candidate_document["version"] + "-candidate-") + revision[:12],
        "GOPULSE_RUNTIME_MODE": "container",
        "PUBLISHED_HOST": "127.0.0.1",
        "FRONTEND_PORT": str(19080 + round_number),
        "HTTP_PORT": str(19090 + round_number),
        "MYSQL_PORT": str(19306 + round_number),
    })
    for name, image in {**(candidate_document.get("images") or {}), **(candidate_document.get("third_party") or {})}.items():
        if isinstance(image, dict):
            image = image.get("ref")
        if isinstance(image, str) and image:
            values["GOPULSE_" + name.upper().replace("-", "_") + "_IMAGE"] = image
    write_env(output, values)
    return values


def compose_override(path, mysql_port):
    path = Path(path)
    path.write_text("""services:\n  mysql:\n    ports:\n      - 127.0.0.1:%d:3306\n""" % mysql_port, encoding="utf-8")
    path.chmod(0o600)
    return path


def mysql_service_address(project, env_file, compose_files):
    result = compose(project, env_file, compose_files, "ps", "-q", "mysql", timeout=30)
    require(result, "resolve owned MySQL service")
    identifiers = result.stdout.split()
    if len(identifiers) != 1:
        raise OwnershipLost("owned Compose project has no unique MySQL service")
    inspected = command(["docker", "inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", identifiers[0]], timeout=30)
    address = require(inspected, "resolve owned MySQL network address").strip()
    if not re.fullmatch(r"[0-9a-fA-F:.]+", address):
        raise OwnershipLost("owned MySQL network address is invalid")
    return address


def build_loadtest(work):
    binaries = Path(work) / "bin"
    binaries.mkdir(parents=True, exist_ok=True, mode=0o700)
    recipe = binaries / "recipe"
    load = binaries / "load"
    for target, output in (("./cmd/recipe", recipe), ("./cmd/load", load)):
        result = command(["go", "-C", str(ROOT / "loadtest"), "build", "-trimpath", "-o", str(output), target], timeout=900, env={**os.environ, "GOWORK": "off"})
        require(result, "build loadtest " + target)
        output.chmod(0o700)
    return recipe, load


def build_preflight_images(project, env_file, compose_files):
    services = ["backend", "business-worker", "search-indexer", "admin-frontend", "frontend", "router", "marshaller", "monitor", "redis-exporter"]
    # The two Node builds share the repository's npm cache mount. Build each
    # service serially so the acceptance path cannot turn a cache race or
    # host-wide concurrent build pressure into a misleading product failure.
    for service in services:
        require(compose(project, env_file, compose_files, "build", service, timeout=3600), "build local preflight image " + service)


def inspect_recipe(recipe_binary, binding, output):
    args = [str(recipe_binary), "--inspect", "--receipt", str(output), "--seed", "18002005", "--candidate-version", binding["version"], "--candidate-revision", binding["revision"], "--candidate-manifest-sha256", binding["manifest_sha256"]]
    require(command(args, timeout=300), "inspect deterministic recipe")
    return json.loads(Path(output).read_text(encoding="utf-8"))


def generate_recipe(recipe_binary, binding, environment, round_dir, mysql_port, mysql_host="127.0.0.1"):
    dsn = "{}:{}@tcp({}:{})/{}?parseTime=true&loc=UTC&timeout=5s".format(environment["MYSQL_USER"], environment["MYSQL_PASSWORD"], mysql_host, mysql_port, environment["MYSQL_DATABASE"])
    dsn_file = write_secret(Path(round_dir) / "mysql.dsn", dsn)
    password_file = write_secret(Path(round_dir) / "load-password", secrets.token_urlsafe(32))
    receipt_path = Path(round_dir) / "recipe-receipt.json"
    corpus_path = Path(round_dir) / "corpus.json"
    credentials_path = Path(round_dir) / "credentials.json"
    common = ["--seed", "18002005", "--candidate-version", binding["version"], "--candidate-revision", binding["revision"], "--candidate-manifest-sha256", binding["manifest_sha256"]]
    try:
        args = [str(recipe_binary), "--dsn-file", str(dsn_file), "--password-file", str(password_file), "--receipt", str(receipt_path), "--corpus", str(corpus_path), "--credentials", str(credentials_path), *common]
        result = command(args, timeout=7200)
        if result.returncode:
            raise RuntimeError("generate deterministic recipe: " + (result.stderr or result.stdout)[-700:])
        rejection = command([str(recipe_binary), "--dsn-file", str(dsn_file), "--password-file", str(password_file), "--receipt", str(Path(round_dir) / "reject-receipt.json"), "--corpus", str(Path(round_dir) / "reject-corpus.json"), "--credentials", str(Path(round_dir) / "reject-credentials.json"), *common], timeout=600)
        if rejection.returncode != 3:
            raise RuntimeError("non-empty recipe target was not rejected safely")
        for name in ("reject-receipt.json", "reject-corpus.json", "reject-credentials.json"):
            (Path(round_dir) / name).unlink(missing_ok=True)
    finally:
        dsn_file.unlink(missing_ok=True)
        password_file.unlink(missing_ok=True)
    receipt = json.loads(receipt_path.read_text(encoding="utf-8"))
    if receipt.get("digest") != RECIPE_DIGEST or receipt.get("candidate") != binding:
        raise RuntimeError("recipe receipt is not bound to the candidate or frozen descriptor")
    return receipt, corpus_path, credentials_path, {"exit_code": 3, "target_unchanged": True}


def _mysql_snapshot(project, env_file, compose_files):
    sql = "SELECT (SELECT COUNT(*) FROM business_outbox WHERE status IN ('pending','leased')),(SELECT COUNT(*) FROM notifications),(SELECT COUNT(*) FROM posts)"
    result = compose(project, env_file, compose_files, "exec", "-T", "mysql", "sh", "-c", 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e "$1"', "sh", sql, timeout=30)
    if result.returncode:
        raise RuntimeError("query MySQL recovery state")
    fields = result.stdout.split()
    if len(fields) != 3:
        raise RuntimeError("MySQL recovery state is incomplete")
    return {"outbox_pending": int(fields[0]), "notifications": int(fields[1]), "posts": int(fields[2])}


def _rabbit_snapshot(project, env_file, compose_files):
    result = compose(project, env_file, compose_files, "exec", "-T", "rabbitmq", "rabbitmqctl", "list_queues", "-q", "name", "messages_ready", "messages_unacknowledged", timeout=30)
    if result.returncode:
        raise RuntimeError("query RabbitMQ recovery state")
    ready = unacked = 0
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) >= 3:
            try:
                ready += int(fields[-2])
                unacked += int(fields[-1])
            except ValueError:
                continue
    return {"ready": ready, "unacked": unacked}


def _kafka_lag(project, env_file, compose_files):
    result = compose(project, env_file, compose_files, "exec", "-T", "kafka", "sh", "-c", '/opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:19092 --describe --group "$1"', "sh", "gopulse-marshaller-metrics-v1", timeout=40)
    if result.returncode:
        raise RuntimeError("query Kafka lag")
    lag = 0
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) >= 6 and fields[0].upper() != "GROUP":
            try:
                lag += int(fields[5])
            except ValueError:
                pass
    return lag


def elasticsearch_count(project, env_file, compose_files, service, alias):
    result = compose(project, env_file, compose_files, "exec", "-T", service, "curl", "-fsS", "http://127.0.0.1:9200/" + alias + "/_count", timeout=30)
    if result.returncode:
        raise RuntimeError("query Elasticsearch count")
    try:
        return int(json.loads(result.stdout)["count"])
    except (KeyError, ValueError, json.JSONDecodeError) as error:
        raise RuntimeError("Elasticsearch count is invalid") from error


def marshaller_store_counts(project, env_file, compose_files):
    script = 'wget -qO- --header="Authorization: Bearer $MARSHALLER_METRICS_TOKEN" http://127.0.0.1:19106/internal/v1/metrics'
    result = compose(project, env_file, compose_files, "exec", "-T", "marshaller", "sh", "-c", script, timeout=30)
    if result.returncode:
        raise RuntimeError("query Marshaller metrics")
    return {kind: metric_sum(result.stdout, "gopulse_marshaller_records_total", {"type": kind, "stage": "store", "result": "stored"}) for kind in ("metrics", "logs", "events")}


def async_snapshot(project, env_file, compose_files):
    mysql = _mysql_snapshot(project, env_file, compose_files)
    rabbit = _rabbit_snapshot(project, env_file, compose_files)
    return {"outbox_pending": mysql["outbox_pending"], "rabbit_ready": rabbit["ready"], "rabbit_unacked": rabbit["unacked"], "kafka_lag": _kafka_lag(project, env_file, compose_files), "notifications": mysql["notifications"], "posts": mysql["posts"]}


def observability_snapshot(project, env_file, compose_files):
    counts = marshaller_store_counts(project, env_file, compose_files)
    logs = elasticsearch_count(project, env_file, compose_files, "observability-elasticsearch", "gopulse-logs-v1-read")
    events = elasticsearch_count(project, env_file, compose_files, "observability-elasticsearch", "gopulse-events-v1-read")
    return {"metric_count": counts["metrics"], "logs_count": logs, "events_count": events, "marshaller_store_counts": counts}


def wait_initial_convergence(project, env_file, compose_files, timeout=3600):
    started = time.monotonic()
    last = None
    while time.monotonic() - started < timeout:
        try:
            state = async_snapshot(project, env_file, compose_files)
            state["search_count"] = elasticsearch_count(project, env_file, compose_files, "elasticsearch", "gopulse-post-search-v1")
            observability = observability_snapshot(project, env_file, compose_files)
            state["logs_count"] = observability["logs_count"]
            state["events_count"] = observability["events_count"]
            last = state
            if state["outbox_pending"] == 0 and state["rabbit_ready"] == 0 and state["rabbit_unacked"] == 0 and state["kafka_lag"] == 0 and state["search_count"] == EXPECTED_COUNTS["posts"] and state["notifications"] >= EXPECTED_COUNTS["notifications"]:
                return state
        except (RuntimeError, ValueError):
            pass
        time.sleep(2)
    raise RuntimeError("initial deterministic recipe convergence timed out: " + repr(last))


def _append_jsonl(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    with path.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(value, sort_keys=True) + "\n")
        stream.flush()
        os.fsync(stream.fileno())
    path.chmod(0o600)


def _recovery_receipts(project, env_file, compose_files, stage, round_dir, profile):
    async_path = Path(round_dir) / ("async-" + stage + ".jsonl")
    obs_path = Path(round_dir) / ("observability-" + stage + ".jsonl")
    first_async = async_snapshot(project, env_file, compose_files)
    first_obs = observability_snapshot(project, env_file, compose_files)
    started = time.time()
    _append_jsonl(async_path, {"schema": "gopulse.phase19.async-recovery.v1", "stage": stage, "observed_at": started, **{key: first_async[key] for key in ("outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag")}})
    _append_jsonl(obs_path, {"schema": "gopulse.phase19.observability-recovery.v1", "stage": stage, "observed_at": started, **{key: first_obs[key] for key in ("metric_count", "logs_count", "events_count", "marshaller_store_counts")}})
    deadline = time.monotonic() + min(120.0, max(5.0, profile["gates"]["asynchronous"]["max_recovery_seconds"]))
    last_async, last_obs = first_async, first_obs
    while time.monotonic() < deadline:
        time.sleep(0.5)
        last_async = async_snapshot(project, env_file, compose_files)
        last_obs = observability_snapshot(project, env_file, compose_files)
        now = time.time()
        _append_jsonl(async_path, {"schema": "gopulse.phase19.async-recovery.v1", "stage": stage, "observed_at": now, **{key: last_async[key] for key in ("outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag")}})
        _append_jsonl(obs_path, {"schema": "gopulse.phase19.observability-recovery.v1", "stage": stage, "observed_at": now, **{key: last_obs[key] for key in ("metric_count", "logs_count", "events_count", "marshaller_store_counts")}})
        async_ready = all(last_async[key] <= 0 for key in ("outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag"))
        obs_ready = last_obs["metric_count"] > first_obs["metric_count"] and last_obs["logs_count"] > first_obs["logs_count"] and last_obs["events_count"] > first_obs["events_count"]
        if async_ready and obs_ready:
            break
    async_records = load_jsonl(async_path)
    obs_records = load_jsonl(obs_path)
    async_terminal = async_records[-1]
    obs_terminal = obs_records[-1]
    return (
        {"stage": stage, "raw_path": str(Path(round_dir.name) / async_path.name), "sha256": sha256_file(async_path), "records": len(async_records), "baseline": {key: async_records[0][key] for key in ("observed_at", "outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag")}, "terminal": {key: async_terminal[key] for key in ("observed_at", "outbox_pending", "rabbit_ready", "rabbit_unacked", "kafka_lag")}, "recovery_seconds": async_terminal["observed_at"] - async_records[0]["observed_at"]},
        {"stage": stage, "raw_path": str(Path(round_dir.name) / obs_path.name), "sha256": sha256_file(obs_path), "records": len(obs_records), "baseline": {key: obs_records[0][key] for key in ("observed_at", "metric_count", "logs_count", "events_count", "marshaller_store_counts")}, "terminal": {key: obs_terminal[key] for key in ("observed_at", "metric_count", "logs_count", "events_count", "marshaller_store_counts")}, "recovery_seconds": obs_terminal["observed_at"] - obs_records[0]["observed_at"]},
    )


def load_jsonl(path):
    return [json.loads(line) for line in Path(path).read_text(encoding="utf-8").splitlines() if line.strip()]


def _placeholder_window(name, target, duration):
    return {"name": name, "target_rps": target, "duration_seconds": duration, "scheduled_slots": 0, "dropped_slots": 0, "max_schedule_lag_ms": 0, "completed_requests": 0, "achieved_rps": 0, "outcomes": {"requests": 0, "succeeded": 0, "explicit_rejects": 0, "rejected_429": 0, "rejected_503": 0, "timeouts": 0, "transport_errors": 0, "unexpected_errors": 0}, "statuses": {}, "latency": {"p50_ms": 0, "p95_ms": 0, "p99_ms": 0, "max_ms": 0}}


def placeholder_report(profile, digest, candidate, repeat, execution_status="incomplete", stop=None, completed=0):
    stages = []
    for index, stage in enumerate(profile["stages"]):
        stages.append({"name": stage["name"], "target_rps": stage["target_rps"], "status": "complete" if index < completed else "not_executed", "warmup": _placeholder_window("warmup", stage["warmup_target_rps"], stage["warmup_seconds"]), "measurement": _placeholder_window("measurement", stage["target_rps"], stage["measurement_seconds"]), "recovery": _placeholder_window("recovery", 0, stage["recovery_seconds"])})
    return {"schema_version": REPORT_SCHEMA, "profile": {"id": profile["profile_id"], "sha256": digest}, "candidate": candidate, "recipe": {"schema_version": RECIPE_SCHEMA, "seed": profile["recipe"]["seed"], "digest": profile["recipe"]["digest"]}, "repeat": {"number": repeat, "total": 3}, "execution_status": execution_status, "started_at": "1970-01-01T00:00:00Z", "finished_at": "1970-01-01T00:00:01Z", "stages": stages, "total": {"requests": 0, "succeeded": 0, "explicit_rejects": 0, "rejected_429": 0, "rejected_503": 0, "timeouts": 0, "transport_errors": 0, "unexpected_errors": 0}, "load_process": {"rss_bytes": 0, "goroutines": 0, "heap_alloc_bytes": 0}, "progress": {"path": "", "sha256": "sha256:" + "0" * 64, "records": 1}, **({"stop": stop} if stop is not None else {})}


def _write_placeholder_resources(round_dir, interval, required_components):
    raw = Path(round_dir) / "resources.raw.jsonl"
    if not raw.exists():
        record = synthetic_sample(0, interval)
        record["missing_signals"] = list(required_components)
        descriptor = os.open(raw, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            stream.write(json.dumps(record, sort_keys=True) + "\n")
    summary_path = Path(round_dir) / "resources.json"
    records = load_samples(raw)
    summary = {
        "schema": "gopulse.phase19.resources.v1",
        "records": records,
        "summary": {
            "schema": "gopulse.phase19.resources.v1",
            "samples": len(records),
            "interval_seconds": interval,
            "required_components": list(required_components),
            "missing_signals": ["summary_failed"],
        },
    }
    summary_path.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    summary_path.chmod(0o600)
    return {"raw_path": str(Path(round_dir.name) / raw.name), "summary_path": str(Path(round_dir.name) / summary_path.name), "sha256": sha256_file(raw), "records": len(records), "required_components": list(required_components)}


def _progress_reference(report, round_dir, repeat):
    if "progress" not in report:
        raise RuntimeError("load report has no progress reference")
    progress_path = Path(round_dir) / ("repeat-%02d" % repeat) / "progress.jsonl"
    report["progress"] = {"path": str(Path(round_dir.name) / ("repeat-%02d" % repeat) / progress_path.name), "sha256": sha256_file(progress_path), "records": len(load_jsonl(progress_path))}


def _run_round(profile, profile_digest, binding, candidate_document, recipe_binary, load_binary, work, repeat, formal=True):
    round_dir = Path(work) / ("round-%02d" % repeat)
    round_dir.mkdir(parents=True, exist_ok=False, mode=0o700)
    project = project_name()
    environment = _candidate_env(candidate_document, round_dir / "candidate.env", repeat, preflight=not formal)
    override = compose_override(round_dir / "compose.override.yaml", int(environment["MYSQL_PORT"]))
    compose_files = [COMPOSE_PATH, override]
    endpoint = "http://127.0.0.1:" + environment["FRONTEND_PORT"]
    started = False
    sampler = None
    process = None
    corpus = None
    credentials = None
    cleanup = None
    stage_async = {}
    stage_obs = {}
    recovery_threads = []
    recovery_errors = {}

    def collect_recovery(stage):
        try:
            async_receipt, obs_receipt = _recovery_receipts(project, round_dir / "baseline.env", compose_files, stage, round_dir, profile)
            stage_async[stage], stage_obs[stage] = async_receipt, obs_receipt
        except Exception as error:
            recovery_errors[stage] = error

    def start_recovery(stage):
        if stage in {thread.name for thread in recovery_threads}:
            return
        recovery_threads.append(threading.Thread(target=collect_recovery, args=(stage,), name=stage, daemon=True))
        recovery_threads[-1].start()

    def join_recovery():
        for thread in recovery_threads:
            thread.join()
        if recovery_errors:
            stage, error = next(iter(recovery_errors.items()))
            raise RuntimeError("recovery receipt failed for %s: %s" % (stage, error))
    try:
        require(compose(project, round_dir / "candidate.env", compose_files, "up", "-d", "--wait", "--wait-timeout", "900", timeout=1200), "start owned Compose project")
        ensure_owned_project(project, round_dir / "candidate.env", compose_files)
        started = True
        mysql_host = mysql_service_address(project, round_dir / "candidate.env", compose_files)
        receipt, corpus, credentials, rejection = generate_recipe(recipe_binary, binding, environment, round_dir, 3306, mysql_host=mysql_host)
        require(compose(project, round_dir / "candidate.env", compose_files, "run", "--rm", "--no-deps", "--entrypoint", "/usr/local/bin/search-reindex", "search-init", timeout=1800), "rebuild deterministic search projection")
        wait_initial_convergence(project, round_dir / "candidate.env", compose_files, timeout=3600 if formal else 900)
        baseline = dict(environment)
        defaults = parse_env(ROOT / ".env.example")
        for key in ("OUTBOX_POLL_INTERVAL", "OUTBOX_CLAIM_BATCH", "OUTBOX_LEASE_DURATION", "BUSINESS_WORKER_PREFETCH", "SEARCH_INDEXER_PREFETCH"):
            baseline[key] = defaults[key]
        write_env(round_dir / "baseline.env", baseline)
        require(compose(project, round_dir / "baseline.env", compose_files, "up", "-d", "--force-recreate", "--wait", "--wait-timeout", "900", "backend", "backend-2", "business-worker", "business-worker-2", "search-indexer", "search-indexer-2", timeout=1200), "restore baseline runtime parameters")
        observability_snapshot(project, round_dir / "baseline.env", compose_files)

        raw_path = round_dir / "resources.raw.jsonl"
        sampler = Sampler(project, compose_files, round_dir / "baseline.env", interval=profile["sampling"]["interval_seconds"], raw_path=raw_path, required_components=profile["sampling"]["required_components"])
        sampler.start()
        candidate_manifest = Path(work) / ("preflight-manifest.json" if not formal else "candidate-manifest.json")
        process = subprocess.Popen([
            str(load_binary), "--profile", str(PROFILE_PATH if formal else Path(work) / "preflight-profile.json"), "--base-url", endpoint,
            "--corpus", str(corpus), "--credentials", str(credentials), "--candidate-manifest", str(candidate_manifest),
            "--workdir", str(round_dir), "--repeat", str(repeat),
        ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        sampler.set_load_pid(process.pid)
        progress_path = round_dir / ("repeat-%02d" % repeat) / "progress.jsonl"
        cursor = 0
        stage_seen = set()
        while process.poll() is None:
            if progress_path.exists():
                lines = progress_path.read_text(encoding="utf-8").splitlines()
                for line in lines[cursor:]:
                    if not line.strip():
                        continue
                    event = json.loads(line)
                    cursor += 1
                    if event.get("event") != "run_finished":
                        sampler.record_progress(event)
                    if event.get("event") == "recovery_started" and event.get("stage") not in stage_seen:
                        stage_seen.add(event["stage"])
                        start_recovery(event["stage"])
            time.sleep(0.2)
        sampler.stop()
        stdout, stderr = process.communicate(timeout=30)
        if progress_path.exists():
            lines = progress_path.read_text(encoding="utf-8").splitlines()
            for line in lines[cursor:]:
                if line.strip():
                    event = json.loads(line)
                    if event.get("event") != "run_finished":
                        sampler.record_progress(event)
                    if event.get("event") == "recovery_started" and event.get("stage") not in stage_seen:
                        stage_seen.add(event["stage"])
                        start_recovery(event["stage"])
        join_recovery()
        if process.returncode:
            raise RuntimeError("load generator failed: " + (stderr or stdout)[-700:])
        _, resource_summary = summarize_samples(raw_path, round_dir / "resources.json", expected_interval=profile["sampling"]["interval_seconds"], required_components=profile["sampling"]["required_components"])
        report_path = round_dir / ("repeat-%02d" % repeat) / "load-report.json"
        report = json.loads(report_path.read_text(encoding="utf-8"))
        _progress_reference(report, round_dir, repeat)
        report["resources"] = {"raw_samples_path": str(Path(round_dir.name) / raw_path.name), "summary_path": str(Path(round_dir.name) / "resources.json"), "raw_samples_sha256": sha256_file(raw_path), "raw_sample_records": resource_summary["samples"]}
        for stage in STAGES:
            if stage not in stage_async:
                start_recovery(stage)
        join_recovery()
        corpus.unlink(missing_ok=True)
        credentials.unlink(missing_ok=True)
        cleanup = cleanup_project(project, round_env_file(round_dir), compose_files)
        return {
            "number": repeat, "project": project, "endpoint": {"base_url": endpoint, "port": int(environment["FRONTEND_PORT"])}, "execution_status": "complete", "recipe": {"receipt": receipt, "nonempty_rejection": rejection}, "load_report": report,
            "resources": {"raw_path": str(Path(round_dir.name) / raw_path.name), "summary_path": str(Path(round_dir.name) / "resources.json"), "sha256": sha256_file(raw_path), "records": resource_summary["samples"], "required_components": list(profile["sampling"]["required_components"])},
            "asynchronous": [stage_async[stage] for stage in STAGES], "observability": [stage_obs[stage] for stage in STAGES], "cleanup": cleanup, "stop": None,
        }
    except (OwnershipLost, UnsafeCleanup) as error:
        reason = "ownership_lost" if isinstance(error, OwnershipLost) else "unsafe_cleanup"
        detail = str(error)
    except Exception as error:
        reason, detail = failure_reason(error), str(error)
    finally:
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
                detail = (detail + "; sampler: " + str(error))[-700:]
        if corpus is not None:
            corpus.unlink(missing_ok=True)
        if credentials is not None:
            credentials.unlink(missing_ok=True)
        if started and cleanup is None:
            try:
                cleanup = cleanup_project(project, round_env_file(round_dir), compose_files)
            except Exception as error:
                reason, detail = "unsafe_cleanup", (detail + "; cleanup: " + str(error))[-700:]
                cleanup = {"status": "failed", "project": project, "owned": False, "global_prune": False}
    required_components = profile["sampling"]["required_components"]
    if not (round_dir / "resources.raw.jsonl").exists():
        resources = _write_placeholder_resources(round_dir, profile["sampling"]["interval_seconds"], required_components)
    else:
        raw = round_dir / "resources.raw.jsonl"
        summary_path = round_dir / "resources.json"
        try:
            _, summary = summarize_samples(raw, summary_path, expected_interval=profile["sampling"]["interval_seconds"], required_components=required_components)
            resources = {"raw_path": str(Path(round_dir.name) / raw.name), "summary_path": str(Path(round_dir.name) / summary_path.name), "sha256": sha256_file(raw), "records": summary["samples"], "required_components": list(required_components)}
        except Exception:
            resources = _write_placeholder_resources(round_dir, profile["sampling"]["interval_seconds"], required_components)
    stop = {"reason": reason, "stage": next((stage for stage in STAGES if stage not in stage_async), "not_executed"), "detail": detail[-700:]}
    report_path = round_dir / ("repeat-%02d" % repeat) / "load-report.json"
    if report_path.exists():
        report = json.loads(report_path.read_text(encoding="utf-8"))
        report["execution_status"] = "incomplete"
        report["stop"] = stop
        if (round_dir / ("repeat-%02d" % repeat) / "progress.jsonl").exists():
            _progress_reference(report, round_dir, repeat)
    else:
        report = placeholder_report(profile, profile_digest, {"profile_sha256": profile_digest, "binding": binding}, repeat, stop=stop)
    # A failed repetition still keeps stage-shaped raw receipts. They are
    # validated as incomplete evidence and never converted into gate booleans.
    for stage in STAGES:
        if stage not in stage_async:
            try:
                async_receipt, obs_receipt = _recovery_receipts(project, round_dir / "baseline.env", compose_files, stage, round_dir, profile) if started else (None, None)
            except Exception:
                async_receipt = obs_receipt = None
            if async_receipt is not None:
                stage_async[stage], stage_obs[stage] = async_receipt, obs_receipt
    if not stage_async:
        stage_async = {stage: {"stage": stage, "raw_path": resources["raw_path"], "sha256": resources["sha256"], "records": resources["records"], "baseline": {"observed_at": 0, "outbox_pending": 0, "rabbit_ready": 0, "rabbit_unacked": 0, "kafka_lag": 0}, "terminal": {"observed_at": 0, "outbox_pending": 0, "rabbit_ready": 0, "rabbit_unacked": 0, "kafka_lag": 0}, "recovery_seconds": 0} for stage in STAGES}
        stage_obs = {stage: {"stage": stage, "raw_path": resources["raw_path"], "sha256": resources["sha256"], "records": resources["records"], "baseline": {"observed_at": 0, "metric_count": 0, "logs_count": 0, "events_count": 0, "marshaller_store_counts": {"metrics": 0, "logs": 0, "events": 0}}, "terminal": {"observed_at": 0, "metric_count": 0, "logs_count": 0, "events_count": 0, "marshaller_store_counts": {"metrics": 0, "logs": 0, "events": 0}}, "recovery_seconds": 0} for stage in STAGES}
    if cleanup is None:
        cleanup = {"status": "failed", "project": project, "owned": False, "global_prune": False}
    return {"number": repeat, "project": project, "endpoint": {"base_url": endpoint, "port": int(environment["FRONTEND_PORT"])}, "execution_status": "incomplete", "recipe": {"receipt": {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "candidate": binding, "digest": RECIPE_DIGEST}, "nonempty_rejection": {"exit_code": 3, "target_unchanged": True}}, "load_report": report, "resources": resources, "asynchronous": [stage_async[stage] for stage in STAGES], "observability": [stage_obs[stage] for stage in STAGES], "cleanup": cleanup, "stop": stop}


def _bindings(work, load_binary, profile_path):
    work = Path(work)
    compose_binding = work / "bound-compose.yaml"
    runtime_binding = work / "bound-runtime-contracts.json"
    shutil.copyfile(COMPOSE_PATH, compose_binding)
    shutil.copyfile(RUNTIME_CONTRACT_PATH, runtime_binding)
    compose_binding.chmod(0o600)
    runtime_binding.chmod(0o600)
    return {"compose": {"path": compose_binding.name, "sha256": sha256_file(compose_binding)}, "runtime_contract": {"path": runtime_binding.name, "sha256": sha256_file(runtime_binding)}, "recipe_descriptor": {"schema_version": RECIPE_SCHEMA, "seed": 18002005, "digest": RECIPE_DIGEST}, "runner": {"source_commit": repository_commit(), "binary_sha256": sha256_file(load_binary)}}


def _document(profile, profile_digest, candidate, rounds, host, bindings):
    document = {"schema": SCHEMA, "execution_status": "complete" if all(item["execution_status"] == "complete" for item in rounds) else "incomplete", "capability_status": None, "profile": {"path": "capacity-profile.json", "id": profile["profile_id"], "sha256": profile_digest}, "candidate": candidate, "recipe": {"schema_version": RECIPE_SCHEMA, "seed": profile["recipe"]["seed"], "digest": profile["recipe"]["digest"]}, "bindings": bindings, "rounds": rounds, "cleanup": rounds[-1]["cleanup"], "capability": {"stage_gates": []}, "host": host}
    if document["execution_status"] == "complete":
        document["aggregates"] = aggregate_repetitions(rounds)
        from phase19_evidence import _stage_gate
        document["capability"]["stage_gates"] = [{"stage": name, "passed": all(_stage_gate(profile, round_value, index) for round_value in rounds)} for index, name in enumerate(STAGES)]
        document["capability_status"] = "target_met" if all(item["passed"] for item in document["capability"]["stage_gates"]) else "boundary_found"
    else:
        document["capability_status"] = "incomplete"
    return document


def _prepare_work(work, allow_existing=False):
    work = Path(work)
    if work.exists() and not allow_existing:
        raise ValueError("capacity work directory must be new")
    work.mkdir(parents=True, exist_ok=allow_existing, mode=0o700)
    work.chmod(0o700)
    lock_path = work / ".lock"
    lock = lock_path.open("x")
    lock_path.chmod(0o600)
    fcntl.flock(lock, fcntl.LOCK_EX)
    return work, lock


def _preflight_profile(profile):
    value = copy.deepcopy(profile)
    for stage in value["stages"]:
        stage["warmup_seconds"] = 1
        stage["measurement_seconds"] = 1
        stage["recovery_seconds"] = 1
    return value


def _write_profile(path, profile):
    atomic_json(path, profile, mode=0o600)


def run_formal(manifest_path, work_path):
    profile, profile_digest = load_profile(PROFILE_PATH)
    candidate, candidate_document = candidate_binding(manifest_path, profile)
    work, lock = _prepare_work(work_path)
    try:
        work.joinpath("capacity-profile.json").write_bytes(PROFILE_PATH.read_bytes())
        shutil.copyfile(manifest_path, work / "candidate-manifest.json")
        (work / "candidate-manifest.json").chmod(0o600)
        host = host_inventory()
        validate_host(host, profile)
        recipe_binary, load_binary = build_loadtest(work)
        first = inspect_recipe(recipe_binary, candidate, work / "recipe-inspect-1.json")
        second = inspect_recipe(recipe_binary, candidate, work / "recipe-inspect-2.json")
        if first["digest"] != second["digest"] or first["counts"] != second["counts"] or first["id_ranges"] != second["id_ranges"]:
            raise RuntimeError("deterministic recipe descriptor drifted")
        bindings = _bindings(work, load_binary, PROFILE_PATH)
        rounds = []
        stopped = False
        stop_reason = None
        for repeat in range(1, profile["repetitions"] + 1):
            if stopped:
                rounds.append(_run_round(profile, profile_digest, candidate, candidate_document, recipe_binary, load_binary, work, repeat, formal=True))
                continue
            result = _run_round(profile, profile_digest, candidate, candidate_document, recipe_binary, load_binary, work, repeat, formal=True)
            rounds.append(result)
            if result["execution_status"] == "incomplete":
                stopped = True
                stop_reason = result["stop"]
        document = _document(profile, profile_digest, candidate, rounds, host, bindings)
        evidence_path = work / "capacity-evidence.json"
        atomic_json(evidence_path, document)
        validate_evidence(document, work)
        print(json.dumps({"execution_status": document["execution_status"], "capability_status": document["capability_status"], "evidence": str(evidence_path)}, sort_keys=True))
        return 0 if document["execution_status"] == "complete" else 2
    finally:
        lock.close()


def run_preflight(work_path):
    profile, formal_digest = load_profile(PROFILE_PATH)
    host = host_inventory()
    validate_host(host, profile)
    work, lock = _prepare_work(work_path)
    try:
        preflight = _preflight_profile(profile)
        preflight_path = work / "preflight-profile.json"
        _write_profile(preflight_path, preflight)
        work.joinpath("capacity-profile.json").write_bytes(PROFILE_PATH.read_bytes())
        revision = repository_commit()
        manifest_path = work / "preflight-manifest.json"
        manifest_path.write_text(json.dumps({"version": profile["target_candidate_version"], "revision": revision}, indent=2) + "\n", encoding="utf-8")
        manifest_path.chmod(0o600)
        candidate, candidate_document = candidate_binding(manifest_path, profile)
        recipe_binary, load_binary = build_loadtest(work)
        first = inspect_recipe(recipe_binary, candidate, work / "recipe-inspect-1.json")
        second = inspect_recipe(recipe_binary, candidate, work / "recipe-inspect-2.json")
        if first["digest"] != second["digest"] or first["counts"] != second["counts"] or first["id_ranges"] != second["id_ranges"]:
            raise RuntimeError("preflight recipe descriptor drifted")
        build_env = _candidate_env(candidate_document, work / "preflight-build.env", 0, preflight=True)
        build_override = compose_override(work / "preflight-build.override.yaml", 19300)
        build_preflight_images(project_name(), work / "preflight-build.env", [COMPOSE_PATH, build_override])
        bindings = _bindings(work, load_binary, preflight_path)
        before = resource_inventory()
        rounds = []
        for repeat in range(1, 4):
            result = _run_round(preflight, sha256_file(preflight_path), candidate, candidate_document, recipe_binary, load_binary, work, repeat, formal=False)
            rounds.append(result)
            if result["execution_status"] != "complete":
                break
        after = resource_inventory()
        strict_validation = {"status": "not_run", "reason": "preflight_incomplete"}
        validation_error = None
        if len(rounds) == 3 and all(round_value["execution_status"] == "complete" for round_value in rounds):
            preflight_evidence = _document(preflight, sha256_file(preflight_path), candidate, rounds, host, bindings)
            preflight_evidence["profile"]["path"] = preflight_path.name
            try:
                validate_evidence(preflight_evidence, work)
                strict_validation = {"status": "passed", "formal": False, "capacity_status": None}
            except Exception as error:
                validation_error = error
                strict_validation = {"status": "failed", "formal": False, "capacity_status": None, "detail": str(error)}
        result = {"mode": "preflight", "formal": False, "candidate": candidate, "profile_sha256": formal_digest, "preflight_profile_sha256": sha256_file(preflight_path), "compose_sha256": sha256_file(COMPOSE_PATH), "runtime_contract_sha256": sha256_file(RUNTIME_CONTRACT_PATH), "bindings": bindings, "rounds": rounds, "resource_inventory_before": before, "resource_inventory_after": after, "resource_inventory_unchanged": before == after, "strict_validation": strict_validation, "writes_formal_summary": False, "capacity_status": None}
        atomic_json(work / "preflight.json", result)
        if validation_error is not None:
            raise RuntimeError("strict preflight validation failed: " + str(validation_error))
        if before != after or any(round_value["execution_status"] != "complete" for round_value in rounds):
            raise RuntimeError("real preflight did not complete its owned rounds and cleanup")
        print(json.dumps({"formal": False, "capacity_status": None, "preflight": str(work / "preflight.json"), "writes_formal_summary": False}, sort_keys=True))
        return 0
    finally:
        lock.close()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--calibration", action="store_true")
    parser.add_argument("--preflight", action="store_true")
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--work", type=Path)
    args = parser.parse_args(argv)
    if args.calibration:
        if args.preflight or args.manifest is not None or args.work is not None:
            parser.error("calibration does not accept execution arguments")
        print(json.dumps(calibration(PROFILE_PATH), sort_keys=True))
        return 0
    if args.preflight:
        if args.manifest is not None or args.work is None:
            parser.error("preflight requires --work and does not accept --manifest")
        return run_preflight(args.work.resolve())
    if args.manifest is None or args.work is None:
        parser.error("formal execution requires --manifest and --work")
    return run_formal(args.manifest.resolve(), args.work.resolve())


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print("Phase 19 capacity execution incomplete: " + str(error), file=sys.stderr)
        raise SystemExit(1)
