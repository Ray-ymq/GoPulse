#!/usr/bin/env python3
"""Run the Phase 18-02 paired scaling and ownership acceptance matrix."""
from __future__ import annotations

import fcntl
import hashlib
import concurrent.futures
import json
import math
import os
import re
import secrets
import shutil
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path

from phase18_capacity import (
    LOAD_CONFIGURATION,
    build_loadtest,
    candidate_environment,
    compose_override,
    elasticsearch_count,
    generate_recipe,
    host_inventory,
    inspect_recipe,
    parse_env,
    preflight,
    require,
    resource_inventory,
    run,
    write_env,
)
from phase18_evidence import (
    SCALING_EXECUTION_ORDER,
    SCALING_MIN_RATIOS,
    atomic,
    secret_scan,
    validate_scaling,
)
from phase18_sampler import Sampler, metric_sum, sha256_file
from phase18_observers import parse_kafka_consumer_group, parse_mysql_json_object
from release_artifacts import verify_bundle

ROOT = Path(__file__).resolve().parents[2]
SCHEMA = "gopulse.phase18.scaling.v2"
BINDING_SCHEMA = "gopulse.phase18.scaling-binding.v1"
VALID_PROJECT = re.compile(r"^gopulse-p18-02-[a-z0-9-]+-[0-9a-f]{12}$")
BACKLOG_LIMIT = 5000
READINESS_BACKLOG_LIMIT = 100
RABBIT_PREFETCH = 10
INDEXER_RESUME_MESSAGES = RABBIT_PREFETCH + 1
ROUTER_MESSAGES = 100000
ROUTER_CONCURRENCY = 64
MARSHALLER_MESSAGES = 12000
MARSHALLER_CALIBRATION_MESSAGES = 50000
MARSHALLER_BACKLOG_SAFETY_SECONDS = 90
BACKEND_LOAD = {
    "virtual_users": LOAD_CONFIGURATION["virtual_users"],
    "warmup_seconds": LOAD_CONFIGURATION["warmup_seconds"],
    "steady_seconds": LOAD_CONFIGURATION["steady_seconds"],
    "burst_seconds": LOAD_CONFIGURATION["burst_seconds"],
    "steady_rps": LOAD_CONFIGURATION["steady_target_rps"],
    "burst_rps": LOAD_CONFIGURATION["burst_target_rps"],
}
BACKEND_SATURATION = {
    "virtual_users": 1024,
    "active_workers": 128,
    "warmup_seconds": 15,
    "steady_seconds": 90,
    "request_timeout_seconds": 5,
    "mode": "closed_loop",
}
BACKEND_REPLACEMENT_LOAD = {
    "virtual_users": 1024,
    "warmup_seconds": 15,
    "steady_seconds": 120,
    "burst_seconds": 30,
    "steady_rps": 150,
    "burst_rps": 300,
}
WORKER_EVENTS = ("comment.created", "post.liked", "user.followed")
INDEXER_EVENTS = ("post.created", "post.updated", "post.deleted")
REPLICA_SERVICES = {
    "backend": ("backend", "backend-2", "backend-3"),
    "business-worker": ("business-worker", "business-worker-2"),
    "search-indexer": ("search-indexer", "search-indexer-2"),
    "router": ("router", "router-2"),
    "marshaller": ("marshaller", "marshaller-2"),
}
INSTANCES = {
    "backend": ("backend-local", "backend-2", "backend-3"),
    "business-worker": ("business-worker-local", "business-worker-2"),
    "search-indexer": ("search-indexer-local", "search-indexer-2"),
    "router": ("router-local", "router-2"),
    "marshaller": ("marshaller-local", "marshaller-2"),
}
METRICS_COMMANDS = {
    "backend": 'wget -qO- --header="Authorization: Bearer $BACKEND_METRICS_TOKEN" http://127.0.0.1:19101/internal/v1/metrics',
    "business-worker": 'wget -qO- --header="Authorization: Bearer $BUSINESS_WORKER_METRICS_TOKEN" http://127.0.0.1:19102/internal/v1/metrics',
    "search-indexer": 'wget -qO- --header="Authorization: Bearer $SEARCH_INDEXER_METRICS_TOKEN" http://127.0.0.1:19103/internal/v1/metrics',
    "router": 'wget -qO- --header="Authorization: Bearer $ROUTER_METRICS_TOKEN" http://127.0.0.1:19105/internal/v1/metrics',
    "marshaller": 'wget -qO- --header="Authorization: Bearer $MARSHALLER_METRICS_TOKEN" http://127.0.0.1:19106/internal/v1/metrics',
}
COUNTER_METRICS = {
    "backend": ("gopulse_backend_http_requests_total", {}),
    "business-worker": ("gopulse_business_worker_messages_total", {"result": "success"}),
    "search-indexer": ("gopulse_search_indexer_messages_total", {"result": "success"}),
    "router": ("gopulse_router_messages_total", {"result": "produced"}),
    "marshaller": ("gopulse_marshaller_records_total", {"stage": "commit", "result": "committed"}),
}
OWNERSHIP_TESTS = {
    "outbox_ownership": ["go", "-C", "backend", "test", "-count=1", "./internal/outbox"],
    "alert_ownership": ["go", "-C", "backend", "test", "-count=1", "./internal/alert"],
    "rabbit_ack_redelivery": ["go", "-C", "backend", "test", "-count=1", "./internal/worker"],
    "kafka_rebalance_fencing": ["go", "-C", "marshaller", "test", "-count=1", "./internal/consumer"],
    "monitor_single_owner": ["go", "-C", "monitor", "test", "-count=1", "./internal/plugin"],
}


def sha256_text(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def digest_json(value) -> str:
    encoded = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return "sha256:" + hashlib.sha256(encoded).hexdigest()


def candidate_binding(manifest_path: Path):
    manifest = verify_bundle(manifest_path)
    if manifest["version"] != "2.0.2":
        raise ValueError("Phase 18-02 requires a 2.0.2 candidate manifest")
    revision = run(["git", "-C", str(ROOT), "rev-parse", "HEAD"]).stdout.strip()
    if revision != manifest["revision"]:
        raise ValueError("candidate manifest does not match the checked-out revision")
    if run(["git", "-C", str(ROOT), "status", "--porcelain"]).stdout.strip():
        raise ValueError("scaling acceptance requires a committed source tree")
    image_digests = {
        name: image["platforms"]["linux/amd64"]
        for name, image in manifest["images"].items()
    }
    plugin_digests = {
        "%s:%s:%s" % (item["id"], item["version"], item["arch"]): item["archive_sha256"]
        for item in manifest["plugins"]
    }
    return manifest, {
        "version": manifest["version"],
        "revision": manifest["revision"],
        "manifest_sha256": "sha256:" + hashlib.sha256(manifest_path.read_bytes()).hexdigest(),
        "bundle_sha256": manifest["bundle_sha256"],
        "image_digests": image_digests,
        "plugin_digests": plugin_digests,
    }


def prepare_workspace(work: Path, binding: dict):
    work.mkdir(parents=True, exist_ok=True, mode=0o700)
    if work.stat().st_mode & 0o77:
        raise ValueError("scaling work directory must be private")
    lock_path = work / ".lock"
    lock_path.touch(exist_ok=True)
    lock_path.chmod(0o600)
    lock = lock_path.open("a")
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        expected = {"schema": BINDING_SCHEMA, **binding}
        binding_path = work / "binding.json"
        if binding_path.exists():
            if json.loads(binding_path.read_text()) != expected:
                raise ValueError("scaling workspace belongs to another candidate or host")
        else:
            atomic(binding_path, expected)
        if (work / "evidence" / "scaling.json").exists():
            raise ValueError("scaling evidence already exists; refusing to overwrite completed acceptance")
        if (work / "evidence" / "qualification.json").exists():
            raise ValueError("qualification evidence already exists; use a fresh candidate workspace")
        if (work / "run-error.txt").exists() or any((work / "evidence").glob("*-pair.json")):
            raise ValueError("scaling workspace contains a previous attempt; use a fresh candidate workspace")
    except Exception:
        lock.close()
        raise
    return lock


class Project:
    def __init__(self, manifest: dict, binding: dict, work: Path, name: str, replicas: int,
                 router_access: bool = False):
        if not VALID_PROJECT.fullmatch(name):
            raise ValueError("unsafe scaling Compose project name")
        self.manifest = manifest
        self.binding = binding
        self.name = name
        self.replicas = replicas
        self.directory = work / name
        self.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.base = ROOT / "deploy" / "compose.yaml"
        self.scale = ROOT / "scripts" / "ci" / "phase18-scale.yaml"
        self.override = self.directory / "compose.override.yaml"
        self.env_file = self.directory / "candidate.env"
        self.environment = candidate_environment(manifest, self.env_file, 2)
        compose_override(self.override, int(self.environment["MYSQL_PORT"]))
        if router_access:
            marker = "\nnetworks:\n  acceptance:\n"
            base = self.override.read_text()
            if not base.endswith(marker):
                raise RuntimeError("acceptance Compose override has an unexpected shape")
            router = ("  router:\n    ports:\n      - 127.0.0.1:19091:9091\n"
                      "    networks:\n      observability:\n      acceptance:\n")
            if replicas > 1:
                router += ("  router-2:\n    ports:\n      - 127.0.0.1:19092:9091\n"
                           "    networks:\n      observability:\n        aliases: [router]\n"
                           "      acceptance:\n")
            self.override.write_text(base[:-len(marker)] + "\n" + router + marker)
        self.files = [self.base]
        if replicas > 1:
            self.files.append(self.scale)
        self.files.append(self.override)

    def command(self):
        command = ["docker", "compose", "--project-name", self.name, "--env-file", str(self.env_file)]
        for path in self.files:
            command.extend(["-f", str(path)])
        return command

    def compose(self, *args, timeout=600, input_path: Path | None = None, output_path: Path | None = None):
        if input_path is not None or output_path is not None:
            with input_path.open("rb") if input_path is not None else _null_input() as source:
                with output_path.open("wb") if output_path is not None else _null_output() as target:
                    return subprocess.run(self.command() + list(args), stdin=source, stdout=target,
                                          stderr=subprocess.PIPE, timeout=timeout)
        return subprocess.run(self.command() + list(args), text=True, capture_output=True, timeout=timeout)

    def require(self, *args, timeout=600, input_path: Path | None = None, output_path: Path | None = None):
        result = self.compose(*args, timeout=timeout, input_path=input_path, output_path=output_path)
        detail = result.stderr.decode() if isinstance(result.stderr, bytes) else (result.stderr or result.stdout or "")
        if result.returncode:
            raise RuntimeError("Compose operation failed: " + detail[-400:])
        return result

    def up(self, *services, wait=True, timeout=1200, dependencies=True):
        args = ["up", "-d"]
        if not dependencies:
            args.append("--no-deps")
        if wait:
            args.extend(["--wait", "--wait-timeout", "900"])
        args.extend(services)
        self.require(*args, timeout=timeout)

    def down(self):
        self.require("down", "--volumes", "--remove-orphans", "--timeout", "30", timeout=300)


class _null_input:
    def __enter__(self):
        return subprocess.DEVNULL

    def __exit__(self, *_):
        return False


class _null_output:
    def __enter__(self):
        return subprocess.DEVNULL

    def __exit__(self, *_):
        return False


def project_name(component: str, mode: str) -> str:
    safe = component.replace("-", "-")
    return "gopulse-p18-02-%s-%s-%s" % (safe, mode, secrets.token_hex(6))


def metric_values(project: Project, component: str, services: tuple[str, ...], timeout=30) -> dict[str, str]:
    deadline = time.monotonic() + timeout
    last_error = "no attempt"
    while True:
        values = {}
        for service in services:
            result = project.compose("exec", "-T", service, "sh", "-c", METRICS_COMMANDS[component], timeout=30)
            if result.returncode:
                detail = (result.stderr or result.stdout or "").strip()[-200:]
                last_error = "%s: %s" % (service, detail or "exit %d" % result.returncode)
                break
            values[service] = result.stdout
        else:
            return values
        if time.monotonic() >= deadline:
            raise RuntimeError("read %s metrics: %s" % (component, last_error))
        time.sleep(1)


def component_counter(project: Project, component: str, services: tuple[str, ...]) -> tuple[int, dict[str, int]]:
    name, labels = COUNTER_METRICS[component]
    values = metric_values(project, component, services)
    per_service = {service: int(metric_sum(text, name, labels)) for service, text in values.items()}
    return sum(per_service.values()), per_service


def instance_counters(component: str, counts: dict[str, int]) -> dict[str, int]:
    mapping = dict(zip(REPLICA_SERVICES[component], INSTANCES[component]))
    return {mapping[service]: value for service, value in counts.items() if service in mapping}


def instance_metrics(project: Project, component: str) -> dict[str, str]:
    values = {}
    for instance in INSTANCES[component]:
        service = "backend" if instance == "backend-local" else instance
        result = project.compose("exec", "-T", service, "sh", "-c", METRICS_COMMANDS[component], timeout=30)
        if result.returncode:
            raise RuntimeError("read private metrics from " + instance)
        match = re.search(r'instance="([^"]+)"', result.stdout)
        if not match or match.group(1) != instance:
            raise RuntimeError("private metrics instance label differs for " + instance)
        values[instance] = result.stdout
    return values


def rabbit_queues(project: Project) -> dict[str, tuple[int, int]]:
    result = project.require("exec", "-T", "rabbitmq", "rabbitmqctl", "list_queues", "-q", "name",
                             "messages_ready", "messages_unacknowledged", timeout=30)
    queues = {}
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) >= 3:
            queues[fields[0]] = (int(fields[-2]), int(fields[-1]))
    return queues


def mysql(project: Project, sql: str) -> str:
    result = project.require(
        "exec", "-T", "mysql", "sh", "-c",
        'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e "$1"',
        "sh", sql, timeout=60,
    )
    return result.stdout


def mysql_exec(project: Project, sql: str) -> None:
    project.require(
        "exec", "-T", "mysql", "sh", "-c",
        'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e "$1"',
        "sh", sql, timeout=120,
    )


def dump_database(project: Project, output: Path) -> None:
    project.require(
        "exec", "-T", "mysql", "sh", "-c",
        'MYSQL_PWD="$MYSQL_PASSWORD" mysqldump -u"$MYSQL_USER" --single-transaction --routines --triggers "$MYSQL_DATABASE"',
        timeout=3600, output_path=output,
    )
    output.chmod(0o600)


def restore_database(project: Project, snapshot: Path) -> None:
    project.require(
        "exec", "-T", "mysql", "sh", "-c",
        'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" "$MYSQL_DATABASE"',
        timeout=3600, input_path=snapshot,
    )


def start_database(project: Project) -> None:
    project.up("mysql", timeout=900)


def prune_backlog(project: Project, component: str, limit: int | None = None) -> tuple[int, str]:
    limit = BACKLOG_LIMIT if limit is None else limit
    if limit < 1:
        raise ValueError("fixed backlog limit must be positive")
    events = WORKER_EVENTS if component == "business-worker" else INDEXER_EVENTS
    quoted = ",".join("'%s'" % value for value in events)
    wait_outbox_empty(project)
    current = int(mysql(project, "SELECT COUNT(*) FROM business_outbox WHERE status IN ('pending','leased') AND event_type IN (%s)" % quoted).strip())
    if current != 0:
        raise RuntimeError(component + " Outbox did not settle before fixed backlog setup")
    selected = mysql(project, "SELECT id,event_id,event_type FROM business_outbox WHERE status='published' AND event_type IN (%s) ORDER BY id LIMIT %d" % (quoted, limit)).strip()
    rows = [line.split() for line in selected.splitlines() if line.strip()]
    if (len(rows) != limit or any(len(row) != 3 for row in rows)
            or any(not row[0].isdigit() or not row[1] or row[2] not in events for row in rows)):
        raise RuntimeError(component + " seed does not contain the fixed published backlog")
    row_ids = ",".join(row[0] for row in rows)
    if component == "business-worker":
        mysql_exec(project, "DELETE FROM notifications WHERE source_event_id IN (SELECT event_id FROM (SELECT event_id FROM business_outbox WHERE id IN (%s)) AS selected_events)" % row_ids)
    mysql_exec(
        project,
        "UPDATE business_outbox SET status='pending',available_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 10 MINUTE),"
        "attempt_count=0,lease_owner=NULL,lease_expires_at=NULL,published_at=NULL,last_error=NULL,"
        "updated_at=UTC_TIMESTAMP(6) WHERE status='published' AND id IN (%s)" % row_ids,
    )
    staged = int(mysql(
        project,
        "SELECT COUNT(*) FROM business_outbox WHERE status='pending' "
        "AND available_at>UTC_TIMESTAMP(6) AND id IN (%s)" % row_ids,
    ).strip())
    if staged != limit:
        raise RuntimeError(component + " fixed backlog could not be staged atomically")
    mysql_exec(
        project,
        "UPDATE business_outbox SET available_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6) "
        "WHERE status='pending' AND id IN (%s)" % row_ids,
    )
    return limit, "sha256:" + sha256_text("\n".join(" ".join(row) for row in rows))


def queue_totals(project: Project, queue: str) -> tuple[int, int]:
    ready, unacked = rabbit_queues(project).get(queue, (0, 0))
    return ready, unacked


def write_observation(path: Path, value) -> None:
    atomic(path, value)


class ObservationJournal:
    """Atomically retain sanitized setup and samples before gate assertions."""

    def __init__(self, path: Path, initial: dict):
        self.path = path
        self.document = {
            "schema": "gopulse.phase18.observation-journal.v1",
            "status": "running",
            "started_at": time.time(),
            "samples": [],
            **initial,
        }
        self.flush()

    def flush(self):
        atomic(self.path, self.document)

    def update(self, **values):
        self.document.update(values)
        self.flush()

    def append(self, sample: dict):
        self.document["samples"].append(sample)
        self.flush()

    def finish(self, status: str, **values):
        self.document.update(values)
        self.document["status"] = status
        self.document["finished_at"] = time.time()
        self.flush()


