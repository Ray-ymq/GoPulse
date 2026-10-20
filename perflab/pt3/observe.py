"""持续采集测试三证据，并发布有时效的停压状态；本程序不启动业务负载。"""
import argparse
from collections import defaultdict
from datetime import datetime, timezone
import json
import math
import os
from pathlib import Path
import shutil
import signal
import subprocess
import threading
import time
import urllib.request
from environment import ROOT, STATE, CONTAINERS, environment, service_binary

GIB=1024**3
LIMITS={'host_available_min_bytes':GIB,'disk_free_min_bytes':20*GIB,'mysql_memory_max_bytes':int(1.8*GIB),
        'k6_rss_max_bytes':3*GIB,'connections_max':131,'backlog_max':9000,'probe_gap_max_seconds':2.5,
        'state_gap_max_seconds':8,'database_failure_seconds':30,'load_rate_min_ratio':.95}

def utc():return datetime.now(timezone.utc).isoformat()

def configuration_matches(actual, expected):
    return all(key in actual and (configuration_matches(actual[key], value) if isinstance(value, dict) else actual[key] == value)
               for key, value in expected.items())

def cgroup_memory_usage(cgroup):
    current = int((cgroup / 'memory.current').read_text())
    stats = dict(line.split() for line in (cgroup / 'memory.stat').read_text().splitlines())
    reclaimable_file = int(stats.get('inactive_file', '0'))
    return current, max(0, current - reclaimable_file), reclaimable_file

def percentile(values, fraction):
    if not values:return None
    values=sorted(values)
    index=(len(values)-1)*fraction
    low=math.floor(index);high=math.ceil(index)
    return values[low]+(values[high]-values[low])*(index-low)

