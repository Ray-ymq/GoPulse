"""执行测试四独立组；正式执行会恢复专用测试环境并产生业务写入。"""
import argparse
from datetime import datetime, timezone
import fcntl
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
from common import ROOT, STATE, CONTAINERS, atomic_json, environment, legacy_module, parse_event, restore, rows, sha256, start_services, utc, verify_prepared
from prepare import K6
from report import build_report
from sql_delta import summarize


def plan(args):
    config = json.loads((ROOT / 'perflab/k6/test4.json').read_text())
    if not 0 < args.rps <= config['max_rps'] or not re.fullmatch(r'[A-Za-z0-9_-]+', args.group):
        raise ValueError('组名或请求率无效')
    value = {**config, 'rps': args.rps, 'group': args.group, 'route_set': args.route_set,
            'measurement_seconds': args.measurement_seconds, 'diagnostics': not args.light,
            'profile_role': args.profile_role,
            'expected_business_requests': round(args.rps * (config['warmup_seconds'] + args.measurement_seconds)),
            'config_sha256': sha256(ROOT / 'perflab/k6/test4.json')}
    value['profile_sampling'] = {name: int(os.environ.get(name, '0')) for name in ('PERFLAB_BLOCK_RATE', 'PERFLAB_MUTEX_FRACTION')}
    if any(rate < 0 or rate > 2**63 - 1 for rate in value['profile_sampling'].values()):
        raise ValueError('剖析采样率必须为非负的有符号 64 位整数')
    result = subprocess.run(['node', str(ROOT / 'perflab/k6/test4-plan.mjs')], input=json.dumps(value), text=True, capture_output=True, cwd=ROOT)
    if result.returncode:
        raise ValueError('完整请求序列或语料验证失败：' + result.stderr)
    value['sequence_validation'] = json.loads(result.stdout)
    return value


def load_environment(config, directory):
    return dict(os.environ, GOPULSE_BASE_URL=config['base_url'], LOAD_RUN_ID=directory.name,
                LOAD_CORPUS_PATH=str(ROOT / config['corpus_path']), LOAD_CREDENTIALS_PATH=str(ROOT / config['credentials_path']),
                LOAD_TARGET_RPS=str(config['rps']), LOAD_VUS=str(config['virtual_users']),
                LOAD_WARMUP_SECONDS=str(config['warmup_seconds']), LOAD_MEASUREMENT_SECONDS=str(config['measurement_seconds']),
                LOAD_SCENARIO_TAIL_SECONDS=str(config['scenario_tail_seconds']),
                LOAD_REQUEST_TIMEOUT_SECONDS=str(config['request_timeout_seconds']), LOAD_ROUTE_SET=config['route_set'],
                LOAD_MIX_JSON=json.dumps(config['mix']), LOAD_SUMMARY_PATH=str(directory / 'k6-summary.json'))


def guard(directory):
    value = json.loads((STATE / 'guard.json').read_text())
    stamp = datetime.fromisoformat(value['timestamp'].replace('Z', '+00:00')).timestamp()
    if value.get('directory') != str(directory) or time.time() - stamp > 3:
        raise RuntimeError('guard 身份不符或心跳超时')
    return value


def assert_target(prepared):
    containers = json.loads(subprocess.check_output(['docker', 'inspect', *CONTAINERS], text=True))
    for item in containers:
        name = item['Name'].lstrip('/')
        if prepared['containers'].get(name) != item['Id'] or item['Config']['Labels'].get('com.docker.compose.project') != 'gopulse-loadtest':
            raise RuntimeError('测试容器身份发生变化，须重新核对并准备')
    for state in (ROOT / '.run/pt3', STATE):
        for name in ('server', 'worker', 'indexer', 'observer'):
            if state == STATE and name != 'observer':
                continue
            path = state / 'logs' / (name + '.pid')
            if path.exists():
                pid = int(path.read_text())
                status = Path(f'/proc/{pid}/stat')
                if status.exists() and not status.read_text().split(') ', 1)[1].startswith('Z '):
                    raise RuntimeError('仍有测试进程运行；先按 README 停止并核对 PID：' + str(path))
    if (STATE / 'STOP').exists():
        raise RuntimeError('存在 .run/pt4/STOP；先核对停压原因')


