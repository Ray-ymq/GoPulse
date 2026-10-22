"""固定阶段编排；负载策略和业务判断由 Go 提供。"""
import json
from pathlib import Path
import signal
import subprocess
import time

from .config import ROOT, contract, load
from .runtime import Run, STATE, utc, write_json, append
from .environment import prepared, preflight, snapshot, source_hashes
from .common import command
from . import baseline
from .observe import Observation, recovery
from .analysis import summarize
from .diagnose import capture_logs
from .audit import build as audit
from .diagnostics_registry import registry


class Abort(RuntimeError):
    def __init__(self, reason, code=3):
        super().__init__(reason)
        self.code = code


def routes():
    raw = subprocess.check_output(['node', '--input-type=module', '-e',
                                  "import {routeTemplates} from './perflab/k6/requests.mjs'; console.log(JSON.stringify(routeTemplates));"],
                                 cwd=ROOT, text=True)
    return json.loads(raw)


def boundary(groups):
    # 边界只使用本次实际完成并达标的结果；异常上界必须高于正常下界。
    normal = []
    abnormal = []
    for group in groups:
        for stage in group.get('load', {}).get('stages', []):
            if stage['business_passed']:
                normal.append((stage['target_rps'], group['group_id']))
            elif stage.get('execution_completed') or stage.get('stop_reason') == 'failure_rate_over_50_percent':
                abnormal.append(stage['target_rps'])
    if abnormal:
        high = min(abnormal)
        below = [(rps, source) for rps, source in normal if rps < high]
        low, source = max(below, default=(0, None))
    else:
        high = 0
        low, source = max(normal, default=(0, None))
    return {'normal_rps': low or None, 'abnormal_rps': high or None, 'normal_source': source,
            'valid': bool(low and high > low)}


