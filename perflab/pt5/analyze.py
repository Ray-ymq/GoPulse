"""汇总测试五四组结果，并按相同负载比较 2/4 vCPU 的扩容效果。"""
import argparse
import json
from pathlib import Path

from common import STATE, atomic_json

GROUPS = ('cpu2-120', 'cpu4-120', 'cpu4-140', 'cpu2-140')


def _series(metrics):
    values = {}
    for source, body in metrics['sources'].items():
        for item in body.get('series', []):
            values.setdefault((source, item['name']), []).append(item)
    return values


def _counter(values, source, name, **labels):
    for item in values.get((source, name), []):
        if all(item['labels'].get(key) == value for key, value in labels.items()):
            return item.get('delta')
    return None


def _gauge(values, source, name, **labels):
    for item in values.get((source, name), []):
        if all(item['labels'].get(key) == value for key, value in labels.items()):
            return item.get('max')
    return None


def _sum_counters(values, source, name, **labels):
    total = 0
    found = False
    for item in values.get((source, name), []):
        if all(item['labels'].get(key) == value for key, value in labels.items()):
            total += item.get('delta') or 0
            found = True
    return total if found else None


def load(name):
    manifest = json.loads((STATE / 'groups' / (name + '.json')).read_text())
    observation = Path(manifest['observation_directory'])
    report = json.loads((observation / 'report.json').read_text())
    metrics = json.loads((observation / 'metrics-summary.json').read_text())
    resources = json.loads((observation / 'resources-summary.json').read_text())
    audit = json.loads((observation / 'write-audit.json').read_text())
    values = _series(metrics)
    commit_count = _counter(values, 'mysql', 'gopulse_mysql_statement_event_executions_total', event='statement/sql/commit')
    commit_seconds = _counter(values, 'mysql', 'gopulse_mysql_statement_event_seconds_total', event='statement/sql/commit')
    execute_count = _counter(values, 'mysql', 'gopulse_mysql_statement_event_executions_total', event='statement/com/Execute')
    execute_seconds = _counter(values, 'mysql', 'gopulse_mysql_statement_event_seconds_total', event='statement/com/Execute')
    wait_seconds = _sum_counters(values, 'mysql', 'gopulse_mysql_wait_event_seconds_total')
    measurements = report['measurement']
    record = {
        'group': name, 'rps': manifest['rps'], 'mysql_cpus': manifest['mysql_cpus'],
        'run_id': report['run_id'], 'observation': str(observation),
        'quota': {'expected_cpus': manifest['mysql_cpus'],
                  'before_cpus': manifest['preflight']['cgroup_cpus'],
                  'after_cpus': manifest['after']['cgroup_cpus'],
                  'memory_gib': manifest['after']['memory'] / 1024**3,
                  'cgroup_memory_gib': manifest['after']['cgroup_memory_gib'],
                  'consistent': manifest['quota_consistent']},
        'requests': {
            'started': measurements['started'], 'successful': measurements['successful'],
            'unfinished': measurements['unfinished'], 'error_rate': measurements['error_rate'],
            'started_rps': measurements['started_rps'], 'successful_rps': measurements['successful_cohort_rps'],
            'p50_ms': measurements['p50_ms'], 'p95_ms': measurements['p95_ms'], 'p99_ms': measurements['p99_ms'],
            'success_p95_ms': measurements['success_p95_ms'], 'success_p99_ms': measurements['success_p99_ms'],
            'dropped_iterations': measurements['dropped_iterations'],
            'error_classes': measurements['error_classes'],
        },
        'cpu': resources['cpu'], 'memory': resources['memory'], 'resource_valid': resources['valid'],
        'resource_reasons': resources['reasons'],
        'commit': {'count': commit_count, 'seconds': commit_seconds,
                   'mean_ms': commit_seconds / commit_count * 1000 if commit_count and commit_seconds else None,
                   'execute_count': execute_count, 'execute_seconds': execute_seconds},
        'locks': {
            'row_lock_waits': _counter(values, 'mysql', 'gopulse_mysql_row_lock_waits_total'),
            'row_lock_seconds': _counter(values, 'mysql', 'gopulse_mysql_row_lock_wait_seconds_total'),
            'budget_waiting_transactions_max': _gauge(values, 'mysql', 'gopulse_mysql_row_lock_waiting_transactions', table='business_async_budget'),
            'budget_longest_wait_seconds': _gauge(values, 'mysql', 'gopulse_mysql_row_lock_longest_wait_seconds', table='business_async_budget'),
        },
        'pool': {
            'wait_total': _counter(values, 'backend', 'gopulse_backend_mysql_pool_wait_total'),
            'wait_seconds': _counter(values, 'backend', 'gopulse_backend_mysql_pool_wait_duration_seconds_total'),
            'in_use_max': _gauge(values, 'backend', 'gopulse_backend_mysql_pool_in_use'),
            'open_max': _gauge(values, 'backend', 'gopulse_backend_mysql_pool_open_connections'),
            'max_connections': _gauge(values, 'backend', 'gopulse_backend_mysql_pool_max_connections'),
            'in_flight_max': _gauge(values, 'backend', 'gopulse_backend_http_requests_in_flight'),
        },
        'rejections': {
            'read': _counter(values, 'backend', 'gopulse_backend_resource_rejected_total', reason='capacity', resource='read'),
            'write': _counter(values, 'backend', 'gopulse_backend_resource_rejected_total', reason='capacity', resource='write'),
        },
        'io': {
            'redo_misc_seconds': _sum_counters(values, 'mysql', 'gopulse_mysql_file_misc_seconds_total', file_type='redo'),
            'binlog_misc_seconds': _sum_counters(values, 'mysql', 'gopulse_mysql_file_misc_seconds_total', file_type='binlog'),
            'wait_event_seconds': wait_seconds,
        },
        'evidence': {
            'execution': report['execution'], 'observations': report['observations']['status'],
            'observation_errors': report['observations']['errors'],
            'recovery': report['recovery']['status'], 'audit': audit['status'], 'audit_failures': audit['failures'],
            'audit_boundary': audit['boundary']['by_reason'],
        },
        'audit': {
            'creates': audit['content_write']['creates'],
            'database_created_in_window': audit['content_write']['database_created_in_window'],
            'database_created_unmatched': audit['content_write']['database_created_unmatched'],
            'updates': audit['content_write']['updates'], 'deletes': audit['content_write']['deletes'],
            'interactions': audit['interaction_write'], 'database': audit['database'],
            'boundary_requests': audit['boundary']['count'],
        },
    }
    record['pass'] = bool(report['business_short_window_pass'] and report['execution'] == 'complete'
                          and report['observations']['status'] == 'complete'
                          and report['recovery']['status'] == 'passed'
                          and resources['valid'] and audit['status'] == 'passed')
    return record