def rabbit_timeline_sample(project: Project, component: str, queue: str,
                           services: tuple[str, ...], started_at: float) -> dict:
    record = {
        "relative_seconds": round(time.monotonic() - started_at, 3),
        "observed_at": time.time(),
        "sources": {},
    }
    try:
        source_started = time.monotonic()
        stats = rabbit_queue_stats(project, queue)
        record["queue"] = stats
        record["sources"]["rabbitmq_management"] = {
            "status": "observed",
            "observed_after_seconds": round(time.monotonic() - source_started, 3),
        }
    except Exception as error:
        record["sources"]["rabbitmq_management"] = {
            "status": "parse_failed", "reason_code": type(error).__name__,
            "reason_sha256": sha256_text(str(error)),
        }
        record["sample_status"] = "acceptance_infrastructure_failure"
        return record

    try:
        source_started = time.monotonic()
        total, counters = component_counter(project, component, services)
        record["component_counters"] = {"total": total, "services": counters}
        record["sources"]["component_metrics"] = {
            "status": "observed",
            "observed_after_seconds": round(time.monotonic() - source_started, 3),
        }
    except Exception as error:
        record["sources"]["component_metrics"] = {
            "status": "parse_failed", "reason_code": type(error).__name__,
            "reason_sha256": sha256_text(str(error)),
        }
        record["sample_status"] = "acceptance_infrastructure_failure"
        return record

    try:
        source_started = time.monotonic()
        if component == "business-worker":
            effect = int(mysql(project, "SELECT COUNT(*) FROM notifications").strip())
            effect_name = "notification_rows"
        else:
            effect = elasticsearch_count(
                project.env_file, project.base, project.name, "gopulse-post-search-v1",
            )
            effect_name = "search_documents"
            if effect < 0:
                raise RuntimeError("search projection count is unavailable")
        record["external_effect"] = {"name": effect_name, "count": effect}
        record["sources"]["external_effect"] = {
            "status": "observed",
            "observed_after_seconds": round(time.monotonic() - source_started, 3),
        }
    except Exception as error:
        record["sources"]["external_effect"] = {
            "status": "parse_failed", "reason_code": type(error).__name__,
            "reason_sha256": sha256_text(str(error)),
        }
        record["sample_status"] = "acceptance_infrastructure_failure"
        return record

    record["sample_status"] = "observed"
    return record


def wait_outbox_empty(project: Project, timeout=1800) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = mysql(project, "SELECT COUNT(*) FROM business_outbox WHERE status IN ('pending','leased')").strip()
        if value == "0":
            return
        time.sleep(2)
    raise RuntimeError("Outbox publishing did not converge")


def wait_counter(project: Project, component: str, services: tuple[str, ...], target: int, timeout=900) -> float:
    started = time.monotonic()
    deadline = started + timeout
    while time.monotonic() < deadline:
        value, _ = component_counter(project, component, services)
        if value >= target:
            return time.monotonic() - started
        time.sleep(1)
    raise RuntimeError(component + " counter did not reach the fixed backlog")


def wait_rabbit_drain(project: Project, queue: str, timeout=1800) -> tuple[float, list[dict]]:
    started = time.monotonic()
    samples = []
    while time.monotonic() - started < timeout:
        ready, unacked = queue_totals(project, queue)
        samples.append({"at": round(time.monotonic() - started, 3), "ready": ready, "unacked": unacked})
        if ready == 0 and unacked == 0:
            return time.monotonic() - started, samples
        time.sleep(1)
    raise RuntimeError("RabbitMQ backlog did not drain")


def kafka_group_observation(project: Project, group: str,
                           topic: str = "gopulse-observability-v1") -> dict:
    result = project.compose("exec", "-T", "kafka", "sh", "-c",
                             '/opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:19092 --describe --group "$1"',
                             "sh", group, timeout=40)
    return parse_kafka_consumer_group(
        result.stdout, expected_group=group, expected_topic=topic,
        stderr=result.stderr, returncode=result.returncode,
    )


def kafka_lag(project: Project, group: str) -> int:
    observation = kafka_group_observation(project, group)
    if observation["status"] != "active" or observation["lag"] is None:
        raise RuntimeError("Kafka consumer group lag is not currently measurable")
    return observation["lag"]


def wait_kafka_lag(project: Project, group: str, target: int, timeout=1800, *,
                   topic: str = "gopulse-observability-v1", component: str | None = None,
                   services: tuple[str, ...] = (), sample_callback=None,
                   started_at: float | None = None) -> tuple[float, list[dict]]:
    started = time.monotonic() if started_at is None else started_at
    samples = []
    while time.monotonic() - started < timeout:
        group_observation = kafka_group_observation(project, group, topic)
        sample = {
            "relative_seconds": round(time.monotonic() - started, 3),
            "observed_at": time.time(),
            "sources": {"kafka_consumer_groups": {"status": group_observation["status"]}},
            "group": group_observation,
        }
        if component is not None:
            try:
                total, per_service = component_counter(project, component, services)
                sample["component_counters"] = {"total": total, "services": per_service}
                sample["sources"]["component_metrics"] = {"status": "observed"}
            except Exception as error:
                sample["sources"]["component_metrics"] = {
                    "status": "parse_failed", "reason_code": type(error).__name__,
                    "reason_sha256": sha256_text(str(error)),
                }
        samples.append(sample)
        if sample_callback is not None:
            sample_callback(sample)
        if group_observation["status"] == "active" and group_observation["lag"] == target:
            return time.monotonic() - started, samples
        time.sleep(1)
    raise RuntimeError("Kafka group lag did not converge")


def measure_kafka_window(project: Project, group: str, component: str,
                         services: tuple[str, ...], journal: ObservationJournal,
                         duration_seconds: float = 30.0,
                         minimum_samples: int = 10) -> tuple[float, list[dict]]:
    if duration_seconds < 30 or minimum_samples < 10:
        raise ValueError("Marshaller qualification windows require at least 30 seconds and 10 samples")
    started = time.monotonic()
    sample_interval = duration_seconds / minimum_samples
    samples = []
    for index in range(minimum_samples + 1):
        due = started + index * sample_interval
        remaining = due - time.monotonic()
        if remaining > 0:
            time.sleep(remaining)
        group_observation = kafka_group_observation(project, group)
        try:
            total, per_service = component_counter(project, component, services)
            counter = {"total": total, "services": per_service}
            metrics_status = "observed"
        except Exception as error:
            counter = None
            metrics_status = "parse_failed"
            metrics_error = {"reason_code": type(error).__name__, "reason_sha256": sha256_text(str(error))}
        sample = {
            "relative_seconds": round(time.monotonic() - started, 3),
            "observed_at": time.time(),
            "group": group_observation,
            "component_counters": counter,
            "sources": {
                "kafka_consumer_groups": {"status": group_observation["status"]},
                "component_metrics": {"status": metrics_status},
            },
        }
        if metrics_status == "parse_failed":
            sample["sources"]["component_metrics"].update(metrics_error)
        samples.append(sample)
        journal.append(sample)
        if group_observation["status"] != "active":
            raise RuntimeError("Marshaller consumer group became unstable during its measured window")
        if metrics_status != "observed":
            raise RuntimeError("Marshaller per-instance counters could not be sampled")
    elapsed = time.monotonic() - started
    if elapsed < duration_seconds or len(samples) < minimum_samples:
        raise RuntimeError("Marshaller measurement window is shorter than its qualification minimum")
    return elapsed, samples


def wait_kafka_group_members(project: Project, group: str, replicas: int,
                             partition_count: int, timeout: float = 120,
                             sample_callback=None) -> tuple[float, dict, list[dict]]:
    started = time.monotonic()
    samples = []
    while time.monotonic() - started < timeout:
        observation = kafka_group_observation(project, group)
        sample = {
            "relative_seconds": round(time.monotonic() - started, 3),
            "observed_at": time.time(),
            "group": observation,
            "sources": {"kafka_consumer_groups": {"status": observation["status"]}},
        }
        samples.append(sample)
        if sample_callback is not None:
            sample_callback(sample)
        owners = {item["member"] for item in observation["partitions"].values() if item["member"]}
        assigned = (
            len(observation["partitions"]) == partition_count
            and all(item["member"] for item in observation["partitions"].values())
        )
        if (observation["status"] == "active" and len(observation["members"]) == replicas
                and len(owners) == replicas and assigned):
            return time.monotonic() - started, observation, samples
        time.sleep(0.5)
    raise RuntimeError("Kafka group did not reach the requested member and partition assignment")


def concurrent_router_publish(project: Project, services: tuple[str, ...], count: int,
                              first_id: int, concurrency: int = ROUTER_CONCURRENCY,
                              allow_failures: bool = False, sampler: Sampler | None = None,
                              timeline_callback=None, sample_callback=None) -> dict:
    """Publish through managed loopback ports with one persistent-connection client."""
    if count < 1 or concurrency < 1 or concurrency > count:
        raise ValueError("invalid Router publish dimensions")
    if services not in (("router",), ("router", "router-2")):
        raise ValueError("Router publisher requires the managed Router topology")
    endpoints = ["http://127.0.0.1:19091"]
    if len(services) == 2:
        endpoints.append("http://127.0.0.1:19092")
    report_path = project.directory / (
        "router-publish-report-%d-%d.json" % (first_id, concurrency)
    )
    binary = project.directory.parent / "bin" / "router-publish"
    process = subprocess.Popen([
        str(binary), "--endpoints", ",".join(endpoints), "--report", str(report_path),
        "--count", str(count), "--concurrency", str(concurrency), "--first-id", str(first_id),
    ], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        env={**os.environ, "ROUTER_API_TOKEN": project.environment["ROUTER_API_TOKEN"]})
    if sampler is not None:
        sampler.set_load_pid(process.pid)
    timeline_samples = 0
    try:
        while process.poll() is None:
            if timeline_callback is not None:
                sample = timeline_callback()
                timeline_samples += 1
                if sample_callback is not None:
                    sample_callback(sample)
                if sample.get("sample_status") != "observed":
                    raise RuntimeError("Router workload timeline could not be parsed")
            time.sleep(1)
        stdout, stderr = process.communicate(timeout=1800)
    except BaseException:
        process.terminate()
        try:
            process.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.communicate()
        raise
    if not report_path.exists():
        raise RuntimeError("Router publisher did not write a report")
    report = json.loads(report_path.read_text())
    if (report["accepted"] + report["failed"] != count or report["accepted"] < 1
            or (not allow_failures and (process.returncode or report["failed"] != 0))):
        raise RuntimeError("Router publisher accepted %d/%d requests" % (report["accepted"], count))
    return {
        "concurrency": concurrency,
        "elapsed_seconds": report["elapsed_seconds"],
        "accepted": report["accepted"],
        "failed": report["failed"],
        "endpoints": report["endpoints"],
        "report_sha256": sha256_file(report_path),
        "report_file": report_path.name,
        "timeline_samples": timeline_samples,
    }


def router_timeline_sample(project: Project, services: tuple[str, ...],
                           started_at: float) -> dict:
    sample = {
        "relative_seconds": round(time.monotonic() - started_at, 3),
        "observed_at": time.time(),
        "sources": {},
    }
    try:
        source_started = time.monotonic()
        offsets = kafka_topic_offsets(project)
        sample["kafka_offsets"] = {str(key): value for key, value in offsets.items()}
        sample["sources"]["kafka_offsets"] = {
            "status": "observed",
            "observed_after_seconds": round(time.monotonic() - source_started, 3),
        }
    except Exception as error:
        sample["sources"]["kafka_offsets"] = {
            "status": "parse_failed", "reason_code": type(error).__name__,
            "reason_sha256": sha256_text(str(error)),
        }
        sample["sample_status"] = "acceptance_infrastructure_failure"
        return sample
    try:
        source_started = time.monotonic()
        total, per_service = component_counter(project, "router", services)
        sample["component_counters"] = {"total": total, "services": per_service}
        sample["sources"]["component_metrics"] = {
            "status": "observed",
            "observed_after_seconds": round(time.monotonic() - source_started, 3),
        }
    except Exception as error:
        sample["sources"]["component_metrics"] = {
            "status": "parse_failed", "reason_code": type(error).__name__,
            "reason_sha256": sha256_text(str(error)),
        }
        sample["sample_status"] = "acceptance_infrastructure_failure"
        return sample
    sample["sample_status"] = "observed"
    return sample


def kafka_topic_offsets(project: Project, topic: str = "gopulse-observability-v1") -> dict[int, int]:
    result = project.require(
        "exec", "-T", "kafka", "sh", "-c",
        '/opt/kafka/bin/kafka-get-offsets.sh --bootstrap-server localhost:19092 --topic "$1"',
        "sh", topic, timeout=40,
    )
    offsets = {}
    for line in result.stdout.splitlines():
        fields = line.strip().split(":")
        if not line.strip():
            continue
        if len(fields) != 3 or fields[0] != topic:
            raise RuntimeError("Kafka topic offset row is malformed")
        if not fields[1].isdigit() or not fields[2].isdigit():
            raise RuntimeError("Kafka topic offset contains a nonnumeric partition or offset")
        partition = int(fields[1])
        if partition in offsets:
            raise RuntimeError("Kafka topic offset repeats a partition")
        offsets[partition] = int(fields[2])
    if not offsets:
        raise RuntimeError("Kafka topic has no partition offsets")
    return offsets


def ensure_kafka_partitions(project: Project, topic: str = "gopulse-observability-v1",
                            minimum: int = 4) -> dict:
    if minimum < 2:
        raise ValueError("Kafka qualification requires at least two partitions")
    describe = '/opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:19092 --describe --topic "$1"'
    result = project.require("exec", "-T", "kafka", "sh", "-c", describe, "sh", topic, timeout=40)
    match = re.search(r"PartitionCount:\s*(\d+)", result.stdout)
    if not match:
        raise RuntimeError("Kafka topic partition count is not present in describe output")
    before = int(match.group(1))
    if before < minimum:
        alter = ('/opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:19092 '
                 '--alter --topic "$1" --partitions "$2"')
        project.require("exec", "-T", "kafka", "sh", "-c", alter, "sh", topic, str(minimum), timeout=60)
    deadline = time.monotonic() + 60
    after = before
    while time.monotonic() < deadline:
        result = project.require("exec", "-T", "kafka", "sh", "-c", describe, "sh", topic, timeout=40)
        match = re.search(r"PartitionCount:\s*(\d+)", result.stdout)
        if not match:
            raise RuntimeError("Kafka topic partition count became unparseable")
        after = int(match.group(1))
        if after >= minimum:
            return {"topic": topic, "before": before, "after": after, "required_minimum": minimum}
        time.sleep(0.5)
    raise RuntimeError("Kafka topic partition increase did not converge")


def kafka_committed_offsets(project: Project, group: str, topic: str = "gopulse-observability-v1",
                            allow_empty: bool = False) -> dict:
    result = project.compose(
        "exec", "-T", "kafka", "sh", "-c",
        '/opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:19092 --describe --group "$1"',
        "sh", group, timeout=40,
    )
    observation = parse_kafka_consumer_group(
        result.stdout, expected_group=group, expected_topic=topic,
        stderr=result.stderr, returncode=result.returncode,
    )
    if observation["status"] == "missing" and allow_empty:
        return {"group": group, "members": [], "partitions": {}, "committed_sum": 0,
                "status": "empty", "lag": 0}
    if observation["status"] == "empty" and allow_empty:
        return {"group": group, "members": [], "partitions": {}, "committed_sum": 0,
                "status": "empty", "lag": 0}
    if observation["status"] in {"rebalancing", "assignment_pending"}:
        raise RuntimeError("Kafka consumer group membership is not stable")
    partitions = observation["partitions"]
    if not partitions and allow_empty and observation["status"] == "empty":
        return {"group": group, "members": [], "partitions": {}, "committed_sum": 0,
                "status": "empty", "lag": 0}
    if not partitions:
        raise RuntimeError("Kafka consumer group has no committed partition observations")
    return {
        "group": group,
        "members": observation["members"],
        "partitions": partitions,
        "committed_sum": sum(max(0, item["committed"] or 0) for item in partitions.values()),
        "status": observation["status"],
        "lag": observation["lag"],
    }


def rabbit_queue_stats(project: Project, queue: str) -> dict:
    script = r'''
set -eu
queue=$1
auth=$(printf '%s:%s' "$RABBITMQ_DEFAULT_USER" "$RABBITMQ_DEFAULT_PASS" | base64 | tr -d '\n')
wget -qO- --header="Authorization: Basic $auth" "http://127.0.0.1:15672/api/queues/%2F/$queue"
'''
    result = project.require("exec", "-T", "rabbitmq", "sh", "-c", script, "sh", queue, timeout=30)
    value = json.loads(result.stdout)
    stats = value.get("message_stats") or {}
    return {
        "ready": int(value.get("messages_ready", 0)),
        "unacknowledged": int(value.get("messages_unacknowledged", 0)),
        "ack": int(stats.get("ack", 0)),
        "redeliver_get": int(stats.get("redeliver_get", 0)),
    }


