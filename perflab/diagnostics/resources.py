"""按窗口汇总容器 CPU、节流、内存及配额；复用 PT5 的统计方法。"""
from datetime import datetime, timezone

INTERVAL = 5.0


def _iso(epoch):
    return datetime.fromtimestamp(epoch, timezone.utc).isoformat()


def summarize_resources(samples, begin, end, expected_cpus, expected_memory,
                        memory_reason='memory_limit_not_expected', oom_reason='cgroup_oom_kill'):
    samples = sorted(samples, key=lambda item: item['epoch'])
    reasons = set()
    before = [item for item in samples if item['epoch'] <= begin]
    after = [item for item in samples if item['epoch'] >= end]
    if not before or not after:
        return {'valid': False, 'reasons': ['measurement_boundary_missing'], 'samples': len(samples),
                'window': {'begin_epoch': begin, 'end_epoch': end, 'begin': _iso(begin), 'end': _iso(end)}}
    first, last = before[-1], after[0]
    window = [item for item in samples if first['epoch'] <= item['epoch'] <= last['epoch']]
    if begin - first['epoch'] > INTERVAL * 2 or last['epoch'] - end > INTERVAL * 2:
        reasons.add('measurement_boundary_too_far')
    for previous, current in zip(window, window[1:]):
        if current['epoch'] - previous['epoch'] > INTERVAL * 2:
            reasons.add('sampling_gap')
    for previous, current in zip(window, window[1:]):
        for name in ('usage_usec', 'nr_periods', 'nr_throttled', 'throttled_usec'):
            if current['cpu'][name] < previous['cpu'][name]:
                reasons.add('counter_reset')
    identities = {item.get('id', item.get('container_id')) for item in window}
    if len(identities) != 1:
        reasons.add('container_changed_during_window')
    quotas = {item['cpu_max'] for item in window}
    if len(quotas) != 1:
        reasons.add('quota_changed_during_window')
    cpus = None
    cpu_max = next(iter(quotas))
    if cpu_max:
        quota, period = (int(value) for value in cpu_max.split())
        cpus = quota / period
        if abs(cpus - expected_cpus) > 1e-9:
            reasons.add('quota_not_expected')
    else:
        reasons.add('quota_missing')
    if any(item['memory_max'] != expected_memory for item in window):
        reasons.add(memory_reason)
    oom = last['memory_events'].get('oom_kill', 0) - first['memory_events'].get('oom_kill', 0)
    if oom:
        reasons.add(oom_reason)
    seconds = last['epoch'] - first['epoch']
    usage = last['cpu']['usage_usec'] - first['cpu']['usage_usec']
    periods = last['cpu']['nr_periods'] - first['cpu']['nr_periods']
    throttled = last['cpu']['nr_throttled'] - first['cpu']['nr_throttled']
    throttled_usec = last['cpu']['throttled_usec'] - first['cpu']['throttled_usec']
    instantaneous = []
    for previous, current in zip(window, window[1:]):
        span = current['epoch'] - previous['epoch']
        if span >= 1:
            delta = current['cpu']['usage_usec'] - previous['cpu']['usage_usec']
            instantaneous.append({'timestamp': current['timestamp'], 'epoch': current['epoch'],
                                  'cpu_percent': delta / 1e6 / span * 100})
    value = {
        'valid': not reasons, 'reasons': sorted(reasons), 'samples': len(samples), 'window_samples': len(window),
        'window': {'begin_epoch': begin, 'end_epoch': end, 'begin': _iso(begin), 'end': _iso(end),
                   'started_at_epoch': first['epoch'], 'ended_at_epoch': last['epoch'], 'seconds': seconds},
        'cpu': {
            'quota_cpus': cpus, 'cpu_max': cpu_max, 'usage_usec': usage,
            'usage_percent_avg': usage / 1e6 / seconds * 100 if seconds else None,
            'usage_percent_peak_5s': max((item['cpu_percent'] for item in instantaneous), default=None),
            'instantaneous': instantaneous,
            'nr_periods': periods, 'nr_throttled': throttled,
            'throttled_period_ratio': throttled / periods if periods else None,
            'throttled_seconds': throttled_usec / 1e6,
            'throttled_time_ratio': throttled_usec / 1e6 / seconds if seconds else None,
        },
        'memory': {
            'current_max': max(item['memory_current'] for item in window),
            'current_avg': sum(item['memory_current'] for item in window) / len(window),
            'peak': max((item['memory_peak'] for item in window if item['memory_peak'] is not None), default=None),
            'max': expected_memory, 'oom_kill': oom,
        },
        'pids_max': max(item['pids_current'] for item in window),
    }
    return value
