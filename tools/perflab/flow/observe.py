"""检查压测期间的环境身份、业务积压和自然恢复。"""
import time
from pathlib import Path

from .common import budget, drained, http
from .runtime import write_json, append
from .environment import snapshot


class Observation:
    def __init__(self, preparation, directory, config, expected=None):
        self.directory, self.config, self.expected = Path(directory), config, expected
        self.clock = (time.time(), time.monotonic())
        self.checked = None
        self.checked_at = 0

    def start(self):
        self.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.check()
        return self

    def check(self):
        current = (time.time(), time.monotonic())
        wall, monotonic = current[0] - self.clock[0], current[1] - self.clock[1]
        if abs(wall - monotonic) > .1:
            write_json(self.directory / 'clock-error.json', {'reason': 'host_clock_discontinuity'})
            raise RuntimeError('系统时间跳变，无法继续使用当前统计窗口')
        self.clock = current

    def safety(self, path):
        self.check()
        if self.checked is not None and time.monotonic() - self.checked_at < self.config['check_seconds']:
            return self.checked
        environment = snapshot(self.config)
        if self.expected is not None and environment != self.expected:
            raise RuntimeError('压测期间环境身份或配置改变')
        value = budget()
        epoch = time.time()
        from datetime import datetime, timezone
        write_json(path, {'timestamp': datetime.fromtimestamp(epoch, timezone.utc).isoformat(),
                          'status': 'ready', 'backlog': value['open_requirements']})
        self.checked = {'epoch': epoch, 'environment': environment}
        self.checked_at = time.monotonic()
        return self.checked

    def close(self):
        return None


def recovery(observation, preparation, config, expected, stopped=lambda: False):
    started = time.time()
    deadline = time.monotonic() + config['recovery_seconds']
    passed, reason = False, 'recovery_timeout'
    while time.monotonic() < deadline:
        if stopped():
            reason = 'manual_stop'
            break
        observation.check()
        if snapshot(config) != expected:
            raise RuntimeError('恢复期环境身份或配置改变')
        try:
            drained(timeout=0)
            passed = http('http://127.0.0.1:18086', 'GET', '/ready')[0] == 200
            append(observation.directory / 'recovery-probes.jsonl', {'at': time.time(), 'passed': passed})
        except (ValueError, OSError, RuntimeError, AssertionError):
            passed = False
        if passed:
            reason = None
            break
        time.sleep(.5)
    result = {'passed': passed, 'reason': reason, 'started_at_epoch': started, 'ended_at_epoch': time.time(),
              'method': 'natural_recovery', 'pressure_requests': 0}
    write_json(observation.directory / 'recovery.json', result)
    return result