def _change(before, after, name):
    left, right = before.get(name), after.get(name)
    if left in (None, 0) or right is None:
        return None
    return (right - left) / abs(left)


def compare(cpu2, cpu4):
    throttle_down = (cpu4['cpu']['throttled_seconds'] < cpu2['cpu']['throttled_seconds']
                     and cpu4['cpu']['throttled_period_ratio'] <= cpu2['cpu']['throttled_period_ratio'])
    pool_down = (cpu4['pool']['wait_seconds'] is not None and cpu2['pool']['wait_seconds'] is not None
                 and cpu4['pool']['wait_seconds'] < cpu2['pool']['wait_seconds'])
    lock_down = (cpu4['locks']['row_lock_seconds'] is not None and cpu2['locks']['row_lock_seconds'] is not None
                 and cpu4['locks']['row_lock_seconds'] < cpu2['locks']['row_lock_seconds'])
    queue_down = pool_down or lock_down
    error_delta = cpu2['requests']['error_rate'] - cpu4['requests']['error_rate']
    success_gain = _change(cpu2['requests'], cpu4['requests'], 'successful_rps')
    p95_gain = _change(cpu2['requests'], cpu4['requests'], 'p95_ms')
    improved = error_delta > 0 or (success_gain or 0) > 0
    small = error_delta < 0.01 and (success_gain or 0) < 0.05 and (p95_gain or 0) > -0.10
    if cpu4['pass'] and not cpu2['pass'] and throttle_down and queue_down:
        verdict = '支持 CPU 配额是当前性能限制的重要因素'
    elif cpu4['pass'] and cpu2['pass']:
        verdict = '两种配额在目标负载下均通过，本负载不能区分配额影响'
    elif cpu4['pass'] and not cpu2['pass']:
        verdict = '4 vCPU 通过、2 vCPU 未通过，但节流或排队未同时减少，需核对非 CPU 因素'
    elif improved and not cpu4['pass']:
        verdict = '扩容有帮助，但目标负载仍未达到要求'
    elif not improved and throttle_down:
        verdict = 'CPU 节流减少但业务改善很小；继续检查提交耗时、日志 I/O 和高成本查询'
    elif cpu2['pass'] and not cpu4['pass']:
        verdict = '4 vCPU 未改善，需检查非 CPU 因素和运行波动'
    else:
        verdict = '扩容未显示稳定改善，需核对证据与运行波动'
    return {
        'rps': cpu2['rps'], 'cpu2': cpu2['group'], 'cpu4': cpu4['group'],
        'pass': {'cpu2': cpu2['pass'], 'cpu4': cpu4['pass']},
        'error_rate_delta_pp': (cpu4['requests']['error_rate'] - cpu2['requests']['error_rate']) * 100,
        'successful_rps_change': success_gain, 'p95_change': p95_gain,
        'throttled_seconds_change': _change(cpu2['cpu'], cpu4['cpu'], 'throttled_seconds'),
        'throttled_ratio_change': _change(cpu2['cpu'], cpu4['cpu'], 'throttled_period_ratio'),
        'pool_wait_seconds_change': _change(cpu2['pool'], cpu4['pool'], 'wait_seconds'),
        'row_lock_seconds_change': _change(cpu2['locks'], cpu4['locks'], 'row_lock_seconds'),
        'commit_mean_ms_change': _change(cpu2['commit'], cpu4['commit'], 'mean_ms'),
        'throttle_down': throttle_down, 'pool_queue_down': pool_down, 'lock_queue_down': lock_down,
        'queue_down': queue_down, 'business_improved': improved, 'improvement_small': small,
        'verdict': verdict,
    }