def make_pair_measurement(single, multi, component: str, operation: str, replicas: int,
                          input_sha: str, configuration_sha: str, counter_source: str) -> dict:
    single_processed, single_elapsed, single_before, single_after, single_report, single_samples = single
    multi_processed, multi_elapsed, multi_before, multi_after, multi_report, multi_samples = multi
    single_rate = single_processed / single_elapsed
    multi_rate = multi_processed / multi_elapsed
    return {
        "operation": operation,
        "acceptance": "minimum_ratio" if component in SCALING_MIN_RATIOS else "characterization",
        "threshold": SCALING_MIN_RATIOS.get(component),
        "single": {
            "replicas": 1, "elapsed_seconds": single_elapsed, "processed": single_processed,
            "counter_before": single_before, "counter_after": single_after,
            "counter_source": counter_source,
            "input_sha256": input_sha, "configuration_sha256": configuration_sha,
            "report_sha256": single_report, "raw_samples_sha256": single_samples,
        },
        "multi": {
            "replicas": replicas, "elapsed_seconds": multi_elapsed, "processed": multi_processed,
            "counter_before": multi_before, "counter_after": multi_after,
            "counter_source": counter_source,
            "input_sha256": input_sha, "configuration_sha256": configuration_sha,
            "report_sha256": multi_report, "raw_samples_sha256": multi_samples,
        },
        "rates_per_second": {"single": single_rate, "multi": multi_rate},
        "ratio": multi_rate / single_rate,
    }


def record_pair_result(work: Path, binding: dict, component: str, pair: dict) -> None:
    threshold = SCALING_MIN_RATIOS.get(component)
    status = "characterized" if threshold is None else (
        "passed" if pair["ratio"] >= threshold else "failed"
    )
    atomic(work / "evidence" / (component + "-pair.json"), {
        "schema": "gopulse.phase18.scaling-pair.v2",
        "candidate": binding,
        "component": component,
        "pair": pair,
        "blocking": threshold is not None,
        "status": status,
    })
    if threshold is not None and pair["ratio"] < threshold:
        raise RuntimeError("%s measured %.4fx, below the %.1fx scaling threshold" %
                           (component, pair["ratio"], threshold))


def _safe_scenario_name(value: str) -> str:
    safe = re.sub(r"[^a-z0-9-]+", "-", value.lower()).strip("-")
    if not safe:
        raise ValueError("scenario name is empty after sanitization")
    return safe[:80]


def _scenario_attachments(work: Path, scenario_dir: Path) -> list[dict]:
    attachments = []
    for path in sorted(scenario_dir.rglob("*")):
        if not path.is_file() or path.name in {"running.json", "result.json", "failure.json"}:
            continue
        attachments.append({
            "path": path.relative_to(work / "evidence").as_posix(),
            "sha256": sha256_file(path),
        })
    return attachments


def attempt_scenario(results: dict, name: str, operation, work: Path | None = None):
    print("Phase 18-02 scenario " + name, flush=True)
    scenario_dir = None
    if work is not None:
        scenario_dir = work / "evidence" / "scenarios" / _safe_scenario_name(name)
        scenario_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        atomic(scenario_dir / "running.json", {
            "schema": "gopulse.phase18.scenario-observation.v1",
            "scenario": name,
            "status": "running",
            "started_at": time.time(),
        })
    try:
        result = operation()
        record = {"status": "passed"}
        if scenario_dir is not None:
            record["attachments"] = _scenario_attachments(work, scenario_dir)
            atomic(scenario_dir / "result.json", {
                "schema": "gopulse.phase18.scenario-result.v1",
                "scenario": name,
                **record,
                "result_sha256": digest_json(result) if isinstance(result, (dict, list)) else None,
                "completed_at": time.time(),
            })
        results[name] = record
        return result
    except Exception as error:
        record = {
            "status": "failed",
            "error_type": type(error).__name__,
            "reason_sha256": sha256_text(str(error)),
        }
        if scenario_dir is not None:
            record["attachments"] = _scenario_attachments(work, scenario_dir)
            atomic(scenario_dir / "failure.json", {
                "schema": "gopulse.phase18.scenario-failure.v1",
                "scenario": name,
                **record,
                "failed_at": time.time(),
            })
        results[name] = record
        return None


def prepare_project(manifest: dict, binding: dict, work: Path, component: str, mode: str,
                    snapshot: Path, services: tuple[str, ...]) -> Project:
    replicas = 1 if mode == "single" else len(REPLICA_SERVICES[component])
    project = Project(manifest, binding, work, project_name(component, mode), replicas,
                      router_access=component in {"router", "marshaller", "readiness"})
    try:
        start_database(project)
        restore_database(project, snapshot)
        project.up(*services, timeout=1800)
        return project
    except Exception:
        project.down()
        raise


def backend_services(replicas: int) -> tuple[str, ...]:
    services = ["frontend"]
    if replicas == 3:
        services.extend(["backend-2", "backend-3"])
    return tuple(services)


class BackendLoadSampler:
    """Record bounded host, load-process and MySQL activity during saturation."""

    def __init__(self, project: Project, pid: int, path: Path):
        self.project, self.pid, self.path = project, pid, path
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.previous_cpu = None
        self.failure = None

    def start(self):
        self.thread.start()

    def stop(self):
        self.stop_event.set()
        self.thread.join(timeout=15)
        if self.thread.is_alive():
            raise RuntimeError("Backend diagnostic sampler did not stop")
        if self.failure is not None:
            raise RuntimeError("Backend diagnostic sampler failed") from self.failure

    def run(self):
        try:
            descriptor = os.open(self.path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(descriptor, "w", encoding="utf-8") as output:
                while True:
                    output.write(json.dumps(self.sample(), sort_keys=True) + "\n")
                    output.flush()
                    if self.stop_event.wait(10):
                        return
        except Exception as error:
            self.failure = error

    def sample(self):
        record = {"observed_at": time.time(), "host_cpu_percent": None,
                  "load_process_cpu_ticks": None, "mysql_status": None}
        try:
            fields = next(line for line in Path("/proc/stat").read_text().splitlines() if line.startswith("cpu ")).split()
            ticks = [int(value) for value in fields[1:]]
            current = (sum(ticks), ticks[3] + ticks[4])
            if self.previous_cpu is not None and current[0] > self.previous_cpu[0]:
                record["host_cpu_percent"] = round(100 * (1 - (current[1] - self.previous_cpu[1]) /
                                                          (current[0] - self.previous_cpu[0])), 2)
            self.previous_cpu = current
            stat = (Path("/proc") / str(self.pid) / "stat").read_text().split()
            record["load_process_cpu_ticks"] = int(stat[13]) + int(stat[14])
        except (OSError, StopIteration, ValueError, IndexError):
            pass
        sql = "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_running','Threads_connected','Queries','Innodb_row_lock_waits','Innodb_row_lock_time')"
        script = 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e "' + sql + '"'
        try:
            result = self.project.compose("exec", "-T", "mysql", "sh", "-c", script, timeout=10)
            if result.returncode == 0:
                record["mysql_status"] = {key: int(value) for key, value in
                                          (line.split("\t") for line in result.stdout.splitlines())}
        except (subprocess.TimeoutExpired, ValueError):
            pass
        return record


def run_backend_load(project: Project, load_binary: Path, corpus: Path, credentials: Path,
                     directory: Path, replace: bool = False, saturate: bool = False) -> dict:
    if replace and saturate:
        raise ValueError("Backend replacement and saturation are separate measurements")
    per_before = component_counter(
        project, "backend", REPLICA_SERVICES["backend"][:project.replicas]
    )[1]
    report_path = directory / "load-report.json"
    diagnostic_path = directory / "load-diagnostic.json"
    started = time.monotonic()
    command = [
        str(load_binary), "--base-url", "http://127.0.0.1:" + project.environment["FRONTEND_PORT"],
        "--corpus", str(corpus), "--credentials", str(credentials), "--report", str(report_path),
        "--diagnostic-report", str(diagnostic_path),
    ]
    if saturate:
        command.extend([
            "--saturate", "--vus", str(BACKEND_SATURATION["virtual_users"]),
            "--active-workers", str(BACKEND_SATURATION["active_workers"]),
            "--warmup", str(BACKEND_SATURATION["warmup_seconds"]) + "s",
            "--steady", str(BACKEND_SATURATION["steady_seconds"]) + "s",
            "--burst", "0s", "--request-timeout",
            str(BACKEND_SATURATION["request_timeout_seconds"]) + "s",
        ])
    else:
        fixed_load = BACKEND_REPLACEMENT_LOAD if replace else BACKEND_LOAD
        command.extend([
            "--vus", str(fixed_load["virtual_users"]),
            "--warmup", str(fixed_load["warmup_seconds"]) + "s",
            "--steady", str(fixed_load["steady_seconds"]) + "s",
            "--burst", str(fixed_load["burst_seconds"]) + "s",
            "--steady-rps", str(fixed_load["steady_rps"]),
            "--burst-rps", str(fixed_load["burst_rps"]),
        ])
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    sampler_path = directory / "backend-load-samples.jsonl"
    sampler = Sampler(project.name, project.files, project.env_file, interval=1, raw_path=sampler_path)
    sampler.set_load_pid(process.pid)
    sampler.start()
    replacement = None
    replacement_journal = None
    try:
        if replace:
            if project.replicas != 3:
                raise ValueError("Backend replacement requires the three-replica topology")
            replacement_journal = ObservationJournal(directory / "replacement-observations.json", {
                "component": "backend", "counter_source": "edge_instance_http_metrics",
                "continuous_workload": "phase18_open_loop_load",
            })
            target = BACKEND_REPLACEMENT_LOAD["warmup_seconds"] + BACKEND_REPLACEMENT_LOAD["steady_seconds"] / 2
            while time.monotonic() - started < target:
                time.sleep(0.25)
            def activity():
                total, counters = component_counter(
                    project, "backend", REPLICA_SERVICES["backend"][:project.replicas],
                )
                return {
                    "status": "observed" if process.poll() is None else "load_finished",
                    "counter": total,
                    "remaining_work": 1 if process.poll() is None else 0,
                    "services": counters,
                }
            replacement = replace_component_and_observe(
                project, "backend", "backend-2", activity_probe=activity,
                journal=replacement_journal,
            )
        stdout, stderr = process.communicate(timeout=1800)
    except BaseException:
        process.terminate()
        try:
            process.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.communicate()
        raise
    finally:
        sampler.stop()
    if process.returncode:
        raise RuntimeError("Backend scaling load failed: " + (stderr or stdout)[-300:])
    report = json.loads(report_path.read_text())
    if saturate:
        phases = report["phases"]
        if ([phase["name"] for phase in phases] != ["warmup", "steady"]
                or any(phase["target_rps"] != 0 for phase in phases)
                or report["steady_target_rps"] != 0
                or report["burst_target_rps"] != 0
                or report["virtual_users"] != BACKEND_SATURATION["virtual_users"]
                or report["active_workers"] != BACKEND_SATURATION["active_workers"]):
            raise RuntimeError("Backend saturation report has an offered-rate cap or invalid phases")
        steady = phases[1]
        if steady["duration_seconds"] < BACKEND_SATURATION["steady_seconds"]:
            raise RuntimeError("Backend saturation steady window ended early")
        processed = int(steady["counts"]["succeeded"])
        measured_elapsed = float(steady["duration_seconds"])
    else:
        processed = int(report["total"]["succeeded"])
        measured_elapsed = time.monotonic() - started
    if processed < 1:
        raise RuntimeError("Backend scaling load completed no successful request")
    per_after = component_counter(
        project, "backend", REPLICA_SERVICES["backend"][:project.replicas]
    )[1]
    if replacement_journal is not None and replacement is not None:
        replacement_journal.finish(
            "passed", reason_code="edge_work_continued_through_replacement",
            replacement=replacement,
            workload_report_sha256=sha256_file(report_path),
            resource_samples_sha256=sha256_file(sampler_path),
        )
    return {
        "processed": processed,
        "elapsed_seconds": measured_elapsed,
        "counter_before": 0,
        "counter_after": processed,
        "per_instance_before": per_before,
        "per_instance_after": per_after,
        "replacement": replacement,
        "report": report,
        "report_sha256": sha256_file(report_path),
        "diagnostic_sha256": sha256_file(diagnostic_path),
        "resource_samples_sha256": sha256_file(sampler_path),
        "measurement_mode": "closed_loop" if saturate else "fixed_rate",
    }


def api_json(project: Project, cookie_jar, method: str, path: str, payload: dict | None = None):
    body = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
    request = urllib.request.Request(
        "http://127.0.0.1:%s%s" % (project.environment["FRONTEND_PORT"], path),
        data=body,
        headers={"Content-Type": "application/json"},
        method=method,
    )
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie_jar))
    try:
        with opener.open(request, timeout=10) as response:
            if response.status < 200 or response.status >= 300:
                raise RuntimeError("acceptance API returned an unexpected status")
            return json.loads(response.read())
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as error:
        raise RuntimeError("acceptance API request failed (%s)" % type(error).__name__) from error


def api_session(project: Project, user: dict, password: str):
    import http.cookiejar
    cookies = http.cookiejar.CookieJar()
    data = api_json(project, cookies, "POST", "/api/v1/auth/login", {
        "username": user["username"], "password": password,
    })
    if not isinstance(data, dict) or not isinstance(data.get("data"), dict):
        raise RuntimeError("acceptance API login response has an invalid shape")
    cookie_name = project.environment.get("AUTH_COOKIE_NAME", "gopulse_session")
    if not any(cookie.name == cookie_name and cookie.value for cookie in cookies):
        raise RuntimeError("acceptance API login omitted its session cookie")
    return cookies


def publish_fresh_posts(project: Project, credentials_path: Path, count: int,
                        include_comments: bool = False) -> dict:
    """Create a new, independently identified acceptance workload through the edge."""
    if count < 1:
        raise ValueError("fresh acceptance workload must contain at least one item")
    credentials = json.loads(credentials_path.read_text())
    users = credentials.get("users")
    password = credentials.get("password")
    if not isinstance(users, list) or len(users) < (2 if include_comments else 1) or not password:
        raise RuntimeError("acceptance credentials are incomplete")
    author = users[0]
    author_cookies = api_session(project, author, password)
    comment_cookies = api_session(project, users[1], password) if include_comments else None
    post_ids = []
    comment_ids = []
    marker = secrets.token_hex(8)
    for index in range(count):
        response = api_json(project, author_cookies, "POST", "/api/v1/posts", {
            "title": "Phase18 qualification %s %d" % (marker, index),
            "content": "fresh replacement workload %s %d" % (marker, index),
        })
        try:
            post_id = int(response["data"]["id"])
        except (KeyError, TypeError, ValueError) as error:
            raise RuntimeError("acceptance post response has no valid ID") from error
        if post_id < 1:
            raise RuntimeError("acceptance post response has no valid ID")
        post_ids.append(post_id)
        if include_comments:
            result = api_json(project, comment_cookies, "POST",
                              "/api/v1/posts/%d/comments" % post_id,
                              {"content": "fresh qualification comment %s %d" % (marker, index)})
            try:
                comment_id = int(result["data"]["id"])
            except (KeyError, TypeError, ValueError) as error:
                raise RuntimeError("acceptance comment response has no valid ID") from error
            if comment_id < 1:
                raise RuntimeError("acceptance comment response has no valid ID")
            comment_ids.append(comment_id)
    return {"post_ids": post_ids, "comment_ids": comment_ids, "marker_sha256": sha256_text(marker)}


def outbox_rows_for_ids(project: Project, event_type: str, field: str, identifiers: list[int],
                        timeout: float = 180) -> list[dict]:
    if event_type not in {"post.created", "comment.created"} or field not in {"post_id", "comment_id"}:
        raise ValueError("unsupported fresh-work outbox query")
    id_list = ",".join(str(int(value)) for value in identifiers)
    if not id_list:
        raise ValueError("fresh-work identifier list is empty")
    query = (
        "SELECT id,event_id,event_type,status FROM business_outbox "
        "WHERE event_type='%s' AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.%s')) IN (%s) ORDER BY id"
        % (event_type, field, id_list)
    )
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        rows = []
        raw = mysql(project, query)
        for line in raw.splitlines():
            values = line.split("\t")
            if len(values) != 4 or not values[0].isdigit() or not values[1]:
                raise RuntimeError("fresh-work Outbox row is malformed")
            rows.append({"id": int(values[0]), "event_id": values[1],
                         "event_type": values[2], "status": values[3]})
        if len(rows) == len(identifiers):
            if len({row["event_id"] for row in rows}) != len(rows):
                raise RuntimeError("fresh-work Outbox event IDs are not unique")
            if all(row["status"] == "published" for row in rows):
                return rows
        time.sleep(0.5)
    raise RuntimeError("fresh-work Outbox events were not published by the dispatcher")


def wait_search_documents(project: Project, post_ids: list[int], timeout: float = 180) -> None:
    ids = json.dumps([str(value) for value in post_ids], separators=(",", ":"))
    script = "curl -fsS -H 'Content-Type: application/json' -d '$1' 'http://127.0.0.1:9200/gopulse-post-search-v1/_mget'"
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        response = project.compose("exec", "-T", "elasticsearch", "sh", "-c", script,
                                   "sh", json.dumps({"ids": json.loads(ids)}), timeout=30)
        if response.returncode == 0:
            try:
                docs = json.loads(response.stdout).get("docs", [])
                if len(docs) == len(post_ids) and all(item.get("found") is True for item in docs):
                    return
            except (TypeError, ValueError, json.JSONDecodeError):
                raise RuntimeError("Search Indexer side-effect response is malformed")
        time.sleep(0.5)
    raise RuntimeError("Search Indexer did not index the fresh replacement posts")


def wait_instance_counter(project: Project, component: str, service: str,
                          target: int, timeout=120) -> tuple[int, float]:
    started = time.monotonic()
    deadline = started + timeout
    while time.monotonic() < deadline:
        _, counters = component_counter(project, component, (service,))
        current = counters[service]
        if current >= target:
            return current, time.monotonic() - started
        time.sleep(0.5)
    raise RuntimeError(component + " replacement instance did not resume work")


