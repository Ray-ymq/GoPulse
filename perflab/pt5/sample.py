"""每 5 秒保存 MySQL 容器 CPU 使用量、节流、内存和配额，并按正式窗口汇总。"""
from datetime import datetime, timezone
import json
from pathlib import Path
import threading
import time

from common import EXPECTED_MEMORY, atomic_json, cgroup, container, utc

INTERVAL = 5.0


def _counter(text):
    return {line.split()[0]: int(line.split()[1]) for line in text.splitlines() if len(line.split()) == 2}


def sample():
    item = container()
    cpu = _counter(cgroup('cpu.stat') or '')
    events = _counter(cgroup('memory.events') or '')
    peak = cgroup('memory.peak')
    memory_max = cgroup('memory.max')
    value = {
        'timestamp': utc(), 'epoch': time.time(), 'container_id': item['Id'],
        'cpu_max': cgroup('cpu.max'),
        'cpu': {name: cpu.get(name, 0) for name in
                ('usage_usec', 'user_usec', 'system_usec', 'nr_periods', 'nr_throttled', 'throttled_usec')},
        'memory_current': int(cgroup('memory.current') or 0),
        'memory_peak': None if peak in (None, 'max') else int(peak),
        'memory_max': None if memory_max in (None, 'max') else int(memory_max),
        'memory_events': events, 'pids_current': int(cgroup('pids.current') or 0),
    }
    return value


class Sampler:
    def __init__(self, path, interval=INTERVAL):
        self.path = Path(path)
        self.interval = interval
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self._run, daemon=True)
        self.samples = 0
        self.errors = 0

    def start(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.thread.start()

    def _run(self):
        started = time.monotonic()
        with self.path.open('a') as output:
            while not self.stop.is_set():
                began = time.monotonic()
                try:
                    row = sample()
                except Exception as error:  # 单次读取失败保留错误，不补零。
                    row = {'timestamp': utc(), 'epoch': time.time(), 'error': type(error).__name__ + ': ' + str(error)}
                    self.errors += 1
                output.write(json.dumps(row) + '\n')
                output.flush()
                self.samples += 1
                self.stop.wait(max(0, self.interval - (time.monotonic() - began)))
        self.elapsed = time.monotonic() - started

    def close(self):
        self.stop.set()
        self.thread.join(timeout=self.interval + 5)
        if self.thread.is_alive():
            raise RuntimeError('CPU 采样线程未按时停止')
        value = {'path': str(self.path), 'interval_seconds': self.interval, 'samples': self.samples,
                 'errors': self.errors, 'elapsed_seconds': getattr(self, 'elapsed', None)}
        atomic_json(self.path.with_suffix('.summary.json'), value)
        return value


def load(path):
    samples = []
    with Path(path).open() as source:
        for line in source:
            row = json.loads(line)
            if 'error' not in row:
                samples.append(row)
    samples.sort(key=lambda item: item['epoch'])
    return samples


def _iso(epoch):
    return datetime.fromtimestamp(epoch, timezone.utc).isoformat()


def summarize(path, begin, end, expected_cpus):
    samples = load(path)
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
    if any(item['memory_max'] != EXPECTED_MEMORY for item in window):
        reasons.add('mysql_memory_not_2_gib')
    oom = last['memory_events'].get('oom_kill', 0) - first['memory_events'].get('oom_kill', 0)
    if oom:
        reasons.add('mysql_cgroup_oom_kill')
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
            'max': EXPECTED_MEMORY, 'oom_kill': oom,
        },
        'pids_max': max(item['pids_current'] for item in window),
    }
    return value
