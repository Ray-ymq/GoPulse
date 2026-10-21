"""离线汇总 PT6 的窗口指标、资源、锁现场和 COMMIT；不连接业务服务。"""
import json
from pathlib import Path
import re
import sys

from common import ROOT, QUOTAS, write_json

sys.path.insert(0, str(ROOT / 'perflab'))
from diagnostics.metrics import parse_metrics, summarize_source, _series, _counter, _sum_counters, _gauge
from diagnostics.resources import summarize_resources

APPLICATIONS = ('backend', 'backend-2', 'platform-api', 'business-worker', 'business-worker-2',
                'search-indexer', 'search-indexer-2', 'node')


def records(path):
    with Path(path).open() as source:
        for line in source:
            yield json.loads(line)


def removed_veth(item, closing_metrics):
    device = item['labels'].get('device', '')
    return (item['name'].startswith('node_network_') and re.fullmatch(r'veth[0-9a-f]+', device) is not None
            and not any(current['name'].startswith('node_network_') and current['labels'].get('device') == device
                        for current in closing_metrics.values()))


def indicators(sources):
    values = _series({'sources': sources})
    def commits(name):
        count = _sum_counters(values, 'mysql', 'gopulse_mysql_statement_event_executions_total', event=name)
        seconds = _sum_counters(values, 'mysql', 'gopulse_mysql_statement_event_seconds_total', event=name)
        return {'count': count, 'seconds': seconds,
                'mean_ms': seconds / count * 1000 if count and seconds is not None else None}
    pools = {name: {'wait_total': _counter(values, name, 'gopulse_backend_mysql_pool_wait_total', pool='api'),
                    'wait_seconds': _counter(values, name, 'gopulse_backend_mysql_pool_wait_duration_seconds_total', pool='api'),
                    'in_use_max': _gauge(values, name, 'gopulse_backend_mysql_pool_in_use', pool='api')}
             for name in ('backend', 'backend-2')}
    return {'commit_events': {name: commits(name) for name in ('statement/sql/commit', 'statement/com/Commit')},
            'execute': commits('statement/com/Execute'), 'api_pools': pools,
            'api_pool_wait_seconds_total': sum(item['wait_seconds'] for item in pools.values())
                if all(item['wait_seconds'] is not None for item in pools.values()) else None,
            'row_locks': {'waits': _counter(values, 'mysql', 'gopulse_mysql_row_lock_waits_total'),
                          'seconds': _counter(values, 'mysql', 'gopulse_mysql_row_lock_wait_seconds_total'),
                          'budget_waiters_max': _gauge(values, 'mysql', 'gopulse_mysql_row_lock_waiting_transactions', table='business_async_budget')},
            'file_io': {kind: {operation: _sum_counters(values, 'mysql', f'gopulse_mysql_file_{operation}_seconds_total', file_type=kind)
                               for operation in ('read', 'write', 'misc')} for kind in ('redo', 'binlog', 'data')},
            'internal_wait_seconds': _sum_counters(values, 'mysql', 'gopulse_mysql_wait_event_seconds_total')}