def recovery(directory, seconds):
    smoke = legacy_module('smoke')
    client = smoke.Client()
    began = time.monotonic(); last = {}
    with (directory / 'recovery.jsonl').open('w') as output:
        while time.monotonic() - began < seconds:
            sample = {'timestamp': utc(), 'elapsed_seconds': time.monotonic() - began}
            try:
                sample['login'] = client.login()
                sample['read'] = client.request('GET', '/api/v1/posts?limit=1')[0]
                status, body = client.request('POST', '/api/v1/posts', {'title': 'pt4 recovery probe', 'content': 'pt4 recovery probe'})
                sample['write'] = status
                path = '/api/v1/posts/' + str(body['data']['id'])
                sample['read_created'] = client.request('GET', path)[0]
                sample['delete'] = client.request('DELETE', path)[0]
                sample['business_ok'] = all(sample[name] == value for name, value in [('login', 200), ('read', 200), ('write', 201), ('read_created', 200), ('delete', 204)])
            except Exception as error:
                sample['business_ok'] = False; sample['error'] = str(error)
            critical = directory / 'critical-latest.json'
            if critical.exists():
                value = json.loads(critical.read_text())
                sample['critical_sample_at'] = value['timestamp']
                sample['backlog'] = rows(value, 'backlog')
            try:
                sample['guard'] = guard(directory)
            except (OSError, ValueError, RuntimeError) as error:
                sample['observation_error'] = str(error)
            output.write(json.dumps(sample) + '\n'); output.flush(); last = sample
            time.sleep(min(10, max(0, seconds - (time.monotonic() - began))))
    value = {'observation_seconds': time.monotonic() - began, 'last_business_ok': last.get('business_ok', False),
             'last_backlog': last.get('backlog'), 'status': 'observed',
             'acceptance': '须结合 probe.jsonl、资源基线、异步结果、队列和写入样本按方案验收'}
    atomic_json(directory / 'recovery-summary.json', value)
    return value


def warm_read_paths():
    smoke = legacy_module('smoke')
    corpus = json.loads((STATE / 'corpus.json').read_text())
    client = smoke.Client()
    client.login(0)
    paths = ['/api/v1/users/me', '/api/v1/posts?limit=1',
             '/api/v1/posts/' + str(corpus['read_post_ids'][0]),
             '/api/v1/search/posts?q=go&limit=1', '/api/v1/notifications?limit=1']
    deadline = time.monotonic() + 60
    failed = []
    while True:
        failed = []
        for path in paths:
            try:
                status, _ = client.request('GET', path)
                if status != 200:
                    failed.append(path + ':' + str(status))
            except Exception as error:
                failed.append(path + ':' + type(error).__name__)
        if not failed:
            return
        if time.monotonic() >= deadline:
            raise RuntimeError('恢复后只读路径预热未完成：' + ','.join(failed))
        time.sleep(1)


def smoke_with_retry():
    smoke = legacy_module('smoke')
    for attempt in range(5):
        try:
            warm_read_paths()
            smoke.smoke()
            return
        except Exception:
            if attempt == 4:
                raise
            time.sleep(2)