def markdown(records, comparisons):
    lines = ['| 组 | 配额 | 负载 | 通过 | 错误率 | 成功 RPS | P95 | P99 | CPU 均值 | 节流周期 | 节流累计 | COMMIT 均值 | 连接池等待 | 行锁等待 |',
             '| --- | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |']
    for item in records:
        request, cpu, commit = item['requests'], item['cpu'], item['commit']
        lines.append('| {group} | {cpus} vCPU | {rps} | {passed} | {error:.2%} | {rps_ok:.2f} | {p95:.0f} ms | {p99:.0f} ms | {cpu:.1f}% | {periods} | {seconds:.1f} s | {commit:.2f} ms | {pool:.0f} s | {lock:.1f} s |'.format(
            group=item['group'], cpus=item['mysql_cpus'], rps=item['rps'], passed='是' if item['pass'] else '否',
            error=request['error_rate'], rps_ok=request['successful_rps'], p95=request['p95_ms'], p99=request['p99_ms'],
            cpu=cpu['usage_percent_avg'], periods=cpu['nr_throttled'], seconds=cpu['throttled_seconds'],
            commit=commit['mean_ms'] or 0, pool=item['pool']['wait_seconds'] or 0, lock=item['locks']['row_lock_seconds'] or 0))
    lines.append('')
    for item in comparisons:
        lines.append('**%d RPS：%s。** 错误率变化 %+.2f pp，成功 RPS 变化 %s，P95 变化 %s；节流累计变化 %s，连接池等待变化 %s，行锁等待变化 %s。%s' % (
            item['rps'], item['verdict'], item['error_rate_delta_pp'],
            'n/a' if item['successful_rps_change'] is None else '%+.1f%%' % (item['successful_rps_change'] * 100),
            'n/a' if item['p95_change'] is None else '%+.1f%%' % (item['p95_change'] * 100),
            'n/a' if item['throttled_seconds_change'] is None else '%+.1f%%' % (item['throttled_seconds_change'] * 100),
            'n/a' if item['pool_wait_seconds_change'] is None else '%+.1f%%' % (item['pool_wait_seconds_change'] * 100),
            'n/a' if item['row_lock_seconds_change'] is None else '%+.1f%%' % (item['row_lock_seconds_change'] * 100),
            '扩容同时减少节流与排队。' if item['throttle_down'] and item['queue_down'] else ''))
    return '\n'.join(lines)


def main():
    records = [load(name) for name in GROUPS]
    comparisons = [compare(records[0], records[1]), compare(records[3], records[2])]
    value = {'schema_version': 'gopulse.pt5.comparison.v1', 'groups': records, 'comparisons': comparisons,
             'markdown': markdown(records, comparisons)}
    atomic_json(STATE / 'comparison.json', value)
    print(value['markdown'])
    print('\n已写入', STATE / 'comparison.json')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.parse_args()
    main()