class Observation:
    def __init__(self, directory):
        self.directory=directory;directory.mkdir(parents=True,exist_ok=True)
        self.env=environment();self.lock=threading.Lock();self.stop=threading.Event();self.children=[]
        self.last_probe=0;self.last_state=0;self.failed_since=None;self.reason=None
        self.connections=0;self.backlog=0;self.initial={};self.started=time.time();self.previous_cpu={};self.telemetry_heartbeat=time.time()
        self.expected_mysql={'Memory':2*GIB,'NanoCpus':2000000000,'MemorySwap':2*GIB,'RestartPolicy':{'Name':'no'}}
        self.opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
        (directory/'limits.json').write_text(json.dumps(LIMITS,indent=2)+'\n')

    def fail(self,reason):
        with self.lock:
            if self.reason is None:
                self.reason=reason
                with (self.directory/'stop-events.jsonl').open('a') as out:out.write(json.dumps({'timestamp':utc(),'reason':reason})+'\n')

    def probe_reader(self,details=False):
        name='database-state' if details else 'database-probe'
        command=[str(STATE/'bin/dbprobe'),'--dsn-file',str(STATE/'probe-dsn')]
        if details:command+=['--details']
        with (self.directory/(name+'-stderr.log')).open('a') as errors:
            process=subprocess.Popen(command,stdout=subprocess.PIPE,stderr=errors,text=True,bufsize=1)
            self.children.append(process)
            with (self.directory/(name+'.jsonl')).open('a') as out:
                for line in process.stdout:
                    out.write(line);out.flush()
                    try:row=json.loads(line)
                    except ValueError:self.fail(name+'_invalid_json');continue
                    self.database_sample(row,details)
        if not self.stop.is_set():self.fail(name+'_process_exited')

    def database_sample(self,row,details):
        sample=datetime.fromisoformat(row['timestamp'].replace('Z','+00:00')).timestamp()
        if details:
            self.last_state=time.time()
            if any(key.endswith('_error') for key in row):self.fail('database_state_incomplete')
            status={r['Variable_name']:r['Value'] for r in row.get('status',[])}
            self.connections=int(status.get('Threads_connected',0))
            environment=row.get('environment',[])
            migration=row.get('migration',[])
            if not environment or environment[0]['status']!='ready' or environment[0]['mode']!='v2' or not migration or str(migration[0]['dirty'])!='0':self.fail('business_state_or_schema_changed')
            if environment:
                epoch=environment[0]['data_epoch']
                if 'data_epoch' not in self.initial:self.initial['data_epoch']=epoch
                elif self.initial['data_epoch']!=epoch:self.fail('business_data_epoch_changed')
            if self.connections>=LIMITS['connections_max']:self.fail('connection_stop_threshold')
            if row.get('backlog'):
                self.backlog=int(row['backlog'][0]['open_requirements'])
                if self.backlog>=LIMITS['backlog_max']:self.fail('backlog_stop_threshold')
        else:
            if self.last_probe and sample-self.last_probe>LIMITS['probe_gap_max_seconds']:self.fail('database_probe_sampling_gap')
            self.last_probe=sample
            if row.get('ok'):self.failed_since=None
            elif self.failed_since is None:self.failed_since=sample
            elif sample-self.failed_since>=LIMITS['database_failure_seconds']:self.fail('database_continuously_unavailable_30s')

    def capture(self,filename,command):
        log=(self.directory/filename).open('ab')
        process=subprocess.Popen(command,stdout=log,stderr=log,start_new_session=True)
        self.children.append(process)

    def resources(self):
        values={}
        for line in Path('/proc/meminfo').read_text().splitlines():
            if line.startswith('MemAvailable:'):values['host_available_bytes']=int(line.split()[1])*1024
        for line in Path('/proc/vmstat').read_text().splitlines():
            if line.startswith('oom_kill '):values['host_oom_kill']=int(line.split()[1])
        if 'oom' not in self.initial:self.initial['oom']=values['host_oom_kill']
        elif values['host_oom_kill']!=self.initial['oom']:self.fail('host_oom_event')
        values['disk_free_bytes']=shutil.disk_usage(ROOT).free
        values['network']=Path('/proc/net/dev').read_text()
        if values['host_available_bytes']<LIMITS['host_available_min_bytes']:self.fail('host_memory_stop_threshold')
        if values['disk_free_bytes']<LIMITS['disk_free_min_bytes']:self.fail('disk_stop_threshold')
        values['processes']={}
        for name in ['server','worker','indexer']:
            path=STATE/'logs'/(name+'.pid')
            if not path.exists():self.fail(name+'_pid_missing');continue
            pid=int(path.read_text())
            try:
                if str(service_binary(name)).encode() not in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0'):self.fail(name+'_process_identity_changed')
                stat=Path(f'/proc/{pid}/stat').read_text().rsplit(') ',1)[1].split()
                status=Path(f'/proc/{pid}/status').read_text()
                rss=next(int(line.split()[1])*1024 for line in status.splitlines() if line.startswith('VmRSS:'))
                values['processes'][name]={'pid':pid,'rss_bytes':rss,'cpu_seconds':(int(stat[11])+int(stat[12]))/os.sysconf('SC_CLK_TCK')}
                if stat[0]=='Z':self.fail(name+'_exited')
            except (OSError,StopIteration):self.fail(name+'_exited')
        # k6 子进程只按本仓库的固定二进制路径匹配，避免采入其他任务。
        for path in Path('/proc').glob('[0-9]*/cmdline'):
            try:
                args=path.read_bytes().split(b'\0')
                if not args or args[0].decode(errors='replace')!=str(ROOT/'.run/tools/k6/0.56.0/linux-amd64/k6') or b'run' not in args:continue
                pid=int(path.parent.name);status=(path.parent/'status').read_text()
                rss=next(int(line.split()[1])*1024 for line in status.splitlines() if line.startswith('VmRSS:'))
                stat=(path.parent/'stat').read_text().rsplit(') ',1)[1].split()
                values['processes']['k6']={'pid':pid,'rss_bytes':rss,'cpu_seconds':(int(stat[11])+int(stat[12]))/os.sysconf('SC_CLK_TCK')}
                if rss>=LIMITS['k6_rss_max_bytes']:self.fail('load_generator_memory_stop_threshold')
            except (OSError,StopIteration):continue
        raw=subprocess.check_output(['docker','inspect','--format','{{json .State}}\t{{.Id}}\t{{.RestartCount}}\t{{.Name}}\t{{json .HostConfig}}',*CONTAINERS],text=True,timeout=2)
        values['containers']={}
        for line in raw.splitlines():
            state,cid,restarts,name,host_config=line.split('\t');state=json.loads(state);host_config=json.loads(host_config)
            evidence={key:state[key] for key in ['Status','OOMKilled','ExitCode','StartedAt','FinishedAt']};evidence['restarts']=int(restarts)
            if name.endswith('mysql-1') and not configuration_matches(host_config,self.expected_mysql):self.fail('mysql_resource_configuration_changed')
            cgroup=Path('/sys/fs/cgroup/system.slice/docker-'+cid+'.scope')
            current, pressure, reclaimable_file = cgroup_memory_usage(cgroup)
            evidence['memory_bytes'] = current
            evidence['memory_pressure_bytes'] = pressure
            evidence['memory_reclaimable_file_bytes'] = reclaimable_file
            evidence['cpu']=dict(row.split() for row in (cgroup/'cpu.stat').read_text().splitlines())
            values['containers'][name]=evidence
            identity=(evidence['StartedAt'],int(restarts))
            if name not in self.initial:self.initial[name]=identity
            if identity!=self.initial[name] or evidence['Status']!='running' or evidence['OOMKilled']:self.fail(name+'_crash_oom_or_restart')
            if name.endswith('mysql-1') and evidence['memory_pressure_bytes']>=LIMITS['mysql_memory_max_bytes']:self.fail('mysql_memory_stop_threshold')
        now=time.monotonic()
        for name,row in values['processes'].items():
            previous=self.previous_cpu.get(name)
            row['cpu_percent']=0 if previous is None else (row['cpu_seconds']-previous[1])/(now-previous[0])*100
            self.previous_cpu[name]=(now,row['cpu_seconds'])
        for name,row in values['containers'].items():
            cpu=int(row['cpu']['usage_usec'])/1e6;previous=self.previous_cpu.get(name)
            row['cpu_percent']=0 if previous is None else (cpu-previous[1])/(now-previous[0])*100
            self.previous_cpu[name]=(now,cpu)
        return values

    def metrics(self):
        rows={}
        for name,port,token in [('backend',19101,'BACKEND_METRICS_TOKEN'),('worker',19102,'BUSINESS_WORKER_METRICS_TOKEN'),('indexer',19103,'SEARCH_INDEXER_METRICS_TOKEN')]:
            request=urllib.request.Request(f'http://127.0.0.1:{port}/internal/v1/metrics',headers={'Authorization':'Bearer '+self.env[token]})
            with self.opener.open(request,timeout=.6) as response:rows[name]=response.read().decode()
        return rows

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
        for details in [False,True]:threading.Thread(target=self.probe_reader,args=(details,),daemon=True).start()
        threading.Thread(target=self.telemetry,daemon=True).start()
        self.capture('container-events.jsonl',['docker','events','--filter','label=com.docker.compose.project=gopulse-loadtest','--format','{{json .}}'])
        self.capture('mysql-error.log',['docker','logs','--follow','--timestamps','--since','1m',CONTAINERS[0]])
        self.capture('kernel.log',['dmesg','--follow','--time-format','iso'])
        with (self.directory/'samples.jsonl').open('a') as out:
            while not self.stop.is_set():
                begin=time.monotonic();sample={'timestamp':utc()}
                try:sample.update(self.resources());sample['metrics']=self.metrics()
                except Exception as error:sample['observation_error']=str(error);self.fail('resource_or_application_observation_failed')
                if (STATE/'STOP').exists():self.fail('manual_stop_requested')
                now=time.time()
                if now-self.started>3 and (not self.last_probe or now-self.last_probe>LIMITS['probe_gap_max_seconds']):self.fail('database_probe_stale')
                if now-self.started>8 and (not self.last_state or now-self.last_state>LIMITS['state_gap_max_seconds']):self.fail('database_state_stale')
                for child in self.children:
                    if child.poll() is not None:self.fail('evidence_capture_process_exited')
                if time.time()-self.telemetry_heartbeat>3:self.fail('workload_telemetry_stale')
                sample.update(backlog=self.backlog,connections=self.connections)
                out.write(json.dumps(sample)+'\n');out.flush()
                status='stop' if self.reason else 'ready' if self.last_probe and self.last_state else 'starting'
                value={'timestamp':utc(),'status':status,'reason':self.reason,'directory':str(self.directory)}
                temp=STATE/'guard.tmp';temp.write_text(json.dumps(value)+'\n');temp.replace(STATE/'guard.json')
                self.stop.wait(max(0,1-(time.monotonic()-begin)))
        for child in self.children:
            if child.poll() is None:child.terminate()
        for child in self.children:
            try:child.wait(timeout=3)
            except subprocess.TimeoutExpired:child.kill()
        self.fail('observer_stopped')
        (STATE/'guard.json').write_text(json.dumps({'timestamp':utc(),'status':'stop','reason':'observer_stopped'})+'\n')

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--directory',type=Path,default=STATE/'observations'/datetime.now().strftime('%Y%m%dT%H%M%S'))
    args=parser.parse_args();observer=Observation(args.directory)
    for sig in [signal.SIGINT,signal.SIGTERM]:signal.signal(sig,lambda *_:observer.stop.set())
    observer.run()
