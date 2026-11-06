"""解析保存的 Prometheus 指标，并校验统计窗口的边界、缺样和计数重置。"""
import math
from prometheus_client.parser import text_string_to_metric_families

INTERVAL = 5


def parse_metrics(body):
    result = {}
    for family in text_string_to_metric_families(body):
        for sample in family.samples:
            key = (sample.name, tuple(sorted(sample.labels.items())))
            if key in result or sample.timestamp is not None or not math.isfinite(sample.value):
                raise ValueError('指标重复、非有限或包含客户端时间戳')
            result[key] = {'name': sample.name, 'labels': dict(sample.labels), 'kind': family.type, 'value': sample.value}
    if not result:
        raise ValueError('指标响应为空')
    return result


def summarize_source(samples, begin, end, *, allow_closing_disappearance=None):
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
    omitted = []
    for key, observations in sorted(series.items()):
        item = observations[-1][1]
        row = {name: item[name] for name in ('name', 'labels', 'kind')}
        values = [sample['value'] for _, sample in observations]
        if item['kind'] == 'counter':
            if any(current < previous for previous, current in zip(values, values[1:])):
                reasons.add('counter_reset')
                continue
            if (key in first[1] and key not in last[1] and last[0] > end
                    and [stamp for stamp, _ in observations] == [stamp for stamp, _ in window[:-1]]
                    and allow_closing_disappearance and allow_closing_disappearance(item, last[1])):
                # 由调用方确认短期设备已消失。省略该序列，不补零或伪造完整窗口增量。
                omitted.append({'name': item['name'], 'labels': item['labels'],
                                'reason': 'device_absent_at_closing_boundary',
                                'last_seen_epoch': observations[-1][0], 'closing_epoch': last[0]})
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
            'partial_series': partial, 'omitted_series': omitted, 'series': result}


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
