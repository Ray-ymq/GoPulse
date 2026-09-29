#!/usr/bin/env python3
"""Persist separate load-process, SUT, host, and saturation samples.

The sampler is intentionally independent of the load generator. A sampler
failure is evidence-incomplete; it is never converted into a zero-valued
resource result. Raw JSONL is fsync'd one record at a time so a stopped or
crashed run retains the last trustworthy observation.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import threading
import time
from pathlib import Path


SCHEMA = "gopulse.phase19.resources.v1"

LINK_METRICS_SCRIPTS = {
    "backend": 'wget -qO- --header="Authorization: Bearer $BACKEND_METRICS_TOKEN" http://127.0.0.1:19101/internal/v1/metrics',
    "business-worker": 'wget -qO- --header="Authorization: Bearer $BUSINESS_WORKER_METRICS_TOKEN" http://127.0.0.1:19102/internal/v1/metrics',
    "search-indexer": 'wget -qO- --header="Authorization: Bearer $SEARCH_INDEXER_METRICS_TOKEN" http://127.0.0.1:19103/internal/v1/metrics',
    "router": 'wget -qO- --header="Authorization: Bearer $ROUTER_METRICS_TOKEN" http://127.0.0.1:19105/internal/v1/metrics',
    "marshaller": 'wget -qO- --header="Authorization: Bearer $MARSHALLER_METRICS_TOKEN" http://127.0.0.1:19106/internal/v1/metrics',
}

LINK_FAMILIES = {
    "backend": [
        "gopulse_backend_outbox_pending",
        "gopulse_backend_outbox_oldest_age_seconds",
        "gopulse_backend_http_requests_in_flight",
        "gopulse_backend_http_concurrency_limit",
        "gopulse_backend_http_rejected_total",
    ],
    "business-worker": [
        "gopulse_business_worker_messages_in_flight",
        "gopulse_business_worker_prefetch_limit",
    ],
    "search-indexer": [
        "gopulse_search_indexer_messages_in_flight",
        "gopulse_search_indexer_retrying",
    ],
    "router": ["gopulse_router_buffered_records", "gopulse_router_buffered_bytes"],
    "marshaller": ["gopulse_marshaller_records_in_flight", "gopulse_marshaller_retrying"],
}

DEFAULT_REQUIRED_COMPONENTS = [
    "mysql", "redis", "rabbitmq", "elasticsearch", "observability-elasticsearch",
    "kafka", "victoriametrics", "backend", "backend-2", "business-worker",
    "business-worker-2", "search-indexer", "search-indexer-2", "router", "router-2",
    "marshaller", "marshaller-2", "monitor", "frontend", "admin-frontend",
]


def command(args, timeout=20, env=None):
    return subprocess.run(args, text=True, capture_output=True, timeout=timeout, env=env)


def parse_meminfo(text):
    values = {}
    for line in text.splitlines():
        if ":" not in line:
            continue
        key, raw = line.split(":", 1)
        fields = raw.strip().split()
        if fields:
            values[key] = int(fields[0]) * 1024
    return values


def parse_cpu_stat(text):
    line = next((line for line in text.splitlines() if line.startswith("cpu ")), "")
    fields = line.split()
    if len(fields) < 5:
        return None
    values = [int(value) for value in fields[1:]]
    total = sum(values)
    idle = values[3] + (values[4] if len(values) > 4 else 0)
    return {"total": total, "idle": idle}


def parse_size(value):
    value = (value or "").split("/", 1)[0]
    match = re.fullmatch(r"\s*([0-9.]+)\s*([KMGT]?i?B)\s*", value or "")
    if not match:
        return 0
    number = float(match.group(1))
    factors = {
        "B": 1,
        "KiB": 1024,
        "MiB": 1024**2,
        "GiB": 1024**3,
        "TiB": 1024**4,
        "KB": 1000,
        "MB": 1000**2,
        "GB": 1000**3,
        "TB": 1000**4,
    }
    return int(number * factors[match.group(2)])


def parse_ratio(value):
    try:
        return float((value or "0").strip().rstrip("%"))
    except ValueError:
        return 0.0


def metric_value(text, name):
    match = re.search(
        r"^" + re.escape(name) + r"(?:\s+|\{[^\n]*\}\s+)(-?[0-9.eE+]+)\s*$",
        text,
        re.MULTILINE,
    )
    return float(match.group(1)) if match else None


def metric_sum(text, name, labels=None):
    required = labels or {}
    total = 0.0
    for line in text.splitlines():
        match = re.fullmatch(re.escape(name) + r"(?:\{([^}]*)\})?\s+(-?[0-9.eE+]+)\s*", line)
        if not match:
            continue
        sample_labels = dict(re.findall(r'([a-zA-Z_][a-zA-Z0-9_]*)="((?:\\.|[^"])*)"', match.group(1) or ""))
        if any(sample_labels.get(key) != value for key, value in required.items()):
            continue
        total += float(match.group(2))
    return int(total) if total.is_integer() else total


def sha256_file(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def _process_stats(pid):
    if pid is None:
        return None
    process = Path("/proc") / str(pid)
    try:
        stat_fields = (process / "stat").read_text().split()
        statm_fields = (process / "statm").read_text().split()
        if len(stat_fields) < 15 or len(statm_fields) < 2:
            return None
        scheduler_fields = (process / "schedstat").read_text().split()
        scheduler_delay_ns = int(scheduler_fields[1]) if len(scheduler_fields) > 1 else 0
        return {
            "pid": int(pid),
            "rss_bytes": int(statm_fields[1]) * os.sysconf("SC_PAGE_SIZE"),
            "cpu_ticks": int(stat_fields[13]) + int(stat_fields[14]),
            "scheduler_delay_ns": scheduler_delay_ns,
        }
    except (OSError, IndexError, ValueError):
        return None


class Sampler:
    def __init__(self, project, compose_file, env_file, interval=5, raw_path=None, load_pid=None, required_components=None):
        if interval <= 0:
            raise ValueError("sampler interval must be positive")
        self.project = project
        if isinstance(compose_file, (list, tuple)):
            self.compose_files = [Path(item) for item in compose_file]
        else:
            self.compose_files = [Path(compose_file)]
        self.env_file = Path(env_file)
        self.interval = float(interval)
        self.raw_path = Path(raw_path) if raw_path is not None else None
        self.records = []
        self._stop = threading.Event()
        self._thread = None
        self._sample_lock = threading.Lock()
        self._load_pid = load_pid
        self._cpu_before = None
        self._load_before = None
        self._raw_stream = None
        self._failure = None
        self._stopped = False
        self.required_components = list(required_components or [])
        self._stage = None
        self._window = None
        self._progress_sequence = None

    def _open_raw(self):
        if self.raw_path is None or self._raw_stream is not None:
            return
        self.raw_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        descriptor = os.open(self.raw_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_APPEND, 0o600)
        self._raw_stream = os.fdopen(descriptor, "a", encoding="utf-8")

    def _persist_raw(self, record):
        if self._raw_stream is None:
            return
        self._raw_stream.write(json.dumps(record, sort_keys=True, separators=(",", ":"), ensure_ascii=False) + "\n")
        self._raw_stream.flush()
        os.fsync(self._raw_stream.fileno())

    def _close_raw(self):
        if self._raw_stream is None:
            return
        self._raw_stream.flush()
        os.fsync(self._raw_stream.fileno())
        self._raw_stream.close()
        self._raw_stream = None

    def set_load_pid(self, pid):
        if pid is not None and (not isinstance(pid, int) or pid <= 0):
            raise ValueError("load pid must be positive")
        self._load_pid = pid
        if pid is not None:
            process = _process_stats(pid)
            self._load_before = (process, time.monotonic()) if process is not None else None

    def set_window(self, stage=None, window=None, progress_sequence=None):
        self._stage = stage
        self._window = window
        self._progress_sequence = progress_sequence

    def record_progress(self, record):
        """Capture an immediate boundary sample for a durable load event."""
        self.set_window(record.get("stage") or None, record.get("window") or None, record.get("sequence"))
        self._sample(time.monotonic(), "boundary", stage=self._stage, window=self._window, progress_sequence=self._progress_sequence)

    def start(self):
        try:
            self._open_raw()
            self._sample(time.monotonic(), "initial")
        except Exception:
            self._close_raw()
            raise
        self._thread = threading.Thread(target=self._run, name="phase19-sampler", daemon=True)
        self._thread.start()

    def stop(self):
        if self._stopped:
            if self._failure is not None:
                raise RuntimeError("resource sampler failed") from self._failure
            return
        self._stopped = True
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=self.interval * 2 + 5)
            if self._thread.is_alive():
                self._failure = self._failure or RuntimeError("resource sampler thread did not stop")
        failure = self._failure
        if failure is None and not self.required_components:
            try:
                self._sample(time.monotonic(), "final")
            except Exception as error:
                failure = error
        try:
            self._close_raw()
        except Exception as error:
            if failure is None:
                failure = error
        if failure is not None:
            raise RuntimeError("resource sampler failed") from failure

    def _run(self):
        next_sample = time.monotonic() + self.interval
        try:
            while not self._stop.wait(max(0, next_sample - time.monotonic())):
                self._sample(next_sample, "scheduled")
                next_sample += self.interval
        except Exception as error:
            self._failure = error
            self._stop.set()

    def _compose(self, *args, timeout=30):
        command_line = ["docker", "compose", "--project-name", self.project, "--env-file", str(self.env_file)]
        for path in self.compose_files:
            command_line.extend(["-f", str(path)])
        return command(
            command_line + list(args),
            timeout=timeout,
        )

    def _sample(self, scheduled_at, sample_kind="scheduled", stage=None, window=None, progress_sequence=None):
        with self._sample_lock:
            memory = {}
            try:
                memory = parse_meminfo(Path("/proc/meminfo").read_text())
            except OSError:
                pass
            cpu = None
            try:
                cpu = parse_cpu_stat(Path("/proc/stat").read_text())
            except OSError:
                pass
            cpu_percent = None
            if cpu is not None and self._cpu_before is not None:
                total = cpu["total"] - self._cpu_before["total"]
                idle = cpu["idle"] - self._cpu_before["idle"]
                if total > 0:
                    cpu_percent = max(0.0, min(100.0, (total - idle) / total * 100))
            self._cpu_before = cpu
            observed_monotonic = time.monotonic()
            load_process = self._load_process()
            if load_process is not None and self._load_before is not None:
                previous, previous_at = self._load_before
                elapsed = observed_monotonic - previous_at
                if elapsed > 0:
                    load_process["cpu_percent"] = max(
                        0.0,
                        (load_process["cpu_ticks"] - previous["cpu_ticks"])
                        / os.sysconf("SC_CLK_TCK")
                        / elapsed
                        * 100,
                    )
            self._load_before = (load_process, observed_monotonic) if load_process is not None else None
            containers, restarts, oom = self._containers()
            links = self._links()
            rabbitmq = self._rabbitmq()
            mysql = self._mysql()
            kafka_lag = self._kafka_lag()
            redis = self._redis()
            elasticsearch = self._elasticsearch("elasticsearch")
            observability_elasticsearch = self._elasticsearch("observability-elasticsearch")
            victoriametrics = self._victoriametrics()
            sut_cpu = max((item.get("cpu_percent", 0) for item in containers), default=0.0)
            sut_rss = sum(item.get("memory_usage_bytes", 0) for item in containers)
            scheduler_lag_ms = max(0.0, (observed_monotonic - scheduled_at) * 1000)
            if load_process is not None:
                load_process["scheduler_lag_ms"] = scheduler_lag_ms
            required_component_set = set(self.required_components)
            component_resources = {
                item["service"]: {
                    "cpu_percent": item.get("cpu_percent"),
                    "rss_bytes": item.get("memory_usage_bytes", 0),
                    "running": item.get("running", False),
                    "restart_count": item.get("restart_count", 0),
                    "oom_killed": item.get("oom_killed", False),
                }
                for item in containers
                if item.get("service") and (not required_component_set or item.get("service") in required_component_set)
            }
            missing_components = sorted(set(self.required_components) - set(component_resources))
            missing_signals = []
            signal_present = {
                "host_cpu": cpu is not None,
                "host_rss": memory.get("MemTotal", 0) > 0 and memory.get("MemAvailable", 0) >= 0,
                "load_cpu": load_process is not None and load_process.get("cpu_percent") is not None,
                "load_rss": load_process is not None and load_process.get("rss_bytes") is not None,
                "load_scheduler_lag": load_process is not None and load_process.get("scheduler_lag_ms") is not None,
                "sut_cpu": bool(containers),
                "sut_rss": bool(containers),
                "sut_saturation": bool(links) and all(value is not None for value in links.values()),
                "outbox": bool((links.get("backend") or {}).get("gopulse_backend_outbox_pending") is not None),
                "rabbitmq": rabbitmq is not None,
                "kafka_lag": kafka_lag is not None,
            }
            missing_signals = sorted(name for name, present in signal_present.items() if not present)
            if sample_kind == "initial":
                missing_signals = [name for name in missing_signals if not name.startswith("load_")]
            if self.required_components and missing_components:
                raise RuntimeError("required resource components missing: " + ",".join(missing_components))
            if self.required_components and missing_signals:
                if self._stopped and set(missing_signals) <= {"load_cpu", "load_rss", "load_scheduler_lag"}:
                    return
                raise RuntimeError("required resource signals missing: " + ",".join(missing_signals))
            record = {
                "schema": SCHEMA,
                "sequence": len(self.records),
                "observed_at": time.time(),
                "interval_seconds": self.interval,
                "sample_kind": sample_kind,
                "scheduled_at": scheduled_at if sample_kind == "scheduled" else None,
                "stage": stage if stage is not None else self._stage,
                "window": window if window is not None else self._window,
                "progress_sequence": progress_sequence if progress_sequence is not None else self._progress_sequence,
                "host": {
                    "mem_total_bytes": memory.get("MemTotal", 0),
                    "mem_available_bytes": memory.get("MemAvailable", 0),
                    "swap_total_bytes": memory.get("SwapTotal", 0),
                    "swap_free_bytes": memory.get("SwapFree", 0),
                    "cpu_total_ticks": cpu["total"] if cpu else None,
                    "cpu_idle_ticks": cpu["idle"] if cpu else None,
                    "cpu_percent": cpu_percent,
                },
                "load_process": load_process,
                "sut": {
                    "containers": containers,
                    "cpu_percent": sut_cpu,
                    "rss_bytes": sut_rss,
                    "restart_count": restarts,
                    "oom_killed": oom,
                    "saturation": links,
                },
                "signals": {"rabbitmq": rabbitmq, "mysql": mysql, "redis": redis, "elasticsearch": elasticsearch, "observability_elasticsearch": observability_elasticsearch, "victoriametrics": victoriametrics, "kafka_lag": kafka_lag},
                # These aliases keep the sampler's source-level contract easy
                # to inspect while the nested fields make ownership explicit.
                "containers": containers,
                "restart_count": restarts,
                "oom_killed": oom,
                "links": links,
                "rabbitmq": rabbitmq,
                "mysql": mysql,
                "kafka_lag": kafka_lag,
                "redis": redis,
                "elasticsearch": elasticsearch,
                "observability_elasticsearch": observability_elasticsearch,
                "victoriametrics": victoriametrics,
                "component_resources": component_resources,
                "missing_components": missing_components,
                "missing_signals": missing_signals,
            }
            self._persist_raw(record)
            self.records.append(record)

    def _containers(self):
        result = self._compose("ps", "-aq")
        if result.returncode:
            raise RuntimeError("inspect owned Compose containers")
        identifiers = result.stdout.split()
        if not identifiers:
            raise RuntimeError("owned Compose project has no containers")
        inspected = command(["docker", "inspect", *identifiers], timeout=30)
        if inspected.returncode:
            raise RuntimeError("inspect owned Compose container labels")
        stats_result = command(["docker", "stats", "--no-stream", "--format", "{{json .}}", *identifiers], timeout=30)
        stats = {}
        if stats_result.returncode == 0:
            for line in stats_result.stdout.splitlines():
                try:
                    item = json.loads(line)
                    stats[item.get("Name", "")] = item
                except json.JSONDecodeError:
                    continue
        containers = []
        restarts = 0
        oom = 0
        for item in json.loads(inspected.stdout):
            labels = item.get("Config", {}).get("Labels") or {}
            if labels.get("com.docker.compose.project") != self.project:
                raise RuntimeError("Compose ownership was lost")
            service = labels.get("com.docker.compose.service", "")
            name = item.get("Name", "").lstrip("/")
            state = item.get("State", {})
            restarts += int(item.get("RestartCount", 0))
            oom += int(bool(state.get("OOMKilled")))
            stat = stats.get(name, {})
            containers.append(
                {
                    "service": service,
                    "container_sha256": hashlib.sha256(item.get("Id", "").encode()).hexdigest(),
                    "running": bool(state.get("Running")),
                    "restart_count": int(item.get("RestartCount", 0)),
                    "oom_killed": bool(state.get("OOMKilled")),
                    "cpu_percent": parse_ratio(stat.get("CPUPerc")),
                    "memory_usage_bytes": parse_size((stat.get("MemUsage") or "0B / 0B").split("/")[0]),
                    "block_io": stat.get("BlockIO", ""),
                    "network_io": stat.get("NetIO", ""),
                }
            )
        return containers, restarts, oom

    def _exec(self, service, script, timeout=20):
        result = self._compose("exec", "-T", service, "sh", "-c", script, timeout=timeout)
        return result.stdout if result.returncode == 0 else ""

    def _load_process(self):
        return _process_stats(self._load_pid)

    def _links(self):
        values = {}
        for service, script in LINK_METRICS_SCRIPTS.items():
            service_names = [service]
            if service in {"backend", "business-worker", "search-indexer", "router", "marshaller"}:
                service_names.append(service + "-2")
            for service_name in service_names:
                text = self._exec(service_name, script)
                values[service_name] = {name: metric_value(text, name) for name in LINK_FAMILIES[service]} if text else None
        return values

    def _rabbitmq(self):
        text = self._exec("rabbitmq", "rabbitmqctl list_queues -q name messages_ready messages_unacknowledged")
        if not text:
            return None
        ready = unacked = 0
        queues = 0
        for line in text.splitlines():
            fields = line.split()
            if len(fields) < 3:
                continue
            try:
                ready += int(fields[-2])
                unacked += int(fields[-1])
                queues += 1
            except ValueError:
                continue
        return {"ready": ready, "unacked": unacked, "queues": queues}

    def _mysql(self):
        sql = "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Innodb_buffer_pool_bytes_data','Innodb_buffer_pool_bytes_dirty')"
        text = self._exec("mysql", 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e ' + repr(sql))
        if not text:
            return None
        values = {}
        for line in text.splitlines():
            fields = line.split()
            if len(fields) == 2:
                try:
                    values[fields[0]] = int(fields[1])
                except ValueError:
                    continue
        return values or None

    def _redis(self):
        text = self._exec("redis", 'redis-cli --no-auth-warning -a "$REDIS_PASSWORD" INFO stats')
        if not text:
            return None
        values = {}
        for line in text.splitlines():
            if ":" not in line or line.startswith("#"):
                continue
            key, value = line.split(":", 1)
            try:
                values[key] = float(value)
            except ValueError:
                continue
        return values or None

    def _elasticsearch(self, service):
        result = self._compose("exec", "-T", service, "sh", "-c", "curl -fsS http://127.0.0.1:9200/_cluster/health", timeout=30)
        if result.returncode:
            return None
        try:
            value = json.loads(result.stdout)
        except json.JSONDecodeError:
            return None
        return {key: value[key] for key in ("status", "number_of_nodes", "active_shards", "unassigned_shards") if key in value}

    def _victoriametrics(self):
        result = self._compose("exec", "-T", "victoriametrics", "sh", "-c", "wget -qO- http://127.0.0.1:8428/metrics", timeout=30)
        if result.returncode:
            return None
        return {"up": metric_value(result.stdout, "vm_rows_inserted_total") is not None, "rows_inserted": metric_value(result.stdout, "vm_rows_inserted_total")}

    def _kafka_lag(self):
        script = 'KAFKA_GROUP=${MARSHALLER_KAFKA_GROUP:-gopulse-marshaller-metrics-v1}; /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:19092 --describe --group "$KAFKA_GROUP"'
        text = self._exec("kafka", script, timeout=30)
        if not text:
            return None
        lag = rows = 0
        for line in text.splitlines():
            fields = line.split()
            if len(fields) < 6 or fields[0].upper() == "GROUP":
                continue
            try:
                lag += int(fields[5])
                rows += 1
            except ValueError:
                continue
        return {"lag": lag, "partitions": rows}


def validate_sample_intervals(records, expected_interval, tolerance=0.25):
    if expected_interval <= 0:
        raise ValueError("sample interval must be positive")
    previous = None
    previous_scheduled = None
    previous_scheduled_at = None
    for index, record in enumerate(records):
        if record.get("sequence") != index:
            raise ValueError("resource sample sequence is not contiguous")
        observed = record.get("observed_at")
        if not isinstance(observed, (int, float)):
            raise ValueError("resource sample timestamp is invalid")
        if previous is not None and observed <= previous:
            raise ValueError("resource sample timestamps are not increasing")
        if record.get("sample_kind", "scheduled") == "scheduled":
            scheduled_at = record.get("scheduled_at")
            if previous_scheduled_at is not None and isinstance(scheduled_at, (int, float)):
                delta = scheduled_at - previous_scheduled_at
            elif previous_scheduled is not None:
                delta = observed - previous_scheduled
            else:
                delta = None
            if delta is not None:
                if abs(delta - expected_interval) > max(tolerance, expected_interval * 0.05):
                    raise ValueError("resource sample interval drifted")
            previous_scheduled = observed
            previous_scheduled_at = scheduled_at if isinstance(scheduled_at, (int, float)) else None
        previous = observed
        declared = record.get("interval_seconds")
        if declared is not None and abs(float(declared) - expected_interval) > max(tolerance, expected_interval * 0.05):
            raise ValueError("resource sample interval drifted")
    return records


def load_samples(path, expected_interval=None):
    records = []
    with Path(path).open(encoding="utf-8") as source:
        for line_number, line in enumerate(source, 1):
            if not line.strip():
                continue
            try:
                record = json.loads(line)
            except json.JSONDecodeError as error:
                raise ValueError("invalid raw resource sample at line %d" % line_number) from error
            if not isinstance(record, dict) or record.get("schema") != SCHEMA:
                raise ValueError("resource sample schema is invalid at line %d" % line_number)
            records.append(record)
    if expected_interval is not None:
        validate_sample_intervals(records, expected_interval)
    return records


def _max_link(records, service, metric):
    return max(
        (((item.get("links") or {}).get(service) or {}).get(metric) or 0) for item in records
    )


def summarize(records, required_components=None):
    if not records:
        raise ValueError("resource sampling produced no records")
    validate_sample_intervals(records, float(records[0].get("interval_seconds") or 0.001))
    started_swap = records[0]["host"].get("swap_free_bytes", 0)
    expected_components = list(required_components or sorted({name for item in records for name in (item.get("component_resources") or {})}))
    missing_signals = sorted({signal for item in records for signal in item.get("missing_signals", [])})
    return {
        "schema": SCHEMA,
        "samples": len(records),
        "interval_seconds": records[0].get("interval_seconds"),
        "oom_killed": max(int(item.get("oom_killed", 0)) for item in records),
        "restart_count": max(int(item.get("restart_count", 0)) for item in records),
        "max_swap_delta_bytes": max(0, started_swap - min(item["host"].get("swap_free_bytes", 0) for item in records)),
        "load_process_peak_rss_bytes": max(((item.get("load_process") or {}).get("rss_bytes") or 0) for item in records),
        "load_process_peak_cpu_ticks": max(((item.get("load_process") or {}).get("cpu_ticks") or 0) for item in records),
        "load_process_peak_cpu_percent": max(((item.get("load_process") or {}).get("cpu_percent") or 0) for item in records),
        "max_load_scheduler_lag_ms": max(((item.get("load_process") or {}).get("scheduler_lag_ms") or 0) for item in records),
        "sut_peak_rss_bytes": max(((item.get("sut") or {}).get("rss_bytes") or 0) for item in records),
        "sut_peak_cpu_percent": max(((item.get("sut") or {}).get("cpu_percent") or 0) for item in records),
        "peak_container_cpu_percent": max((container.get("cpu_percent", 0) for item in records for container in item.get("containers", [])), default=0),
        "max_outbox_pending": _max_link(records, "backend", "gopulse_backend_outbox_pending"),
        "max_outbox_oldest_age_seconds": _max_link(records, "backend", "gopulse_backend_outbox_oldest_age_seconds"),
        "max_backend_in_flight": _max_link(records, "backend", "gopulse_backend_http_requests_in_flight"),
        "max_backend_concurrency_limit": _max_link(records, "backend", "gopulse_backend_http_concurrency_limit"),
        "max_backend_rejected_total": _max_link(records, "backend", "gopulse_backend_http_rejected_total"),
        "max_router_buffered_records": _max_link(records, "router", "gopulse_router_buffered_records"),
        "max_search_indexer_retrying": _max_link(records, "search-indexer", "gopulse_search_indexer_retrying"),
        "max_marshaller_retrying": _max_link(records, "marshaller", "gopulse_marshaller_retrying"),
        "max_rabbit_ready": max(((item.get("rabbitmq") or {}).get("ready") or 0) for item in records),
        "max_rabbit_unacked": max(((item.get("rabbitmq") or {}).get("unacked") or 0) for item in records),
        "max_kafka_lag": max(((item.get("kafka_lag") or {}).get("lag") or 0) for item in records),
        "required_components": expected_components,
        "missing_signals": missing_signals,
    }


def write_samples(path, records, summary):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps({"schema": SCHEMA, "records": records, "summary": summary}, indent=2) + "\n")
    temporary.chmod(0o600)
    temporary.replace(path)


def summarize_samples(raw_path, output_path, expected_interval=None, required_components=None):
    records = load_samples(raw_path, expected_interval=expected_interval)
    summary = summarize(records, required_components=required_components)
    write_samples(output_path, records, summary)
    return records, summary
