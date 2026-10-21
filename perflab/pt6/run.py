"""从 250 RPS 开始逐档增加 50 RPS；首次失败停止并保存结果。"""
import argparse
import json
import os
from pathlib import Path
import secrets
import fcntl
import re
import subprocess
import time

from common import (ROOT, STATE, PROJECT, MIX, check_quotas, check_host, drained,
                    utc, verify_prepared, write_json)
from sample import Sampler


def evaluate(summary, target, seconds, code):
    metrics = summary.get('metrics', {})
    def value(name, field='count'):
        return metrics.get(name, {}).get('values', {}).get(field)
    complete = value('pt6_completed{phase:measurement}')
    success = value('pt6_successes{phase:measurement}')
    p95 = value('pt6_latency{phase:measurement}', 'p(95)')
    p99 = value('pt6_latency{phase:measurement}', 'p(99)')
    reasons = []
    if code != 0:
        reasons.append('k6_failed')
    if value('pt6_failures') not in (None, 0):
        reasons.append('business_failure')
    if value('dropped_iterations') not in (None, 0):
        reasons.append('dropped_iterations')
    if complete is None or success is None or p95 is None or p99 is None:
        reasons.append('measurement_incomplete')
    else:
        if complete != success:
            reasons.append('business_failure')
        if complete / seconds < target * .95:
            reasons.append('achieved_rps_below_95_percent')
        if p95 > 1000 or p99 > 2000:
            reasons.append('latency_threshold')
    if value('pt6_started') != value('pt6_completed'):
        reasons.append('unfinished_requests')
    return {'passed': not reasons, 'reasons': reasons, 'target_rps': target,
            'actual_rps': None if complete is None else complete / seconds,
            'successful_rps': None if success is None else success / seconds,
            'p95_ms': p95, 'p99_ms': p99,
            'business_failures': value('pt6_failures'), 'dropped_iterations': value('dropped_iterations'),
            'measurement_completed': complete, 'measurement_successes': success,
            'started': value('pt6_started'), 'completed': value('pt6_completed')}


def stage(prepared, directory, target, offset):
    preparation = Path(prepared['directory'])
    output = directory / f'{target}-rps'
    output.mkdir(mode=0o700)
    variables = {'GOPULSE_BASE_URL': 'http://frontend:8080', 'LOAD_VUS': '512',
                 'LOAD_TARGET_RPS': str(target), 'LOAD_WARMUP_SECONDS': '30',
                 'LOAD_MEASUREMENT_SECONDS': '300', 'LOAD_REQUEST_TIMEOUT_SECONDS': '5',
                 'LOAD_SLOT_OFFSET': str(offset), 'LOAD_MIX_JSON': json.dumps(MIX),
                 'LOAD_CORPUS_PATH': '/input/corpus.json', 'LOAD_CREDENTIALS_PATH': '/input/credentials.json',
                 'LOAD_SUMMARY_PATH': '/output/summary.json'}
    args = ['docker', 'compose', '--project-name', PROJECT, '--env-file', str(preparation / 'test.env'),
            '-f', str(ROOT / 'deploy/compose.business.yaml'), 'run', '--rm', '--no-deps',
            '--name', PROJECT + '-k6-active', '--user', f'{os.getuid()}:{os.getgid()}',
            '-v', f'{ROOT / "perflab"}:/perflab:ro', '-v', f'{preparation}:/input:ro', '-v', f'{output}:/output']
    for key, value in variables.items():
        args += ['-e', key + '=' + value]
    args += ['k6', 'run', '--quiet', '/perflab/pt6/workload.js']
    sampler = Sampler(output / 'resources.jsonl')
    sampler.start()
    start = time.time()
    interrupted = None
    process = None
    try:
        with (output / 'k6.log').open('x') as log:
            process = subprocess.Popen(args, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT)
            while process.poll() is None:
                if sampler.error or (STATE / 'STOP').exists():
                    interrupted = sampler.error or 'manual_stop'
                    subprocess.run(['docker', 'stop', '--timeout', '5', PROJECT + '-k6-active'],
                                   capture_output=True, timeout=10)
                    break
                time.sleep(.1)
            code = process.wait(timeout=15)
    except BaseException:
        subprocess.run(['docker', 'stop', '--timeout', '5', PROJECT + '-k6-active'], capture_output=True, timeout=10)
        if process is not None:
            process.wait(timeout=15)
        raise
    finally:
        try:
            sampler.close()
        except Exception as error:
            interrupted = str(error)
    path = output / 'summary.json'
    summary = json.loads(path.read_text()) if path.exists() else {}
    result = evaluate(summary, target, 300, code)
    events = []
    for line in (output / 'k6.log').read_text().splitlines():
        # k6 的控制台输出对 JSON 字符串加引号和转义。
        match = re.search(r'msg=("(?:[^"\\]|\\.)*")', line)
        message = json.loads(match[1]) if match else line
        if 'PT6 {' in message:
            events.append(json.loads(message[message.index('PT6 {') + 4:]))
    result['first_failure'] = next((event for event in events if event['type'] == 'first_failure'), None)
    clock = next((event for event in events if event['type'] == 'clock'), None)
    measured_seconds = 0
    if clock:
        end = time.time()
        if result['first_failure']:
            from datetime import datetime
            end = datetime.fromisoformat(result['first_failure']['timestamp'].replace('Z', '+00:00')).timestamp()
        result['scenario_elapsed_seconds'] = max(0, end - clock['scenario_started_at_ms'] / 1000)
        measured_seconds = min(300, max(0, result['scenario_elapsed_seconds'] - 30))
    result['measurement_seconds'] = measured_seconds
    measured = summary.get('metrics', {})
    for key, counter in (('actual_rps', 'pt6_completed{phase:measurement}'),
                         ('successful_rps', 'pt6_successes{phase:measurement}')):
        count = measured.get(counter, {}).get('values', {}).get('count')
        result[key] = count / measured_seconds if count is not None and measured_seconds > 0 else None
    if measured_seconds < 300:
        result['passed'] = False
        result['reasons'] = [reason for reason in result['reasons'] if reason != 'achieved_rps_below_95_percent']
        if 'measurement_incomplete' not in result['reasons']:
            result['reasons'].append('measurement_incomplete')
    if interrupted:
        result['passed'] = False
        result['reasons'].append(interrupted)
    result.update(started_at=utc_from_epoch(start), ended_at=utc(), elapsed_seconds=time.time() - start,
                  k6_exit_code=code, slot_offset=offset, directory=str(output))
    write_json(output / 'result.json', result)
    return result


