"""管理公共采集、自然恢复和低频独立探针。"""
from datetime import datetime, timezone
import json
import math
from pathlib import Path
import secrets
import time

from .common import ROOT, PROJECT, command, http, metrics, sql
from .runtime import utc, write_json, append
from .diagnose import Diagnostics, capture_logs
from .sample import Sampler
from .diagnostics_registry import registry
from .environment import snapshot
from diagnostics.budget import parse_budget


class Observation:
    def __init__(self, preparation, directory, config, expected=None):
        self.preparation, self.directory, self.config = Path(preparation), Path(directory), config
        self.expected = expected
        self.diagnostics = self.sampler = None
        self.started = time.time()

    def start(self):
        self.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        try:
            self.diagnostics = Diagnostics(self.preparation, self.directory).start()
            self.sampler = Sampler(self.directory / 'resources.jsonl', self.diagnostics,
                                   self.expected)
            self.sampler.start()
            deadline = time.monotonic() + 10
            while not self.sampler.samples:
                self.check()
                if time.monotonic() > deadline:
                    raise RuntimeError('首次资源采样超时')
                time.sleep(.05)
            self.started = time.time()
            return self
        except BaseException:
            self.close()
            raise

    def check(self):
        error = (self.sampler.error if self.sampler else None) or (self.diagnostics.error if self.diagnostics else None)
        if error:
            raise RuntimeError('必需观测失败：' + str(error))
        if self.sampler and self.sampler.samples:
            path = self.directory / 'resources.jsonl'
            if time.time() - path.stat().st_mtime > 12:
                raise RuntimeError('资源采样过期')

    def latest(self):
        if not self.sampler or self.sampler.latest_sample is None:
            raise RuntimeError('资源样本缺失')
        return self.sampler.latest_sample

    def safety(self, path):
        self.check()
        row = self.latest()
        write_json(path, {'timestamp': row['timestamp'], 'status': 'ready',
                          'backlog': row['async_budget']['open_requirements']})
        return row

    def close(self):
        error = None
        try:
            if self.sampler:
                self.sampler.close()
        except Exception as caught:
            error = caught
        finally:
            if self.diagnostics:
                self.diagnostics.close()
                error = error or self.diagnostics.error
            capture_logs(self.directory, datetime.fromtimestamp(self.started, timezone.utc).isoformat())
        if error:
            write_json(self.directory / 'collection-error.json', {'error': str(error)})
        return error


def probe(preparation, not_before=None):
    """独立诊断账号最多 4 连接：dbdiag 2、exporter 1、探针 1。"""
    preparation = Path(preparation)
    result = json.loads(command([str(preparation / 'bin/dbprobe'), '--once', '--dsn-file',
                                 str(preparation / 'diagnostic.dsn')], timeout=4))
    result['role'] = 'recovery_probe'
    result['business'] = http('http://127.0.0.1:18086', 'GET', '/ready')[0] == 200
    result['budget'] = parse_budget(metrics('backend'), not_before=not_before if not_before is not None else int(time.time()) - 10)
    return result


def recovered(value, sample, not_before=None):
    budget = value['budget']
    if not_before is not None and (budget.get('sampled_at', 0) < not_before or sample.get('epoch', 0) < not_before):
        return False
    return bool(value['ok'] and value['business'] and budget['open_requirements'] == 0 and budget['parked_messages'] == 0
                and sample['rabbitmq']['messages_ready'] == 0 and sample['rabbitmq']['messages_unacknowledged'] == 0)


def recovery(observation, preparation, config, expected, stopped=lambda: False):
    started = time.time()
    deadline = time.monotonic() + config['recovery_seconds']
    passed = False
    reason = 'recovery_timeout'
    while time.monotonic() < deadline:
        if stopped():
            reason = 'manual_stop'
            break
        observation.check()
        if snapshot(config) != expected:
            raise RuntimeError('恢复期环境身份或配置改变')
        try:
            value = probe(preparation, math.ceil(started))
            append(observation.directory / 'recovery-probes.jsonl', value)
            passed = recovered(value, observation.latest(), math.ceil(started))
        except (ValueError, OSError, RuntimeError):
            passed = False
        if passed:
            reason = None
            break
        for _ in range(50):
            if stopped():
                break
            observation.check()
            time.sleep(.1)
    result = {'passed': passed, 'reason': reason, 'started_at_epoch': started, 'ended_at_epoch': time.time(),
              'method': 'natural_recovery', 'pressure_requests': 0}
    write_json(observation.directory / 'recovery.json', result)
    return result


def precheck(preparation):
    from .config import load
    output = Path(preparation) / ('precheck-' + secrets.token_hex(4))
    observation = Observation(preparation, output, load()).start()
    try:
        deadline = time.monotonic() + 11
        while time.monotonic() < deadline:
            observation.check()
            time.sleep(.1)
        value = probe(preparation, math.ceil(observation.started))
        if not recovered(value, observation.latest(), math.ceil(observation.started)):
            raise ValueError('独立探针或初始积压未就绪')
        write_json(output / 'coverage.json', registry())
    finally:
        error = observation.close()
    if error:
        raise ValueError('观测预检收尾失败：' + str(error))
