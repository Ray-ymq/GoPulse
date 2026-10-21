"""默认从 250 RPS 开始，可指定起点；每档增加 50 RPS，首次失败停止并保存结果。"""
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
                    utc, verify_prepared, write_json, compose_environment)
from sample import Sampler
from diagnose import Diagnostics, capture_logs
from report import summarize


def parse_events(lines):
    result = []
    for line in lines:
        match = re.search(r'msg=("(?:[^"\\]|\\.)*")', line)
        message = json.loads(match[1]) if match else line
        if 'PT6 {' in message:
            result.append(json.loads(message[message.index('PT6 {') + 4:]))
    return result


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
    diagnostics = Diagnostics(preparation, output).start()
    sampler = Sampler(output / 'resources.jsonl', diagnostics)
    start = time.time()
    interrupted = None
    process = None
    first_notified = False
    load_ended = None
    try:
        sampler.start()
        deadline = time.monotonic() + 8
        while not sampler.samples:
            assert not sampler.error and not diagnostics.error, '开测前诊断采样失败'
            assert time.monotonic() < deadline, '开测前资源采样超时'
            time.sleep(.05)
        with (output / 'k6.log').open('x') as log:
            process = subprocess.Popen(args, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT,
                                       env=compose_environment(preparation))
            with (output / 'k6.log').open() as events_log:
                while True:
                    position = events_log.tell()
                    line = events_log.readline()
                    if line and not line.endswith('\n'):
                        events_log.seek(position)
                        line = ''
                    if line:
                        for event in parse_events([line]):
                            if event['type'] == 'first_failure' and not first_notified:
                                diagnostics.trigger('first_failure')
                                write_json(output / 'failure-trigger.json', {'observed_at': utc(), 'client_event': event})
                                first_notified = True
                        continue
                    if process.poll() is not None:
                        break
                    if sampler.error or diagnostics.error or (STATE / 'STOP').exists():
                        interrupted = sampler.error or diagnostics.error or 'manual_stop'
                        diagnostics.trigger('first_failure')
                        subprocess.run(['docker', 'stop', '--timeout', '5', PROJECT + '-k6-active'],
                                       capture_output=True, timeout=10)
                        break
                    time.sleep(.05)
            code = process.wait(timeout=15)
            load_ended = time.time()
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
        finally:
            diagnostics.close()
            if diagnostics.error:
                interrupted = interrupted or diagnostics.error
            capture_logs(output, utc_from_epoch(start))
    path = output / 'summary.json'
    summary = json.loads(path.read_text()) if path.exists() else {}
    result = evaluate(summary, target, 300, code)
    events = parse_events((output / 'k6.log').read_text().splitlines())
    result['first_failure'] = next((event for event in events if event['type'] == 'first_failure'), None)
    clock = next((event for event in events if event['type'] == 'clock'), None)
    measured_seconds = 0
    if clock:
        end = load_ended or time.time()
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
    result['load_passed'] = result['passed']
    if interrupted:
        result['passed'] = False
        result['reasons'].append(interrupted)
    result.update(started_at=utc_from_epoch(start), ended_at=utc(), elapsed_seconds=time.time() - start,
                  k6_exit_code=code, slot_offset=offset, directory=str(output))
    if measured_seconds > 0:
        begin = clock['scenario_started_at_ms'] / 1000 + 30
        phase, end = 'measurement', begin + measured_seconds
    else:
        phase, begin, end = 'setup_or_warmup', start, load_ended or time.time()
    try:
        result['diagnostics'] = summarize(output, begin, end)
        result['diagnostics']['window']['phase'] = phase
    except Exception as error:
        result['diagnostics'] = {'status': 'incomplete', 'reasons': [type(error).__name__], 'file': 'diagnostics-summary.json'}
        write_json(output / 'diagnostics-summary.json', result['diagnostics'])
    if result['diagnostics']['status'] != 'complete':
        result['passed'] = False
        result['reasons'].append('diagnostics_incomplete')
    write_json(output / 'result.json', result)
    return result


def utc_from_epoch(epoch):
    from datetime import datetime, timezone
    return datetime.fromtimestamp(epoch, timezone.utc).isoformat()