def utc_from_epoch(epoch):
    from datetime import datetime, timezone
    return datetime.fromtimestamp(epoch, timezone.utc).isoformat()


def observe_recovery(directory):
    sampler = Sampler(directory / 'recovery.jsonl')
    sampler.start()
    try:
        deadline = time.monotonic() + 600
        while time.monotonic() < deadline and not sampler.error and not (STATE / 'STOP').exists():
            time.sleep(.5)
    finally:
        try:
            sampler.close()
        except Exception:
            pass
        write_json(directory / 'recovery-result.json', {'finished_at': utc(),
                   'samples': sampler.samples, 'collection_error': sampler.error})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--preflight', action='store_true', help='只检查准备结果并采样，不发送业务负载')
    args = parser.parse_args()
    os.umask(0o077)
    STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    lock = (STATE / 'active.lock').open('a')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    prepared = verify_prepared()
    check_host()
    actual = check_quotas()
    assert actual == prepared['containers'], '服务容器或镜像与准备记录不符'
    drained()
    assert not (STATE / 'STOP').exists(), '存在 PT6 停止标记'
    if args.preflight:
        output = STATE / ('preflight-' + time.strftime('%Y%m%dT%H%M%S') + '-' + secrets.token_hex(3))
        output.mkdir()
        sampler = Sampler(output / 'resources.jsonl')
        sampler.start()
        time.sleep(11)
        sampler.close()
        assert sampler.samples >= 3, '采样次数不足'
        write_json(output / 'result.json', {'ok': True, 'samples': sampler.samples, 'finished_at': utc()})
        print('PT6 preflight passed:', output)
        return
    output = STATE / ('run-' + time.strftime('%Y%m%dT%H%M%S') + '-' + secrets.token_hex(3))
    output.mkdir()
    prepared.update(ready=False, phase='load_started', run_directory=str(output))
    write_json(STATE / 'prepared.json', prepared)
    report = {'started_at': utc(), 'directory': str(output), 'stages': [], 'last_passed_rps': None,
              'preparation': prepared['directory'], 'source_sha256': prepared['source_sha256']}
    target, offset = 250, 0
    try:
        while True:
            print(f'PT6: {target} RPS', flush=True)
            result = stage(prepared, output, target, offset)
            report['stages'].append(result)
            if not result['passed']:
                report.update(failed_rps=target, stopped_at=utc(), reasons=result['reasons'])
                break
            report['last_passed_rps'] = target
            offset += result['started']
            target += 50
            write_json(output / 'report.json', report)
    except BaseException as error:
        report.update(stopped_at=utc(), error=str(error))
        raise
    finally:
        write_json(output / 'report.json', report)
        prepared.update(phase='load_stopped', stopped_at=utc())
        write_json(STATE / 'prepared.json', prepared)
        print(json.dumps({'report': str(output / 'report.json'), 'failed_rps': report.get('failed_rps'),
                          'last_passed_rps': report['last_passed_rps'],
                          'first_failure': report['stages'][-1].get('first_failure') if report['stages'] else None,
                          'reasons': report.get('reasons', [report.get('error')])}, ensure_ascii=False), flush=True)
    observe_recovery(output)


if __name__ == '__main__':
    main()
