"""保存 exporter 输出，并执行可用性、环境和恢复所需的检查。"""
import argparse
from collections import defaultdict
from datetime import datetime, timezone
import json
import math
from pathlib import Path
import signal
import subprocess
import threading
import time
from environment import ROOT, STATE, environment
from exporters import INTERVAL, private_json, scalar, scrape_all, targets

GIB = 1024**3
LIMITS = {'host_available_min_bytes': GIB, 'disk_free_min_bytes': 20 * GIB,
          'connections_max': 131, 'backlog_max': 9000, 'probe_gap_max_seconds': 2.5,
          'state_gap_max_seconds': 8, 'database_failure_seconds': 30, 'load_rate_min_ratio': .95}


def utc():
    return datetime.now(timezone.utc).isoformat()


def percentile(values, fraction):
    if not values:
        return None
    values = sorted(values)
    index = (len(values) - 1) * fraction
    low, high = math.floor(index), math.ceil(index)
    return values[low] + (values[high] - values[low]) * (index - low)


class Observation:
    def __init__(self, directory):
        self.directory = directory
        directory.mkdir(parents=True, exist_ok=True)
        self.env = environment()
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.children = []
        self.exporter_pids = []
        self.last_probe = self.last_state = self.last_metrics = 0
        self.failed_since = self.reason = None
        self.connections = self.backlog = 0
        self.initial = {}
        self.started = self.telemetry_heartbeat = time.time()
        self.metrics_healthy = False
        private_json(directory / 'limits.json', LIMITS)

    def fail(self, reason):
        with self.lock:
            if self.reason is None:
                self.reason = reason
                with (self.directory / 'stop-events.jsonl').open('a') as output:
                    output.write(json.dumps({'timestamp': utc(), 'reason': reason}) + '\n')

    def probe_reader(self, details=False):
        name = 'database-state' if details else 'database-probe'
        command = [str(STATE / 'bin/dbprobe'), '--dsn-file', str(STATE / 'probe-dsn')]
        if details:
            command.append('--details')
        with (self.directory / (name + '-stderr.log')).open('a') as errors:
            process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=errors, text=True, bufsize=1)
            self.children.append(process)
            try:
                with (self.directory / (name + '.jsonl')).open('a') as output:
                    for line in process.stdout:
                        output.write(line)
                        output.flush()
                        self.database_sample(json.loads(line), details)
            except (ValueError, KeyError, TypeError):
                self.fail(name + '_invalid_sample')
            finally:
                process.stdout.close()
        if not self.stop.is_set():
            self.fail(name + '_process_exited')

    def database_sample(self, row, details):
        sample = datetime.fromisoformat(row['timestamp'].replace('Z', '+00:00')).timestamp()
        if details:
            if not row.get('ok') or any(row.get(name + '_error') for name in ('identity', 'environment', 'migration', 'backlog')):
                return
            identity, current, migration, backlog = [row.get(name, []) for name in ('identity', 'environment', 'migration', 'backlog')]
            if not all((identity, current, migration, backlog)):
                return
            if identity[0]['database_name'] != self.env.get('MYSQL_DATABASE'):
                return self.fail('database_identity_changed')
            uuid = identity[0]['server_uuid']
            if self.initial.setdefault('mysql_uuid', uuid) != uuid:
                return self.fail('database_identity_changed')
            if current[0]['status'] != 'ready' or current[0]['mode'] != 'v2' or str(migration[0]['dirty']) != '0':
                return self.fail('business_state_or_schema_changed')
            epoch = current[0]['data_epoch']
            if self.initial.setdefault('data_epoch', epoch) != epoch:
                return self.fail('business_data_epoch_changed')
            version = migration[0]['version']
            if self.initial.setdefault('migration_version', version) != version:
                return self.fail('business_state_or_schema_changed')
            self.backlog = sum(int(item['open_requirements']) for item in backlog)
            if self.backlog >= LIMITS['backlog_max']:
                self.fail('backlog_stop_threshold')
            private_json(self.directory / 'database-state-latest.json', row)
            self.last_state = sample
        else:
            if self.last_probe and sample - self.last_probe > LIMITS['probe_gap_max_seconds']:
                self.fail('database_probe_sampling_gap')
            self.last_probe = sample
            if row.get('ok'):
                self.failed_since = None
            elif self.failed_since is None:
                self.failed_since = sample
            elif sample - self.failed_since >= LIMITS['database_failure_seconds']:
                self.fail('database_continuously_unavailable_30s')

    def start_exporters(self):
        manifest = json.loads((STATE / 'exporters.json').read_text())
        for name, item in manifest.items():
            with (self.directory / (name + '-exporter.log')).open('ab') as log:
                process = subprocess.Popen(item['command'], cwd=ROOT, stdout=log, stderr=log)
            self.children.append(process)
            path = STATE / 'logs' / (name + '-exporter.pid')
            path.write_text(str(process.pid))
            self.exporter_pids.append(path)

    def check_metrics(self, collected):
        mysql = collected['mysql']['parsed']
        self.connections = int(scalar(mysql, 'gopulse_mysql_connections'))
        if self.connections >= LIMITS['connections_max']:
            self.fail('connection_stop_threshold')
        node = collected['node']['parsed']
        if scalar(node, 'node_memory_MemAvailable_bytes') < LIMITS['host_available_min_bytes']:
            self.fail('host_memory_stop_threshold')
        filesystems = [item for item in node.values() if item['name'] == 'node_filesystem_avail_bytes'
                       and ROOT.is_relative_to(item['labels'].get('mountpoint', '/nonexistent'))]
        if not filesystems:
            raise ValueError('node_exporter 未提供工作区文件系统指标')
        disk = max(filesystems, key=lambda item: len(item['labels']['mountpoint']))
        if disk['value'] < LIMITS['disk_free_min_bytes']:
            self.fail('disk_stop_threshold')
        oom = scalar(node, 'node_vmstat_oom_kill')
        if self.initial.setdefault('oom', oom) != oom:
            self.fail('host_oom_event')

    def metrics_reader(self):
        try:
            config = targets(self.env)
            with (self.directory / 'exporter-metrics.jsonl').open('a') as output:
                while not self.stop.is_set():
                    began = time.monotonic()
                    collected, errors = scrape_all(config)
                    row = {'timestamp': utc(), 'metrics': {name: item['body'] for name, item in collected.items()},
                           'sampled_at': {name: item['timestamp'] for name, item in collected.items()}, 'errors': errors}
                    output.write(json.dumps(row) + '\n')
                    output.flush()
                    if not errors:
                        self.check_metrics(collected)
                        self.last_metrics = time.time()
                        self.metrics_healthy = True
                    else:
                        self.metrics_healthy = False
                        if self.last_metrics or time.time() - self.started > 10:
                            self.fail('exporter_or_application_observation_failed')
                    self.stop.wait(max(0, INTERVAL - (time.monotonic() - began)))
        except Exception:
            self.fail('exporter_observation_invalid')

    def telemetry(self):
        handles={};buffers={};clock_start=None;counts=defaultdict(int);checked=set();last_flush=time.time()
        output=(self.directory/'requests-per-second.jsonl').open('a')
        while not self.stop.wait(.05):
            self.telemetry_heartbeat=time.time()
            active_path=STATE/'active-run.json'
            if active_path.exists():
                active=json.loads(active_path.read_text());stages=active['stages']
            else:stages=[]
            for path in (STATE/'reports').glob('*-samples.json'):
                if path.stat().st_mtime<self.started:continue
                if path not in handles:handles[path]=path.open();buffers[path]=''
                handle=handles[path]
                for _ in range(200000):
                    offset=handle.tell();line=handle.readline()
                    if not line:break
                    if not line.endswith('\n'):handle.seek(offset);break
                    row=json.loads(line)
                    if row.get('type')!='Point':continue
                    data=row['data'];metric=row['metric'];tags=data.get('tags',{})
                    stamp=datetime.fromisoformat(data['time'].replace('Z','+00:00')).timestamp()
                    if metric=='stage_clock_seconds':
                        clock_start=stamp-data['value']
                        boundary=0
                        for index,stage in enumerate(stages):
                            boundary+=stage['warmup_seconds']+stage['measurement_seconds']
                            if data['value']>=boundary+6 and index not in checked:
                                checked.add(index)
                                rate=counts[index]/stage['measurement_seconds']
                                if rate<stage['target_rps']*LIMITS['load_rate_min_ratio']:self.fail('actual_rate_below_95_percent_requires_generator_diagnosis')
                        continue
                    if metric=='corpus_exhausted':self.fail('corpus_exhausted');continue
                    if metric=='dropped_iterations' and clock_start is not None:
                        elapsed=stamp-clock_start;offset=0
                        for index,stage in enumerate(stages):
                            end=offset+stage['warmup_seconds']+stage['measurement_seconds']
                            if offset<=elapsed<end:
                                tags=dict(tags,stage=str(index),phase='warmup' if elapsed<offset+stage['warmup_seconds'] else 'measurement');break
                            offset=end
                    if metric not in ('business_requests','business_success_rate','business_latency','business_success_latency','errors_429','errors_503','errors_timeout','errors_transport','errors_unexpected','dropped_iterations'):continue
                    second=int(stamp);key=(second,tags.get('stage',''),tags.get('phase',''))
                    if key not in buffers:buffers[key]={'timestamp':datetime.fromtimestamp(second,timezone.utc).isoformat(),'stage':key[1],'phase':key[2],'requests':0,'successful':0,'outcomes':0,'target_rps':stages[int(tags['stage'])]['target_rps'] if tags.get('stage','').isdigit() and int(tags['stage'])<len(stages) else None,'latency':[],'successful_latency':[],'dropped_iterations':0,'errors':{}}
                    bucket=buffers[key]
                    if metric=='business_requests':
                        bucket['requests']+=data['value']
                        if tags.get('phase')=='measurement' and tags.get('stage','').isdigit():counts[int(tags['stage'])]+=data['value']
                    elif metric=='business_success_rate':bucket['successful']+=data['value'];bucket['outcomes']+=1
                    elif metric=='business_latency':bucket['latency'].append(data['value'])
                    elif metric=='business_success_latency':bucket['successful_latency'].append(data['value'])
                    elif metric=='dropped_iterations':bucket['dropped_iterations']+=data['value']
                    else:bucket['errors'][metric]=bucket['errors'].get(metric,0)+data['value']
            if time.time()-last_flush>=1:
                for key in list(buffers):
                    if not isinstance(key,tuple) or key[0]>time.time()-2:continue
                    bucket=buffers.pop(key)
                    for name in ['latency','successful_latency']:
                        samples=bucket.pop(name)
                        bucket[name+'_p95_ms']=percentile(samples,.95);bucket[name+'_p99_ms']=percentile(samples,.99)
                    bucket['error_rate']=1-bucket['successful']/bucket['outcomes'] if bucket['outcomes'] else None
                    bucket['actual_completed_rps']=bucket['requests']
                    bucket['effective_success_throughput']=bucket['successful']
                    output.write(json.dumps(bucket)+'\n')
                output.flush();last_flush=time.time()
        for handle in handles.values():handle.close()
        output.close()


    def run(self):
        threads = []
        try:
            self.start_exporters()
            for target, args in [(self.probe_reader, (False,)), (self.probe_reader, (True,)),
                                 (self.metrics_reader, ()), (self.telemetry, ())]:
                thread = threading.Thread(target=target, args=args, daemon=True)
                thread.start()
                threads.append(thread)
            while not self.stop.is_set():
                if (STATE / 'STOP').exists():
                    self.fail('manual_stop_requested')
                now = time.time()
                if now - self.started > 3 and (not self.last_probe or now - self.last_probe > LIMITS['probe_gap_max_seconds']):
                    self.fail('database_probe_stale')
                if now - self.started > 8 and (not self.last_state or now - self.last_state > LIMITS['state_gap_max_seconds']):
                    self.fail('database_state_stale')
                if now - self.started > 10 and (not self.last_metrics or now - self.last_metrics > INTERVAL * 2):
                    self.fail('exporter_metrics_stale')
                if any(child.poll() is not None for child in self.children):
                    self.fail('observation_process_exited')
                if now - self.telemetry_heartbeat > 3:
                    self.fail('workload_telemetry_stale')
                status = 'stop' if self.reason else 'ready' if self.last_probe and self.last_state and self.metrics_healthy else 'starting'
                private_json(STATE / 'guard.json', {'timestamp': utc(), 'status': status, 'reason': self.reason,
                                                  'directory': str(self.directory)})
                self.stop.wait(.5)
        finally:
            self.stop.set()
            for child in self.children:
                if child.poll() is None:
                    child.terminate()
            for child in self.children:
                try:
                    child.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()
            for thread in threads:
                thread.join(timeout=4)
            for path in self.exporter_pids:
                path.unlink(missing_ok=True)
            private_json(STATE / 'guard.json', {'timestamp': utc(), 'status': 'stop', 'reason': self.reason or 'observer_stopped',
                                              'directory': str(self.directory)})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, default=STATE / 'observations' / datetime.now().strftime('%Y%m%dT%H%M%S'))
    args = parser.parse_args()
    observer = Observation(args.directory)
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: observer.stop.set())
    observer.run()