def search_indexer_resume_work_probe(project: Project, replacement_service: str,
                                     credentials_path: Path) -> dict:
    peers = tuple(service for service in REPLICA_SERVICES["search-indexer"]
                  if service != replacement_service)
    if len(peers) != 1:
        raise ValueError("Search Indexer resume probe requires exactly one peer")
    peer = peers[0]
    baseline = component_counter(project, "search-indexer", (replacement_service,))[1][replacement_service]
    queue = "gopulse.search-indexer.v1"
    queue_before = rabbit_queue_stats(project, queue)
    project.require("pause", peer, timeout=30)
    try:
        fresh = publish_fresh_posts(project, credentials_path, INDEXER_RESUME_MESSAGES)
        rows = outbox_rows_for_ids(project, "post.created", "post_id", fresh["post_ids"])
        deadline = time.monotonic() + 180
        resumed = baseline
        while time.monotonic() < deadline:
            resumed = component_counter(project, "search-indexer", (replacement_service,))[1][replacement_service]
            queue_now = rabbit_queue_stats(project, queue)
            try:
                wait_search_documents(project, fresh["post_ids"], timeout=1)
                indexed = len(fresh["post_ids"])
            except RuntimeError:
                indexed = 0
            if resumed >= baseline + len(fresh["post_ids"]) and indexed == len(fresh["post_ids"]):
                break
            time.sleep(0.5)
        if resumed < baseline + len(fresh["post_ids"]):
            raise RuntimeError("replacement Search Indexer did not handle every new event")
        wait_search_documents(project, fresh["post_ids"])
        queue_after = rabbit_queue_stats(project, queue)
        if queue_after["ack"] - queue_before["ack"] < len(rows):
            raise RuntimeError("fresh Search Indexer events were not acknowledged")
        return {
            "published_messages": len(rows),
            "event_ids_sha256": sha256_text("\n".join(sorted(row["event_id"] for row in rows))),
            "fresh_post_ids_sha256": sha256_text("\n".join(map(str, sorted(fresh["post_ids"])))),
            "published_outbox_messages": sum(row["status"] == "published" for row in rows),
            "acknowledged_messages": queue_after["ack"] - queue_before["ack"],
            "indexed_documents": len(fresh["post_ids"]),
            "replacement_service": replacement_service,
            "peer_service": peer,
            "replacement_counter_before": baseline,
            "replacement_counter_after": resumed,
            "queue_before": queue_before,
            "queue_after": queue_after,
        }
    finally:
        project.require("unpause", peer, timeout=30)


def business_worker_resume_work_probe(project: Project, replacement_service: str,
                                      credentials_path: Path) -> dict:
    peers = tuple(service for service in REPLICA_SERVICES["business-worker"]
                  if service != replacement_service)
    if len(peers) != 1:
        raise ValueError("Business Worker resume probe requires exactly one peer")
    peer = peers[0]
    baseline = component_counter(project, "business-worker", (replacement_service,))[1][replacement_service]
    queue = "gopulse.business-worker.v1"
    queue_before = rabbit_queue_stats(project, queue)
    project.require("pause", peer, timeout=30)
    try:
        fresh = publish_fresh_posts(project, credentials_path, INDEXER_RESUME_MESSAGES, include_comments=True)
        rows = outbox_rows_for_ids(project, "comment.created", "comment_id", fresh["comment_ids"])
        event_ids = [row["event_id"] for row in rows]
        quoted = ",".join("'" + value.replace("'", "") + "'" for value in event_ids)
        if len(quoted) == 0:
            raise RuntimeError("fresh Business Worker event set is empty")
        notification_query = (
            "SELECT COUNT(*),COUNT(DISTINCT source_event_id) FROM notifications "
            "WHERE source_event_id IN (%s)" % quoted
        )
        deadline = time.monotonic() + 180
        observed = (0, 0)
        per_service = {replacement_service: baseline}
        while time.monotonic() < deadline:
            raw = mysql(project, notification_query).strip().split("\t")
            if len(raw) != 2 or any(not value.isdigit() for value in raw):
                raise RuntimeError("fresh Business Worker side-effect query is malformed")
            observed = (int(raw[0]), int(raw[1]))
            per_service = component_counter(project, "business-worker", (replacement_service,))[1]
            queue_after = rabbit_queue_stats(project, queue)
            if (observed == (len(rows), len(rows))
                    and per_service[replacement_service] >= baseline + len(rows)
                    and queue_after["ack"] - queue_before["ack"] >= len(rows)):
                break
            time.sleep(0.5)
        if observed != (len(rows), len(rows)):
            raise RuntimeError("fresh Business Worker events lack one-to-one notification side effects")
        if per_service[replacement_service] < baseline + len(rows):
            raise RuntimeError("replacement Business Worker did not process every fresh event")
        queue_after = rabbit_queue_stats(project, queue)
        if queue_after["ack"] - queue_before["ack"] < len(rows):
            raise RuntimeError("fresh Business Worker events were not acknowledged")
        return {
            "published_messages": len(rows),
            "event_ids_sha256": sha256_text("\n".join(sorted(event_ids))),
            "published_outbox_messages": sum(row["status"] == "published" for row in rows),
            "acknowledged_messages": queue_after["ack"] - queue_before["ack"],
            "notifications": observed[0],
            "distinct_source_events": observed[1],
            "replacement_service": replacement_service,
            "peer_service": peer,
            "replacement_counter_before": baseline,
            "replacement_counter_after": per_service[replacement_service],
            "queue_before": queue_before,
            "queue_after": queue_after,
        }
    finally:
        project.require("unpause", peer, timeout=30)


def replace_component_and_observe(project: Project, component: str, service: str,
                                 require_resume: bool = True, force_kill: bool = False,
                                 resume_work_probe=None, activity_probe=None,
                                 journal: ObservationJournal | None = None) -> dict:
    services = REPLICA_SERVICES[component][:project.replicas]
    if service not in services:
        raise ValueError("replacement service is outside the managed topology")
    before_total, per_before = component_counter(project, component, services)
    survivors = tuple(item for item in services if item != service)
    initial_progress = {item: per_before[item] for item in services}
    if journal is not None:
        journal.update(
            status="waiting_for_live_work",
            component_counters_before={"total": before_total, "services": per_before},
            containers_before=container_state_snapshot(project, services),
        )
    active = activity_probe() if activity_probe is not None else {
        "counter": before_total, "remaining_work": 1, "status": "not_instrumented",
    }
    active_samples = []
    progress_deadline = time.monotonic() + 60
    while time.monotonic() < progress_deadline:
        current_total, current = component_counter(project, component, services)
        active = activity_probe() if activity_probe is not None else active
        active_samples.append({
            "observed_at": time.time(), "component_counters": current,
            "activity": active,
        })
        if all(current[item] > initial_progress[item] for item in services):
            break
        if activity_probe is not None and int(active.get("remaining_work", 0)) <= 0:
            raise RuntimeError("replacement_work_exhausted_before_target_progress")
        time.sleep(0.25)
    else:
        if journal is not None:
            journal.update(status="failed", reason_code="replacement_live_work_not_observed",
                           before_samples=active_samples)
        raise RuntimeError(component + " target and survivor did not both advance before removal")
    before_total = current_total
    per_before = current
    if journal is not None:
        journal.update(status="target_and_survivor_progressed_before_removal",
                       before_samples=active_samples, activity_before=active)
    stopped_at = time.time()
    project.require("kill" if force_kill else "stop", service, timeout=120)
    initial_total, initial_per_during = component_counter(project, component, survivors)
    activity_during = activity_probe() if activity_probe is not None else active
    if activity_probe is not None and int(activity_during.get("remaining_work", 0)) <= 0:
        if journal is not None:
            journal.update(status="failed", reason_code="replacement_work_exhausted_during_removal",
                           activity_during=activity_during)
        raise RuntimeError("replacement_work_exhausted_during_removal")
    deadline = time.monotonic() + 30
    during_samples = []
    while time.monotonic() < deadline:
        survivor_total, per_during = component_counter(project, component, survivors)
        activity_during = activity_probe() if activity_probe is not None else activity_during
        state = {
            "observed_at": time.time(),
            "component_counters": per_during,
            "activity": activity_during,
            "containers": container_state_snapshot(project, services),
        }
        during_samples.append(state)
        if journal is not None:
            journal.append({"phase": "during_removal", **state})
        if activity_probe is not None and int(activity_during.get("remaining_work", 0)) <= 0:
            if journal is not None:
                journal.update(status="failed", reason_code="replacement_work_exhausted_during_removal",
                               activity_during=activity_during)
            raise RuntimeError("replacement_work_exhausted_during_removal")
        if all(per_during[item] > per_before[item] for item in survivors):
            break
        time.sleep(0.25)
    else:
        if journal is not None:
            journal.update(status="failed", reason_code="replacement_survivor_not_progressing",
                           activity_during=activity_during, during_samples=during_samples)
        raise RuntimeError(component + " survivors did not advance while one replica was removed")
    per_during[service] = per_before[service]
    during_total = survivor_total + per_before[service]
    if journal is not None:
        journal.update(status="survivor_progressed_during_removal",
                       activity_during=activity_during,
                       component_counters_during={"total": during_total, "services": per_during},
                       during_samples=during_samples)
    restarted_at = time.time()
    project.up(service, timeout=600, dependencies=False)
    resume_work = resume_work_probe() if resume_work_probe is not None else None
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        after_total, per_after = component_counter(project, component, services)
        if not require_resume or per_after.get(service, 0) > 0:
            break
        time.sleep(1)
    else:
        if journal is not None:
            journal.update(status="failed", reason_code="replacement_instance_not_resuming",
                           resume_work=resume_work)
        raise RuntimeError(component + " replacement instance did not resume work")
    activity_after = activity_probe() if activity_probe is not None else active
    result = {
        "instance": INSTANCES[component][services.index(service)],
        "service": service,
        "before": before_total,
        "during": during_total,
        "after": after_total,
        "per_instance_before": per_before,
        "per_instance_during": per_during,
        "per_instance_after": per_after,
        "survivor_progress": survivor_total - initial_total,
        "stopped_seconds": round(restarted_at - stopped_at, 3),
        "process_started_at": restarted_at,
        "activity_before": active,
        "activity_during_removal": activity_during,
        "activity_after": activity_after,
        "active_samples": active_samples,
        "during_samples": during_samples,
        "containers_after": container_state_snapshot(project, services),
    }
    if resume_work is not None:
        result["resume_work"] = resume_work
    if journal is not None:
        journal.update(status="replacement_complete", activity_after=activity_after,
                       component_counters_after={"total": after_total, "services": per_after},
                       containers_after=result["containers_after"], resume_work=resume_work)
    return result


def container_state_snapshot(project: Project, services: tuple[str, ...]) -> dict:
    states = {}
    for service in services:
        result = project.compose("ps", "--format", "json", service, timeout=30)
        if result.returncode:
            states[service] = {"status": "unavailable", "reason_code": "compose_ps_failed"}
            continue
        rows = []
        try:
            parsed = json.loads(result.stdout)
            items = parsed if isinstance(parsed, list) else [parsed]
        except json.JSONDecodeError:
            items = []
            for line in result.stdout.splitlines():
                if not line.strip():
                    continue
                try:
                    items.append(json.loads(line))
                except json.JSONDecodeError:
                    items = []
                    break
        if not all(isinstance(item, dict) for item in items):
            items = []
        rows = [{"state": item.get("State"), "health": item.get("Health")} for item in items]
        states[service] = {"status": "observed", "containers": rows}
    return states


def rabbit_activity_probe(project: Project, queue: str) -> dict:
    stats = rabbit_queue_stats(project, queue)
    return {
        "status": "observed", "counter": stats["ack"],
        "remaining_work": stats["ready"] + stats["unacknowledged"],
        "queue": stats,
    }


def replacement_document(component: str, expected: int, completed: int,
                         replacement: dict, external_before: int, external_during: int,
                         external_after: int, counter_source: str,
                         observations_sha256: str, duplicate_side_effects: int = 0) -> dict:
    return {
        "instance": replacement["instance"],
        "accepted": expected,
        "completed": completed,
        "lost": max(0, expected - completed),
        "duplicate_side_effects": duplicate_side_effects,
        "counter_source": counter_source,
        "external_counter_before": external_before,
        "external_counter_during_removal": external_during,
        "external_counter_after": external_after,
        "instance_counters_before": instance_counters(component, replacement["per_instance_before"]),
        "instance_counters_during_removal": instance_counters(component, replacement["per_instance_during"]),
        "instance_counters_after": instance_counters(component, replacement["per_instance_after"]),
        "survivor_progress": int(replacement["survivor_progress"]),
        "stopped_seconds": replacement["stopped_seconds"],
        "observations_sha256": observations_sha256,
    }


def pair_backend(manifest: dict, binding: dict, work: Path, snapshot: Path, corpus: Path,
                 credentials: Path, load_binary: Path) -> dict:
    measurements = {}
    for replicas, mode in ((1, "single"), (3, "multi")):
        directory = work / ("backend-" + mode)
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        project = prepare_project(
            manifest, binding, work, "backend", mode, snapshot, backend_services(replicas)
        )
        try:
            detail = run_backend_load(
                project, load_binary, corpus, credentials, directory, saturate=True
            )
            raw = directory / "backend-observations.json"
            write_observation(raw, detail)
            measurements[mode] = (
                detail["processed"], detail["elapsed_seconds"],
                detail["counter_before"], detail["counter_after"],
                detail["report_sha256"], sha256_file(raw),
            )
        finally:
            project.down()
    input_sha = digest_json({"snapshot": sha256_file(snapshot), "corpus": sha256_file(corpus)})
    configuration_sha = digest_json(BACKEND_SATURATION)
    pair = make_pair_measurement(
        measurements["single"], measurements["multi"], "backend", "backend_mixed", 3,
        input_sha, configuration_sha, "load_report",
    )
    pair["measurement_mode"] = "closed_loop"
    return pair


def replacement_backend(manifest: dict, binding: dict, work: Path, snapshot: Path, corpus: Path,
                        credentials: Path, load_binary: Path) -> tuple[dict, dict]:
    directory = work / "evidence" / "backend-replacement"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    project = prepare_project(
        manifest, binding, work, "backend", "multi", snapshot, backend_services(3)
    )
    try:
        detail = run_backend_load(
            project, load_binary, corpus, credentials, directory, replace=True
        )
        replacement = detail["replacement"]
        if replacement is None:
            raise RuntimeError("Backend replacement observation is missing")
        raw = directory / "backend-replacement-observations.json"
        write_observation(raw, detail)
        report = detail["report"]
        if report["total"]["errors"] != 0 or report["total"]["timeouts"] != 0:
            raise RuntimeError("Backend replacement load reported unexpected request failures")
        document = replacement_document(
            "backend", detail["processed"], detail["processed"], replacement,
            0, 0, detail["processed"], "load_report", sha256_file(raw),
        )
        per_before = instance_counters("backend", replacement["per_instance_before"])
        per_during = instance_counters("backend", replacement["per_instance_during"])
        per_after = instance_counters("backend", replacement["per_instance_after"])
        removed = replacement["instance"]
        survivors = sorted(set(per_before) - {removed})
        if any(per_before[item] < 1 for item in per_before):
            raise RuntimeError("not every Backend replica received edge traffic")
        if any(per_during[item] <= per_before[item] for item in survivors):
            raise RuntimeError("not every surviving Backend replica served while one was removed")
        if per_after[removed] < 1:
            raise RuntimeError("replacement Backend did not serve after restart")
        edge = {
            "requests": detail["processed"],
            "successful": detail["processed"],
            "instance_counts": per_before,
            "removed_instance": removed,
            "counts_after_removal": per_during,
            "counts_after_restart": per_after,
            "successful_during_removal": int(replacement["survivor_progress"]),
            "successful_after_restart": per_after[removed],
            "observations_sha256": sha256_file(raw),
        }
        return document, edge
    finally:
        project.down()


def wait_rabbit_stats(project: Project, queue: str, timeout=1800,
                     started_at: float | None = None, *, component: str | None = None,
                     services: tuple[str, ...] = (), sample_callback=None) -> tuple[float, list[dict]]:
    started = time.monotonic() if started_at is None else started_at
    samples = []
    while time.monotonic() - started < timeout:
        if component is not None:
            sample = rabbit_timeline_sample(project, component, queue, services, started)
            samples.append(sample)
            if sample_callback is not None:
                sample_callback(sample)
            if sample.get("sample_status") != "observed":
                raise RuntimeError("RabbitMQ timeline sample could not be parsed")
            stats = sample["queue"]
        else:
            stats = rabbit_queue_stats(project, queue)
            sample = {"at": round(time.monotonic() - started, 3), **stats}
            samples.append(sample)
            if sample_callback is not None:
                sample_callback(sample)
        if stats["ready"] == 0 and stats["unacknowledged"] == 0:
            return time.monotonic() - started, samples
        time.sleep(0.5)
    raise RuntimeError("RabbitMQ backlog did not drain")


def wait_rabbit_queue_stats(project: Project, queue: str, expected: int,
                            timeout=60) -> tuple[dict, dict]:
    started = time.monotonic()
    deadline = started + timeout
    samples = []
    while True:
        stats = rabbit_queue_stats(project, queue)
        elapsed = time.monotonic() - started
        samples.append({"at": round(elapsed, 3), **stats})
        count = stats["ready"] + stats["unacknowledged"]
        if count == expected:
            return stats, {"elapsed_seconds": round(elapsed, 3), "samples": samples}
        if count > expected:
            raise RuntimeError("RabbitMQ queue contains more messages than the fixed backlog")
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise RuntimeError("RabbitMQ management queue count did not converge to the fixed backlog")
        time.sleep(min(0.5, remaining))


