#!/usr/bin/env python3
"""Bounded host, Compose, and link sampling for the Phase 18 baseline."""
from __future__ import annotations

import json
import os
import re
import subprocess
import threading
import time
from pathlib import Path

# Every component exposes its private metrics exposition at
# /internal/v1/metrics on its own process listener. The application ports
# (8080/9090/9091/9093) never serve this route.
LINK_METRICS_SCRIPTS = {
    'backend': 'wget -qO- --header="Authorization: Bearer $BACKEND_METRICS_TOKEN" http://127.0.0.1:19101/internal/v1/metrics',
    'business-worker': 'wget -qO- --header="Authorization: Bearer $BUSINESS_WORKER_METRICS_TOKEN" http://127.0.0.1:19102/internal/v1/metrics',
    'search-indexer': 'wget -qO- --header="Authorization: Bearer $SEARCH_INDEXER_METRICS_TOKEN" http://127.0.0.1:19103/internal/v1/metrics',
    'router': 'wget -qO- --header="Authorization: Bearer $ROUTER_METRICS_TOKEN" http://127.0.0.1:19105/internal/v1/metrics',
    'marshaller': 'wget -qO- --header="Authorization: Bearer $MARSHALLER_METRICS_TOKEN" http://127.0.0.1:19106/internal/v1/metrics',
}


def command(args, timeout=20, env=None):
    return subprocess.run(args, text=True, capture_output=True, timeout=timeout, env=env)


def parse_meminfo(text):
    values = {}
    for line in text.splitlines():
        if ':' not in line:
            continue
        key, raw = line.split(':', 1)
        fields = raw.strip().split()
        if fields:
            values[key] = int(fields[0]) * 1024
    return values


def parse_cpu_stat(text):
    line = next((line for line in text.splitlines() if line.startswith('cpu ')), '')
    fields = line.split()
    if len(fields) < 5:
        return None
    values = [int(value) for value in fields[1:]]
    total = sum(values)
    idle = values[3] + (values[4] if len(values) > 4 else 0)
    return {"total": total, "idle": idle}


def parse_size(value):
    value = (value or '').split('/', 1)[0]
    match = re.fullmatch(r'\s*([0-9.]+)\s*([KMGT]?i?B)\s*', value or '')
    if not match:
        return 0
    number = float(match.group(1))
    unit = match.group(2)
    factors = {'B': 1, 'KiB': 1024, 'MiB': 1024 ** 2, 'GiB': 1024 ** 3, 'TiB': 1024 ** 4,
               'KB': 1000, 'MB': 1000 ** 2, 'GB': 1000 ** 3, 'TB': 1000 ** 4}
    return int(number * factors[unit])


def parse_ratio(value):
    try:
        return float((value or '0').strip().rstrip('%'))
    except ValueError:
        return 0.0


def metric_value(text, name):
    match = re.search(r'^' + re.escape(name) + r'(?:\s+|\{[^\n]*\}\s+)(-?[0-9.eE+]+)\s*$', text, re.MULTILINE)
    return float(match.group(1)) if match else None


def metric_sum(text, name, labels=None):
    required = labels or {}
    total = 0.0
    for line in text.splitlines():
        match = re.fullmatch(re.escape(name) + r'(?:\{([^}]*)\})?\s+(-?[0-9.eE+]+)\s*', line)
        if not match:
            continue
        sample_labels = dict(re.findall(r'([a-zA-Z_][a-zA-Z0-9_]*)="((?:\\.|[^"])*)"', match.group(1) or ''))
        if any(sample_labels.get(key) != value for key, value in required.items()):
            continue
        total += float(match.group(2))
    return int(total) if total.is_integer() else total


