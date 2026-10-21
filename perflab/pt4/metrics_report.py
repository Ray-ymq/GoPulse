"""从保存的 exporter 输出计算正式窗口统计，不查询数据库或操作系统。"""
from datetime import datetime
import json
from common import atomic_json
from exporters import INTERVAL, parse_metrics


def epoch(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()


def summarize_source(samples, begin, end):
    before = [item for item in samples if item[0] <= begin]
    after = [item for item in samples if item[0] >= end]
    if not before or not after:
        return {'valid': False, 'reasons': ['measurement_boundary_missing']}
    first, last = before[-1], after[0]
    window = [item for item in samples if first[0] <= item[0] <= last[0]]
    reasons = set()
    if begin - first[0] > INTERVAL * 2 or last[0] - end > INTERVAL * 2:
        reasons.add('measurement_boundary_too_far')
    for previous, current in zip(window, window[1:]):
        if current[0] - previous[0] > INTERVAL * 2:
            reasons.add('sampling_gap')
    series = {}
    for stamp, metrics in window:
        for key, item in metrics.items():
            series.setdefault(key, []).append((stamp, item))
            name, value = item['name'], item['value']
            if name == 'gopulse_mysql_collector_truncated' and value:
                reasons.add('collector_truncated')
            if name == 'gopulse_mysql_collector_failures_total' and key in first[1] and value > first[1][key]['value']:
                reasons.add('collector_failed')
            if name in ('gopulse_mysql_performance_schema_consumer_enabled', 'gopulse_mysql_performance_schema_enabled') and value != 1:
                if name.endswith('_enabled') and (not item['labels'] or item['labels'].get('consumer') in
                                                  ('global_instrumentation', 'thread_instrumentation', 'statements_digest')):
                    reasons.add('required_instrumentation_disabled')
    result = []
    partial = []
    for key, observations in sorted(series.items()):
        item = observations[-1][1]
        row = {name: item[name] for name in ('name', 'labels', 'kind')}
        values = [sample['value'] for _, sample in observations]
        if item['kind'] == 'counter':
            if any(current < previous for previous, current in zip(values, values[1:])):
                reasons.add('counter_reset')
                continue
            if key not in first[1]:
                # 仅在收尾边界样本才出现的计数序列属于窗口外的事件（例如恢复期
                # 的偶发失败），不参与正式窗口，也不因此把整份证据判为不完整。
                if observations[0][0] >= end:
                    continue
                start = next(index for index, (stamp, _) in enumerate(window) if stamp == observations[0][0])
                if len(observations) != len(window) - start:
                    reasons.add('counter_series_missing')
                    continue
                # 新标签组合在窗口内首次出现，此前计数为 0。以 0 为基线计入，
                # 但标注部分覆盖；出现后再次缺失仍按序列缺失处理。
                row['delta'] = values[-1]
                row['partial'] = True
                partial.append({'name': item['name'], 'labels': item['labels'],
                                'first_seen_epoch': observations[0][0]})
                result.append(row)
                continue
            if key not in last[1] or len(observations) != len(window):
                reasons.add('counter_series_missing')
                continue
            row['delta'] = values[-1] - values[0]
            if item['name'] == 'gopulse_mysql_performance_schema_digest_lost_total' and row['delta']:
                reasons.add('digest_lost')
        else:
            current = [sample['value'] for stamp, sample in observations if begin <= stamp < end]
            if not current:
                continue
            row.update(min=min(current), max=max(current), last=current[-1])
        result.append(row)
    # Uptime 是 gauge，也需要检查重启导致的统计重置。
    uptimes = [item['value'] for _, metrics in window for item in metrics.values() if item['name'] == 'gopulse_mysql_uptime_seconds']
    if any(current < previous for previous, current in zip(uptimes, uptimes[1:])):
        reasons.add('mysql_restarted')
    return {'valid': not reasons, 'reasons': sorted(reasons), 'samples': len(window),
            'started_at_epoch': first[0], 'ended_at_epoch': last[0],
            'boundary_extra_seconds': {'before': begin - first[0], 'after': last[0] - end},
            'partial_series': partial, 'series': result}


def summarize_metrics(directory, begin, end):
    path = directory / 'exporter-metrics.jsonl'
    if not path.exists():
        return {'status': 'missing'}
    samples, errors, finished = {}, [], set()
    expected = ('mysql', 'node', 'backend', 'worker', 'indexer')
    with path.open() as source:
        for line in source:
            try:
                row = json.loads(line)
                stamp = epoch(row['timestamp'])
                if begin <= stamp <= end:
                    errors.extend(row.get('errors', {}))
                for name, body in row.get('metrics', {}).items():
                    if name in finished:
                        continue
                    at = epoch(row['sampled_at'][name])
                    item = (at, parse_metrics(body))
                    if at <= begin:
                        samples[name] = [item]
                    else:
                        samples.setdefault(name, []).append(item)
                    if at >= end:
                        finished.add(name)
                if all(name in finished for name in expected):
                    break
            except (ValueError, KeyError, TypeError):
                errors.append('invalid_metrics_record')
    sources = {name: summarize_source(sorted(samples.get(name, []), key=lambda item: item[0]), begin, end)
               for name in expected}
    value = {'status': 'complete' if not errors and all(item['valid'] for item in sources.values()) else 'incomplete',
             'errors': sorted(set(errors)), 'sources': sources,
             'coverage': '分项成功和摘要覆盖范围须结合配置指标判断；聚合耗时不等同于 CPU 时间'}
    atomic_json(directory / 'metrics-summary.json', value)
    return {'status': value['status'], 'errors': value['errors'], 'sources':
            {name: {key: item[key] for key in ('valid', 'reasons')} for name, item in sources.items()},
            'file': 'metrics-summary.json'}