def prepare_rabbit_backlog(project: Project, component: str,
                           limit: int | None = None) -> tuple[str, int, dict, str, dict]:
    queue = "gopulse.business-worker.v1" if component == "business-worker" else "gopulse.search-indexer.v1"
    expected, backlog_sha = prune_backlog(project, component, limit=limit)
    wait_outbox_empty(project)
    stats, convergence = wait_rabbit_queue_stats(project, queue, expected)
    return queue, expected, stats, backlog_sha, convergence


def release_rabbit_backlog(project: Project, component: str, consumers: tuple[str, ...],
                           sampler, journal: ObservationJournal) -> tuple:
    readiness_started = time.monotonic()
    project.up(*consumers, timeout=900)
    cold_start_readiness = time.monotonic() - readiness_started
    project.require("pause", *consumers, timeout=30)
    journal.update(
        status="consumers_ready_and_paused",
        cold_start_readiness_seconds=round(cold_start_readiness, 3),
        consumer_services=list(consumers),
    )
    queue, expected, before_stats, backlog_sha, publish_convergence = prepare_rabbit_backlog(
        project, component,
    )
    before_total, per_before = component_counter(project, component, consumers)
    journal.update(
        status="fixed_backlog_confirmed",
        queue=queue,
        expected_messages=expected,
        backlog_event_ids_sha256=backlog_sha,
        publish_convergence=publish_convergence,
        before_queue=before_stats,
        component_counter_before={"total": before_total, "services": per_before},
    )
    sampler.start()
    try:
        release_started = time.monotonic()
        unpause_started = time.monotonic()
        project.require("unpause", *consumers, timeout=30)
        journal.update(
            status="backlog_released",
            release_relative_seconds=0.0,
            unpause_command_seconds=round(time.monotonic() - unpause_started, 3),
        )
    except BaseException:
        try:
            sampler.stop()
        except Exception:
            pass
        raise
    return (
        queue, expected, before_stats, backlog_sha, publish_convergence,
        cold_start_readiness, before_total, per_before, release_started,
    )


def pair_rabbit_backlog(manifest: dict, binding: dict, work: Path, snapshot: Path,
                        component: str, evidence_root: Path | None = None) -> dict:
    operation = "business_worker_backlog" if component == "business-worker" else "search_indexer_backlog"
    measurements = {}
    backlog_hashes = {}
    for replicas, mode in ((1, "single"), (2, "multi")):
        project = prepare_project(
            manifest, binding, work, component, mode, snapshot, ("backend",)
        )
        directory = (evidence_root or work) / (component + "-" + mode)
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        consumers = (component,) if replicas == 1 else (component, component + "-2")
        journal = ObservationJournal(directory / "drain-observations.json", {
            "component": component,
            "replicas": replicas,
            "input_snapshot_sha256": sha256_file(snapshot),
            "counter_source": "rabbitmq_management_ack",
            "timing_contract": "healthy_consumers_paused_until_fixed_backlog_release",
        })
        resource_path = directory / "resource-samples.jsonl"
        sampler = Sampler(
            project.name, project.files, project.env_file, interval=1, raw_path=resource_path,
        )
        sampler_started = False
        try:
            (
                queue, expected, before_stats, backlog_sha, publish_convergence,
                cold_start_readiness_seconds, before_total, per_before, drain_started_at,
            ) = release_rabbit_backlog(project, component, consumers, sampler, journal)
            sampler_started = True
            elapsed, samples = wait_rabbit_stats(
                project, queue, started_at=drain_started_at,
                component=component, services=consumers, sample_callback=journal.append,
            )
            sampler.stop()
            sampler_started = False
            after_stats = samples[-1]
            after_queue = after_stats["queue"]
            processed = after_queue["ack"] - before_stats["ack"]
            after_total, per_after = component_counter(project, component, consumers)
            record = {
                "queue": queue, "expected": expected,
                "backlog_event_ids_sha256": backlog_sha,
                "publish_convergence": publish_convergence,
                "timing_window": "release_to_queue_drain",
                "cold_start_readiness_seconds": round(cold_start_readiness_seconds, 3),
                "before": before_stats,
                "after": after_queue,
                "component_counter_before": {"total": before_total, "services": per_before},
                "component_counter_after": {"total": after_total, "services": per_after},
                "drain_seconds": round(elapsed, 3),
                "samples": samples,
                "resource_samples_sha256": sha256_file(resource_path),
            }
            journal.update(**record)
            if processed != expected:
                journal.finish("failed", reason_code="ack_count_mismatch")
                raise RuntimeError(component + " acknowledged count differs from the fixed backlog")
            if not per_after or any(per_after[service] <= per_before[service] for service in consumers):
                journal.finish("failed", reason_code="target_consumer_did_not_advance")
                raise RuntimeError(component + " did not record work on every target consumer")
            journal.finish("passed", reason_code="fixed_backlog_drained")
            measurements[mode] = (
                processed, elapsed, before_stats["ack"], after_queue["ack"],
                sha256_file(directory / "drain-observations.json"), sha256_file(resource_path),
            )
            backlog_hashes[mode] = backlog_sha
        except Exception as error:
            if journal.document["status"] not in {"failed", "passed"}:
                journal.finish(
                    "failed", reason_code=type(error).__name__,
                    reason_sha256=sha256_text(str(error)),
                )
            raise
        finally:
            if sampler_started:
                sampler.stop()
            project.down()
    if backlog_hashes["single"] != backlog_hashes["multi"]:
        raise RuntimeError(component + " single/multi backlog selections differ")
    input_sha = digest_json({
        "snapshot_sha256": sha256_file(snapshot),
        "backlog_event_ids_sha256": backlog_hashes["single"],
    })
    constants = {
        "queue": queue, "backlog": BACKLOG_LIMIT, "prefetch": RABBIT_PREFETCH,
        "replay_policy": "lowest-id published rows reset to pending",
        "queue_count_wait_seconds": 60,
    }
    return make_pair_measurement(
        measurements["single"], measurements["multi"], component, operation, 2,
        input_sha, digest_json(constants), "rabbit_ack",
    )


def replacement_rabbit(manifest: dict, binding: dict, work: Path, snapshot: Path,
                       component: str, credentials_path: Path) -> dict:
    project = prepare_project(manifest, binding, work, component, "multi", snapshot, ("backend",))
    directory = work / "evidence" / (component + "-replacement")
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    journal = ObservationJournal(directory / "replacement-observations.json", {
        "component": component,
        "counter_source": "rabbitmq_management_ack_and_instance_metrics",
        "fresh_work_required": True,
        "replay_policy": "new independent event IDs created through the application edge",
    })
    resource_path = directory / "resource-samples.jsonl"
    sampler = Sampler(project.name, project.files, project.env_file, interval=1,
                      raw_path=resource_path)
    sampler_started = False
    try:
        queue, expected, before_stats, backlog_sha, publish_convergence = prepare_rabbit_backlog(project, component)
        project.up(component, timeout=900)
        project.up(component + "-2", timeout=900)
        sampler.start()
        sampler_started = True
        journal.update(status="consumers_ready_with_fixed_backlog", queue=queue,
                       expected_messages=expected, backlog_event_ids_sha256=backlog_sha,
                       before_queue=before_stats)
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            current = rabbit_queue_stats(project, queue)
            if current["ack"] > before_stats["ack"] or current["unacknowledged"] > 0:
                break
            time.sleep(0.25)
        resume_work_probe = None
        if component == "search-indexer":
            resume_work_probe = lambda: search_indexer_resume_work_probe(
                project, component + "-2", credentials_path
            )
        elif component == "business-worker":
            resume_work_probe = lambda: business_worker_resume_work_probe(
                project, component + "-2", credentials_path
            )
        replacement = replace_component_and_observe(
            project, component, component + "-2", force_kill=True,
            resume_work_probe=resume_work_probe,
            activity_probe=lambda: rabbit_activity_probe(project, queue),
            journal=journal,
        )
        resume_work = replacement.get("resume_work") or {"published_messages": 0}
        expected += int(resume_work["published_messages"])
        during_stats = rabbit_queue_stats(project, queue)
        elapsed, samples = wait_rabbit_stats(project, queue)
        after_stats = samples[-1]
        completed = after_stats["ack"] - before_stats["ack"]
        if completed != expected:
            raise RuntimeError(component + " replacement completed an unexpected count")
        duplicate_side_effects, side_effect_observation = rabbit_side_effect_observation(
            project, component
        )
        sampler.stop()
        sampler_started = False
        raw = directory / "replacement-observations.json"
        journal.update(
            queue=queue, expected=expected,
            backlog_event_ids_sha256=backlog_sha,
            publish_convergence=publish_convergence,
            before=before_stats,
            during=during_stats, after=after_stats, samples=samples,
            replacement=replacement, resume_work=resume_work,
            side_effects=side_effect_observation,
            resource_samples_sha256=sha256_file(resource_path),
        )
        document = replacement_document(
            component, expected, completed, replacement,
            before_stats["ack"], during_stats["ack"], after_stats["ack"],
            "rabbit_ack", sha256_file(raw), duplicate_side_effects,
        )
        journal.finish("passed", reason_code="fresh_work_processed_with_backlog_retained",
                       replacement_document=document)
        ownership = {
            "published": expected,
            "acknowledged": completed,
            "redelivery_attempts": max(0, after_stats["redeliver_get"] - before_stats["redeliver_get"]),
            "duplicate_side_effects": duplicate_side_effects,
            "final_ready": after_stats["ready"],
            "final_unacknowledged": after_stats["unacknowledged"],
            "observations_sha256": sha256_file(raw),
        }
        return document, ownership
    except Exception as error:
        if journal.document["status"] not in {"failed", "passed"}:
            journal.finish("failed", reason_code=type(error).__name__,
                           reason_sha256=sha256_text(str(error)))
        raise
    finally:
        if sampler_started:
            sampler.stop()
        project.down()


def rabbit_side_effect_observation(project: Project, component: str) -> tuple[int, dict]:
    if component == "business-worker":
        rows = int(mysql(project, "SELECT COUNT(*) FROM notifications").strip())
        distinct = int(mysql(project, "SELECT COUNT(DISTINCT source_event_id) FROM notifications").strip())
        return max(0, rows - distinct), {"notifications": rows, "distinct_source_events": distinct}
    if component == "search-indexer":
        refresh = project.compose(
            "exec", "-T", "elasticsearch", "curl", "-fsS", "-XPOST",
            "http://127.0.0.1:9200/gopulse-post-search-v1/_refresh", timeout=60,
        )
        if refresh.returncode:
            raise RuntimeError("refresh search index for replacement evidence")
        counted = project.require(
            "exec", "-T", "elasticsearch", "curl", "-fsS",
            "http://127.0.0.1:9200/gopulse-post-search-v1/_count", timeout=60,
        )
        indexed = int(json.loads(counted.stdout)["count"])
        authoritative = int(mysql(project, "SELECT COUNT(*) FROM posts").strip())
        return abs(indexed - authoritative), {
            "indexed_documents": indexed, "authoritative_posts": authoritative,
        }
    raise ValueError("side-effect observation requires a RabbitMQ consumer component")


def pair_router(manifest: dict, binding: dict, work: Path, snapshot: Path,
                evidence_root: Path | None = None) -> dict:
    measurements = {}
    for replicas, mode in ((1, "single"), (2, "multi")):
        services = ("router",) if replicas == 1 else ("router", "router-2")
        project = prepare_project(manifest, binding, work, "router", mode, snapshot, services)
        directory = (evidence_root or work) / ("router-" + mode)
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        raw = directory / "publish-observations.json"
        journal = ObservationJournal(raw, {
            "component": "router",
            "replicas": replicas,
            "snapshot_sha256": sha256_file(snapshot),
            "counter_source": "kafka_topic_offsets",
            "publisher": "host_go_persistent_http",
            "concurrency": ROUTER_CONCURRENCY,
            "message_count": ROUTER_MESSAGES,
        })
        resource_path = directory / "resource-samples.jsonl"
        sampler = Sampler(
            project.name, project.files, project.env_file, interval=1, raw_path=resource_path,
        )
        sampler_started = False
        try:
            partitions = ensure_kafka_partitions(project, minimum=4)
            before_offsets = kafka_topic_offsets(project)
            journal.update(
                status="topic_ready",
                partition_observation=partitions,
                before_offsets=before_offsets,
            )
            sampler.start()
            sampler_started = True
            measurement_started = time.monotonic()
            detail = concurrent_router_publish(
                project, services, ROUTER_MESSAGES, 1000001,
                concurrency=ROUTER_CONCURRENCY,
                sampler=sampler,
                timeline_callback=lambda: router_timeline_sample(
                    project, services, measurement_started,
                ),
                sample_callback=journal.append,
            )
            sampler.stop()
            sampler_started = False
            after_offsets = kafka_topic_offsets(project)
            processed = sum(after_offsets.values()) - sum(before_offsets.values())
            _, per_service = component_counter(project, "router", services)
            journal.update(
                status="published",
                after_offsets=after_offsets,
                publisher=detail,
                per_service_counters=per_service,
                measurement_started_after_topology_ready=True,
                resource_samples_sha256=sha256_file(resource_path),
            )
            if detail["accepted"] != processed or processed != ROUTER_MESSAGES:
                journal.finish("failed", reason_code="accepted_offset_count_mismatch")
                raise RuntimeError("Router accepted responses differ from Kafka topic offsets")
            if any(per_service.get(service, 0) <= 0 for service in services):
                journal.finish("failed", reason_code="router_replica_received_no_messages")
                raise RuntimeError("Router publication did not reach every replica")
            if detail["timeline_samples"] < 1:
                journal.finish("failed", reason_code="router_workload_too_short_to_sample")
                raise RuntimeError("Router publication finished before a timeline sample was collected")
            journal.finish("passed", reason_code="fixed_publication_matches_broker_offsets")
            measurements[mode] = (
                processed, detail["elapsed_seconds"], sum(before_offsets.values()),
                sum(after_offsets.values()), sha256_file(raw), sha256_file(resource_path),
            )
        except Exception as error:
            if journal.document["status"] not in {"failed", "passed"}:
                journal.finish(
                    "failed", reason_code=type(error).__name__,
                    reason_sha256=sha256_text(str(error)),
                )
            raise
        finally:
            if sampler_started:
                sampler.stop()
            project.down()
    constants = {"messages": ROUTER_MESSAGES, "concurrency": ROUTER_CONCURRENCY,
                 "publisher": "host-go-persistent-http", "topic": "gopulse-observability-v1"}
    return make_pair_measurement(
        measurements["single"], measurements["multi"], "router", "router_concurrent_publish", 2,
        sha256_file(snapshot), digest_json(constants), "kafka_end_offset",
    )


def router_concurrency_staircase(manifest: dict, binding: dict, work: Path,
                                 snapshot: Path, multi: bool) -> dict:
    mode = "multi" if multi else "single"
    services = ("router", "router-2") if multi else ("router",)
    project = prepare_project(manifest, binding, work, "router", mode, snapshot, services)
    directory = work / "evidence" / ("router-staircase-" + mode)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    journal = ObservationJournal(directory / "staircase-observations.json", {
        "component": "router", "mode": mode,
        "fixed_concurrency_steps": [1, 8, 16, 32, 64],
        "fixed_messages_per_step": 20000,
        "publisher": "host-persistent-http",
    })
    resource_path = directory / "resource-samples.jsonl"
    sampler = Sampler(project.name, project.files, project.env_file, interval=1,
                      raw_path=resource_path)
    sampler_started = False
    steps = []
    try:
        partitions = ensure_kafka_partitions(project, minimum=4)
        journal.update(status="topic_ready", partition_observation=partitions)
        sampler.start()
        sampler_started = True
        for index, concurrency in enumerate((1, 8, 16, 32, 64)):
            before_offsets = kafka_topic_offsets(project)
            before_total = sum(before_offsets.values())
            counter_before, per_before = component_counter(project, "router", services)
            started = time.monotonic()
            timeline = []
            detail = concurrent_router_publish(
                project, services, 20000,
                (7000001 if not multi else 8000001) + index * 20000,
                concurrency=concurrency, sampler=sampler,
                timeline_callback=lambda: router_timeline_sample(project, services, started),
                sample_callback=timeline.append,
            )
            after_offsets = kafka_topic_offsets(project)
            after_total = sum(after_offsets.values())
            counter_after, per_after = component_counter(project, "router", services)
            step = {
                "concurrency": concurrency,
                "accepted": detail["accepted"], "failed": detail["failed"],
                "elapsed_seconds": detail["elapsed_seconds"],
                "records_per_second": detail["accepted"] / detail["elapsed_seconds"],
                "offset_delta": after_total - before_total,
                "counter_before": {"total": counter_before, "services": per_before},
                "counter_after": {"total": counter_after, "services": per_after},
                "offsets_before": before_offsets, "offsets_after": after_offsets,
                "timeline_samples": timeline,
                "publisher_report_sha256": detail["report_sha256"],
            }
            steps.append(step)
            journal.update(status="staircase_step_recorded", completed_steps=steps)
            if detail["accepted"] != 20000 or after_total - before_total != 20000:
                raise RuntimeError("Router staircase accepted count differs from broker offsets")
            if any(per_after.get(service, 0) <= per_before.get(service, 0) for service in services):
                raise RuntimeError("Router staircase did not reach every managed instance")
            if not timeline or any(item.get("sample_status") != "observed" for item in timeline):
                raise RuntimeError("Router staircase has an invalid observation timeline")
        sampler.stop()
        sampler_started = False
        result = {
            "status": "passed", "mode": mode,
            "steps": steps,
            "execution_order": [item["concurrency"] for item in steps],
            "resource_samples_sha256": sha256_file(resource_path),
            "fixed_messages_per_step": 20000,
            "configuration_sha256": digest_json({
                "steps": [1, 8, 16, 32, 64], "messages_per_step": 20000,
                "topic": "gopulse-observability-v1",
            }),
        }
        atomic(directory / "staircase.json", result)
        journal.finish("passed", result=result)
        return {
            **result,
            "observations_sha256": sha256_file(journal.path),
            "resource_samples_sha256": sha256_file(resource_path),
        }
    except Exception as error:
        if journal.document["status"] not in {"failed", "passed"}:
            journal.finish("failed", reason_code=type(error).__name__,
                           reason_sha256=sha256_text(str(error)), completed_steps=steps)
        raise
    finally:
        if sampler_started:
            sampler.stop()
        project.down()