def observe_recovery(directory, preparation):
    output = directory / 'recovery'
    output.mkdir(mode=0o700)
    diagnostics = Diagnostics(preparation, output).start()
    sampler = Sampler(output / 'resources.jsonl', diagnostics)
    start = time.time()
    sampler.start()
    try:
        ready_deadline = time.monotonic() + 8
        while not sampler.samples:
            assert not sampler.error and not diagnostics.error, '恢复期首次诊断采样失败'
            assert time.monotonic() < ready_deadline, '恢复期首次资源采样超时'
            time.sleep(.05)
        start = time.time()
        deadline = time.monotonic() + 600
        while time.monotonic() < deadline and not sampler.error and not diagnostics.error and not (STATE / 'STOP').exists():
            time.sleep(.5)
    finally:
        # 开始边界位于首样本之后，结束边界位于末次采样之前。
        end = time.time()
        close_error = None
        try:
            diagnostics.trigger('recovery_end')
        except Exception as error:
            close_error = str(error)
        try:
            sampler.close()
        except Exception as error:
            close_error = close_error or str(error)
        diagnostics.close()
        capture_logs(output, utc_from_epoch(start))
        try:
            summary = summarize(output, start, end)
        except Exception as error:
            summary = {'status': 'incomplete', 'reasons': [type(error).__name__]}
        write_json(directory / 'recovery-result.json', {'finished_at': utc(),
                   'samples': sampler.samples, 'observation_seconds': max(0, end - start),
                   'collection_error': sampler.error or diagnostics.error or close_error,
                   'diagnostics': summary})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--preflight', action='store_true', help='只检查准备结果并采样，不发送业务负载')
    parser.add_argument('--start-rps', type=int, default=250,
                        help='起始 RPS，默认 250；须不低于 250 且为 50 的整数倍')
    args = parser.parse_args()
    if args.start_rps < 250 or args.start_rps % 50:
        parser.error('--start-rps 须不低于 250 且为 50 的整数倍')
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
        diagnostics = Diagnostics(prepared['directory'], output).start()
        sampler = Sampler(output / 'resources.jsonl', diagnostics)
        try:
            sampler.start()
            deadline = time.monotonic() + 11
            while time.monotonic() < deadline:
                assert not diagnostics.error, diagnostics.error
                time.sleep(.1)
        finally:
            try:
                sampler.close()
            finally:
                diagnostics.close()
        assert not diagnostics.error, diagnostics.error
        assert sampler.samples >= 3, '采样次数不足'
        write_json(output / 'result.json', {'ok': True, 'samples': sampler.samples, 'finished_at': utc()})
        print('PT6 preflight passed:', output)
        return
    output = STATE / ('run-' + time.strftime('%Y%m%dT%H%M%S') + '-' + secrets.token_hex(3))
    output.mkdir()
    prepared.update(ready=False, phase='load_started', run_directory=str(output))
    write_json(STATE / 'prepared.json', prepared)
    report = {'started_at': utc(), 'directory': str(output), 'start_rps': args.start_rps,
              'stages': [], 'last_passed_rps': None, 'load_passed_rps': None,
              'preparation': prepared['directory'], 'source_sha256': prepared['source_sha256']}
    target, offset = args.start_rps, 0
    try:
        while True:
            print(f'PT6: {target} RPS', flush=True)
            result = stage(prepared, output, target, offset)
            report['stages'].append(result)
            if result['load_passed']:
                report['load_passed_rps'] = target
            if not result['passed']:
                report.update(stopped_rps=target, failed_rps=None if result['load_passed'] else target,
                              stopped_at=utc(), reasons=result['reasons'])
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
                          'stopped_rps': report.get('stopped_rps'), 'load_passed_rps': report['load_passed_rps'],
                          'last_passed_rps': report['last_passed_rps'],
                          'first_failure': report['stages'][-1].get('first_failure') if report['stages'] else None,
                          'reasons': report.get('reasons', [report.get('error')])}, ensure_ascii=False), flush=True)
    observe_recovery(output, prepared['directory'])


if __name__ == '__main__':
    main()
