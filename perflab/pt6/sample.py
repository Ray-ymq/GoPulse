"""每 5 秒采集 PT6 容器资源、应用指标、数据库等待和异步积压；队列走 Prometheus 指标接口。"""
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import threading
import time

from common import PROJECT, QUOTAS, containers, command, sql, utc, manifest


def counters(path):
    return {line.split()[0]: int(line.split()[1]) for line in path.read_text().splitlines()}


def cgroup(item):
    pid = item['State']['Pid']
    line = next(row for row in Path(f'/proc/{pid}/cgroup').read_text().splitlines() if row.startswith('0::'))
    return Path('/sys/fs/cgroup') / line[3:].lstrip('/')


def resource(name, item):
    assert item['State']['Running'], f'{name} 已停止'
    if name != 'k6':
        assert item['State'].get('Health', {}).get('Status') == 'healthy', f'{name} 未就绪'
    path = cgroup(item)
    cpu, mib = (1.5, 1536) if name == 'k6' else QUOTAS[name]
    quota, period = map(int, (path / 'cpu.max').read_text().split())
    assert quota / period == cpu, f'{name} 实际 CPU 配额不符'
    assert int((path / 'memory.max').read_text()) == mib * 1024**2, f'{name} 实际内存配额不符'
    assert int((path / 'memory.swap.max').read_text()) == 0, f'{name} 实际 swap 上限不符'
    events = counters(path / 'memory.events')
    assert events.get('oom_kill', 0) == 0, f'{name} 发生 OOM kill'
    return {'id': item['Id'], 'cpu_max': f'{quota} {period}', 'cpu': counters(path / 'cpu.stat'),
            'memory_current': int((path / 'memory.current').read_text()),
            'memory_peak': int((path / 'memory.peak').read_text()), 'memory_events': events}


def metrics(name):
    if name.startswith('business-worker'):
        port, token = 19102, 'BUSINESS_WORKER_METRICS_TOKEN'
    elif name.startswith('search-indexer'):
        port, token = 19103, 'SEARCH_INDEXER_METRICS_TOKEN'
    else:
        port, token = 19101, 'BACKEND_METRICS_TOKEN'
    shell = f'exec wget -q -O - --header "Authorization: Bearer ${token}" http://127.0.0.1:{port}/internal/v1/metrics'
    body = command(['docker', 'exec', f'{PROJECT}-{name}-1', 'sh', '-c', shell], timeout=4)
    assert body and '# TYPE' in body, f'{name} 指标为空'
    return body


RABBITMQ_GAUGES = {'rabbitmq_queue_messages_ready': 'messages_ready',
                   'rabbitmq_queue_messages_unacked': 'messages_unacknowledged',
                   'rabbitmq_queue_messages': 'messages'}


def parse_rabbitmq(text):
    """汇总 rabbitmq_prometheus 的全局队列指标；逐队列标签存在时按标签求和。"""
    values = dict.fromkeys(RABBITMQ_GAUGES.values(), 0)
    seen = set()
    for line in text.splitlines():
        parts = line.split()
        if not parts or parts[0].startswith('#'):
            continue
        name = parts[0].split('{', 1)[0]
        if name not in RABBITMQ_GAUGES:
            continue
        assert len(parts) > 1, f'RabbitMQ 指标缺少取值：{name}'
        values[RABBITMQ_GAUGES[name]] += int(float(parts[1]))
        seen.add(name)
    missing = sorted(set(RABBITMQ_GAUGES) - seen)
    assert not missing, 'RabbitMQ 指标缺失：' + '、'.join(missing)
    assert all(value >= 0 for value in values.values()), 'RabbitMQ 队列指标为负'
    return values


def rabbitmq():
    body = command(['docker', 'exec', f'{PROJECT}-rabbitmq-1', 'wget', '-q', '-O', '-',
                    'http://127.0.0.1:15692/metrics'], timeout=4)
    return parse_rabbitmq(body)


def sample():
    begin = time.monotonic()
    rows = containers(include_load=True)
    resources = {name: resource(name, rows[name]) for name in QUOTAS}
    if 'k6' in rows and rows['k6']['State']['Running']:
        try:
            resources['k6'] = resource('k6', rows['k6'])
        except FileNotFoundError:
            current = containers(include_load=True).get('k6')
            if current and current['State']['Running']:
                raise
    targets = ('backend', 'backend-2', 'platform-api', 'business-worker', 'business-worker-2', 'search-indexer', 'search-indexer-2')
    with ThreadPoolExecutor(max_workers=8) as executor:
        queues = executor.submit(rabbitmq)
        applications = dict(zip(targets, executor.map(metrics, targets)))
        queue = queues.result()
    database = sql("SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Innodb_row_lock_waits','Innodb_row_lock_time','Innodb_log_waits','Innodb_buffer_pool_wait_free'); SELECT open_requirements,parked_messages,retained_groups,retained_bytes FROM business_async_budget; SELECT data_epoch,status,mode FROM business_environment; SELECT version,dirty FROM schema_migrations;")
    baseline = json.loads((Path(manifest()['directory']) / 'baseline.json').read_text())
    assert database.splitlines()[-2] == baseline['environment'], '数据 epoch 或运行模式改变'
    assert database.splitlines()[-1].endswith('\t0'), '数据库迁移状态异常'
    assert time.monotonic() - begin < 5, '采样耗时超过 5 秒'
    return {'timestamp': utc(), 'epoch': time.time(), 'containers': resources,
            'application_metrics': applications, 'database': database,
            'rabbitmq': queue,
            'elapsed_seconds': time.monotonic() - begin}


class Sampler:
    def __init__(self, path):
        self.path = Path(path)
        self.stop = threading.Event()
        self.error = None
        self.samples = 0
        self.thread = threading.Thread(target=self.run, daemon=True)

    def start(self):
        self.thread.start()

    def run(self):
        try:
            with self.path.open('x') as stream:
                while not self.stop.is_set():
                    start = time.monotonic()
                    stream.write(json.dumps(sample()) + '\n')
                    stream.flush()
                    self.samples += 1
                    self.stop.wait(max(0, 5 - (time.monotonic() - start)))
        except Exception as error:
            self.error = str(error)

    def close(self):
        self.stop.set()
        self.thread.join(timeout=10)
        assert not self.thread.is_alive(), '采集器未按时停止'
        assert self.error is None, '采集失败：' + str(self.error)
