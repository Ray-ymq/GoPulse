"""每 5 秒采集 固定 PT 容器资源、应用指标、数据库等待和异步积压；队列走 Prometheus 指标接口。"""
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import threading
import time

from .common import PROJECT, QUOTAS, containers, command, sql, manifest, metrics


from diagnostics.sources import ContainerSource
from diagnostics.budget import parse_budget


def resource(name, item, value):
    assert item['State']['Running'], f'{name} 已停止'
    if name != 'k6':
        assert item['State'].get('Health', {}).get('Status') == 'healthy', f'{name} 未就绪'
    cpu, mib = (1.5, 1536) if name == 'k6' else QUOTAS[name]
    quota, period = map(int, value['cpu_max'].split())
    assert quota / period == cpu, f'{name} 实际 CPU 配额不符'
    assert value['memory_max'] == mib * 1024**2, f'{name} 实际内存配额不符'
    assert value['memory_swap_max'] == 0, f'{name} 实际 swap 上限不符'
    assert value['memory_events']['oom_kill'] == 0, f'{name} 发生 OOM kill'
    return value


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


def sample(source, node_url=None, expected=None):
    begin = time.monotonic()
    rows = containers(include_load=True)
    from .environment import inspect_rows
    from .config import load
    current = inspect_rows(rows, load()['resources'])
    if expected is not None and current != expected:
        raise ValueError('运行期间环境身份或配置改变')
    selected = {name: rows[name] for name in QUOTAS}
    if 'k6' in rows and rows['k6']['State']['Running']:
        selected['k6'] = rows['k6']

    def collect():
        return source.snapshot([{'name': name, 'id': item['Id'], 'pid': item['State']['Pid']}
                                for name, item in selected.items()])

    try:
        snapshot = collect()
    except Exception:
        current = containers(include_load=True).get('k6')
        if 'k6' not in selected or (current and current['State']['Running']):
            raise
        # k6 正常退出时只重试一次；业务容器缺失仍使本次采集失败。
        del selected['k6']
        snapshot = collect()
    resources = {name: resource(name, item, snapshot['containers'][name]) for name, item in selected.items()}
    targets = ('backend', 'backend-2', 'platform-api', 'business-worker', 'business-worker-2', 'search-indexer', 'search-indexer-2')
    with ThreadPoolExecutor(max_workers=8) as executor:
        queues = executor.submit(rabbitmq)
        if node_url:
            from .diagnose import fetch_node
            node = executor.submit(fetch_node, node_url)
        applications = dict(zip(targets, executor.map(metrics, targets)))
        queue = queues.result()
        if node_url:
            applications['node'] = node.result()
    redis = command(['docker', 'exec', f'{PROJECT}-redis-1', 'redis-cli', '--no-auth-warning', 'INFO', 'stats'], timeout=4)
    assert 'total_commands_processed:' in redis, 'Redis 观测失败'
    database = sql("SELECT data_epoch,status,mode FROM business_environment; SELECT version,dirty FROM schema_migrations;", timeout=4)
    baseline = json.loads((Path(manifest()['directory']) / 'baseline.json').read_text())
    assert database.splitlines()[-2] == baseline['environment'], '数据 epoch 或运行模式改变'
    assert database.splitlines()[-1].endswith('\t0'), '数据库迁移状态异常'
    assert time.monotonic() - begin < 5, '采样耗时超过 5 秒'
    return {'timestamp': snapshot['timestamp'], 'epoch': snapshot['epoch'], 'containers': resources, 'environment': current,
            'container_source': snapshot['schema_version'], 'async_budget': parse_budget(applications['backend']),
            'application_metrics': applications, 'database': database,
            'rabbitmq': queue, 'redis_info_stats': redis,
            'elapsed_seconds': time.monotonic() - begin}


class Sampler:
    def __init__(self, path, diagnostics=None, expected=None):
        self.expected = expected
        self.path = Path(path)
        self.stop = threading.Event()
        self.error = None
        self.samples = 0
        self.diagnostics = diagnostics
        self.source = ContainerSource(Path(manifest()["directory"]) / "bin/container-exporter", self.path.parent)
        self.thread = threading.Thread(target=self.run, daemon=True)

    def start(self):
        self.source.start()
        try:
            self.thread.start()
        except BaseException:
            self.source.close()
            raise

    def run(self):
        try:
            with self.path.open('x') as stream:
                while not self.stop.is_set():
                    start = time.monotonic()
                    stream.write(json.dumps(sample(self.source, self.diagnostics.node_url if self.diagnostics else None, self.expected)) + '\n')
                    stream.flush()
                    self.samples += 1
                    self.stop.wait(max(0, 5 - (time.monotonic() - start)))
        except Exception as error:
            self.error = str(error)

    def close(self, final=True):
        self.stop.set()
        if self.thread.ident is None:
            self.source.close()
            return
        self.thread.join(timeout=10)
        if self.thread.is_alive():
            self.source.close()
            raise RuntimeError('采集器未按时停止')
        if final and self.error is None:
            try:
                with self.path.open('a') as stream:
                    stream.write(json.dumps(sample(self.source, self.diagnostics.node_url if self.diagnostics else None, self.expected)) + '\n')
                    self.samples += 1
            except Exception as error:
                self.error = type(error).__name__
        self.source.close()
        assert self.error is None, '采集失败：' + str(self.error)