def summarize(directory, begin, end):
    directory = Path(directory)
    samples = {'mysql': []}
    scenes = {'samples': 0, 'errors': [], 'truncated': [], 'row_lock_samples': 0, 'metadata_lock_samples': 0,
              'statement_history_events': set(), 'sync_events': set(), 'sync_seconds_observed': 0,
              'max_sample_seconds': 0, 'first_epoch': None, 'last_epoch': None, 'max_gap_seconds': 0}
    mysql_errors = []
    scene_epochs = []
    for row in records(directory / 'database-diagnostics.jsonl'):
        if row['type'] == 'metrics':
            samples['mysql'].append((row['epoch'], parse_metrics(row['body'])))
            if row['status'] != 200:
                mysql_errors.append('mysql_metrics_failed')
        elif row['type'] == 'scene':
            if begin <= row['epoch'] <= end:
                scenes['samples'] += 1
                scene_epochs.append(row['epoch'])
                scenes['errors'].extend(row['errors'])
                scenes['truncated'].extend(row['truncated'])
                scenes['row_lock_samples'] += bool(row.get('row_locks'))
                scenes['metadata_lock_samples'] += bool(row.get('metadata_locks'))
                scenes['max_sample_seconds'] = max(scenes['max_sample_seconds'], row['elapsed_seconds'])
                for item in row.get('statement_history', []):
                    scenes['statement_history_events'].add((str(item['THREAD_ID']), str(item['EVENT_ID'])))
                for item in row.get('sync_history', []):
                    key = (str(item['THREAD_ID']), str(item['EVENT_ID']))
                    if key not in scenes['sync_events']:
                        scenes['sync_events'].add(key)
                        scenes['sync_seconds_observed'] += float(item['seconds'] or 0)
    sources = {}
    resource_rows = list(records(directory / 'resources.jsonl'))
    for name in APPLICATIONS:
        samples[name] = [(row['epoch'], parse_metrics(row['application_metrics'][name]))
                         for row in resource_rows if name in row['application_metrics']]
    for name, rows in samples.items():
        sources[name] = summarize_source(sorted(rows, key=lambda item: item[0]), begin, end,
                                        allow_closing_disappearance=removed_veth if name == 'node' else None)
    resources = {}
    for name, (cpus, mib) in QUOTAS.items():
        rows = [dict(row['containers'][name], epoch=row['epoch'], timestamp=row['timestamp'],
                     memory_max=mib * 1024**2) for row in resource_rows]
        resources[name] = summarize_resources(rows, begin, end, cpus, mib * 1024**2)
    for name in ('statement_history_events', 'sync_events'):
        scenes[name] = len(scenes[name])
    if scene_epochs:
        scene_epochs.sort()
        scenes['first_epoch'], scenes['last_epoch'] = scene_epochs[0], scene_epochs[-1]
        scenes['max_gap_seconds'] = max((b - a for a, b in zip(scene_epochs, scene_epochs[1:])), default=0)
    scenes['errors'], scenes['truncated'] = sorted(set(scenes['errors'])), sorted(set(scenes['truncated']))
    reasons = set(mysql_errors)
    if not scenes['samples'] or scenes['errors']:
        reasons.add('scene_missing_or_failed')
    if scenes['max_gap_seconds'] > 1 or (scenes['first_epoch'] or end) - begin > 1 or end - (scenes['last_epoch'] or begin) > 1:
        reasons.add('scene_sampling_gap')
    if scenes['truncated']:
        reasons.add('scene_truncated')
    if any(not row['valid'] for row in sources.values()):
        reasons.add('metrics_incomplete')
    if any(not row['valid'] for row in resources.values()):
        reasons.add('resources_incomplete')
    runtime = json.loads((directory / 'diagnostic-runtime.json').read_text())
    if runtime['error']:
        reasons.add('diagnostic_runtime_failed')
    logs = directory / 'service-logs.json'
    if not logs.exists() or json.loads(logs.read_text())['errors']:
        reasons.add('service_logs_incomplete')
    value = {'schema_version': 'gopulse.pt6.diagnostics.v1', 'status': 'complete' if not reasons else 'incomplete',
             'reasons': sorted(reasons), 'window': {'begin_epoch': begin, 'end_epoch': end},
             'sources': sources, 'resources': resources, 'scene': scenes, 'indicators': indicators(sources),
             'limits': ['汇总边界最多向前后各扩展 10 秒，实际扩展量见各 source。',
                        '仅在末次边界样本消失的 veth 计数序列列入 omitted_series，不计算完整窗口增量。窗口内缺样仍使证据不完整。',
                        '现场查询依次执行，不构成数据库原子快照；200ms 采样可能遗漏更短事件。',
                        '历史缓冲区和每次 128 行输出均有上限；SQL 文本最多保留 2048 个字符。',
                        'file_io misc 包含其他文件操作，不能直接等同于 fsync。',
                        'sync_history 仅汇总实际观察并去重的 sync 事件，可能覆盖窗口前的历史，不能当作窗口完整总量。',
                        '预编译模板与线程关联，不保证对应当前执行；元数据锁同对象持有者是候选阻塞者。']}
    write_json(directory / 'diagnostics-summary.json', value)
    return {'status': value['status'], 'reasons': value['reasons'], 'file': 'diagnostics-summary.json',
            'window': value['window'], 'indicators': value['indicators']}
