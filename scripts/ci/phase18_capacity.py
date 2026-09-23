#!/usr/bin/env python3
"""Run the immutable Phase 18 single-replica three-round capacity baseline."""
from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import platform
import secrets
import shutil
import subprocess
import sys
import time
from pathlib import Path

from phase18_evidence import GIB, atomic, evaluate_slo, repeatability, validate_capacity
from phase18_sampler import Sampler, command, metric_sum, parse_meminfo, summarize, write_samples
from release_artifacts import platform_ref, verify_bundle

ROOT = Path(__file__).resolve().parents[2]
EXPECTED_COUNTS = {
    "users": 5000, "posts": 50000, "comments": 100000,
    "post_likes": 200000, "user_follows": 200000,
    "post_bookmarks": 25000, "business_outbox": 550000,
    "notifications": 500000,
}
EXPECTED_RANGES = {
    "users": {"first": 1, "last": 5000},
    "posts": {"first": 1, "last": 50000},
    "comments": {"first": 1, "last": 100000},
}
VALID_PROJECT = __import__('re').compile(r'^gopulse-p18-01-[0-9a-f]{12}$')
LOAD_PASSWORD_BYTES = 32


def sha256_text(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


def run(args, timeout=300, env=None):
    return subprocess.run(args, text=True, capture_output=True, timeout=timeout, env=env)


def require(result, operation):
    if result.returncode:
        raise RuntimeError(operation + ' failed')
    return result.stdout


def json_output(result, operation):
    require(result, operation)
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError(operation + ' returned invalid JSON') from error


def parse_env(path):
    values = {}
    for line in path.read_text().splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith('#') or '=' not in line:
            continue
        key, value = line.split('=', 1)
        values[key] = value
    return values


def write_env(path, values):
    path.write_text(''.join(key + '=' + value + '\n' for key, value in sorted(values.items())))
    path.chmod(0o600)


def write_secret(path, value):
    path.write_text(value + '\n')
    path.chmod(0o600)
    return path


def host_inventory():
    meminfo = parse_meminfo(Path('/proc/meminfo').read_text())
    docker = json_output(run(['docker', 'info', '--format', '{{json .}}']), 'inspect Docker')
    compose = run(['docker', 'compose', 'version'])
    identifiers = run(['docker', 'ps', '--format', '{{.ID}}'])
    require(identifiers, 'inspect active containers')
    active_compose_projects = []
    if identifiers.stdout.split():
        inspected = json_output(run(['docker', 'inspect', *identifiers.stdout.split()]), 'inspect active container labels')
        active_compose_projects = sorted({
            (item.get('Config', {}).get('Labels') or {}).get('com.docker.compose.project', '')
            for item in inspected
        } - {''})
    machine = platform.machine()
    architecture = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(machine, machine)
    docker_architecture = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(docker.get('Architecture', ''), docker.get('Architecture', ''))
    return {
        'platform': 'linux/' + architecture,
        'host_os': platform.system(),
        'host_arch': machine,
        'kernel': platform.release(),
        'cpu_count': os.cpu_count(),
        'memory_bytes': meminfo.get('MemTotal', 0),
        'swap_total_bytes': meminfo.get('SwapTotal', 0),
        'disk_available_bytes': shutil.disk_usage(ROOT).free,
        'docker_server_os': docker.get('OSType', ''),
        'docker_server_arch': docker_architecture,
        'docker_server_version': docker.get('ServerVersion', ''),
        'compose_version': compose.stdout.strip() if compose.returncode == 0 else '',
        'active_compose_projects': active_compose_projects,
    }


def preflight(host):
    problems = []
    if host['platform'] != 'linux/amd64' or host['host_os'] != 'Linux' or 'WSL2' not in host['kernel']:
        problems.append('reference host must be WSL2 Linux amd64')
    if host['cpu_count'] != 8:
        problems.append('exactly 8 vCPU is required')
    if host['memory_bytes'] < 12 * GIB:
        problems.append('12 GiB RAM is required')
    if host['swap_total_bytes'] < 8 * GIB:
        problems.append('8 GiB swap is required')
    if host['disk_available_bytes'] < 100 * GIB:
        problems.append('at least 100 GiB free disk is required')
    if host['docker_server_os'] != 'linux' or host['docker_server_arch'] != 'amd64' or not host['docker_server_version']:
        problems.append('real Linux amd64 Docker Engine is required')
    if not host['compose_version']:
        problems.append('Docker Compose v2 is required')
    if host['active_compose_projects']:
        problems.append('no competing Compose project may be running')
    return problems


def resource_inventory():
    result = {'containers': [], 'volumes': [], 'networks': []}
    for key, args in (
        ('containers', ['docker', 'ps', '-aq']),
        ('volumes', ['docker', 'volume', 'ls', '-q']),
        ('networks', ['docker', 'network', 'ls', '-q']),
    ):
        value = run(args)
        if value.returncode:
            raise RuntimeError('inspect Docker resource inventory')
        result[key] = sorted(value.stdout.split())
    return result


def candidate_binding(manifest_path):
    manifest = verify_bundle(manifest_path)
    if manifest['version'] != '2.0.1':
        raise ValueError('Phase 18-01 requires a 2.0.1 candidate manifest')
    image_digests = {
        name: platform_ref(image, 'linux/amd64').split('@', 1)[1]
        for name, image in manifest['images'].items()
    }
    return manifest, {
        'version': manifest['version'],
        'revision': manifest['revision'],
        'manifest_sha256': 'sha256:' + hashlib.sha256(manifest_path.read_bytes()).hexdigest(),
        'bundle_sha256': manifest['bundle_sha256'],
        'image_digests': image_digests,
    }


def prepare_workspace(work, binding):
    work.mkdir(parents=True, exist_ok=True, mode=0o700)
    if work.stat().st_mode & 0o77:
        raise ValueError('capacity work directory must be private')
    lock_path = work / '.lock'
    lock_path.touch(exist_ok=True)
    lock_path.chmod(0o600)
    lock = lock_path.open('a')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    binding_path = work / 'binding.json'
    expected = {'schema': 'gopulse.phase18.binding.v1', **binding}
    if binding_path.exists():
        if json.loads(binding_path.read_text()) != expected:
            raise ValueError('capacity workspace belongs to another candidate or host')
    else:
        atomic(binding_path, expected)
    if (work / 'evidence' / 'capacity.json').exists():
        raise ValueError('capacity evidence already exists; refusing to overwrite completed acceptance')
    return lock


def candidate_environment(manifest, output, round_number):
    values = parse_env(ROOT / '.env.example')
    values.update({
        'GOPULSE_VERSION': manifest['version'],
        'GOPULSE_REVISION': manifest['revision'],
        'GOPULSE_IMAGE_TAG': manifest['version'] + '-phase18-' + manifest['revision'][:12],
        'GOPULSE_RUNTIME_MODE': 'container',
        'PUBLISHED_HOST': '127.0.0.1',
        'FRONTEND_PORT': str(18080 + round_number),
        'HTTP_PORT': str(18090 + round_number),
        'MYSQL_PORT': str(18306 + round_number),
    })
    for name, image in {**manifest['images'], **manifest['third_party']}.items():
        values['GOPULSE_' + name.upper().replace('-', '_') + '_IMAGE'] = platform_ref(image, 'linux/amd64')
    write_env(output, values)
    return values


def compose(env_file, compose_file, project, *args, timeout=600):
    return run(['docker', 'compose', '--project-name', project, '--env-file', str(env_file), '-f', str(compose_file), *args], timeout=timeout)


def compose_override(path, mysql_port):
    # MySQL joins the internal business network. Docker does not publish a
    # host port from an internal-only network, so add a dedicated bridge that
    # only this service joins for the duration of the acceptance project.
    path.write_text('''services:
  mysql:
    ports:
      - 127.0.0.1:%d:3306
    networks:
      business:
      acceptance:
networks:
  acceptance:
''' % mysql_port)
    path.chmod(0o600)


def write_partial(path, value):
    atomic(path, value)


def elasticsearch_count(env_file, compose_file, project, alias):
    result = compose(env_file, compose_file, project, 'exec', '-T', 'elasticsearch', 'curl', '-fsS',
                     'http://127.0.0.1:9200/' + alias + '/_count', timeout=30)
    if result.returncode:
        return -1
    try:
        return int(json.loads(result.stdout)['count'])
    except (KeyError, ValueError, json.JSONDecodeError):
        return -1


def marshaller_store_counts(env_file, compose_file, project):
    script = 'wget -qO- --header="Authorization: Bearer $MARSHALLER_METRICS_TOKEN" http://127.0.0.1:9093/metrics'
    result = compose(env_file, compose_file, project, 'exec', '-T', 'marshaller', 'sh', '-c', script, timeout=30)
    if result.returncode:
        return None
    return {
        kind: metric_sum(result.stdout, 'gopulse_marshaller_records_total', {
            'type': kind, 'stage': 'store', 'result': 'stored',
        })
        for kind in ('metrics', 'logs', 'events')
    }


def observability_snapshot(env_file, compose_file, project):
    logs_count = elasticsearch_count(env_file, compose_file, project, 'gopulse-logs-v1-read')
    events_count = elasticsearch_count(env_file, compose_file, project, 'gopulse-events-v1-read')
    store_counts = marshaller_store_counts(env_file, compose_file, project)
    if logs_count < 0 or events_count < 0 or store_counts is None:
        return None
    return {
        'logs_count': logs_count,
        'events_count': events_count,
        'marshaller_store_counts': store_counts,
    }


def observability_progress(snapshot, baseline):
    if snapshot is None:
        return False
    if baseline is None:
        return snapshot['logs_count'] > 0 and snapshot['events_count'] > 0 and all(
            value > 0 for value in snapshot['marshaller_store_counts'].values()
        )
    return (
        snapshot['logs_count'] > baseline['logs_count']
        and snapshot['events_count'] > baseline['events_count']
        and all(
            snapshot['marshaller_store_counts'][kind] > baseline['marshaller_store_counts'][kind]
            for kind in ('metrics', 'logs', 'events')
        )
    )


def wait_convergence(env_file, compose_file, project, timeout=3600, baseline=None):
    started = time.monotonic()
    search_seconds = None
    notification_seconds = None
    observability_seconds = None
    last = None
    stable_state = None
    stable_since = None
    stable_observability = None
    stable_observability_since = None
    latest_observability = None
    while time.monotonic() - started < timeout:
        sql = ("SELECT (SELECT COUNT(*) FROM business_outbox WHERE status IN ('pending','leased')),"
               "(SELECT COUNT(*) FROM notifications),(SELECT COUNT(*) FROM posts)")
        result = compose(env_file, compose_file, project, 'exec', '-T', 'mysql', 'sh', '-c',
                         'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e "$1"',
                         'sh', sql, timeout=30)
        if result.returncode:
            time.sleep(2)
            continue
        fields = result.stdout.split()
        if len(fields) != 3:
            time.sleep(2)
            continue
        outbox, notifications, posts = (int(value) for value in fields)
        rabbit_result = compose(env_file, compose_file, project, 'exec', '-T', 'rabbitmq', 'rabbitmqctl',
                                'list_queues', '-q', 'name', 'messages_ready', 'messages_unacknowledged', timeout=30)
        ready = unacked = 0
        if rabbit_result.returncode == 0:
            for line in rabbit_result.stdout.splitlines():
                queue = line.split()
                if len(queue) >= 3:
                    try:
                        ready += int(queue[-2])
                        unacked += int(queue[-1])
                    except ValueError:
                        pass
        search_count = elasticsearch_count(env_file, compose_file, project, 'gopulse-post-search-v1')
        kafka_lag = kafka_group_lag(env_file, compose_file, project)
        observability = observability_snapshot(env_file, compose_file, project)
        if observability is not None:
            latest_observability = observability
        now = time.monotonic() - started
        if search_count == posts and search_seconds is None:
            search_seconds = now
        if outbox == 0 and ready == 0 and unacked == 0 and notifications >= EXPECTED_COUNTS['notifications'] and notification_seconds is None:
            notification_seconds = now
        observability_ready = observability_progress(observability, baseline) and kafka_lag == 0
        if observability_ready:
            observability_state = (
                observability['logs_count'], observability['events_count'],
                tuple(observability['marshaller_store_counts'][kind] for kind in ('metrics', 'logs', 'events')),
            )
            if observability_state != stable_observability:
                stable_observability = observability_state
                stable_observability_since = now
        else:
            stable_observability = None
            stable_observability_since = None
        state = (outbox, notifications, posts, ready, unacked, search_count, kafka_lag)
        if state != stable_state:
            stable_state = state
            stable_since = now
        complete = (outbox == 0 and ready == 0 and unacked == 0 and kafka_lag == 0
                    and search_count == posts and notifications >= EXPECTED_COUNTS['notifications'])
        if complete and observability_ready and observability_seconds is None and stable_observability_since is not None and now - stable_observability_since >= 6:
            observability_seconds = now
        if complete and observability_ready and search_seconds is not None and notification_seconds is not None and observability_seconds is not None and stable_since is not None and now - stable_since >= 6:
            if latest_observability is None or not observability_progress(latest_observability, baseline):
                raise RuntimeError('observability convergence evidence disappeared')
            before = baseline or {
                'logs_count': 0, 'events_count': 0,
                'marshaller_store_counts': {kind: 0 for kind in ('metrics', 'logs', 'events')},
            }
            return {
                'outbox_pending': outbox, 'rabbit_ready': ready, 'rabbit_unacked': unacked,
                'search_count': search_count, 'mysql_posts': posts, 'notifications': notifications,
                'kafka_lag': kafka_lag,
                'logs_count_before': before['logs_count'], 'logs_count_after': latest_observability['logs_count'],
                'events_count_before': before['events_count'], 'events_count_after': latest_observability['events_count'],
                'marshaller_store_before': before['marshaller_store_counts'],
                'marshaller_store_after': latest_observability['marshaller_store_counts'],
                'search_seconds': round(search_seconds, 3),
                'notification_seconds': round(notification_seconds, 3),
                'metrics_logs_events_seconds': round(observability_seconds, 3),
                'recovery_seconds': round(now, 3),
            }
        last = state
        time.sleep(3)
    raise RuntimeError('projection convergence timeout: ' + repr(last))


def kafka_group_lag(env_file, compose_file, project):
    group = 'gopulse-marshaller-metrics-v1'
    result = compose(env_file, compose_file, project, 'exec', '-T', 'kafka', 'sh', '-c',
                     '/opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:19092 --describe --group "$1"',
                     'sh', group, timeout=40)
    if result.returncode:
        return -1
    lag = 0
    for line in result.stdout.splitlines():
        fields = line.split()
        if len(fields) < 6 or fields[0].upper() == 'GROUP':
            continue
        try:
            lag += int(fields[5])
        except ValueError:
            continue
    return lag


def build_loadtest(work):
    binaries = work / 'bin'
    binaries.mkdir(parents=True, exist_ok=True, mode=0o700)
    recipe = binaries / 'recipe'
    load = binaries / 'load'
    for name, target in ((recipe, './cmd/recipe'), (load, './cmd/load')):
        result = run(['go', '-C', str(ROOT / 'loadtest'), 'build', '-trimpath', '-o', str(name), target], timeout=300, env={**os.environ, 'GOWORK': 'off'})
        require(result, 'build loadtest ' + target)
        name.chmod(0o700)
    return recipe, load


def inspect_recipe(recipe_binary, binding, output):
    args = [str(recipe_binary), '--inspect', '--receipt', str(output), '--seed', '18002005',
            '--candidate-version', binding['version'], '--candidate-revision', binding['revision'],
            '--candidate-manifest-sha256', binding['manifest_sha256']]
    require(run(args, timeout=300), 'inspect deterministic recipe')
    return json.loads(output.read_text())


def generate_recipe(recipe_binary, binding, env, round_dir, mysql_port):
    dsn = '{}:{}@tcp(127.0.0.1:{})/{}?parseTime=true&loc=UTC&timeout=5s'.format(
        env['MYSQL_USER'], env['MYSQL_PASSWORD'], mysql_port, env['MYSQL_DATABASE'])
    dsn_file = write_secret(round_dir / 'mysql.dsn', dsn)
    password = secrets.token_urlsafe(LOAD_PASSWORD_BYTES)
    password_file = write_secret(round_dir / 'load-password', password)
    args = [str(recipe_binary), '--dsn-file', str(dsn_file), '--password-file', str(password_file), '--seed', '18002005',
            '--receipt', str(round_dir / 'recipe-receipt.json'), '--corpus', str(round_dir / 'corpus.json'),
            '--credentials', str(round_dir / 'credentials.json'),
            '--candidate-version', binding['version'], '--candidate-revision', binding['revision'],
            '--candidate-manifest-sha256', binding['manifest_sha256']]
    try:
        result = run(args, timeout=7200)
        if result.returncode:
            raise RuntimeError('generate deterministic recipe')
        rejection_args = [str(recipe_binary), '--dsn-file', str(dsn_file), '--password-file', str(password_file),
                          '--seed', '18002005', '--receipt', str(round_dir / 'reject-receipt.json'),
                          '--corpus', str(round_dir / 'reject-corpus.json'),
                          '--credentials', str(round_dir / 'reject-credentials.json')]
        rejection = run(rejection_args, timeout=300)
        if rejection.returncode != 3:
            raise RuntimeError('non-empty recipe target was not rejected safely')
        for path in ('reject-receipt.json', 'reject-corpus.json', 'reject-credentials.json'):
            (round_dir / path).unlink(missing_ok=True)
    finally:
        dsn_file.unlink(missing_ok=True)
        password_file.unlink(missing_ok=True)
    return json.loads((round_dir / 'recipe-receipt.json').read_text())


def baseline_environment(environment, output):
    values = dict(environment)
    for key in ('OUTBOX_POLL_INTERVAL', 'OUTBOX_CLAIM_BATCH', 'OUTBOX_LEASE_DURATION',
                'BUSINESS_WORKER_PREFETCH', 'SEARCH_INDEXER_PREFETCH'):
        values.pop(key, None)
    defaults = parse_env(ROOT / '.env.example')
    for key in ('OUTBOX_POLL_INTERVAL', 'OUTBOX_CLAIM_BATCH', 'OUTBOX_LEASE_DURATION',
                'BUSINESS_WORKER_PREFETCH', 'SEARCH_INDEXER_PREFETCH'):
        values[key] = defaults[key]
    write_env(output, values)
    return values


def first_bottleneck(report, resources, convergence, records):
    phases = report.get('phases', [])
    scheduled = sum(phase.get('scheduled_slots', 0) for phase in phases)
    dropped = sum(phase.get('dropped_slots', 0) for phase in phases)
    max_lag = max((phase.get('max_schedule_lag_ms', 0) for phase in phases), default=0)
    if report.get('load_process', {}).get('rss_bytes', 0) > 2 * GIB or dropped / max(scheduled, 1) > .001 or max_lag > 50:
        return {
            'component': 'loadtest',
            'reason_code': 'load_generator_scheduling_or_memory',
            'fact': 'dropped slots=%d/%d, max scheduling lag=%.3fms, load RSS=%d bytes' % (
                dropped, scheduled, max_lag, report.get('load_process', {}).get('rss_bytes', 0)),
        }
    if resources['oom_killed']:
        component = next((container.get('service', 'unknown') for record in records for container in record.get('containers', []) if container.get('oom_killed')), 'project')
        return {
            'component': component,
            'reason_code': 'oom_killed',
            'fact': 'sampler observed at least one container OOM kill',
        }
    if resources['restart_count']:
        component = next((container.get('service', 'unknown') for record in records for container in record.get('containers', []) if container.get('restart_count', 0)), 'project')
        return {
            'component': component,
            'reason_code': 'container_restart',
            'fact': 'sampler observed %d container restarts' % resources['restart_count'],
        }
    if resources['max_swap_delta_bytes'] > 256 * (1024 ** 2):
        return {
            'component': 'host',
            'reason_code': 'swap_growth',
            'fact': 'swap use grew by %d bytes' % resources['max_swap_delta_bytes'],
        }

    signals = (
        ('backend', 'outbox_backlog', lambda record: (((record.get('links') or {}).get('backend') or {}).get('gopulse_backend_outbox_pending') or 0)),
        ('rabbitmq-consumers', 'rabbitmq_backlog', lambda record: max((((record.get('rabbitmq') or {}).get('ready') or 0), ((record.get('rabbitmq') or {}).get('unacked') or 0)))),
        ('marshaller', 'kafka_lag', lambda record: ((record.get('kafka_lag') or {}).get('lag') or 0)),
        ('router', 'router_buffer', lambda record: (((record.get('links') or {}).get('router') or {}).get('gopulse_router_buffered_records') or 0)),
        ('search-indexer', 'search_indexer_retrying', lambda record: (((record.get('links') or {}).get('search-indexer') or {}).get('gopulse_search_indexer_retrying') or 0)),
        ('marshaller', 'marshaller_retrying', lambda record: (((record.get('links') or {}).get('marshaller') or {}).get('gopulse_marshaller_retrying') or 0)),
    )
    for component, reason, select in signals:
        for record in records:
            value = select(record)
            if value:
                return {
                    'component': component,
                    'reason_code': reason,
                    'fact': '%s first reached %s while load was active' % (reason, value),
                }

    for record in records:
        containers = record.get('containers') or []
        if not containers:
            continue
        container = max(containers, key=lambda item: item.get('cpu_percent', 0))
        if container.get('cpu_percent', 0) >= 80:
            return {
                'component': container.get('service', 'unknown'),
                'reason_code': 'container_cpu_saturation',
                'fact': 'container CPU first reached %.2f percent' % container['cpu_percent'],
            }
        if (record.get('host') or {}).get('cpu_percent', 0) >= 95:
            return {
                'component': 'host',
                'reason_code': 'host_cpu_saturation',
                'fact': 'host CPU first reached %.2f percent' % record['host']['cpu_percent'],
            }

    if convergence['search_seconds'] > 30:
        return {'component': 'search-projection', 'reason_code': 'search_convergence_timeout', 'fact': 'search projection took %.3fs' % convergence['search_seconds']}
    if convergence['notification_seconds'] > 30:
        return {'component': 'notification-projection', 'reason_code': 'notification_convergence_timeout', 'fact': 'notification projection took %.3fs' % convergence['notification_seconds']}
    if convergence['metrics_logs_events_seconds'] > 60:
        return {'component': 'observability-pipeline', 'reason_code': 'observability_convergence_timeout', 'fact': 'Metrics/Logs/Events took %.3fs' % convergence['metrics_logs_events_seconds']}
    if convergence['recovery_seconds'] > 600:
        return {'component': 'queue-recovery', 'reason_code': 'recovery_timeout', 'fact': 'queue recovery took %.3fs' % convergence['recovery_seconds']}

    steady = next((phase for phase in phases if phase.get('name') == 'steady'), None)
    if steady:
        candidates = []
        read = steady['latency_by_category']['read']
        candidates.append(('read', max(read['p95_ms'] / 500, read['p99_ms'] / 1500), 'read latency exceeded the steady SLO'))
        for category in ('content_write', 'interaction_write'):
            latency = steady['latency_by_category'][category]
            candidates.append((category, max(latency['p95_ms'] / 800, latency['p99_ms'] / 2000), category + ' latency exceeded the steady SLO'))
        category, _, fact = max(candidates, key=lambda item: item[1])
        if any(value > 1 for _, value, _ in candidates):
            return {'component': 'backend-or-storage', 'reason_code': category + '_latency_slo', 'fact': fact}
        failures = steady['counts']['timeouts'] + steady['counts']['errors']
        if failures / max(steady['counts']['requests'], 1) > .01:
            return {'component': 'edge', 'reason_code': 'unexpected_request_failure', 'fact': 'steady failures=%d/%d' % (failures, steady['counts']['requests'])}
    return {'component': 'capacity', 'reason_code': 'slo_gate_failure', 'fact': 'SLO failed without a stronger resource or queue signal'}


def run_round(recipe_binary, load_binary, manifest, binding, work, round_number):
    project = 'gopulse-p18-01-' + secrets.token_hex(6)
    if not VALID_PROJECT.fullmatch(project):
        raise RuntimeError('generated unsafe project name')
    round_dir = work / ('round-%d' % round_number)
    round_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    environment = candidate_environment(manifest, round_dir / 'candidate.env', round_number)
    recipe_environment = dict(environment)
    recipe_environment.update({
        'OUTBOX_POLL_INTERVAL': '10ms',
        'OUTBOX_CLAIM_BATCH': '100',
        'OUTBOX_LEASE_DURATION': '10m',
        'BUSINESS_WORKER_PREFETCH': '100',
        'SEARCH_INDEXER_PREFETCH': '100',
    })
    recipe_env_file = round_dir / 'recipe.env'
    write_env(recipe_env_file, recipe_environment)
    baseline_env_file = round_dir / 'baseline.env'
    baseline_environment(environment, baseline_env_file)
    override = round_dir / 'compose.override.yaml'
    compose_override(override, int(environment['MYSQL_PORT']))
    project_hash = sha256_text(project)
    before = resource_inventory()
    try:
        result = compose(recipe_env_file, manifest['compose']['path'], project,
                         '-f', str(override), 'up', '-d', '--wait', '--wait-timeout', '900', timeout=1000)
        require(result, 'start isolated candidate project')
        receipt = generate_recipe(recipe_binary, binding, recipe_environment, round_dir, int(environment['MYSQL_PORT']))
        reindex = compose(recipe_env_file, Path(manifest['compose']['path']), project, 'run', '--rm', '--no-deps',
                          '--entrypoint', '/usr/local/bin/search-reindex', 'search-init', timeout=1800)
        require(reindex, 'run formal search reindex')
        recipe_convergence = wait_convergence(recipe_env_file, Path(manifest['compose']['path']), project, timeout=3600)
        restart = compose(baseline_env_file, Path(manifest['compose']['path']), project, 'up', '-d', '--force-recreate',
                          '--wait', '--wait-timeout', '900', 'backend', 'business-worker', 'search-indexer', timeout=1000)
        require(restart, 'restart candidate with baseline runtime configuration')
        observability_baseline = observability_snapshot(baseline_env_file, Path(manifest['compose']['path']), project)
        if observability_baseline is None:
            raise RuntimeError('capture observability baseline before load')
        sampler = Sampler(project, Path(manifest['compose']['path']), baseline_env_file)
        sampler.start()
        load_process = subprocess.Popen([
            str(load_binary), '--base-url', 'http://127.0.0.1:' + environment['FRONTEND_PORT'],
            '--corpus', str(round_dir / 'corpus.json'), '--credentials', str(round_dir / 'credentials.json'),
            '--report', str(round_dir / 'load-report.json'), '--vus', '1024',
            '--warmup', '5m', '--steady', '15m', '--burst', '2m', '--steady-rps', '150', '--burst-rps', '300',
        ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        sampler.set_load_pid(load_process.pid)
        try:
            stdout, stderr = load_process.communicate(timeout=1800)
        except subprocess.TimeoutExpired:
            load_process.kill()
            stdout, stderr = load_process.communicate()
            raise RuntimeError('load generator exceeded the fixed 22-minute window')
        if load_process.returncode:
            raise RuntimeError('load generator failed: ' + (stderr or stdout)[-300:])
        recovery_started = time.monotonic()
        convergence = wait_convergence(baseline_env_file, Path(manifest['compose']['path']), project,
                                       timeout=600, baseline=observability_baseline)
        convergence['recovery_seconds'] = round(time.monotonic() - recovery_started, 3)
        sampler.stop()
        resources = summarize(sampler.records)
        containers = [container for record in sampler.records for container in record['containers']]
        if containers:
            peak = max(containers, key=lambda item: item.get('cpu_percent', 0))
            resources['peak_container_cpu_component'] = peak.get('service', 'unknown')
        write_samples(round_dir / 'resources.json', sampler.records, resources)
        load_report = json.loads((round_dir / 'load-report.json').read_text())
        round_value = {
            'id': round_number, 'project_sha256': project_hash,
            'recipe_receipt': receipt, 'load_report': load_report,
            'resources': {key: resources[key] for key in (
                'samples', 'oom_killed', 'restart_count', 'max_swap_delta_bytes',
                'load_process_peak_rss_bytes', 'peak_container_cpu_percent', 'first_bottleneck')},
            'convergence': convergence,
        }
        round_value['resources']['first_bottleneck'] = (
            first_bottleneck(load_report, resources, convergence, sampler.records)
            if evaluate_slo([round_value])['status'] == 'failed'
            else {'component': 'none', 'reason_code': 'none_observed', 'fact': 'all capacity gates passed'}
        )
        resources['first_bottleneck'] = round_value['resources']['first_bottleneck']
        write_samples(round_dir / 'resources.json', sampler.records, resources)
        return round_value, project_hash
    finally:
        cleanup = compose(recipe_env_file, Path(manifest['compose']['path']), project, 'down', '--volumes', '--remove-orphans', '--timeout', '30', timeout=300)
        if cleanup.returncode:
            raise RuntimeError('cleanup owned project failed')
        after = resource_inventory()
        if after != before:
            raise RuntimeError('owned project cleanup changed unrelated Docker resources')


def run_capacity(manifest_path, rounds, work):
    preflight_document = run_preflight(work)
    if preflight_document['problems']:
        raise RuntimeError('reference-host preflight failed: ' + '; '.join(preflight_document['problems']))
    manifest, binding = candidate_binding(manifest_path)
    manifest['compose']['path'] = str(manifest_path.parent / manifest['compose']['path'])
    lock = prepare_workspace(work, binding)
    if rounds != 3:
        raise ValueError('Phase 18-01 requires exactly three rounds')
    recipe_binary, load_binary = build_loadtest(work)
    first = inspect_recipe(recipe_binary, binding, work / 'recipe-inspect-1.json')
    second = inspect_recipe(recipe_binary, binding, work / 'recipe-inspect-2.json')
    if first['digest'] != second['digest'] or first['counts'] != second['counts'] or first['id_ranges'] != second['id_ranges']:
        raise RuntimeError('two recipe inspections were not deterministic')
    results = []
    for round_number in range(1, rounds + 1):
        result, _ = run_round(recipe_binary, load_binary, manifest, binding, work, round_number)
        results.append(result)
        print('PASS: capacity round %d complete' % round_number, flush=True)
    repeated = repeatability(results)
    slo = evaluate_slo(results)
    slo['first_bottleneck'] = None
    if slo['status'] == 'failed':
        slo['first_bottleneck'] = next((
            item['resources']['first_bottleneck'] for item in results
            if item['resources']['first_bottleneck'].get('reason_code') != 'none_observed'
        ), {'component': 'capacity', 'reason_code': 'slo_gate_failure', 'fact': 'SLO failed without a stronger round-level signal'})
    document = {
        'schema': 'gopulse.phase18.capacity.v1', 'execution_status': 'complete', 'complete': True,
        'candidate': binding, 'host': preflight_document['host'],
        'recipe': {
            'schema_version': first['schema_version'], 'seed': first['seed'],
            'counts': first['counts'], 'id_ranges': first['id_ranges'], 'digest': first['digest'],
            'same_seed_repeat': True, 'nonempty_rejection': True,
        },
        'rounds': results, 'repeatability': repeated, 'slo': slo,
        'cleanup': 'passed', 'secret_scan': 'passed',
    }
    validate_capacity(document, manifest_path)
    atomic(work / 'evidence' / 'capacity.json', document)
    print('PASS: Phase 18-01 three-round single-replica capacity baseline', flush=True)
    return document


def run_preflight(work):
    work.mkdir(parents=True, exist_ok=True, mode=0o700)
    if work.stat().st_mode & 0o77:
        raise ValueError('capacity work directory must be private')
    lock_path = work / '.lock'
    lock_path.touch(exist_ok=True)
    lock_path.chmod(0o600)
    with lock_path.open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if (work / 'evidence' / 'capacity.json').exists():
            raise ValueError('capacity evidence already exists; refusing to overwrite completed acceptance')
        document = {'schema': 'gopulse.phase18.preflight.v1', 'host': host_inventory()}
        document['problems'] = preflight(document['host'])
        write_partial(work / 'evidence' / 'preflight.json', document)
        return document


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path)
    parser.add_argument('--rounds', type=int, default=3)
    parser.add_argument('--work', type=Path, required=True)
    parser.add_argument('--preflight-only', action='store_true')
    arguments = parser.parse_args()
    work = arguments.work.resolve()
    if arguments.preflight_only:
        document = run_preflight(work)
        print(json.dumps({'host': document['host'], 'problems': document['problems']}, sort_keys=True))
        if document['problems']:
            raise SystemExit(1)
        return
    if arguments.manifest is None:
        parser.error('--manifest is required unless --preflight-only is used')
    manifest = arguments.manifest.resolve()
    # Bundle-relative paths are resolved inside the immutable candidate directory.
    if not (manifest.parent / 'deploy/product/compose.yaml').exists():
        raise ValueError('manifest parent is not a complete release bundle')
    run_capacity(manifest, arguments.rounds, work)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        # Never include exception text: it can contain a DSN, credential, or host path.
        print('Phase 18 capacity execution failed (' + type(error).__name__ + ')', file=sys.stderr)
        raise SystemExit(1)
