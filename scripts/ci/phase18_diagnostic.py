#!/usr/bin/env python3
"""Run one bounded Phase 18 diagnostic without producing capacity acceptance evidence."""
from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import re
import secrets
import shlex
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

from phase18_capacity import (
    ROOT, baseline_environment, build_loadtest, candidate_binding, candidate_environment,
    compose, compose_override, generate_recipe, inspect_recipe, prepare_workspace, require,
    resource_inventory, run_preflight, sha256_text, wait_convergence, write_env,
)
from phase18_sampler import LINK_METRICS_SCRIPTS, Sampler, metric_value

VALID_PROJECT = re.compile(r'^gopulse-p18-diag-[0-9a-f]{12}$')
REQUEST_ID = re.compile(r'^[0-9a-f]{32}$')
OUTBOX_MESSAGES = {
    'outbox event published': 'publish_success',
    'outbox claim failed': 'claim_failed',
    'outbox publish failed': 'publish_failed',
    'outbox release failed': 'release_failed',
    'outbox mark published failed': 'mark_failed',
    'outbox event invalid': 'event_invalid',
}


def atomic_json(path, value):
    path = Path(path)
    temporary = path.with_name(path.name + '.tmp')
    temporary.write_text(json.dumps(value, sort_keys=True, indent=2) + '\n')
    temporary.chmod(0o600)
    temporary.replace(path)