def kafka_producer_ceiling(manifest: dict, binding: dict, work: Path,
                           snapshot: Path) -> dict:
    project = prepare_project(manifest, binding, work, "router", "single", snapshot, ("router",))
    directory = work / "evidence" / "kafka-producer-ceiling"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    journal = ObservationJournal(directory / "producer-ceiling-observations.json", {
        "component": "kafka", "producer": "kafka-producer-perf-test",
        "fixed_records": 500000, "record_size_bytes": 64,
        "acknowledgements": "all",
    })
    resource_path = directory / "resource-samples.jsonl"
    sampler = Sampler(project.name, project.files, project.env_file, interval=1,
                      raw_path=resource_path)
    sampler_started = False
    topic = "gopulse-p18-ceiling-" + secrets.token_hex(6)
    try:
        create = project.compose(
            "exec", "-T", "kafka", "sh", "-c",
            '/opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:19092 --create '
            '--topic "$1" --partitions 4 --replication-factor 1', "sh", topic, timeout=60,
        )
        if create.returncode:
            journal.finish("inconclusive", reason_code="direct_topic_creation_unavailable",
                           command_output_sha256=sha256_text(create.stderr or create.stdout or ""))
            return {
                "status": "inconclusive", "reason_code": "direct_topic_creation_unavailable",
                "observations_sha256": sha256_file(journal.path),
                "resource_samples_sha256": None,
            }
        sampler.start()
        sampler_started = True
        command_result = project.compose(
            "exec", "-T", "kafka", "sh", "-c",
            '/opt/kafka/bin/kafka-producer-perf-test.sh --topic "$1" '
            '--num-records 500000 --record-size 64 --throughput -1 '
            '--producer-props bootstrap.servers=localhost:19092 acks=all linger.ms=0',
            "sh", topic, timeout=1800,
        )
        sampler.stop()
        sampler_started = False
        text = (command_result.stdout or "") + "\n" + (command_result.stderr or "")
        report_match = re.search(
            r"([0-9]+) records sent,\s*([0-9.]+) records/sec", text,
        )
        captured = {
            "command_exit_code": command_result.returncode,
            "command_output_sha256": sha256_text(text),
            "reported_records": int(report_match.group(1)) if report_match else None,
            "records_per_second": float(report_match.group(2)) if report_match else None,
            "resource_samples_sha256": sha256_file(resource_path),
        }
        journal.update(status="producer_result_recorded", result=captured)
        if command_result.returncode or report_match is None:
            journal.finish("inconclusive", reason_code="direct_producer_result_unparseable",
                           result=captured)
            return {
                "status": "inconclusive", "reason_code": "direct_producer_result_unparseable",
                "observations_sha256": sha256_file(journal.path),
                "resource_samples_sha256": sha256_file(resource_path),
            }
        if captured["reported_records"] != 500000 or captured["records_per_second"] <= 0:
            journal.finish("inconclusive", reason_code="direct_producer_count_mismatch",
                           result=captured)
            return {
                "status": "inconclusive", "reason_code": "direct_producer_count_mismatch",
                "records_per_second": captured["records_per_second"],
                "observations_sha256": sha256_file(journal.path),
                "resource_samples_sha256": sha256_file(resource_path),
            }
        journal.finish("passed", reason_code="direct_kafka_producer_ceiling_observed",
                       result=captured)
        return {
            "status": "observed", "records_per_second": captured["records_per_second"],
            "records": captured["reported_records"],
            "observations_sha256": sha256_file(journal.path),
            "resource_samples_sha256": sha256_file(resource_path),
        }
    except Exception as error:
        if journal.document["status"] not in {"failed", "passed", "inconclusive"}:
            journal.finish("inconclusive", reason_code=type(error).__name__,
                           reason_sha256=sha256_text(str(error)))
        return {
            "status": "inconclusive", "reason_code": type(error).__name__,
            "observations_sha256": sha256_file(journal.path),
            "resource_samples_sha256": sha256_file(resource_path) if resource_path.exists() else None,
        }
    finally:
        if sampler_started:
            sampler.stop()
        project.down()


def replacement_router(manifest: dict, binding: dict, work: Path, snapshot: Path) -> dict:
    services = ("router", "router-2")
    project = prepare_project(manifest, binding, work, "router", "multi", snapshot, services)
    directory = work / "evidence" / "router-replacement"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    journal = ObservationJournal(directory / "replacement-observations.json", {
        "component": "router",
        "counter_source": "kafka_topic_offsets_and_router_instance_metrics",
        "fixed_messages": ROUTER_MESSAGES,
        "publisher_concurrency": 1,
    })
    resource_path = directory / "resource-samples.jsonl"
    sampler = Sampler(project.name, project.files, project.env_file, interval=1, raw_path=resource_path)
    sampler_started = False
    try:
        before_offsets = kafka_topic_offsets(project)
        before_total = sum(before_offsets.values())
        journal.update(status="publisher_ready", before_offsets=before_offsets)
        sampler.start()
        sampler_started = True
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
            publishing = executor.submit(
                concurrent_router_publish, project, services, ROUTER_MESSAGES, 1,
                allow_failures=True, sampler=sampler,
            )
            deadline = time.monotonic() + 60
            in_flight = False
            while time.monotonic() < deadline:
                observed_offsets = kafka_topic_offsets(project)
                offset = sum(observed_offsets.values())
                journal.append({
                    "phase": "before_removal", "observed_at": time.time(),
                    "relative_seconds": round(60 - max(0, deadline - time.monotonic()), 3),
                    "offsets": observed_offsets,
                    "sources": {"kafka_offsets": {"status": "observed"}},
                })
                if before_total < offset < before_total + ROUTER_MESSAGES:
                    in_flight = True
                    break
                if publishing.done():
                    break
                time.sleep(0.1)
            if not in_flight:
                raise RuntimeError("Router publication completed before replacement began")
            def activity():
                total = sum(kafka_topic_offsets(project).values())
                return {
                    "status": "observed", "counter": total,
                    "remaining_work": max(0, ROUTER_MESSAGES - (total - before_total)),
                }
            replacement = replace_component_and_observe(
                project, "router", "router-2", require_resume=True,
                activity_probe=activity, journal=journal,
            )
            detail = publishing.result(timeout=1800)
        sampler.stop()
        sampler_started = False
        during_offsets = kafka_topic_offsets(project)
        final_offsets = kafka_topic_offsets(project)
        completed = sum(final_offsets.values()) - before_total
        if detail["accepted"] != completed or completed < 1:
            raise RuntimeError("Router replacement accepted/completed counts differ")
        raw = journal.path
        journal.update(
            before_offsets=before_offsets, during_offsets=during_offsets,
            final_offsets=final_offsets, publish=detail, replacement=replacement,
            resource_samples_sha256=sha256_file(resource_path),
        )
        journal.finish("passed", reason_code="router_work_continued_through_replacement")
        return replacement_document(
            "router", detail["accepted"], completed, replacement,
            before_total, sum(during_offsets.values()),
            sum(final_offsets.values()), "kafka_end_offset", sha256_file(raw),
        )
    finally:
        if sampler_started:
            sampler.stop()
        project.down()


def pair_marshaller(manifest: dict, binding: dict, work: Path, snapshot: Path,
                    evidence_root: Path | None = None,
                    message_count: int = MARSHALLER_MESSAGES) -> dict:
    group = "gopulse-marshaller-metrics-v1"
    measurements = {}
    for replicas, mode in ((1, "single"), (2, "multi")):
        services = ("router",) if replicas == 1 else ("router", "router-2")
        project = prepare_project(manifest, binding, work, "marshaller", mode, snapshot, services)
        directory = (evidence_root or work) / ("marshaller-" + mode)
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        raw = directory / "measurement-observations.json"
        journal = ObservationJournal(raw, {
            "component": "marshaller",
            "replicas": replicas,
            "snapshot_sha256": sha256_file(snapshot),
            "counter_source": "marshaller_commit_metric_and_kafka_offsets",
            "topic": "gopulse-observability-v1",
            "group": group,
            "fixed_messages": message_count,
            "minimum_window_seconds": 30,
            "minimum_samples": 10,
        })
        resource_path = directory / "resource-samples.jsonl"
        sampler = Sampler(
            project.name, project.files, project.env_file, interval=1, raw_path=resource_path,
        )
        sampler_started = False
        try:
            partition_record = ensure_kafka_partitions(project, minimum=4)
            offsets_before = kafka_topic_offsets(project)
            journal.update(status="topic_ready", partition_observation=partition_record,
                           offsets_before=offsets_before)
            published = concurrent_router_publish(
                project, services, message_count, 3000001,
                concurrency=ROUTER_CONCURRENCY,
            )
            offsets_after_publish = kafka_topic_offsets(project)
            published_from_offsets = sum(offsets_after_publish.values()) - sum(offsets_before.values())
            if published["accepted"] != message_count or published_from_offsets != message_count:
                journal.finish("failed", reason_code="fixed_publish_count_mismatch")
                raise RuntimeError("Marshaller preload did not publish every message")
            journal.update(
                status="fixed_backlog_confirmed",
                published_offsets=offsets_after_publish,
                published_count=published_from_offsets,
                publisher=published,
            )
            readiness_started = time.monotonic()
            consumer_services = ("marshaller",) if replicas == 1 else ("marshaller", "marshaller-2")
            project.up(*consumer_services, timeout=900)
            cold_start_seconds = time.monotonic() - readiness_started
            assignment_seconds, assignment, assignment_samples = wait_kafka_group_members(
                project, group, replicas, 4, sample_callback=journal.append,
            )
            committed_before = kafka_committed_offsets(project, group)
            counter_before, per_before = component_counter(project, "marshaller", consumer_services)
            journal.update(
                status="group_assigned_before_measurement",
                cold_start_seconds=round(cold_start_seconds, 3),
                assignment_seconds=round(assignment_seconds, 3),
                assignment=assignment,
                committed_before=committed_before,
                component_counters_before={"total": counter_before, "services": per_before},
                measurement_started_after_assignment=True,
            )
            sampler.start()
            sampler_started = True
            elapsed, samples = measure_kafka_window(
                project, group, "marshaller", consumer_services, journal,
                duration_seconds=30.0, minimum_samples=10,
            )
            sampler.stop()
            sampler_started = False
            committed_after_window = kafka_committed_offsets(project, group)
            counter_after, per_after = component_counter(project, "marshaller", consumer_services)
            window_processed = counter_after - counter_before
            drain_seconds, drain_samples = wait_kafka_lag(
                project, group, 0, component="marshaller", services=consumer_services,
                sample_callback=journal.append,
            )
            committed_after = kafka_committed_offsets(project, group)
            total_processed = committed_after["committed_sum"] - committed_before["committed_sum"]
            if total_processed != message_count:
                journal.finish("failed", reason_code="committed_count_mismatch")
                raise RuntimeError("Marshaller committed offset count differs from the fixed backlog")
            if elapsed < 30 or len(samples) < 10 or window_processed < 1:
                journal.finish("failed", reason_code="measured_window_incomplete")
                raise RuntimeError("Marshaller qualification window lacks duration, samples, or progress")
            owners = {item["member"] for item in assignment["partitions"].values() if item["member"]}
            if replicas == 2 and (len(assignment["members"]) != 2 or len(owners) != 2):
                journal.finish("failed", reason_code="two_member_partition_assignment_missing")
                raise RuntimeError("Marshaller qualification did not observe two assigned members")
            journal.update(
                status="measurement_window_complete",
                measurement_seconds=round(elapsed, 3),
                measurement_sample_count=len(samples),
                committed_after_window=committed_after_window,
                component_counters_after={"total": counter_after, "services": per_after},
                window_processed=window_processed,
                drain_seconds=round(drain_seconds, 3),
                drain_samples=drain_samples,
                committed_after=committed_after,
                total_processed=total_processed,
                resource_samples_sha256=sha256_file(resource_path),
            )
            journal.finish("passed", reason_code="timing_and_member_contract_qualified")
            measurements[mode] = (
                window_processed, elapsed, counter_before, counter_after,
                sha256_file(raw), sha256_file(resource_path),
            )
        except Exception as error:
            if journal.document["status"] not in {"failed", "passed"}:
                journal.finish(
                    "failed", reason_code=type(error).__name__,
                    reason_sha256=sha256_text(str(error)),
                )
            raise
        finally:
            if sampler_started:
                sampler.stop()
            project.down()
    constants = {
        "messages": message_count,
        "preload_concurrency": ROUTER_CONCURRENCY,
        "group": group,
        "topic": "gopulse-observability-v1",
        "partition_count_minimum": 4,
        "measurement_window_seconds": 30,
        "minimum_samples": 10,
    }
    return make_pair_measurement(
        measurements["single"], measurements["multi"], "marshaller", "marshaller_backlog", 2,
        sha256_file(snapshot), digest_json(constants), "kafka_committed",
    )


def marshaller_timeline_sample(project: Project, group: str, services: tuple[str, ...],
                               started_at: float) -> dict:
    record = {
        "relative_seconds": round(time.monotonic() - started_at, 3),
        "observed_at": time.time(), "sources": {},
    }
    try:
        observation = kafka_group_observation(project, group)
        record["group"] = observation
        record["sources"]["kafka_consumer_groups"] = {"status": observation["status"]}
        if observation["status"] not in {"active", "assignment_pending", "rebalancing"}:
            raise RuntimeError("Kafka group membership became unavailable")
    except Exception as error:
        record["sources"]["kafka_consumer_groups"] = {
            "status": "parse_failed", "reason_code": type(error).__name__,
            "reason_sha256": sha256_text(str(error)),
        }
        record["sample_status"] = "acceptance_infrastructure_failure"
        return record
    try:
        total, per_service = component_counter(project, "marshaller", services)
        record["component_counters"] = {"total": total, "services": per_service}
        record["sources"]["component_metrics"] = {"status": "observed"}
    except Exception as error:
        record["sources"]["component_metrics"] = {
            "status": "parse_failed", "reason_code": type(error).__name__,
            "reason_sha256": sha256_text(str(error)),
        }
        record["sample_status"] = "acceptance_infrastructure_failure"
        return record
    record["sample_status"] = "observed"
    return record