def execute(config):
    prepared = verify_prepared()
    os.environ.pop('GOPULSE_PERFLAB_PROFILE_ROLE', None)
    os.environ.pop('PERFLAB_PPROF_ADDR', None)
    if config['profile_role']:
        os.environ['GOPULSE_PERFLAB_PROFILE_ROLE'] = config['profile_role']
        port = {'server': 19201, 'worker': 19202, 'indexer': 19203}[config['profile_role']]
        os.environ['PERFLAB_PPROF_ADDR'] = f'127.0.0.1:{port}'
    lock = (STATE / 'execution.lock').open('w')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    assert_target(prepared)
    run_id = config['group'] + '-' + datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    directory = STATE / 'observations' / run_id
    directory.mkdir(mode=0o700)
    config.update(run_id=run_id, created_at=utc(), prepared_sha256=sha256(STATE / 'prepared.json'))
    atomic_json(directory / 'run.json', config)
    (directory / 'prepared.json').write_bytes((STATE / 'prepared.json').read_bytes())
    restore('baseline'); start_services()
    # 功能检查写入后重新恢复，避免改变正式组初始业务事实。
    smoke_with_retry()
    restore('baseline'); start_services()
    command = [sys.executable, str(ROOT / 'perflab/pt4/observe.py'), '--directory', str(directory)]
    if not config['diagnostics']:
        command.append('--light')
    observer_log = (directory / 'observer.log').open('wb')
    observer = subprocess.Popen(command, cwd=ROOT, stdout=observer_log, stderr=observer_log, start_new_session=True)
    (STATE / 'logs/observer.pid').write_text(str(observer.pid))
    requested = [None]
    def interrupt(*_):
        if requested[0] is None:
            requested[0] = time.time()
    previous = {sig: signal.signal(sig, interrupt) for sig in (signal.SIGINT, signal.SIGTERM)}
    process = None
    try:
        for _ in range(150):
            if observer.poll() is not None:
                raise RuntimeError('观测进程提前退出')
            try:
                value = guard(directory)
                if value['status'] == 'stop':
                    raise RuntimeError('观测拒绝启动：' + str(value['reason']))
                if value['status'] == 'ready':
                    break
            except FileNotFoundError:
                pass
            time.sleep(.1)
        else:
            raise RuntimeError('观测链路未准备好')
        idle = time.monotonic()
        while time.monotonic() - idle < config['idle_seconds']:
            if requested[0] or guard(directory)['status'] != 'ready':
                raise RuntimeError('空载观测中止')
            time.sleep(.2)
        env = load_environment(config, directory)
        log = (directory / 'load.log').open('wb')
        command = [str(K6), 'run', '--address', '', '--quiet', '--log-format', 'json',
                   '--log-output', 'file=' + str(directory / 'events.log'), '--out', 'json=' + str(directory / 'k6-samples.json'),
                   str(ROOT / 'perflab/k6/test4.js')]
        config['load_started_epoch'] = time.time()
        atomic_json(directory / 'run.json', config)
        process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=log, stderr=log)
        event_handle = None; scenario_start = None; before_triggered = False
        try:
            while process.poll() is None:
                if event_handle is None and (directory / 'events.log').exists():
                    event_handle = (directory / 'events.log').open()
                if event_handle:
                    for _ in range(10000):
                        offset = event_handle.tell(); line = event_handle.readline()
                        if not line or not line.endswith('\n'):
                            event_handle.seek(offset); break
                        event = parse_event(line)
                        if event and event.get('scenario_started_at_ms'):
                            scenario_start = event['scenario_started_at_ms'] / 1000
                if scenario_start and not before_triggered and time.time() >= scenario_start + config['warmup_seconds'] - 2:
                    observer.send_signal(signal.SIGUSR1); before_triggered = True
                try:
                    value = guard(directory)
                    if value['status'] != 'ready':
                        config['stop_reason'] = value.get('reason') or 'guard_not_ready'
                        interrupt()
                except (OSError, ValueError, RuntimeError):
                    config['stop_reason'] = 'guard_unavailable'; interrupt()
                if requested[0]:
                    process.send_signal(signal.SIGINT)
                    try:
                        process.wait(timeout=8)
                    except subprocess.TimeoutExpired:
                        process.kill(); process.wait()
                    break
                time.sleep(.1)
        except Exception as error:
            config['stop_reason'] = 'runner_observation_error: ' + type(error).__name__
            interrupt()
        if process.poll() is None:
            process.send_signal(signal.SIGINT)
            try:
                process.wait(timeout=8)
            except subprocess.TimeoutExpired:
                process.kill(); process.wait()
        config['stop_requested_epoch'] = requested[0] or time.time()
        config['load_exit_code'] = process.returncode
        if observer.poll() is None:
            observer.send_signal(signal.SIGUSR1)
        if event_handle:
            event_handle.close()
        atomic_json(directory / 'run.json', config)
        log.close()
        recovery(directory, config['recovery_seconds'])
        # 恢复完成后仍需在停止观测前取得最后一份摘要。
    finally:
        if process is not None and process.poll() is None:
            process.send_signal(signal.SIGINT)
            try:
                process.wait(timeout=8)
            except subprocess.TimeoutExpired:
                process.kill(); process.wait()
        observer.terminate()
        try:
            observer.wait(timeout=8)
        except subprocess.TimeoutExpired:
            observer.kill(); observer.wait()
        observer_log.close()
        for sig, handler in previous.items():
            signal.signal(sig, handler)
        (STATE / 'logs/observer.pid').unlink(missing_ok=True)
        lock.close()
    report = build_report(directory)
    if (directory / 'digest.jsonl').exists():
        summarize(directory / 'digest.jsonl', directory / 'sql-delta.json',
                  report['measurement']['started_at_epoch'], report['measurement']['ended_at_epoch'])
    print(json.dumps({'run_id': run_id, 'directory': str(directory), 'execution': report['execution']}, ensure_ascii=False))
    return 0


if __name__ == '__main__':
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--group', required=True)
    parser.add_argument('--rps', type=float, required=True)
    parser.add_argument('--measurement-seconds', type=int, choices=(300, 1800), default=300)
    parser.add_argument('--route-set', choices=('mixed', 'feed', 'detail', 'list', 'comments', 'user', 'write') + tuple('route-' + str(index) for index in range(22)), default='mixed')
    parser.add_argument('--light', action='store_true')
    parser.add_argument('--profile-role', choices=('server', 'worker', 'indexer'))
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    value = plan(args)
    if args.dry_run:
        print(json.dumps(value, ensure_ascii=False, indent=2))
    else:
        sys.exit(execute(value))