class Sampler:
    def __init__(self, project, compose_file, env_file, interval=5):
        self.project = project
        self.compose_file = Path(compose_file)
        self.env_file = Path(env_file)
        self.interval = interval
        self.records = []
        self._stop = threading.Event()
        self._thread = None
        self._sample_lock = threading.Lock()
        self._load_pid = None
        self._cpu_before = None

    def set_load_pid(self, pid):
        self._load_pid = pid

    def start(self):
        self._sample()
        self._thread = threading.Thread(target=self._run, name='phase18-sampler', daemon=True)
        self._thread.start()

    def stop(self):
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=self.interval * 2 + 5)
        self._sample()

    def _run(self):
        while not self._stop.wait(self.interval):
            self._sample()

    def _compose(self, *args, timeout=30):
        return command(['docker', 'compose', '--project-name', self.project, '--env-file', str(self.env_file),
                        '-f', str(self.compose_file), *args], timeout=timeout)

    def _sample(self):
        with self._sample_lock:
            memory = {}
            try:
                memory = parse_meminfo(Path('/proc/meminfo').read_text())
            except OSError:
                pass
            cpu = None
            try:
                cpu = parse_cpu_stat(Path('/proc/stat').read_text())
            except OSError:
                pass
            cpu_delta = None
            if cpu is not None and self._cpu_before is not None:
                total = cpu['total'] - self._cpu_before['total']
                idle = cpu['idle'] - self._cpu_before['idle']
                if total > 0:
                    cpu_delta = max(0.0, min(100.0, (total - idle) / total * 100))
            self._cpu_before = cpu
            containers, restarts, oom = self._containers()
            record = {
                'schema': 1,
                'observed_at': time.time(),
                'host': {
                    'mem_total_bytes': memory.get('MemTotal', 0),
                    'mem_available_bytes': memory.get('MemAvailable', 0),
                    'swap_total_bytes': memory.get('SwapTotal', 0),
                    'swap_free_bytes': memory.get('SwapFree', 0),
                    'cpu_total_ticks': cpu['total'] if cpu else None,
                    'cpu_idle_ticks': cpu['idle'] if cpu else None,
                    'cpu_percent': cpu_delta,
                },
                'containers': containers,
                'restart_count': restarts,
                'oom_killed': oom,
                'load_process': self._load_process(),
                'links': self._links(),
                'rabbitmq': self._rabbitmq(),
                'mysql': self._mysql(),
                'kafka_lag': self._kafka_lag(),
            }
            self.records.append(record)

    def _containers(self):
        result = self._compose('ps', '-q')
        if result.returncode:
            return [], 0, 0
        identifiers = result.stdout.split()
        if not identifiers:
            return [], 0, 0
        inspected = command(['docker', 'inspect', *identifiers], timeout=30)
        if inspected.returncode:
            return [], 0, 0
        stats_result = command(['docker', 'stats', '--no-stream', '--format', '{{json .}}', *identifiers], timeout=30)
        stats = {}
        if stats_result.returncode == 0:
            for line in stats_result.stdout.splitlines():
                try:
                    item = json.loads(line)
                    stats[item.get('Name', '')] = item
                except json.JSONDecodeError:
                    continue
        containers = []
        restarts = 0
        oom = 0
        for item in json.loads(inspected.stdout):
            labels = item.get('Config', {}).get('Labels') or {}
            service = labels.get('com.docker.compose.service', '')
            name = item.get('Name', '').lstrip('/')
            state = item.get('State', {})
            restarts += int(item.get('RestartCount', 0))
            oom += int(bool(state.get('OOMKilled')))
            stat = stats.get(name, {})
            containers.append({
                'service': service,
                'container_sha256': __import__('hashlib').sha256(item.get('Id', '').encode()).hexdigest(),
                'running': bool(state.get('Running')),
                'restart_count': int(item.get('RestartCount', 0)),
                'oom_killed': bool(state.get('OOMKilled')),
                'cpu_percent': parse_ratio(stat.get('CPUPerc')),
                'memory_usage_bytes': parse_size((stat.get('MemUsage') or '0B / 0B').split('/')[0]),
                'block_io': stat.get('BlockIO', ''),
                'network_io': stat.get('NetIO', ''),
            })
        return containers, restarts, oom

    def _load_process(self):
        if self._load_pid is None:
            return None
        path = Path('/proc') / str(self._load_pid)
        try:
            stat = (path / 'stat').read_text().split()
            rss_pages = int((path / 'statm').read_text().split()[1])
            utime = int(stat[13])
            stime = int(stat[14])
            return {'rss_bytes': rss_pages * os.sysconf('SC_PAGE_SIZE'), 'cpu_ticks': utime + stime}
        except (OSError, IndexError, ValueError):
            return None

    def _exec(self, service, script, timeout=20):
        result = self._compose('exec', '-T', service, 'sh', '-c', script, timeout=timeout)
        return result.stdout if result.returncode == 0 else ''

    def _links(self):
        scripts = LINK_METRICS_SCRIPTS
        families = {
            'backend': ['gopulse_backend_outbox_pending', 'gopulse_backend_outbox_oldest_age_seconds'],
            'business-worker': ['gopulse_business_worker_messages_in_flight', 'gopulse_business_worker_prefetch_limit'],
            'search-indexer': ['gopulse_search_indexer_messages_in_flight', 'gopulse_search_indexer_retrying'],
            'router': ['gopulse_router_buffered_records', 'gopulse_router_buffered_bytes'],
            'marshaller': ['gopulse_marshaller_records_in_flight', 'gopulse_marshaller_retrying'],
        }
        values = {}
        for service, script in scripts.items():
            text = self._exec(service, script)
            values[service] = {name: metric_value(text, name) for name in families[service]} if text else None
        return values

    def _rabbitmq(self):
        text = self._exec('rabbitmq', 'rabbitmqctl list_queues -q name messages_ready messages_unacknowledged')
        if not text:
            return None
        ready = 0
        unacked = 0
        queues = []
        for line in text.splitlines():
            fields = line.split()
            if len(fields) < 3:
                continue
            try:
                queue_ready, queue_unacked = int(fields[-2]), int(fields[-1])
            except ValueError:
                continue
            ready += queue_ready
            unacked += queue_unacked
            queues.append({'ready': queue_ready, 'unacked': queue_unacked})
        return {'ready': ready, 'unacked': unacked, 'queues': len(queues)}

    def _mysql(self):
        sql = "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Innodb_buffer_pool_bytes_data','Innodb_buffer_pool_bytes_dirty')"
        text = self._exec('mysql', 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e ' + repr(sql))
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

    def _kafka_lag(self):
        script = 'KAFKA_GROUP=${MARSHALLER_KAFKA_GROUP:-gopulse-marshaller-metrics-v1}; /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:19092 --describe --group "$KAFKA_GROUP"'
        text = self._exec('kafka', script, timeout=30)
        if not text:
            return None
        lag = 0
        rows = 0
        for line in text.splitlines():
            fields = line.split()
            if len(fields) < 6 or fields[0].upper() == 'GROUP':
                continue
            try:
                lag += int(fields[5])
                rows += 1
            except ValueError:
                continue
        return {'lag': lag, 'partitions': rows}


def summarize(records):
    if not records:
        raise ValueError('resource sampling produced no records')
    started_swap = records[0]['host']['swap_free_bytes']
    max_swap_delta = max(0, started_swap - min(item['host']['swap_free_bytes'] for item in records))
    load_rss = max((item.get('load_process') or {}).get('rss_bytes', 0) for item in records)
    peak_cpu = max((container.get('cpu_percent', 0) for item in records for container in item['containers']), default=0)
    def link_peak(service, metric):
        return max((((item.get('links') or {}).get(service) or {}).get(metric) or 0) for item in records)
    return {
        'samples': len(records),
        'oom_killed': max(int(item.get('oom_killed', 0)) for item in records),
        'restart_count': max(int(item.get('restart_count', 0)) for item in records),
        'max_swap_delta_bytes': max_swap_delta,
        'load_process_peak_rss_bytes': load_rss,
        'peak_container_cpu_percent': peak_cpu,
        'max_outbox_pending': link_peak('backend', 'gopulse_backend_outbox_pending'),
        'max_outbox_oldest_age_seconds': link_peak('backend', 'gopulse_backend_outbox_oldest_age_seconds'),
        'max_router_buffered_records': link_peak('router', 'gopulse_router_buffered_records'),
        'max_search_indexer_retrying': link_peak('search-indexer', 'gopulse_search_indexer_retrying'),
        'max_marshaller_retrying': link_peak('marshaller', 'gopulse_marshaller_retrying'),
        'max_rabbit_ready': max(((item.get('rabbitmq') or {}).get('ready') or 0) for item in records),
        'max_rabbit_unacked': max(((item.get('rabbitmq') or {}).get('unacked') or 0) for item in records),
        'max_kafka_lag': max(((item.get('kafka_lag') or {}).get('lag') or 0) for item in records),
        'first_bottleneck': 'none observed',
    }


def write_samples(path, records, summary):
    path = Path(path)
    path.write_text(json.dumps({'schema': 'gopulse.phase18.resources.v1', 'records': records, 'summary': summary}, indent=2) + '\n')
    path.chmod(0o600)