def calibrate_marshaller_backlog(manifest: dict, binding: dict, work: Path,
                                 snapshot: Path) -> tuple[int, dict]:
    """Measure fixed-rate processing before freezing a qualification batch size."""
    group = "gopulse-marshaller-metrics-v1"
    directory = work / "evidence" / "marshaller-calibration"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    calibration = ObservationJournal(directory / "calibration-observations.json", {
        "component": "marshaller",
        "pilot_messages": MARSHALLER_CALIBRATION_MESSAGES,
        "backlog_safety_seconds": MARSHALLER_BACKLOG_SAFETY_SECONDS,
        "group": group,
        "topic": "gopulse-observability-v1",
    })
    sides = {}
    for replicas, mode in ((1, "single"), (2, "multi")):
        services = ("router",) if replicas == 1 else ("router", "router-2")
        consumers = ("marshaller",) if replicas == 1 else ("marshaller", "marshaller-2")
        project = prepare_project(manifest, binding, work, "marshaller", mode, snapshot, services)
        side_dir = directory / mode
        side_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        raw_path = side_dir / "calibration.json"
        resource_path = side_dir / "resource-samples.jsonl"
        sampler = Sampler(project.name, project.files, project.env_file, interval=1,
                          raw_path=resource_path)
        sampler_started = False
        try:
            partitions = ensure_kafka_partitions(project, minimum=4)
            readiness_started = time.monotonic()
            project.up(*consumers, timeout=900)
            cold_start = time.monotonic() - readiness_started
            assignment_seconds, assignment, assignment_samples = wait_kafka_group_members(
                project, group, replicas, 4,
            )
            committed_before = kafka_committed_offsets(project, group)
            counter_before, per_before = component_counter(project, "marshaller", consumers)
            offsets_before = kafka_topic_offsets(project)
            sampler.start()
            sampler_started = True
            measurement_started = time.monotonic()
            pilot_samples = []
            published = concurrent_router_publish(
                project, services, MARSHALLER_CALIBRATION_MESSAGES,
                5000001 if mode == "single" else 6000001,
                concurrency=ROUTER_CONCURRENCY, sampler=sampler,
                timeline_callback=lambda: marshaller_timeline_sample(
                    project, group, consumers, measurement_started,
                ),
                sample_callback=pilot_samples.append,
            )
            offsets_after = kafka_topic_offsets(project)
            observed_messages = sum(offsets_after.values()) - sum(offsets_before.values())
            counter_after, per_after = component_counter(project, "marshaller", consumers)
            calibration.update(
                status="pilot_published", active_side=mode,
                publisher=published, offsets_before=offsets_before,
                offsets_after=offsets_after,
                counters_before={"total": counter_before, "services": per_before},
                counters_after={"total": counter_after, "services": per_after},
                timeline_samples=pilot_samples,
            )
            drain_seconds, drain_samples = wait_kafka_lag(
                project, group, 0, component="marshaller", services=consumers,
            )
            committed_after = kafka_committed_offsets(project, group)
            sampler.stop()
            sampler_started = False
            if published["accepted"] != MARSHALLER_CALIBRATION_MESSAGES or observed_messages != MARSHALLER_CALIBRATION_MESSAGES:
                raise RuntimeError("Marshaller calibration publisher and Kafka offsets differ")
            committed_delta = committed_after["committed_sum"] - committed_before["committed_sum"]
            if committed_delta != MARSHALLER_CALIBRATION_MESSAGES:
                raise RuntimeError("Marshaller calibration commit count differs from its fixed input")
            valid_samples = [item for item in pilot_samples if item.get("sample_status") == "observed"]
            elapsed = (valid_samples[-1]["relative_seconds"] - valid_samples[0]["relative_seconds"]
                       if len(valid_samples) > 1 else 0.0)
            processed = (valid_samples[-1]["component_counters"]["total"]
                         - valid_samples[0]["component_counters"]["total"]
                         if len(valid_samples) > 1 else 0)
            if elapsed <= 0 or processed <= 0:
                elapsed = max(float(published["elapsed_seconds"]), 0.001)
                processed = counter_after - counter_before
            rate = processed / elapsed if elapsed > 0 else 0.0
            if processed < 1 or rate <= 0:
                raise RuntimeError("Marshaller calibration did not observe processing progress")
            state = {
                "status": "passed", "replicas": replicas,
                "cold_start_seconds": round(cold_start, 3),
                "assignment_seconds": round(assignment_seconds, 3),
                "assignment": assignment, "assignment_samples": assignment_samples,
                "partition_observation": partitions,
                "committed_before": committed_before, "committed_after": committed_after,
                "counter_before": {"total": counter_before, "services": per_before},
                "counter_after": {"total": counter_after, "services": per_after},
                "fixed_messages": MARSHALLER_CALIBRATION_MESSAGES,
                "processed": processed, "measurement_seconds": round(elapsed, 3),
                "records_per_second": round(rate, 3),
                "publisher": published, "offsets_before": offsets_before,
                "offsets_after": offsets_after, "drain_seconds": round(drain_seconds, 3),
                "drain_samples": drain_samples,
                "pilot_timeline_samples": valid_samples,
                "resource_samples_sha256": sha256_file(resource_path),
            }
            atomic(raw_path, state)
            sides[mode] = state
        except Exception as error:
            atomic(raw_path, {
                "status": "failed", "replicas": replicas,
                "reason_code": type(error).__name__,
                "reason_sha256": sha256_text(str(error)),
            })
            raise
        finally:
            if sampler_started:
                sampler.stop()
            project.down()
    peak_rate = max(sides["single"]["records_per_second"], sides["multi"]["records_per_second"])
    fixed_messages = max(
        MARSHALLER_MESSAGES,
        int(math.ceil(peak_rate * MARSHALLER_BACKLOG_SAFETY_SECONDS)),
    )
    calibration.update(
        status="calibrated", single=sides["single"], multi=sides["multi"],
        measured_peak_records_per_second=peak_rate,
        safety_backlog_seconds=MARSHALLER_BACKLOG_SAFETY_SECONDS,
        fixed_messages=fixed_messages,
        single_observations_sha256=sha256_file(directory / "single" / "calibration.json"),
        multi_observations_sha256=sha256_file(directory / "multi" / "calibration.json"),
    )
    calibration.finish("passed", reason_code="fixed_batch_frozen_before_formal_measurement")
    return fixed_messages, calibration.document


def replacement_marshaller(manifest: dict, binding: dict, work: Path, snapshot: Path,
                           message_count: int = MARSHALLER_MESSAGES) -> tuple[dict, dict]:
    group = "gopulse-marshaller-metrics-v1"
    services = ("router", "router-2")
    project = prepare_project(manifest, binding, work, "marshaller", "multi", snapshot, services)
    directory = work / "evidence" / "marshaller-replacement"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    journal = ObservationJournal(directory / "replacement-observations.json", {
        "component": "marshaller", "group": group,
        "counter_source": "kafka_committed_offsets_and_instance_metrics",
        "fixed_messages": message_count,
    })
    resource_path = directory / "resource-samples.jsonl"
    sampler = Sampler(project.name, project.files, project.env_file, interval=1, raw_path=resource_path)
    sampler_started = False
    try:
        partition_record = ensure_kafka_partitions(project, minimum=4)
        published = concurrent_router_publish(project, services, message_count, 1,
                                              concurrency=16)
        if published["accepted"] != message_count:
            raise RuntimeError("Marshaller replacement preload did not publish every message")
        before = kafka_committed_offsets(project, group, allow_empty=True)
        journal.update(status="fixed_backlog_confirmed", partition_observation=partition_record,
                       publish=published, committed_before=before)
        project.up("marshaller", timeout=900)
        project.up("marshaller-2", timeout=900)
        deadline = time.monotonic() + 60
        started_group = None
        while time.monotonic() < deadline:
            lag = kafka_lag(project, group)
            observed = kafka_committed_offsets(project, group, allow_empty=True)
            if 0 < lag < message_count and len(observed["members"]) >= 2:
                started_group = observed
                break
            time.sleep(0.25)
        if started_group is None:
            raise RuntimeError("Marshaller replacement could not capture two active owners")
        journal.update(status="two_members_processing", members_before=started_group["members"],
                       committed_started=started_group)
        sampler.start()
        sampler_started = True

        def activity():
            observation = kafka_group_observation(project, group)
            committed = sum(max(0, item["committed"] or 0)
                            for item in observation["partitions"].values())
            lag_value = observation["lag"]
            return {
                "status": observation["status"],
                "counter": committed,
                "remaining_work": int(lag_value) if lag_value is not None else 0,
                "members": observation["members"],
                "partitions": observation["partitions"],
            }
        replacement = replace_component_and_observe(
            project, "marshaller", "marshaller-2", require_resume=True,
            activity_probe=activity, journal=journal,
        )
        during = kafka_group_observation(project, group)
        elapsed, samples = wait_kafka_lag(project, group, 0)
        deadline = time.monotonic() + 60
        after = kafka_committed_offsets(project, group, allow_empty=True)
        while len(after["members"]) < 2 and time.monotonic() < deadline:
            time.sleep(0.25)
            after = kafka_committed_offsets(project, group, allow_empty=True)
        completed = after["committed_sum"] - before["committed_sum"]
        if completed != message_count:
            raise RuntimeError("Marshaller replacement committed an unexpected count")
        sampler.stop()
        sampler_started = False
        raw = journal.path
        journal.update(
            messages=message_count, samples=samples,
            before=before, during=during, after=after,
            publish=published, replacement=replacement,
            resource_samples_sha256=sha256_file(resource_path),
        )
        document = replacement_document(
            "marshaller", message_count, completed, replacement,
            before["committed_sum"],
            sum(max(0, item["committed"] or 0) for item in during["partitions"].values()),
            after["committed_sum"],
            "kafka_committed", sha256_file(raw),
        )
        end_offsets = kafka_topic_offsets(project)
        duplicate_commits = max(0, after["committed_sum"] - sum(end_offsets.values()))
        ownership = {
            "messages": message_count,
            "committed": completed,
            "duplicate_commits": duplicate_commits,
            "old_owner_commits_after_revoke": duplicate_commits,
            "final_lag": int(samples[-1]["group"]["lag"]),
            "members_before": started_group["members"],
            "members_during_removal": during["members"],
            "members_after_restart": after["members"],
            "observations_sha256": sha256_file(raw),
        }
        journal.finish("passed", reason_code="two_member_work_continued_through_replacement",
                       ownership=ownership, replacement_document=document)
        return document, ownership
    except Exception as error:
        if journal.document["status"] not in {"failed", "passed"}:
            journal.finish("failed", reason_code=type(error).__name__,
                           reason_sha256=sha256_text(str(error)))
        raise
    finally:
        if sampler_started:
            sampler.stop()
        project.down()


def create_snapshot(manifest: dict, binding: dict, work: Path, recipe_binary: Path):
    project = Project(manifest, binding, work, project_name("seed", "seed"), 1)
    directory = work / "seed"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    corpus = directory / "corpus.json"
    credentials = directory / "credentials.json"
    receipt = directory / "recipe-receipt.json"
    snapshot = directory / "initial.sql"
    try:
        start_database(project)
        project.require("run", "--rm", "migrate", timeout=1800)
        receipt_value = generate_recipe(recipe_binary, binding, project.environment, directory,
                                        int(project.environment["MYSQL_PORT"]))
        if receipt_value["candidate"] != {
            "version": binding["version"], "revision": binding["revision"],
            "manifest_sha256": binding["manifest_sha256"],
        }:
            raise RuntimeError("recipe receipt is not bound to the scaling candidate")
        dump_database(project, snapshot)
        return snapshot, corpus, credentials, receipt
    finally:
        project.down()


def build_router_publisher(work: Path) -> Path:
    binary = work / "bin" / "router-publish"
    result = run([
        "go", "-C", str(ROOT / "loadtest"), "build", "-trimpath", "-o", str(binary),
        "./cmd/routerpublish",
    ], timeout=300, env={**os.environ, "GOWORK": "off"})
    require(result, "build Router publisher")
    binary.chmod(0o700)
    return binary


def readiness_rabbit_lifecycle(manifest: dict, binding: dict, work: Path,
                               snapshot: Path, component: str) -> dict:
    project = prepare_project(
        manifest, binding, work, "readiness-" + component, "single", snapshot, ("backend",)
    )
    directory = work / "evidence" / "readiness" / component
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    try:
        queue, expected, before, backlog_sha, convergence = prepare_rabbit_backlog(
            project, component, limit=READINESS_BACKLOG_LIMIT
        )
        started_at = time.monotonic()
        project.up(component, timeout=900)
        startup_seconds = round(time.monotonic() - started_at, 3)
        elapsed, samples = wait_rabbit_stats(
            project, queue, timeout=180, started_at=started_at
        )
        after = samples[-1]
        if after["ack"] - before["ack"] != expected:
            raise RuntimeError(component + " readiness lifecycle acknowledged an unexpected count")
        observation = directory / "rabbit-lifecycle.json"
        write_observation(observation, {
            "queue": queue,
            "expected": expected,
            "backlog_sha256": backlog_sha,
            "publish_convergence": convergence,
            "consumer_startup_seconds": startup_seconds,
            "drain_seconds": elapsed,
            "before": before,
            "after": after,
            "samples": samples,
        })
        return {
            "queue": queue,
            "expected": expected,
            "observed": before["ready"] + before["unacknowledged"],
            "acknowledged": after["ack"] - before["ack"],
            "backlog_sha256": backlog_sha,
            "queue_convergence_seconds": convergence["elapsed_seconds"],
            "consumer_startup_seconds": startup_seconds,
            "drain_seconds": round(elapsed, 3),
            "observations_sha256": sha256_file(observation),
        }
    finally:
        project.down()


def readiness_probe(manifest: dict, binding: dict, work: Path, snapshot: Path,
                    corpus: Path, credentials: Path, load_binary: Path) -> None:
    """Prove fixed inputs and transport paths before the expensive paired matrix."""
    directory = work / "evidence" / "readiness"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    report_path = directory / "load-report.json"
    rabbit = {
        component: readiness_rabbit_lifecycle(
            manifest, binding, work, snapshot, component
        )
        for component in ("business-worker", "search-indexer")
    }
    project = prepare_project(manifest, binding, work, "readiness", "single", snapshot,
                              ("backend", "frontend"))
    try:
        project.up("router", timeout=900)
        before_offsets = kafka_topic_offsets(project)
        if len(before_offsets) != 4:
            raise RuntimeError("readiness Kafka topic does not have four partitions")
        publish_probe = concurrent_router_publish(
            project, ("router",), 100, ROUTER_MESSAGES + MARSHALLER_MESSAGES + 1,
            concurrency=8,
        )
        after_offsets = kafka_topic_offsets(project)
        if publish_probe["accepted"] != 100 or sum(after_offsets.values()) - sum(before_offsets.values()) != 100:
            raise RuntimeError("Router readiness publish did not reach Kafka")
        command = [
            str(load_binary), "--base-url", "http://127.0.0.1:" + project.environment["FRONTEND_PORT"],
            "--corpus", str(corpus), "--credentials", str(credentials),
            "--report", str(report_path), "--saturate",
            "--vus", str(BACKEND_SATURATION["virtual_users"]),
            "--active-workers", str(BACKEND_SATURATION["active_workers"]),
            "--warmup", "2s", "--steady", "3s", "--burst", "0s",
        ]
        load_result = subprocess.run(command, text=True, capture_output=True, timeout=90)
        require(load_result, "run Backend saturation readiness probe")
        report = json.loads(report_path.read_text())
        if (report.get("active_workers") != BACKEND_SATURATION["active_workers"]
                or report.get("steady_target_rps") != 0
                or [phase["name"] for phase in report.get("phases", [])] != ["warmup", "steady"]
                or report["phases"][1]["counts"]["succeeded"] < 1):
            raise RuntimeError("Backend saturation readiness report is incomplete")
        atomic(directory / "readiness.json", {
            "candidate_revision": binding["revision"],
            "snapshot_sha256": sha256_file(snapshot),
            "load_report_sha256": sha256_file(report_path),
            "backend_steady_successes": report["phases"][1]["counts"]["succeeded"],
            "rabbit": rabbit,
            "kafka_partitions": len(after_offsets),
            "router_published": publish_probe["accepted"],
            "router_report_sha256": publish_probe["report_sha256"],
        })
    finally:
        project.down()


def wait_row(project: Project, sql: str, timeout=60) -> str:
    deadline = time.monotonic() + timeout
    last = ""
    while time.monotonic() < deadline:
        last = mysql(project, sql).strip()
        if last:
            return last
        time.sleep(1)
    raise RuntimeError("database observation did not converge: " + last)


def runtime_ownership(manifest: dict, binding: dict, work: Path, snapshot: Path) -> dict:
    project = Project(manifest, binding, work, project_name("ownership", "runtime"), 2)
    directory = work / "evidence" / "ownership-runtime"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    journal = ObservationJournal(directory / "runtime-ownership-observations.json", {
        "component": "runtime_ownership",
        "candidate_revision": binding["revision"],
    })
    result = {}
    try:
        start_database(project)
        restore_database(project, snapshot)
        chosen = mysql(project, "SELECT id,event_type FROM business_outbox WHERE status='published' ORDER BY id LIMIT 1").split()
        if len(chosen) != 2:
            raise RuntimeError("snapshot has no published Outbox row for lease evidence")
        row_id = int(chosen[0])
        mysql_exec(project, "UPDATE business_outbox SET status='leased',lease_owner='phase18-foreign',lease_expires_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND),published_at=NULL,updated_at=UTC_TIMESTAMP(6) WHERE id=%d" % row_id)
        journal.update(status="future_outbox_lease_created", outbox_row_id=row_id)
        project.up("backend", timeout=1200)
        time.sleep(3)
        blocked = outbox_observation(project, row_id)
        journal.update(status="future_outbox_lease_observed", blocked_observation=blocked)
        if (blocked["owner"] != "phase18-foreign" or blocked["status"] != "leased"
                or blocked["published"] != 0 or not blocked["lease_until"]):
            raise RuntimeError("foreign Outbox lease was overwritten before expiry")
        mysql_exec(project, "UPDATE business_outbox SET lease_expires_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE id=%d" % row_id)
        wait_row(project, "SELECT status FROM business_outbox WHERE id=%d AND status='published' AND published_at IS NOT NULL" % row_id, timeout=180)
        reclaimed = outbox_observation(project, row_id)
        journal.update(status="expired_outbox_lease_reclaimed", reclaimed_observation=reclaimed)
        if reclaimed["published"] != 1 or reclaimed["status"] != "published":
            raise RuntimeError("expired Outbox lease was not reclaimed exactly once")
        result["outbox_lease"] = {
            "future_owner": "phase18-foreign",
            "future_lease_until": blocked["lease_until"],
            "blocked_observation": {
                "owner": blocked["owner"], "lease_until": blocked["lease_until"],
                "status": blocked["status"], "updated_at": blocked["updated_at"],
                "counter_before": 0, "counter_after": blocked["published"],
            },
            "reclaimed_observation": {
                "owner": reclaimed["owner"], "lease_until": reclaimed["lease_until"],
                "status": reclaimed["status"], "updated_at": reclaimed["updated_at"],
                "counter_before": blocked["published"], "counter_after": reclaimed["published"],
            },
        }

        now_sql = "UTC_TIMESTAMP(6)"
        mysql_exec(project,
                   "INSERT INTO alert_rules(id,name,enabled,severity,source,selector,reducer,operator,threshold,window_seconds,for_seconds,revision,created_by,updated_by,created_at,updated_at) "
                   "VALUES(9000001,'phase18-lease',1,'warning','metrics',JSON_OBJECT('metric','up','labels',JSON_OBJECT()),'last','gt',999,60,0,1,1,1,%s,%s) "
                   "ON DUPLICATE KEY UPDATE enabled=1,revision=revision+1,updated_at=%s" % (now_sql, now_sql, now_sql))
        mysql_exec(project,
                   "INSERT INTO alert_rule_states(rule_id,state,data_status,next_evaluation_at,lease_owner,lease_until) "
                   "VALUES(9000001,'normal','unknown',DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND),'phase18-foreign',DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND)) "
                   "ON DUPLICATE KEY UPDATE next_evaluation_at=VALUES(next_evaluation_at),lease_owner=VALUES(lease_owner),lease_until=VALUES(lease_until)")
        time.sleep(3)
        alert_blocked = alert_observation(project)
        journal.update(status="future_alert_lease_observed", alert_blocked=alert_blocked)
        if (alert_blocked["owner"] != "phase18-foreign" or not alert_blocked["lease_until"]
                or alert_blocked["last_evaluated_at"]):
            raise RuntimeError("foreign alert lease was overwritten before expiry")
        mysql_exec(project, "UPDATE alert_rule_states SET lease_until=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND),next_evaluation_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND) WHERE rule_id=9000001")
        wait_row(project, "SELECT state FROM alert_rule_states WHERE rule_id=9000001 AND lease_owner='' AND last_evaluated_at IS NOT NULL", timeout=180)
        alert_reclaimed = alert_observation(project)
        journal.update(status="expired_alert_lease_reclaimed", alert_reclaimed=alert_reclaimed)
        if alert_reclaimed["status"] != "applied":
            raise RuntimeError("expired alert lease was not reclaimed")
        result["alert_lease"] = {
            "future_owner": "phase18-foreign",
            "future_lease_until": alert_blocked["lease_until"],
            "blocked_observation": {
                "owner": alert_blocked["owner"], "lease_until": alert_blocked["lease_until"],
                "status": "leased", "last_evaluated_at": alert_blocked["last_evaluated_at"],
            },
            "reclaimed_observation": {
                "owner": alert_reclaimed["owner"], "lease_until": alert_reclaimed["lease_until"],
                "status": "applied", "last_evaluated_at": alert_reclaimed["last_evaluated_at"],
            },
        }

        project.up("monitor", timeout=1200)
        registry_hash = monitor_file_hash(project, "registry.json")
        process_hash = monitor_file_hash(project, "process.json")
        journal.update(status="monitor_owner_baseline_recorded",
                       registry_sha256=registry_hash, process_record_sha256=process_hash)
        conflict = project.compose(
            "--profile", "qualification-conflict", "up", "-d", "monitor-2", timeout=300
        )
        if conflict.returncode:
            raise RuntimeError("Monitor conflict probe could not be started")
        deadline = time.monotonic() + 90
        exit_code = None
        while time.monotonic() < deadline:
            ids = project.compose("ps", "-q", "monitor-2", timeout=30).stdout.split()
            if ids:
                inspected = run(["docker", "inspect", ids[0]], timeout=30)
                if inspected.returncode == 0:
                    state = json.loads(inspected.stdout)[0].get("State", {})
                    if state.get("Status") == "exited":
                        exit_code = int(state.get("ExitCode", 0))
                        break
            time.sleep(1)
        if exit_code is None:
            raise RuntimeError("second Monitor instance did not terminate")
        after_registry = monitor_file_hash(project, "registry.json")
        after_process = monitor_file_hash(project, "process.json")
        journal.update(status="monitor_conflict_observed",
                       registry_sha256_after=after_registry,
                       process_record_sha256_after=after_process,
                       second_exit_code=exit_code)
        result["monitor_single_owner"] = {
            "second_exit_code": exit_code,
            "registry_sha256_before": registry_hash, "registry_sha256_after": after_registry,
            "process_record_sha256_before": process_hash, "process_record_sha256_after": after_process,
        }
    except Exception as error:
        if journal.document["status"] not in {"failed", "passed"}:
            journal.finish("failed", reason_code=type(error).__name__,
                           reason_sha256=sha256_text(str(error)))
        raise
    finally:
        project.down()
    journal.finish("passed", reason_code="lease_reclaim_and_monitor_single_owner_observed",
                   result=result)
    result["observations_sha256"] = sha256_file(journal.path)
    return result


