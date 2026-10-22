"""核对固定资源、配置及准备指纹。"""
import json
from pathlib import Path

from .common import ROOT, STATE, APPS, command, containers, digest, sql, es, check_host, check_quotas, drained
from .config import contract, fingerprint, load
from .runtime import write_json


def source_hashes():
    paths = [ROOT / 'deploy/compose.business.yaml', ROOT / 'exporters/node/upstream.json', ROOT / 'VERSION']
    for relative in ('perflab/flow', 'perflab/internal', 'perflab/cmd', 'perflab/k6', 'perflab/diagnostics',
                     'exporters/mysql', 'runtimekit/containerstats', 'runtimekit/cmd/container-exporter'):
        paths += [path for path in (ROOT / relative).rglob('*') if path.suffix in ('.py', '.go', '.js', '.mjs', '.json', '.mod', '.sum')]
    paths += [ROOT / 'perflab/go.mod', ROOT / 'perflab/go.sum', ROOT / 'runtimekit/go.mod']
    return {str(path.relative_to(ROOT)): digest(path) for path in sorted(set(paths))}


def inspect_rows(rows, resources):
    expected = set(resources['services']) - {'k6'}
    running = {name for name, item in rows.items() if item['State']['Running'] and name != 'k6'}
    if running != expected:
        raise ValueError('实例集合不符：' + str(sorted(running ^ expected)))
    result = {}
    for name in sorted(expected):
        item = rows[name]
        c, state = item['HostConfig'], item['State']
        expected_row = resources['services'][name]
        cpus = c['NanoCpus'] / 1e9 if c['NanoCpus'] else c['CpuQuota'] / c['CpuPeriod']
        if (cpus != expected_row['cpus'] or c['Memory'] != expected_row['memory_mib'] * 1024**2
                or c['MemorySwap'] != c['Memory'] or c['RestartPolicy']['Name'] != 'no'):
            raise ValueError(name + ' Docker 资源配置不符')
        if state.get('OOMKilled') or item.get('RestartCount', 0) or state.get('Health', {}).get('Status') != 'healthy':
            raise ValueError(name + ' 崩溃、重启、OOM 或未就绪')
        result[name] = {'id': item['Id'], 'image': item['Image'], 'started_at': state['StartedAt'],
                        'configuration_sha256': fingerprint({'env': sorted(item['Config']['Env']),
                                                             'command': item['Config'].get('Cmd'), 'host': c})}
    return result


def snapshot(config):
    rows = containers()
    result = inspect_rows(rows, config['resources'])
    buffer_pool = int(sql('SELECT @@innodb_buffer_pool_size;', root=True).strip())
    if buffer_pool != config['resources']['mysql_buffer_pool_bytes']:
        raise ValueError('MySQL buffer pool 不符')
    nodes = es('/_nodes/jvm')['nodes'].values()
    if len(nodes) != 1 or any(node['jvm']['mem']['heap_max_in_bytes'] != config['resources']['elasticsearch_heap_bytes']
                              or node['jvm']['mem']['heap_init_in_bytes'] != config['resources']['elasticsearch_heap_bytes'] for node in nodes):
        raise ValueError('Elasticsearch 堆不符')
    for name in ('backend', 'backend-2', 'platform-api'):
        env = dict(value.split('=', 1) for value in rows[name]['Config']['Env'])
        if env.get('REDIS_HOST') != 'redis':
            raise ValueError(name + ' Redis 未启用')
    # 全部容器参数写入私有归档，公开结果只保存指纹。
    return {'containers': result, 'buffer_pool_bytes': buffer_pool,
            'heap_bytes': config['resources']['elasticsearch_heap_bytes'],
            'database_identity': sql('SELECT @@server_uuid; SELECT data_epoch,status,mode FROM business_environment;', root=True).strip()}


def prepared(config):
    path = STATE / 'prepared.json'
    value = json.loads(path.read_text())
    if value.get('schema_version') != 'gopulse.pt.preparation.v1' or not value.get('ready'):
        raise ValueError('公共准备记录未就绪；不导入旧轮次状态')
    directory = Path(value['directory'])
    if directory != STATE / 'private':
        raise ValueError('准备目录不属于公共流程')
    if value['source_sha256'] != source_hashes():
        raise ValueError('准备后源码或配置已改变，请重新准备')
    for name, expected in value['artifact_sha256'].items():
        if digest(directory / name) != expected:
            raise ValueError('准备后工具或私有输入已改变：' + name)
    if fingerprint(config['resources']) != value['resources_sha256']:
        raise ValueError('准备后固定资源已改变')
    return value


def preflight(config):
    value = prepared(config)
    check_host()
    actual = snapshot(config)
    if actual != value['environment']:
        raise ValueError('环境身份或固定配置与准备记录不符')
    drained()
    from .observe import precheck
    precheck(Path(value['directory']))
    return value