class Pipeline:
    def __init__(self, config, backend):
        self.config, self.backend = config, backend
        self.groups = []
        self.skipped = []
        self.capacity = {}

    def group(self, name, target, seconds=None, scan=False, route=-1, restore=True):
        if restore:
            self.backend.restore()
        result = self.backend.group(name, target, seconds or self.config['measurement_seconds'], scan, route)
        self.groups.append(result)
        if not result['recovery']['passed']:
            raise Abort('恢复窗口内未恢复')
        if not result['audit']['passed']:
            raise Abort('数据正确性核对失败', 1)
        if not result['resources']['passed'] or not result['evidence']['passed']:
            raise Abort('固定资源或必需证据不完整')
        return result

    def run(self):
        self.backend.idle()
        low = self.group('low', 10, restore=False)
        if not low['business_passed']:
            self.skipped.append({'stages': ['scan', 'refine', 'repeat', 'soak', 'routes'], 'reason': '低负载未达标'})
            return 1
        self.group('scan', self.config['start_rps'], scan=True)
        candidates = list(self.groups)
        self.capacity = boundary(candidates)
        if self.capacity['valid']:
            for index in range(self.config['refine_iterations']):
                stage = self.backend.refine(self.capacity['normal_rps'], self.capacity['abnormal_rps'])
                if stage is None:
                    break
                result = self.group(f'refine-{index+1}', stage['target_rps'])
                candidates.append(result)
                self.capacity = boundary(candidates)
        else:
            self.skipped.append({'stage': 'refine', 'reason': '缺少有效正常下界或异常上界'})
        self.capacity = boundary(candidates)
        if self.capacity['abnormal_rps']:
            for index in range(self.config['repeat_count']):
                self.group(f'repeat-{index+1}', self.capacity['abnormal_rps'])
        else:
            self.skipped.append({'stage': 'repeat', 'reason': '扫描上限内未找到异常档'})
        normal = self.capacity['normal_rps']
        if normal:
            self.group('soak-100', normal, self.config['soak_seconds'])
            self.group('soak-80', max(1, normal * 80 // 100), self.config['soak_seconds'])
            for index, template in enumerate(self.backend.routes()):
                self.group(f'route-{index+1:02d}', normal, route=index)
        else:
            self.skipped.append({'stages': ['soak', 'routes'], 'reason': '缺少已验证正常档'})
        mandatory = [g for g in self.groups if not g['group_id'].startswith(('scan', 'refine', 'repeat'))]
        return 0 if all(g['business_passed'] and g['audit']['passed'] for g in mandatory) else 1


class Backend:
    def __init__(self, run, config, value):
        self.run, self.config, self.value = run, config, value
        self.preparation = Path(value['directory'])
        self.expected = value['environment']
        self.collection = None

    def check_stop(self):
        if self.run.stopped():
            self.run.update('stopping')
            raise Abort('manual_stop', 130)

    def track_observation(self, observation):
        for process in (observation.diagnostics.process, observation.diagnostics.node,
                        observation.diagnostics.mysql_source.process, observation.sampler.source.process):
            self.run.processes.track(process)

    def restore(self):
        self.check_stop()
        if source_hashes() != self.value['source_sha256']:
            raise Abort('运行期间源码或固定配置文件改变')
        if snapshot(self.config) != self.expected:
            raise Abort('恢复基线前固定配置或环境身份改变')
        baseline.restore(self.preparation)
        actual = snapshot(self.config)
        for name, before in self.expected['containers'].items():
            if {k: v for k, v in before.items() if k != 'started_at'} != {k: v for k, v in actual['containers'][name].items() if k != 'started_at'}:
                raise Abort('恢复期间固定配置或容器改变')
        if {k: v for k, v in actual.items() if k != 'containers'} != {k: v for k, v in self.expected.items() if k != 'containers'}:
            raise Abort('恢复期间环境身份改变')
        self.expected = actual
        append(self.run.directory / 'events.jsonl', {'stage': 'baseline_restored', 'at': utc(), 'environment': actual})

    def idle(self):
        self.run.update(stage='idle')
        output = self.run.directory / 'idle'
        observation = Observation(self.preparation, output, self.config, self.expected).start()
        self.collection = observation
        self.track_observation(observation)
        start = time.time()
        try:
            deadline = time.monotonic() + self.config['idle_seconds']
            while time.monotonic() < deadline:
                self.check_stop()
                observation.check()
                time.sleep(.1)
        finally:
            end = time.time()
            error = observation.close()
            self.collection = None
        evidence = summarize(output, start, end)
        if error or evidence['status'] != 'complete':
            raise Abort('空载观测证据不完整')

    def group(self, name, target, seconds, scan, route):
        self.check_stop()
        self.run.update(stage=name, target_rps=target)
        output = self.run.directory / 'groups' / name
        for folder in ('metrics', 'diagnostics', 'logs'):
            (output / folder).mkdir(parents=True, mode=0o700)
        task = contract('task', group_id=name, run_id=self.run.id, start_rps=self.config['start_rps'], max_rps=self.config['max_rps'],
                        stages=[{'name': name, 'target_rps': target, 'warmup_seconds': self.config['warmup_seconds'], 'measurement_seconds': seconds}],
                        output=str(output), credentials_file=str(self.preparation / 'credentials.json'), preparation=str(self.preparation),
                        root=str(ROOT), project='gopulse-pt-business', safety_file=str(output / 'safety.json'),
                        route=route, scan=scan, offset=0, vus=self.config['vus'], mix=self.config['mix'])
        write_json(output / 'task.json', task)
        result = contract('group', group_id=name, target_rps=target, execution_completed=False, business_passed=False, stop_reason=None,
                          recovery={'passed': False, 'reason': 'not_executed'}, audit={'passed': False, 'reason': 'not_executed'},
                          evidence={'passed': False}, resources={'passed': False}, load={'stages': []})
        observation = None
        process = None
        start, end = time.time(), time.time()
        failure = None
        try:
            if source_hashes() != self.value['source_sha256']:
                raise Abort('运行期间源码或固定配置文件改变')
            if snapshot(self.config) != self.expected:
                raise Abort('组边界环境身份或配置改变')
            observation = Observation(self.preparation, output, self.config, self.expected).start()
            self.collection = observation
            self.track_observation(observation)
            observation.safety(output / 'safety.json')
            with (output / 'logs/load.log').open('x') as log:
                process = self.run.processes.start([str(self.preparation / 'bin/pt-load'), '--task', str(output / 'task.json')],
                                                   cwd=ROOT, stdout=log, stderr=subprocess.STDOUT)
                previous = None
                while process.poll() is None:
                    self.check_stop()
                    row = observation.safety(output / 'safety.json')
                    if row['epoch'] != previous:
                        append(self.run.directory / 'resource-checks.jsonl', {'epoch': row['epoch'], 'group_id': name,
                                                                            'environment': row['environment']})
                        previous = row['epoch']
                    status = output / 'load-state.json'
                    if status.exists():
                        self.run.update(load=json.loads(status.read_text()), collection='active')
                    time.sleep(.25)
                end = time.time()
            load_result = json.loads((output / 'load-result.json').read_text())
            result['load'] = load_result
            result['execution_completed'] = all(s['execution_completed'] or s.get('stop_reason') == 'failure_rate_over_50_percent' for s in load_result['stages'])
            result['business_passed'] = all(s['business_passed'] for s in load_result['stages'])
            result['stop_reason'] = load_result['stop_reason']
            if process.returncode:
                raise Abort('负载执行器异常：' + str(load_result['stop_reason']))
            observation.diagnostics.trigger('stage_end')
            capture_logs(output / 'logs', __import__('datetime').datetime.fromtimestamp(start, __import__('datetime').timezone.utc).isoformat())
            result['recovery'] = recovery(observation, self.preparation, self.config, self.expected, self.run.stopped)
            if self.run.stopped():
                raise Abort('manual_stop', 130)
            if not result['recovery']['passed']:
                raise Abort('恢复窗口内未恢复')
            checked = audit(output, self.preparation)
            result['audit'] = {'passed': checked['status'] == 'passed', 'file': 'audit.json', 'failures': checked['failures']}
            result['resources'] = {'passed': snapshot(self.config) == self.expected}
        except BaseException as error:
            failure = error
            result['stop_reason'] = str(error)
            if process is not None and process.poll() is None:
                process.send_signal(signal.SIGTERM)
                try:
                    process.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    self.run.processes.close()
        finally:
            if observation:
                close_error = observation.close()
                self.collection = None
                windows = result['load']['stages']
                window_results = []
                for stage in windows:
                    begin, finish = stage.get('begin_ms', 0) / 1000, stage.get('end_ms', 0) / 1000
                    phase = 'measurement'
                    if finish <= begin:
                        begin, finish = stage.get('scenario_started_ms', 0) / 1000, stage.get('observed_end_ms', 0) / 1000
                        phase = 'warmup'
                    if finish > begin > 0:
                        try:
                            value = summarize(output, begin, finish)
                            target = output / 'diagnostics' / (stage['name'] + '.json')
                            (output / 'diagnostics-summary.json').replace(target)
                            value['file'] = str(target.relative_to(output))
                            value['window']['phase'] = phase
                            window_results.append(value)
                        except Exception as error:
                            window_results.append({'status': 'incomplete', 'reasons': [type(error).__name__]})
                result['evidence'] = {'passed': bool(window_results) and not close_error and all(v['status'] == 'complete' for v in window_results),
                                      'windows': window_results, 'collection_error': str(close_error) if close_error else None}
            result['finished_at'] = utc()
            write_json(output / 'result.json', result)
        if failure:
            raise failure
        return result

    def refine(self, low, high):
        return json.loads(command([str(self.preparation / 'bin/pt-load'), '--low', str(low), '--high', str(high)]))

    def routes(self):
        return routes()


def execute(config):
    run = Run(config)
    pipeline = None
    backend = None
    code, reason = 3, None
    previous_handlers = {}
    def interrupted(signum, frame):
        raise KeyboardInterrupt()
    for sig in (signal.SIGINT, signal.SIGTERM):
        previous_handlers[sig] = signal.signal(sig, interrupted)
    try:
        run.update('checking', stage='environment')
        if not config.get('acceptance'):
            from .acceptance import check
            check(config)
        value = preflight(config)
        from .__main__ import plan
        write_json(run.directory / 'plan.json', plan(config))
        write_json(run.directory / 'environment.json', value['environment'])
        write_json(run.directory / 'coverage.json', registry())
        backend = Backend(run, config, value)
        run.update(stage='data')
        backend.restore()
        baseline.smoke(backend.preparation)
        backend.expected = snapshot(config)
        backend.restore()
        value.update(ready=False, last_run_id=run.id)
        write_json(STATE / 'prepared.json', value)
        run.update('running', stage='observation')
        pipeline = Pipeline(config, backend)
        code = pipeline.run()
    except KeyboardInterrupt:
        code, reason = 130, 'manual_stop'
        if run.state['status'] in ('checking', 'running'):
            run.update('stopping')
    except Abort as error:
        code, reason = error.code, str(error)
    except (ValueError, FileNotFoundError, AssertionError) as error:
        code, reason = (2 if run.state['status'] == 'checking' else 3), str(error)
    except Exception as error:
        code, reason = 3, f'{type(error).__name__}: {error}'
    finally:
        for sig, handler in previous_handlers.items():
            signal.signal(sig, handler)
        if backend and backend.collection:
            backend.collection.close()
        write_json(run.directory / 'outcome.json', {'exit_code': code, 'reason': reason,
                   'capacity': pipeline.capacity if pipeline else {}, 'skipped': pipeline.skipped if pipeline else [],
                   'pipeline_completed': code in (0, 1) and reason is None})
        run.finish(code, reason)
        run.update(execution_completed=code in (0, 1) and reason is None)
        from .report import report
        summary = report(run.directory, recompute=False)
        if config.get('acceptance'):
            from .acceptance import record
            record(run, config, summary)
    print(f'运行 {run.id} 已收尾，退出码 {code}；报告：{run.directory / "report.md"}')
    return code