def outbox_observation(project: Project, row_id: int) -> dict:
    value = mysql(
        project,
        "SELECT JSON_OBJECT('owner',lease_owner,"
        "'lease_until',DATE_FORMAT(lease_expires_at,'%%Y-%%m-%%dT%%H:%%i:%%s.%%fZ'),"
        "'status',status,'updated_at',DATE_FORMAT(updated_at,'%%Y-%%m-%%dT%%H:%%i:%%s.%%fZ'),"
        "'published',published_at IS NOT NULL) FROM business_outbox WHERE id=%d" % row_id,
    )
    fields = parse_mysql_json_object(
        value, {"owner", "lease_until", "status", "updated_at", "published"}, "Outbox lease",
    )
    if not isinstance(fields["status"], str) or not isinstance(fields["published"], int):
        raise RuntimeError("Outbox lease observation has invalid field types")
    return {
        "owner": fields["owner"], "lease_until": fields["lease_until"],
        "status": fields["status"], "updated_at": fields["updated_at"],
        "published": fields["published"],
    }


def alert_observation(project: Project) -> dict:
    value = mysql(
        project,
        "SELECT JSON_OBJECT('owner',lease_owner,"
        "'lease_until',DATE_FORMAT(lease_until,'%%Y-%%m-%%dT%%H:%%i:%%s.%%fZ'),"
        "'state',state,'last_evaluated_at',DATE_FORMAT(last_evaluated_at,'%%Y-%%m-%%dT%%H:%%i:%%s.%%fZ')) "
        "FROM alert_rule_states WHERE rule_id=9000001",
    )
    fields = parse_mysql_json_object(
        value, {"owner", "lease_until", "state", "last_evaluated_at"}, "alert lease",
    )
    if not isinstance(fields["state"], str):
        raise RuntimeError("alert lease observation has invalid state")
    return {
        "owner": fields["owner"], "lease_until": fields["lease_until"],
        "state": fields["state"], "last_evaluated_at": fields["last_evaluated_at"],
        "status": "applied" if fields["last_evaluated_at"] else "leased",
    }


def monitor_file_hash(project: Project, name: str) -> str:
    script = ('find /var/lib/gopulse-monitor/plugins -name %s -type f -print0 | sort -z | '
              'xargs -0 -r sha256sum') % name
    result = project.require("exec", "-T", "monitor", "sh", "-c", script, timeout=30)
    lines = [line for line in result.stdout.splitlines() if line.strip()]
    if not lines:
        raise RuntimeError("Monitor " + name + " is unavailable")
    raw = "\n".join(sorted(lines)).encode()
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def ownership_tests(work: Path) -> dict:
    output = work / "ownership-tests.log"
    results = {}
    with output.open("w", encoding="utf-8") as stream:
        for name, command in OWNERSHIP_TESTS.items():
            result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, timeout=900)
            stream.write("$ " + " ".join(command) + "\n")
            stream.write(result.stdout)
            stream.write(result.stderr)
            stream.flush()
            results[name] = {
                "command": " ".join(command), "exit_code": result.returncode,
                "output_sha256": sha256_text(result.stdout + result.stderr),
            }
    return results


def topology_evidence(manifest: dict, binding: dict, work: Path) -> dict:
    candidate_environment(manifest, work / "topology.env", 2)
    override = work / "topology.override.yaml"
    override.write_text("services:\n  backend:\n    ports: !reset []\n")
    override.chmod(0o600)
    command = ["docker", "compose", "--project-name", "gopulse-p18-02-topology-check",
               "--env-file", str(work / "topology.env"),
               "-f", str(ROOT / "deploy" / "compose.yaml"),
               "-f", str(ROOT / "scripts" / "ci" / "phase18-scale.yaml"),
               "-f", str(override),
               "config", "--format", "json"]
    result = subprocess.run(command, text=True, capture_output=True, timeout=120)
    require(result, "render Phase 18 scaling topology")
    config = json.loads(result.stdout)
    bindings = []
    for service, spec in config.get("services", {}).items():
        for port in spec.get("ports", []) or []:
            host_ip = port.get("host_ip") or "0.0.0.0"
            bindings.append({"service": service, "host_ip": host_ip, "host_port": int(port["published"])})
    return {
        "replicas": {component: len(instances) for component, instances in INSTANCES.items()},
        "aliases": {
            "backend": list(INSTANCES["backend"]),
            "router": list(INSTANCES["router"]),
        },
        "edge_bindings": bindings,
    }


def inventory_sha() -> str:
    return digest_json(resource_inventory())


def owned_project_resources() -> list[dict]:
    prefix = "gopulse-p18-02-"
    resources = []
    commands = (
        ("container", ["docker", "ps", "-a", "--filter", "label=com.docker.compose.project",
                       "--format", '{{.Names}}\t{{.Label "com.docker.compose.project"}}']),
        ("volume", ["docker", "volume", "ls", "--filter", "label=com.docker.compose.project",
                    "--format", '{{.Name}}\t{{.Label "com.docker.compose.project"}}']),
        ("network", ["docker", "network", "ls", "--filter", "label=com.docker.compose.project",
                     "--format", '{{.Name}}\t{{.Label "com.docker.compose.project"}}']),
    )
    for kind, command in commands:
        result = run(command)
        require(result, "inspect " + kind + " project ownership")
        for line in result.stdout.splitlines():
            fields = line.split("\t")
            if len(fields) == 2 and fields[1].startswith(prefix):
                resources.append({"kind": kind, "name": fields[0], "project": fields[1]})
    return sorted(resources, key=lambda item: (item["kind"], item["name"]))


def scaling_document(preflight_document, binding, topology, inputs, pairs, replacements,
                     edge, ownership, tests, inventory_before) -> dict:
    document = {
        "schema": SCHEMA,
        "execution_status": "complete",
        "complete": True,
        "candidate": binding,
        "host": preflight_document["host"],
        "topology": topology,
        "inputs": inputs,
        "pairs": pairs,
        "replacements": replacements,
        "edge": edge,
        "ownership": ownership,
        "tests": tests,
        "cleanup": {
            "resource_inventory_before_sha256": inventory_before,
            "resource_inventory_after_sha256": inventory_sha(),
            "owned_projects_remaining": len(owned_project_resources()),
            "label_cleanup": "passed" if not owned_project_resources() else "failed",
        },
        "secret_scan": "passed",
    }
    secret_scan(document)
    return document


def run_preflight(work: Path) -> dict:
    work.mkdir(parents=True, exist_ok=True, mode=0o700)
    if work.stat().st_mode & 0o77:
        raise ValueError("scaling work directory must be private")
    document = {"schema": "gopulse.phase18.preflight.v1", "host": host_inventory()}
    document["problems"] = preflight(document["host"])
    atomic(work / "evidence" / "preflight.json", document)
    if document["problems"]:
        raise RuntimeError("reference-host preflight failed: " + "; ".join(document["problems"]))
    return document


def run_scaling(manifest_path: Path, work: Path) -> dict:
    work = work.resolve()
    preflight_document = run_preflight(work)
    manifest, binding = candidate_binding(manifest_path.resolve())
    lock = prepare_workspace(work, binding)  # Hold the workspace lock for the full matrix.
    inventory_before = inventory_sha()
    topology = topology_evidence(manifest, binding, work)
    if topology["edge_bindings"] != [{"service": "frontend", "host_ip": "127.0.0.1", "host_port": 18082}]:
        raise RuntimeError("Compose topology does not expose exactly the managed Frontend edge")
    recipe_binary, load_binary = build_loadtest(work)
    router_binary = build_router_publisher(work)
    first = inspect_recipe(recipe_binary, binding, work / "recipe-inspect-1.json")
    second = inspect_recipe(recipe_binary, binding, work / "recipe-inspect-2.json")
    if first["digest"] != second["digest"]:
        raise RuntimeError("scaling recipe inspection was not deterministic")
    snapshot, corpus, credentials, receipt = create_snapshot(manifest, binding, work, recipe_binary)
    print("Phase 18-02 readiness probe", flush=True)
    readiness_probe(manifest, binding, work, snapshot, corpus, credentials, load_binary)
    execution_order = list(SCALING_EXECUTION_ORDER)
    scenario_results = {}
    pairs = {}
    replacements = {}
    rabbit_ownership = None
    kafka_ownership = None

    edge = None

    def backend_replacement():
        nonlocal edge
        replacements["backend"], edge = replacement_backend(
            manifest, binding, work, snapshot, corpus, credentials, load_binary
        )

    attempt_scenario(scenario_results, "backend:replacement", backend_replacement, work=work)

    for component in ("business-worker", "search-indexer"):
        def rabbit_replacement(component=component):
            nonlocal rabbit_ownership
            replacements[component], ownership_fragment = replacement_rabbit(
                manifest, binding, work, snapshot, component, credentials
            )
            if component == "business-worker":
                rabbit_ownership = ownership_fragment

        attempt_scenario(scenario_results, component + ":replacement", rabbit_replacement, work=work)

    attempt_scenario(
        scenario_results,
        "router:replacement",
        lambda: replacements.__setitem__(
            "router", replacement_router(manifest, binding, work, snapshot)
        ),
        work=work,
    )

    def marshaller_replacement():
        nonlocal kafka_ownership
        replacements["marshaller"], kafka_ownership = replacement_marshaller(
            manifest, binding, work, snapshot
        )

    attempt_scenario(scenario_results, "marshaller:replacement", marshaller_replacement, work=work)

    ownership = None

    def collect_runtime_ownership():
        nonlocal ownership
        ownership = runtime_ownership(manifest, binding, work, snapshot)
        if rabbit_ownership is None or kafka_ownership is None:
            raise RuntimeError("runtime ownership fragments are incomplete")
        ownership["rabbit_ack_redelivery"] = rabbit_ownership
        ownership["kafka_rebalance_fencing"] = kafka_ownership

    attempt_scenario(scenario_results, "ownership:runtime", collect_runtime_ownership, work=work)
    tests = attempt_scenario(
        scenario_results, "ownership:tests", lambda: ownership_tests(work), work=work
    )

    def collect_pair(component, operation):
        pair = operation()
        pairs[component] = pair
        record_pair_result(work, binding, component, pair)

    attempt_scenario(
        scenario_results,
        "backend:single+multi",
        lambda: collect_pair(
            "backend",
            lambda: pair_backend(
                manifest, binding, work, snapshot, corpus, credentials, load_binary
            ),
        ),
        work=work,
    )
    for component in ("business-worker", "search-indexer"):
        attempt_scenario(
            scenario_results,
            component + ":single+multi",
            lambda component=component: collect_pair(
                component,
                lambda: pair_rabbit_backlog(manifest, binding, work, snapshot, component),
            ),
            work=work,
        )
    attempt_scenario(
        scenario_results,
        "router:single+multi",
        lambda: collect_pair(
            "router", lambda: pair_router(manifest, binding, work, snapshot)
        ),
        work=work,
    )
    attempt_scenario(
        scenario_results,
        "marshaller:single+multi",
        lambda: collect_pair(
            "marshaller", lambda: pair_marshaller(manifest, binding, work, snapshot)
        ),
        work=work,
    )

    inputs = {
        "corpus_sha256": sha256_file(corpus),
        "snapshot_sha256": sha256_file(snapshot),
        "load_source_commit": binding["revision"],
        "load_binary_sha256": sha256_file(load_binary),
        "router_load_binary_sha256": sha256_file(router_binary),
        "recipe_receipt_sha256": sha256_file(receipt),
        "execution_order": execution_order,
    }
    failed_scenarios = sorted(
        name for name, result in scenario_results.items() if result["status"] == "failed"
    )
    if failed_scenarios:
        remaining = owned_project_resources()
        failure = {
            "schema": "gopulse.phase18.scaling-failure.v1",
            "execution_status": "failed",
            "complete": False,
            "candidate": binding,
            "host": preflight_document["host"],
            "topology": topology,
            "inputs": inputs,
            "scenarios": scenario_results,
            "failed_scenarios": failed_scenarios,
            "completed_pairs": sorted(pairs),
            "completed_replacements": sorted(replacements),
            "cleanup": {
                "resource_inventory_before_sha256": inventory_before,
                "resource_inventory_after_sha256": inventory_sha(),
                "owned_projects_remaining": len(remaining),
                "label_cleanup": "passed" if not remaining else "failed",
            },
            "secret_scan": "passed",
        }
        secret_scan(failure)
        atomic(work / "evidence" / "scaling-failure.json", failure)
        raise RuntimeError(
            "Phase 18-02 matrix failed scenarios: " + ", ".join(failed_scenarios)
        )

    document = scaling_document(
        preflight_document, binding, topology, inputs, pairs, replacements,
        edge, ownership, tests, inventory_before,
    )
    validate_scaling(document, manifest_path.resolve())
    atomic(work / "evidence" / "scaling.json", document)
    print("PASS: Phase 18-02 paired scaling, replacement, ownership, and cleanup", flush=True)
    return document


def main():
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--preflight-only", action="store_true")
    parser.add_argument("--qualify", action="store_true")
    arguments = parser.parse_args()
    if arguments.qualify and arguments.preflight_only:
        parser.error("--qualify and --preflight-only cannot be combined")
    if arguments.preflight_only:
        document = run_preflight(arguments.work.resolve())
        print(json.dumps({"host": document["host"], "problems": document["problems"]}, sort_keys=True))
        return
    if arguments.qualify:
        if arguments.manifest is None:
            parser.error("--manifest is required with --qualify")
        from phase18_qualification import run_qualification
        run_qualification(arguments.manifest, arguments.work)
        return
    if arguments.manifest is None:
        parser.error("--manifest is required unless --preflight-only is used")
    run_scaling(arguments.manifest, arguments.work)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        work = None
        for index, value in enumerate(sys.argv):
            if value == "--work" and index + 1 < len(sys.argv):
                work = Path(sys.argv[index + 1]).resolve()
        if work is not None:
            try:
                path = work / "run-error.txt"
                if "--qualify" in sys.argv:
                    path.write_text(type(error).__name__ + " sha256:" + sha256_text(str(error)) + "\n")
                else:
                    path.write_text(type(error).__name__ + ": " + str(error) + "\n")
                path.chmod(0o600)
            except OSError:
                pass
        print("Phase 18 scaling execution failed (" + type(error).__name__ + ")", file=sys.stderr)
        raise SystemExit(1)
