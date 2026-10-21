"""记录并核验 MySQL 容器的预期/实际 CPU 配额与内存上限。"""
import json
import time

from common import EXPECTED_MEMORY, EXPECTED_SWAP, MYSQL, cgroup, container, run, utc


def parse_cpu_max(value):
    if not value:
        return None, None
    quota, period = (int(item) for item in value.split())
    if quota <= 0 or period <= 0:
        return None, period
    return quota / period, period


def parse_memory_max(value):
    return None if value in (None, 'max') else int(value)


def observe():
    """读取 docker 配置和 cgroup 实际值，二者必须一致。"""
    item = container()
    config = item['HostConfig']
    cpus, period = parse_cpu_max(cgroup('cpu.max'))
    memory_max = parse_memory_max(cgroup('memory.max'))
    value = {
        'container_id': item['Id'], 'state': item['State']['Status'], 'observed_at': utc(),
        'expected_cpus': config['NanoCpus'] / 1e9,
        'nano_cpus': config['NanoCpus'], 'cgroup_cpus': cpus, 'cgroup_cpu_period': period,
        'memory': config['Memory'], 'memory_swap': config['MemorySwap'],
        'cgroup_memory_max': memory_max,
        'memory_gib': config['Memory'] / 1024**3, 'cgroup_memory_gib': None if memory_max is None else memory_max / 1024**3,
        'restart_policy': config['RestartPolicy'] or {'Name': 'no'},
    }
    reasons = []
    if value['state'] != 'running':
        reasons.append('mysql_container_not_running')
    if value['memory'] != EXPECTED_MEMORY or value['memory_swap'] != EXPECTED_SWAP:
        reasons.append('mysql_memory_not_2_gib')
    if memory_max != EXPECTED_MEMORY:
        reasons.append('mysql_cgroup_memory_not_2_gib')
    if value['restart_policy'].get('Name') != 'no':
        reasons.append('mysql_restart_policy_changed')
    if cpus is None:
        reasons.append('mysql_cpu_quota_missing')
    elif abs(cpus - value['expected_cpus']) > 1e-9:
        reasons.append('mysql_cpu_quota_config_mismatch')
    value['reasons'] = reasons
    value['valid'] = not reasons
    return value


def apply(cpus, timeout=15):
    """设置容器 CPU 配额，并等待 docker 配置与 cgroup 同时生效。"""
    if cpus not in (2, 4):
        raise ValueError('测试五只允许 2 或 4 vCPU')
    if container()['HostConfig']['NanoCpus'] != int(cpus * 1e9):
        run(['docker', 'update', '--cpus', str(cpus), MYSQL])
    deadline = time.monotonic() + timeout
    while True:
        value = observe()
        if abs(value['expected_cpus'] - cpus) <= 1e-9 and abs((value['cgroup_cpus'] or 0) - cpus) <= 1e-9:
            break
        if time.monotonic() >= deadline:
            raise RuntimeError('CPU 配额未在 %d 秒内生效：%s' % (timeout, json.dumps(value, ensure_ascii=False)))
        time.sleep(.25)
    if value['reasons']:
        raise RuntimeError('MySQL 固定资源条件不满足：' + ', '.join(value['reasons']))
    return value


def verify(cpus):
    value = observe()
    reasons = list(value['reasons'])
    if abs(value['expected_cpus'] - cpus) > 1e-9 or abs((value['cgroup_cpus'] or 0) - cpus) > 1e-9:
        reasons.append('cpu_quota_changed')
    if reasons:
        raise RuntimeError('MySQL 资源核验失败：' + ', '.join(sorted(set(reasons))))
    return value


if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cpus', type=int, choices=(2, 4))
    args = parser.parse_args()
    value = apply(args.cpus) if args.cpus else observe()
    print(json.dumps(value, ensure_ascii=False, indent=2))
