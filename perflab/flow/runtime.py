"""原子状态、环境锁和有身份约束的进程管理。"""
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import time

from .config import ROOT, contract

STATE = ROOT / '.run/perflab'


def utc():
    return datetime.now(timezone.utc).isoformat()


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temp = path.with_name(path.name + '.' + secrets.token_hex(4) + '.tmp')
    with temp.open('x') as stream:
        os.chmod(temp, 0o600)
        json.dump(value, stream, ensure_ascii=False, indent=2, allow_nan=False)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    temp.replace(path)


def append(path, value):
    with Path(path).open('a') as stream:
        stream.write(json.dumps(value, ensure_ascii=False, allow_nan=False) + '\n')
        stream.flush()


@contextmanager
def lock(root=STATE):
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (root / 'active.lock').open('a') as stream:
        try:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise ValueError('已有流程占用环境锁') from error
        yield


def identity(pid):
    try:
        return Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()[19]
    except (OSError, IndexError):
        return None


class Processes:
    def __init__(self, directory):
        self.directory = Path(directory)
        self.children = []

    def start(self, args, **kwargs):
        process = subprocess.Popen(args, start_new_session=True, **kwargs)
        self.track(process, own_session=True)
        return process

    def track(self, process, own_session=False):
        if process is None or any(child is process for child, _ in self.children):
            return
        item = {'pid': process.pid, 'start_ticks': identity(process.pid), 'own_session': own_session}
        self.children.append((process, item))
        write_json(self.directory / 'processes.json', [row for _, row in self.children])

    def close(self):
        for process, expected in reversed(self.children):
            if process.poll() is None and expected['start_ticks'] and identity(process.pid) == expected['start_ticks']:
                try:
                    if expected['own_session']:
                        os.killpg(process.pid, signal.SIGTERM)
                    else:
                        process.terminate()
                except ProcessLookupError:
                    continue
                try:
                    process.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    if identity(process.pid) == expected['start_ticks']:
                        if expected['own_session']:
                            os.killpg(process.pid, signal.SIGKILL)
                        else:
                            process.kill()
                    process.wait(timeout=5)


class Run:
    transitions = {'created': {'checking', 'finalizing'}, 'checking': {'running', 'stopping', 'finalizing'},
                   'running': {'stopping', 'finalizing'}, 'stopping': {'finalizing'}, 'finalizing': {'completed'}}

    def __init__(self, config, root=STATE):
        self.root = Path(root)
        previous = self.root / 'active.json'
        if previous.exists():
            old = json.loads(previous.read_text())
            old_state = json.loads((self.root / old['run_id'] / 'state.json').read_text())
            if old_state['status'] != 'completed':
                raise ValueError('发现未完成运行；先执行 make pt-stop 保存中断状态，不自动重放写入')
        self.id = time.strftime('%Y%m%dT%H%M%S') + '-' + secrets.token_hex(4)
        self.directory = self.root / self.id
        self.directory.mkdir(parents=True, mode=0o700)
        (self.directory / 'groups').mkdir(mode=0o700)
        self.state = {'run_id': self.id, 'status': 'created', 'stage': None, 'started_at': utc(),
                      'execution_completed': False, 'business_passed': None}
        self.processes = Processes(self.directory)
        write_json(self.directory / 'effective-config.json', config)
        write_json(self.directory / 'manifest.json', contract('manifest', run_id=self.id, started_at=utc()))
        self.update()
        write_json(self.root / 'active.json', {'run_id': self.id})

    def update(self, status=None, **fields):
        if status and status != self.state['status']:
            if status not in self.transitions.get(self.state['status'], set()):
                raise ValueError('执行状态转换无效')
            self.state['status'] = status
        self.state.update(fields, updated_at=utc())
        write_json(self.directory / 'state.json', self.state)
        append(self.directory / 'events.jsonl', dict(self.state))

    def stopped(self):
        return (self.directory / 'STOP').exists()

    def finish(self, code, reason=None):
        self.update('finalizing', exit_code=code, reason=reason)
        try:
            self.processes.close()
        finally:
            self.update('completed', execution_completed=code in (0, 1), finished_at=utc())


def active(root=STATE):
    pointer = json.loads((Path(root) / 'active.json').read_text())
    run_id = pointer['run_id']
    if Path(run_id).name != run_id or run_id in ('.', '..'):
        raise ValueError('运行编号无效')
    return Path(root) / run_id


def stop(root=STATE):
    directory = active(root)
    write_json(directory / 'STOP', {'requested_at': utc()})
    # 未完成运行只标记中断；不重放业务，不按进程名回收。
    try:
        with lock(Path(root)):
            processes = json.loads((directory / 'processes.json').read_text()) if (directory / 'processes.json').exists() else []
            for item in reversed(processes):
                if item['start_ticks'] and identity(item['pid']) == item['start_ticks']:
                    try:
                        if item.get('own_session'):
                            os.killpg(item['pid'], signal.SIGTERM)
                        else:
                            os.kill(item['pid'], signal.SIGTERM)
                    except ProcessLookupError:
                        pass
            cleanup_containers(directory)
            state = json.loads((directory / 'state.json').read_text())
            if state['status'] != 'completed':
                state.update(status='completed', execution_completed=False, exit_code=130,
                             reason='interrupted_run', finished_at=utc())
                write_json(directory / 'state.json', state)
                append(directory / 'events.jsonl', state)
    except ValueError as error:
        if str(error) != '已有流程占用环境锁':
            raise


def cleanup_containers(directory):
    tasks = list((Path(directory) / 'groups').glob('*/task.json'))
    if not tasks:
        return
    run_id = Path(directory).name
    output = subprocess.check_output(['docker', 'ps', '-aq', '--filter', 'label=io.gopulse.pt.run=' + run_id], text=True, timeout=5)
    for container_id in output.split():
        rows = json.loads(subprocess.check_output(['docker', 'inspect', container_id], text=True, timeout=5))
        labels = rows[0]['Config']['Labels']
        if (labels.get('io.gopulse.pt.run') == run_id and labels.get('com.docker.compose.project') == 'gopulse-pt-business'
                and labels.get('com.docker.compose.service') == 'k6'):
            subprocess.run(['docker', 'stop', '--timeout', '5', container_id], check=True, capture_output=True, timeout=10)
            subprocess.run(['docker', 'rm', '-f', container_id], check=True, capture_output=True, timeout=5)
