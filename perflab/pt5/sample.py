"""每 5 秒保存 MySQL 容器 CPU 使用量、节流、内存和配额，并按正式窗口汇总。"""
import json
from pathlib import Path
import sys
import threading
import time

from common import EXPECTED_MEMORY, atomic_json, cgroup, container, utc
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

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


def summarize(path, begin, end, expected_cpus):
    from diagnostics.resources import summarize_resources
    return summarize_resources(load(path), begin, end, expected_cpus, EXPECTED_MEMORY,
                               'mysql_memory_not_2_gib', 'mysql_cgroup_oom_kill')
