"""请求测试四停压，或在独立组结束后停止已核对身份的测试四进程。"""
import argparse
import fcntl
from pathlib import Path
import os
import signal
import time
from common import ROOT, STATE, legacy_module
from exporters import stop_exporters


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--request-stop', action='store_true', help='写入停压标记，保留自然恢复流程')
    args = parser.parse_args()
    if args.request_stop:
        (STATE / 'STOP').touch(mode=0o600)
        print('已请求停压；runner 将保留 600 秒自然恢复。')
    else:
        with (STATE / 'execution.lock').open('a') as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise SystemExit('独立组仍在执行；使用 --request-stop 并等待恢复结束。')
            path = STATE / 'logs/observer.pid'
            if path.exists():
                pid = int(path.read_text())
                command = Path(f'/proc/{pid}/cmdline')
                status = Path(f'/proc/{pid}/stat')
                zombie = status.exists() and status.read_text().split(') ', 1)[1].startswith('Z ')
                if command.exists() and not zombie:
                    if str(ROOT / 'perflab/pt4/observe.py').encode() not in command.read_bytes().split(b'\0'):
                        raise RuntimeError('observer PID 不属于测试四')
                    os.kill(pid, signal.SIGTERM)
                    for _ in range(100):
                        if not command.exists() or status.read_text().split(') ', 1)[1].startswith('Z '):
                            break
                        time.sleep(.1)
                    else:
                        raise RuntimeError('observer 未按时停止')
                path.unlink(missing_ok=True)
            stop_exporters()
            legacy_module('environment').stop_services()
            print('测试四进程已停止。')
