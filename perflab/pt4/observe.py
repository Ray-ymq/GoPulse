"""测试四观测器；关键状态、可用性和诊断分别使用独立连接。"""
import argparse
import json
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time
from common import ROOT, STATE, atomic_json, parse_event, rows, legacy_module

LegacyObservation = legacy_module('observe').Observation


class Observation(LegacyObservation):
    def __init__(self, directory, diagnostics=True):
        super().__init__(directory)
        self.diagnostics = diagnostics
        self.run_id = directory.name
        run_path = directory / 'run.json'
        if run_path.exists():
            config = json.loads(run_path.read_text())
            self.expected_mysql = config.get('expected_mysql', self.expected_mysql)
        self.diagnostic_last = {name: time.monotonic() for name in ('digest', 'locks')}

    def database_sample(self, row, details):
        if not details:
            return super().database_sample(row, False)
        # 不完整的关键状态不刷新有效样本时刻；8 秒规则仍由父类执行。
        if not row.get('ok'):
            return
        converted = {'timestamp': row['timestamp']}
        for name in ('status', 'environment', 'migration', 'backlog'):
            converted[name] = rows(row, name)
        if any(not converted[name] for name in ('status', 'environment', 'migration', 'backlog')):
            return
        super().database_sample(converted, True)
        identity = rows(row, 'identity')
        if not identity:
            return self.fail('database_identity_missing')
        value = identity[0]['server_uuid']
        if self.initial.setdefault('mysql_uuid', value) != value:
            self.fail('database_identity_changed')
        atomic_json(self.directory / 'critical-latest.json', row)

    def probe_reader(self, details=False):
        stream = 'critical' if details else 'probe'
        self.stream_reader(stream)

    def stream_reader(self, stream):
        command = [str(STATE / 'bin' / ('dbprobe' if stream == 'probe' else 'dbdiag')),
                   '--dsn-file', str(STATE / 'probe-dsn')]
        if stream != 'probe':
            command += ['--stream', stream, '--run-id', self.run_id]
        with (self.directory / (stream + '-stderr.log')).open('a') as errors:
            process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=errors, text=True, bufsize=1)
            self.children.append(process)
            with (self.directory / (stream + '.jsonl')).open('a') as output:
                for line in process.stdout:
                    row = json.loads(line)
                    row['run_id'] = self.run_id
                    output.write(json.dumps(row) + '\n'); output.flush()
                    if stream in ('probe', 'critical'):
                        self.database_sample(row, stream == 'critical')
                    elif row.get('ok'):
                        self.diagnostic_last[stream] = time.monotonic()
        if not self.stop.is_set():
            self.fail(stream + '_collector_exited')

    def telemetry(self):
        handle = None
        try:
            while not self.stop.wait(.05):
                self.telemetry_heartbeat = time.time()
                if self.diagnostics:
                    for name, interval in (('digest', 10), ('locks', 5)):
                        if time.monotonic() - self.diagnostic_last[name] > interval * 3 + 2:
                            self.fail(name + '_diagnostic_evidence_missing')
                path = self.directory / 'events.log'
                if handle is None:
                    if not path.exists():
                        continue
                    handle = path.open()
                for _ in range(20000):
                    offset = handle.tell(); line = handle.readline()
                    if not line or not line.endswith('\n'):
                        handle.seek(offset); break
                    event = parse_event(line)
                    if event and event.get('type') == 'invalid_corpus':
                        self.fail('corpus_exhausted_or_invalid')
        except Exception:
            self.fail('workload_event_stream_invalid')
        finally:
            if handle:
                handle.close()

    def resources(self):
        values = super().resources()
        values['run_id'] = self.run_id
        for name, row in values['containers'].items():
            # 父类已核对容器身份；按同一容器获取补充 cgroup 原始计数。
            cid = subprocess.check_output(['docker', 'inspect', '--format', '{{.Id}}', name.lstrip('/')], text=True, timeout=2).strip()
            cgroup = Path('/sys/fs/cgroup/system.slice/docker-' + cid + '.scope')
            for filename in ('memory.stat', 'memory.events', 'io.stat', 'cpu.max'):
                row[filename] = (cgroup / filename).read_text()
        return values

    def run(self):
        pids = [str(int((STATE / 'logs' / (name + '.pid')).read_text())) for name in ('server', 'worker', 'indexer')]
        pids.append(subprocess.check_output(['docker', 'inspect', '--format', '{{.State.Pid}}', 'gopulse-loadtest-mysql-1'], text=True).strip())
        self.capture('pidstat.log', ['pidstat', '-H', '-h', '-u', '-r', '-d', '-p', ','.join(pids), '1'])
        self.capture('iostat.log', ['iostat', '-x', '-t', '-y', '1'])
        self.capture('vmstat.log', ['vmstat', '-w', '-t', '-y', '1'])
        if self.diagnostics:
            for stream in ('digest', 'locks'):
                threading.Thread(target=self.stream_reader, args=(stream,), daemon=True).start()
        return super().run()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--light', action='store_true', help='仅保留安全采样和基础资源，用于开销对照')
    args = parser.parse_args()
    observer = Observation(args.directory, not args.light)
    def snapshot(*_):
        for process in observer.children:
            if '--stream' in process.args and 'digest' in process.args and process.poll() is None:
                process.send_signal(signal.SIGUSR1)
    signal.signal(signal.SIGUSR1, snapshot)
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: observer.stop.set())
    observer.run()
