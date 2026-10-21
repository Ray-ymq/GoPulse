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
from common import ROOT, STATE, CONTAINERS, atomic_json, environment, legacy_module, restore, sha256, sql, start_services, utc, verify_prepared
from prepare import K6, configure_diagnostics
from report import build_report
from profiles import PACKAGES, configure as configure_profiles, settings as profile_settings

# 恢复和迁移会产生大量摘要，占用插件的有界输出并让正式窗口继承它们。每组在
# 观测开始前清空这些汇总表，使摘要/事件/错误/内部等待都只反映本组流量。
RESET_SUMMARY_TABLES = (
    'performance_schema.events_statements_summary_by_digest',
    'performance_schema.events_statements_summary_global_by_event_name',
    'performance_schema.events_errors_summary_global_by_error',
    'performance_schema.events_waits_summary_global_by_event_name',
)


def reset_summary():
    sql(';'.join('TRUNCATE TABLE ' + table for table in RESET_SUMMARY_TABLES) + ';', root=True)
    return {'tables': list(RESET_SUMMARY_TABLES), 'reset_at': utc()}


def plan(args):
    config = json.loads((ROOT / 'perflab/k6/test4.json').read_text())
    if not 0 < args.rps <= config['max_rps'] or not re.fullmatch(r'[A-Za-z0-9_-]+', args.group):
        raise ValueError('组名或请求率无效')
    value = {**config, 'rps': args.rps, 'group': args.group, 'route_set': args.route_set,
            'measurement_seconds': args.measurement_seconds,
            **profile_settings(args.profile_role, args.block_rate, args.mutex_fraction),
            'expected_business_requests': round(args.rps * (config['warmup_seconds'] + args.measurement_seconds)),
            'config_sha256': sha256(ROOT / 'perflab/k6/test4.json')}
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
            critical = directory / 'database-state-latest.json'
            if critical.exists():
                value = json.loads(critical.read_text())
                sample['critical_sample_at'] = value['timestamp']
                sample['backlog'] = value.get('backlog', [])
                stamp = datetime.fromisoformat(value['timestamp'].replace('Z', '+00:00')).timestamp()
                sample['environment_fresh'] = 0 <= time.time() - stamp <= 8
            try:
                sample['guard'] = guard(directory)
            except (OSError, ValueError, RuntimeError) as error:
                sample['observation_error'] = str(error)
            output.write(json.dumps(sample) + '\n'); output.flush(); last = sample
            time.sleep(min(10, max(0, seconds - (time.monotonic() - began))))
    backlog = last.get('backlog', [])
    drained = bool(backlog) and all(int(item['open_requirements']) == 0 and int(item['parked_messages']) == 0 for item in backlog)
    passed = last.get('business_ok', False) and last.get('environment_fresh', False) and drained
    value = {'observation_seconds': time.monotonic() - began, 'last_business_ok': last.get('business_ok', False),
             'last_backlog': backlog, 'status': 'passed' if passed else 'failed',
             'acceptance': '确认恢复末次业务检查成功，环境检查有效且全部 epoch 的积压归零'}
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
    lock = (STATE / 'execution.lock').open('w')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    assert_target(prepared)
    config['diagnostics'] = configure_diagnostics()
    configure_profiles(config['profile_role'], **config['profile_sampling'])
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
    config['summary_reset'] = reset_summary()
    atomic_json(directory / 'run.json', config)
    command = [sys.executable, str(ROOT / 'perflab/pt4/observe.py'), '--directory', str(directory)]
    (STATE / 'guard.json').unlink(missing_ok=True)
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
        try:
            while process.poll() is None:
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
        atomic_json(directory / 'run.json', config)
        log.close()
        recovery(directory, config['recovery_seconds'])
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
    print(json.dumps({'run_id': run_id, 'directory': str(directory), 'execution': report['execution']}, ensure_ascii=False))
    return 0


if __name__ == '__main__':
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--group', required=True)
    parser.add_argument('--rps', type=float, required=True)
    parser.add_argument('--measurement-seconds', type=int, choices=(300, 1800), default=300)
    parser.add_argument('--route-set', choices=('mixed', 'feed', 'detail', 'list', 'comments', 'user', 'write') + tuple('route-' + str(index) for index in range(22)), default='mixed')
    parser.add_argument('--profile-role', choices=PACKAGES, help='按需启用一个服务的本机 pprof 入口')
    parser.add_argument('--block-rate', type=int, default=0, help='阻塞采样间隔，单位为纳秒；0 关闭')
    parser.add_argument('--mutex-fraction', type=int, default=0, help='锁竞争采样比例分母；0 关闭')
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    value = plan(args)
    if args.dry_run:
        print(json.dumps(value, ensure_ascii=False, indent=2))
    else:
        sys.exit(execute(value))
