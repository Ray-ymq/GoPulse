"""执行一组测试五 CPU 配额对照压测：设置配额、5 秒采样、运行测试四流程、核对写入。"""
import argparse
from datetime import datetime, timezone
import fcntl
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

from audit import build as build_audit
from common import PT4, ROOT, STATE, atomic_json
import quota
import sample


def group_directory(group):
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    path = STATE / 'groups' / (group + '-' + stamp)
    path.mkdir(mode=0o700, parents=True)
    return path


def stream(command, log_path):
    output = []
    with log_path.open('wb') as log:
        process = subprocess.Popen(command, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        for line in process.stdout:
            log.write(line)
            log.flush()
            sys.stdout.buffer.write(line)
            sys.stdout.buffer.flush()
            output.append(line.decode('utf-8', 'replace').rstrip())
        process.wait()
    return process.returncode, output


def report_result(lines):
    for line in reversed(lines):
        try:
            value = json.loads(line)
        except ValueError:
            continue
        if isinstance(value, dict) and 'directory' in value and 'run_id' in value:
            return value
    return None


def main(args):
    STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    (STATE / 'groups').mkdir(mode=0o700, exist_ok=True)
    lock = (STATE / 'series.lock').open('w')
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        raise SystemExit('已有测试五组在执行')
    if not (PT4 / 'prepared.json').is_file():
        raise SystemExit('测试四环境尚未准备；先执行 python3 perflab/pt4/prepare.py')
    if (PT4 / 'STOP').exists():
        raise SystemExit('存在 .run/pt4/STOP；先核对停压原因并移除')
    if args.rps not in (120, 140):
        raise SystemExit('测试五只执行 120 或 140 RPS')

    from diagnostics.sources import build_container_source
    build_container_source(ROOT, STATE / "bin/container-exporter")
    before = quota.apply(args.cpus)
    directory = group_directory(args.group)
    atomic_json(directory / 'quota-before.json', before)
    if args.preflight:
        started = time.time()
        sampler = sample.Sampler(directory / 'resources.jsonl')
        sampler.start()
        time.sleep(11)
        value = sampler.close()
        summary = sample.summarize(directory / 'resources.jsonl', started + 1, started + 9, args.cpus)
        print(json.dumps({'quota': before, 'sampling': value, 'summary': summary}, ensure_ascii=False, indent=2))
        return 0

    started = time.time()
    sampler = sample.Sampler(directory / 'resources.jsonl')
    sampler.start()
    command = [sys.executable, '-u', str(ROOT / 'perflab/pt4/run.py'), '--group', args.group,
               '--rps', str(args.rps), '--measurement-seconds', '300']
    try:
        exit_code, lines = stream(command, directory / 'run.log')
    finally:
        sampling = sampler.close()
    # 每组恢复结束后停止测试服务，下一组才能重新设置配额并启动干净进程。
    stop_code, _ = stream([sys.executable, str(ROOT / 'perflab/pt4/stop.py')], directory / 'stop.log')
    finished = time.time()
    after = quota.observe()
    consistent = (before['nano_cpus'] == after['nano_cpus'] and before['cgroup_cpus'] == after['cgroup_cpus']
                  and before['memory'] == after['memory'] and before['cgroup_memory_max'] == after['cgroup_memory_max'])
    atomic_json(directory / 'quota-after.json', after)
    atomic_json(directory / 'quota.json', {
        'group': args.group, 'rps': args.rps, 'expected_cpus': args.cpus, 'consistent': consistent,
        'before': before, 'after': after,
        'memory_gib': after['memory'] / 1024**3, 'cgroup_memory_gib': after['cgroup_memory_gib'],
    })

    result = report_result(lines)
    manifest = {'group': args.group, 'rps': args.rps, 'mysql_cpus': args.cpus,
                'started_at': datetime.fromtimestamp(started, timezone.utc).isoformat(),
                'finished_at': datetime.fromtimestamp(finished, timezone.utc).isoformat(),
                'exit_code': exit_code, 'stop_exit_code': stop_code, 'quota_consistent': consistent, 'sampling': sampling,
                'preflight': before, 'after': after, 'group_directory': str(directory)}
    if result is None:
        manifest.update(status='failed', reason='pt4_run_result_missing', work_directory=str(directory))
        atomic_json(directory / 'manifest.json', manifest)
        raise SystemExit('测试四流程未输出运行结果；查看 ' + str(directory / 'run.log'))
    observation = Path(result['directory'])
    manifest['observation_directory'] = str(observation)
    shutil.copyfile(directory / 'resources.jsonl', observation / 'resources.jsonl')
    shutil.copyfile(directory / 'quota.json', observation / 'quota.json')

    report = json.loads((observation / 'report.json').read_text())
    begin = report['measurement']['started_at_epoch']
    end = report['measurement']['ended_at_epoch']
    resources = sample.summarize(observation / 'resources.jsonl', begin, end, args.cpus)
    atomic_json(observation / 'resources-summary.json', resources)
    audit = build_audit(observation)
    summary = {
        'run_id': report['run_id'], 'group': args.group, 'rps': args.rps, 'mysql_cpus': args.cpus,
        'quota': {'expected': args.cpus, 'before': before['cgroup_cpus'], 'after': after['cgroup_cpus'],
                  'memory_gib': after['memory'] / 1024**3, 'consistent': consistent},
        'resources': resources, 'audit': {'status': audit['status'], 'failures': audit['failures'],
                                          'boundary': audit['boundary']['by_reason']},
        'requests': report['measurement'], 'short_window_pass': report['business_short_window_pass'],
        'execution': report['execution'], 'observations': report['observations'],
        'recovery': report['recovery']['status'], 'stop_reason': report.get('stop_reason'),
    }
    manifest.update(status='completed', resources=resources, audit_status=audit['status'])
    atomic_json(directory / 'manifest.json', manifest)
    atomic_json(directory / 'summary.json', summary)
    atomic_json(observation / 'pt5-summary.json', summary)
    atomic_json(STATE / 'groups' / (args.group + '.json'), manifest)
    print(json.dumps(summary, ensure_ascii=False))
    return 0 if exit_code == 0 and stop_code == 0 and audit['status'] == 'passed' and resources['valid'] else 1


if __name__ == '__main__':
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--group', required=True)
    parser.add_argument('--rps', type=int, required=True)
    parser.add_argument('--cpus', type=int, required=True, choices=(2, 4))
    parser.add_argument('--preflight', action='store_true', help='只设置配额并采样 10 秒，不执行压测')
    sys.exit(main(parser.parse_args()))