def sha256_file(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(chunk)
    return 'sha256:' + digest.hexdigest()


def git_value(*args):
    result = subprocess.run(['git', *args], cwd=ROOT, text=True, capture_output=True, timeout=30)
    return result.stdout.strip() if result.returncode == 0 else ''


class DiagnosticSampler(Sampler):
    """Keep the sampler bounded while adding the fields needed for this diagnosis."""

    def _links(self):
        text = self._exec('backend', LINK_METRICS_SCRIPTS['backend'])
        if not text:
            return {'backend': None}
        return {'backend': {
            'gopulse_backend_outbox_pending': metric_value(text, 'gopulse_backend_outbox_pending'),
            'gopulse_backend_outbox_oldest_age_seconds': metric_value(text, 'gopulse_backend_outbox_oldest_age_seconds'),
            'gopulse_backend_outbox_last_publish_success_timestamp_seconds': metric_value(
                text, 'gopulse_backend_outbox_last_publish_success_timestamp_seconds'),
            'gopulse_backend_dependency_up_mysql': metric_value(
                text, 'gopulse_backend_dependency_up{dependency="mysql"}'),
            'gopulse_backend_dependency_up_rabbitmq': metric_value(
                text, 'gopulse_backend_dependency_up{dependency="rabbitmq"}'),
        }}

    def _mysql(self):
        sql = (
            "SELECT CONCAT('status\\t', status, '\\t', COUNT(*), '\\t', "
            "COALESCE(DATE_FORMAT(MIN(available_at), '%Y-%m-%dT%H:%i:%s.%fZ'), ''), '\\t', "
            "COALESCE(MAX(attempt_count), 0)) FROM business_outbox GROUP BY status;"
            "SELECT CONCAT('retry\\t', COUNT(*), '\\t', COALESCE(SUM(attempt_count), 0)) "
            "FROM business_outbox WHERE status IN ('pending','leased') AND attempt_count > 0;"
            "SELECT CONCAT('last_published\\t', COALESCE(UNIX_TIMESTAMP(MAX(published_at)), 0)) "
            "FROM business_outbox WHERE status = 'published';"
            "SHOW GLOBAL STATUS WHERE Variable_name IN ("
            "'Threads_connected','Threads_running','Queries','Slow_queries',"
            "'Innodb_row_lock_current_waits','Innodb_row_lock_waits','Innodb_row_lock_time',"
            "'Innodb_row_lock_time_avg','Innodb_row_lock_time_max',"
            "'Innodb_buffer_pool_bytes_data','Innodb_buffer_pool_bytes_dirty');"
        )
        script = 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e ' + shlex.quote(sql)
        text = self._exec('mysql', script, timeout=20)
        if not text:
            return None
        result = {'status': {}, 'retry_count': 0, 'attempt_count': 0,
                  'last_published_unix': 0, 'global': {}}
        for line in text.splitlines():
            fields = line.split('\t')
            if len(fields) == 5 and fields[0] == 'status':
                try:
                    result['status'][fields[1]] = {
                        'count': int(fields[2]), 'oldest_available_at': fields[3],
                        'max_attempt_count': int(fields[4]),
                    }
                except ValueError:
                    continue
            elif len(fields) == 3 and fields[0] == 'retry':
                try:
                    result['retry_count'] = int(fields[1])
                    result['attempt_count'] = int(fields[2])
                except ValueError:
                    continue
            elif len(fields) == 2 and fields[0] == 'last_published':
                try:
                    result['last_published_unix'] = int(fields[1])
                except ValueError:
                    continue
            elif len(fields) == 2:
                try:
                    result['global'][fields[0]] = int(fields[1])
                except ValueError:
                    continue
        return result

    def _rabbitmq(self):
        return super()._rabbitmq()

    def _kafka_lag(self):
        return None


def backend_log_summary(env_file, compose_file, project, since):
    result = compose(env_file, Path(compose_file), project, 'logs', '--since', since,
                     '--no-log-prefix', 'backend', timeout=300)
    summary = {
        'available': result.returncode == 0,
        'messages': {value: 0 for value in OUTBOX_MESSAGES.values()},
        'publish_failure_reasons': {},
        'request_statuses': {},
        'server_requests': {},
    }
    if result.returncode:
        return summary
    for line in result.stdout.splitlines():
        start = line.find('{')
        if start < 0:
            continue
        try:
            record = json.loads(line[start:])
        except json.JSONDecodeError:
            continue
        message = record.get('message')
        if message in OUTBOX_MESSAGES:
            kind = OUTBOX_MESSAGES[message]
            summary['messages'][kind] += 1
            if kind == 'publish_failed':
                reason = str(record.get('reason') or 'unknown')
                if re.fullmatch(r'[a-z_]{1,64}', reason):
                    summary['publish_failure_reasons'][reason] = summary['publish_failure_reasons'].get(reason, 0) + 1
        if message != 'http request completed':
            continue
        try:
            status = int(record.get('status'))
        except (TypeError, ValueError):
            continue
        summary['request_statuses'][str(status)] = summary['request_statuses'].get(str(status), 0) + 1
        request_id = str(record.get('request_id') or '')
        if status < 500 or not REQUEST_ID.fullmatch(request_id):
            continue
        error_code = str(record.get('error_code') or '')
        summary['server_requests'][request_id] = {
            'request_id': request_id,
            'method': str(record.get('method') or ''),
            'route': str(record.get('route') or ''),
            'status': status,
            'error_code': error_code if re.fullmatch(r'[a-z0-9_]{1,64}', error_code) else '',
            'duration_ms': record.get('duration_ms'),
        }
    return summary


def load_500_samples(load_report):
    samples = []
    for item in load_report.get('server_errors', []):
        if item.get('status') == 500:
            samples.append(dict(item))
    return samples


def aggregate_load_windows(load_report):
    by_phase = {}
    routes = {}
    top_windows = []
    for window in load_report.get('windows', []):
        phase = window.get('phase', '')
        phase_value = by_phase.setdefault(phase, {'requests': 0, 'statuses': {}})
        phase_value['requests'] += int(window.get('requests', 0))
        for status, count in (window.get('statuses') or {}).items():
            phase_value['statuses'][status] = phase_value['statuses'].get(status, 0) + int(count)
        for route, window_route in (window.get('routes') or {}).items():
            aggregate = routes.setdefault(route, {'requests': 0, 'statuses': {}})
            aggregate['requests'] += int((window_route.get('counts') or {}).get('requests', 0))
            for status, count in (window_route.get('statuses') or {}).items():
                aggregate['statuses'][status] = aggregate['statuses'].get(status, 0) + int(count)
        if phase == 'steady':
            latency = window.get('latency') or {}
            top_windows.append({
                'sequence': window.get('sequence'), 'started_at': window.get('started_at'),
                'requests': window.get('requests'), 'p95_ms': latency.get('p95_ms'),
                'p99_ms': latency.get('p99_ms'), 'max_ms': latency.get('max_ms'),
            })
    top_windows.sort(key=lambda item: (item.get('p95_ms') or 0, item.get('p99_ms') or 0), reverse=True)
    return {'by_phase': by_phase, 'top_steady_windows': top_windows[:20]}


def nearest_sample(samples, when):
    if not samples or not when:
        return None
    target = datetime.fromisoformat(when.replace('Z', '+00:00')).timestamp()
    return min(samples, key=lambda item: abs(float(item.get('observed_at', 0)) - target))


def outbox_summary(records, load_started_at):
    usable = [item for item in records if item.get('observed_at', 0) >= load_started_at
              and ((item.get('mysql') or {}).get('status') or {})]
    if not usable:
        return {'available': False}
    first = usable[0]
    last = usable[-1]
    first_mysql = first.get('mysql') or {}
    last_mysql = last.get('mysql') or {}
    first_status = first_mysql.get('status') or {}
    last_status = last_mysql.get('status') or {}
    first_published = int((first_status.get('published') or {}).get('count', 0))
    last_published = int((last_status.get('published') or {}).get('count', 0))
    first_pending = sum(int((first_status.get(name) or {}).get('count', 0)) for name in ('pending', 'leased'))
    last_pending = sum(int((last_status.get(name) or {}).get('count', 0)) for name in ('pending', 'leased'))
    elapsed = max(float(last['observed_at']) - float(first['observed_at']), 0.001)
    intervals = [usable[index]['observed_at'] - usable[index - 1]['observed_at'] for index in range(1, len(usable))]
    return {
        'available': True,
        'sample_count': len(usable),
        'sample_interval_seconds': {
            'min': min(intervals) if intervals else 0,
            'max': max(intervals) if intervals else 0,
            'average': sum(intervals) / len(intervals) if intervals else 0,
        },
        'first_sample_at': first.get('observed_at'),
        'last_sample_at': last.get('observed_at'),
        'first_pending': first_pending,
        'last_pending': last_pending,
        'pending_delta': last_pending - first_pending,
        'pending_net_per_minute': (last_pending - first_pending) * 60 / elapsed,
        'first_published': first_published,
        'last_published': last_published,
        'published_delta': last_published - first_published,
        'publish_success_per_minute': (last_published - first_published) * 60 / elapsed,
        'backend_last_publish_success_timestamp_seconds': (
            (last.get('links') or {}).get('backend') or {}
        ).get('gopulse_backend_outbox_last_publish_success_timestamp_seconds'),
    }


def run_diagnostic(manifest_path, work):
    preflight = run_preflight(work)
    if preflight['problems']:
        raise RuntimeError('reference-host preflight failed: ' + '; '.join(preflight['problems']))
    manifest, binding = candidate_binding(manifest_path)
    manifest['compose']['path'] = str(manifest_path.parent / manifest['compose']['path'])
    lock = prepare_workspace(work, binding)  # noqa: F841 - retain the workspace lock.
    recipe_binary, load_binary = build_loadtest(work)
    descriptor = inspect_recipe(recipe_binary, binding, work / 'recipe-inspect.json')
    output = work / 'diagnostic'
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    environment = candidate_environment(manifest, output / 'candidate.env', 9)
    recipe_environment = dict(environment)
    recipe_environment.update({
        'OUTBOX_POLL_INTERVAL': '10ms', 'OUTBOX_CLAIM_BATCH': '100',
        'OUTBOX_LEASE_DURATION': '10m', 'BUSINESS_WORKER_PREFETCH': '100',
        'SEARCH_INDEXER_PREFETCH': '100',
    })
    recipe_env_file = output / 'recipe.env'
    baseline_env_file = output / 'baseline.env'
    write_env(recipe_env_file, recipe_environment)
    baseline_environment(environment, baseline_env_file)
    override = output / 'compose.override.yaml'
    compose_override(override, int(environment['MYSQL_PORT']))
    project = 'gopulse-p18-diag-' + secrets.token_hex(6)
    if not VALID_PROJECT.fullmatch(project):
        raise RuntimeError('generated unsafe diagnostic project name')
    project_hash = sha256_text(project)
    before = resource_inventory()
    cleanup = {'attempted': False, 'status': 'not_started', 'project': project, 'project_sha256': project_hash}
    load_process = None
    sampler = None
    started_at = None
    summary = None
    try:
        result = compose(recipe_env_file, manifest['compose']['path'], project,
                         '-f', str(override), 'up', '-d', '--wait', '--wait-timeout', '900', timeout=1000)
        require(result, 'start isolated diagnostic project')
        receipt = generate_recipe(recipe_binary, binding, recipe_environment, output, int(environment['MYSQL_PORT']))
        reindex = compose(recipe_env_file, manifest['compose']['path'], project, 'run', '--rm', '--no-deps',
                          '--entrypoint', '/usr/local/bin/search-reindex', 'search-init', timeout=1800)
        require(reindex, 'run formal search reindex')
        convergence = wait_convergence(recipe_env_file, manifest['compose']['path'], project, timeout=3600)
        restart = compose(baseline_env_file, manifest['compose']['path'], project, 'up', '-d', '--force-recreate',
                          '--wait', '--wait-timeout', '900', 'backend', 'business-worker', 'search-indexer', timeout=1000)
        require(restart, 'restart diagnostic project with baseline runtime configuration')
        sampler = DiagnosticSampler(project, manifest['compose']['path'], baseline_env_file, interval=2)
        start_wall = datetime.now(timezone.utc)
        started_at = time.time()
        load_process = subprocess.Popen([
            str(load_binary), '--base-url', 'http://127.0.0.1:' + environment['FRONTEND_PORT'],
            '--corpus', str(output / 'corpus.json'), '--credentials', str(output / 'credentials.json'),
            '--report', str(output / 'load-report.json'),
            '--diagnostic-report', str(output / 'load-diagnostic.json'), '--vus', '1024',
            '--warmup', '0s', '--steady', '2m', '--burst', '1m', '--steady-rps', '150', '--burst-rps', '300',
        ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        sampler.set_load_pid(load_process.pid)
        sampler.start()
        try:
            stdout, stderr = load_process.communicate(timeout=360)
        except subprocess.TimeoutExpired:
            load_process.kill()
            stdout, stderr = load_process.communicate()
            raise RuntimeError('diagnostic load generator exceeded the fixed six-minute window')
        if load_process.returncode:
            raise RuntimeError('diagnostic load generator failed: ' + (stderr or stdout)[-300:])
        drain_deadline = time.monotonic() + 120
        while time.monotonic() < drain_deadline:
            time.sleep(min(5, max(0, drain_deadline - time.monotonic())))
        load_finished_at = time.time()
        sampler.stop()
        logs = backend_log_summary(baseline_env_file, manifest['compose']['path'], project, start_wall.isoformat())
        load_report = json.loads((output / 'load-report.json').read_text())
        load_diagnostic = json.loads((output / 'load-diagnostic.json').read_text())
        server_errors = []
        for sample in load_500_samples(load_diagnostic):
            matched = dict(sample)
            matched['backend_log'] = logs['server_requests'].get(sample.get('request_id', ''))
            sample_window = nearest_sample(sampler.records, sample.get('completed_at'))
            if sample_window is not None:
                matched['nearest_resource_sample'] = {
                    'observed_at': sample_window.get('observed_at'),
                    'containers': [{
                        'service': item.get('service'), 'cpu_percent': item.get('cpu_percent'),
                        'memory_usage_bytes': item.get('memory_usage_bytes'),
                    } for item in sample_window.get('containers', []) if item.get('service') in ('backend', 'mysql')],
                    'mysql': sample_window.get('mysql'),
                    'links': sample_window.get('links'),
                }
            server_errors.append(matched)
        summary = {
            'schema': 'gopulse.phase18.diagnostic.v1',
            'candidate': binding,
            'load_code': {
                'base_commit': git_value('rev-parse', 'HEAD'),
                'working_tree_diff_sha256': 'sha256:' + hashlib.sha256(subprocess.run(
                    ['git', 'diff', '--binary'], cwd=ROOT, capture_output=True, timeout=30).stdout).hexdigest(),
                'load_binary_sha256': sha256_file(load_binary),
                'recipe_binary_sha256': sha256_file(recipe_binary),
            },
            'recipe': {
                'descriptor_digest': descriptor.get('digest'),
                'corpus_sha256': sha256_file(output / 'corpus.json'),
                'receipt': receipt,
            },
            'duration_seconds': load_finished_at - started_at,
            'convergence_before_load': convergence,
            'load_summary': aggregate_load_windows(load_diagnostic),
            'server_errors': server_errors,
            'outbox': outbox_summary(sampler.records, started_at),
            'backend_logs': logs,
            'cleanup': cleanup,
        }
        return summary
    finally:
        if load_process is not None and load_process.poll() is None:
            load_process.kill()
            load_process.communicate()
        if sampler is not None and sampler._thread is not None:
            sampler.stop()
        cleanup['attempted'] = True
        cleanup_result = compose(recipe_env_file, manifest['compose']['path'], project,
                                 '-f', str(override), 'down', '--volumes', '--remove-orphans',
                                 '--timeout', '30', timeout=300)
        if cleanup_result.returncode:
            cleanup['status'] = 'failed'
            cleanup['residual_project'] = project
            (work / 'cleanup-error.txt').write_text('owned diagnostic project cleanup failed: ' + project + '\n')
        else:
            try:
                after = resource_inventory()
                if after != before:
                    raise RuntimeError('owned project cleanup changed unrelated Docker resources')
                cleanup['status'] = 'passed'
            except RuntimeError:
                cleanup['status'] = 'failed'
                cleanup['residual_project'] = project
                (work / 'cleanup-error.txt').write_text(
                    'owned diagnostic project cleanup changed unrelated Docker resources: ' + project + '\n')
        if summary is not None:
            summary['cleanup'] = cleanup
            atomic_json(output / 'diagnostic-summary.json', summary)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--work', type=Path, required=True)
    arguments = parser.parse_args()
    work = arguments.work.resolve()
    try:
        summary = run_diagnostic(arguments.manifest.resolve(), work)
        print(json.dumps({
            'project_sha256': summary['cleanup']['project_sha256'],
            'cleanup': summary['cleanup']['status'],
            'server_errors': len(summary['server_errors']),
            'outbox': summary['outbox'],
        }, sort_keys=True))
    except Exception as error:
        work.mkdir(parents=True, exist_ok=True, mode=0o700)
        (work / 'run-error.txt').write_text(type(error).__name__ + ': ' + str(error) + '\n')
        print('Phase 18 diagnostic execution failed (' + type(error).__name__ + ')', file=sys.stderr)
        raise SystemExit(1)


if __name__ == '__main__':
    main()
