"""Bounded Phase 20 observer with persistent Kafka clients and timed probes.

kafka-python 2.2.15 is an acceptance-tool dependency, installed outside the repo.
No observer joins or commits offsets to the product consumer group.
"""
from __future__ import annotations
import base64
from collections import deque
import json
import os
import re
import socket
import threading
import time
import urllib.request
from pathlib import Path
from phase19_capacity import compose, require, command, ensure_owned_project
from phase19_sampler import parse_meminfo, parse_cpu_stat, parse_size, parse_ratio, metric_sum

_original_getaddrinfo=socket.getaddrinfo

def broker_address(address):
    # Per-process resolution of the one owned Compose broker. No host DNS/hosts
    # file is changed, and all clients are closed before the next stage starts.
    def resolve(host,*args,**kwargs):
        return _original_getaddrinfo(address if host=='kafka' else host,*args,**kwargs)
    socket.getaddrinfo=resolve

def service_address(project,env_file,files,service):
    cid=require(compose(project,env_file,files,'ps','-q',service,timeout=5),'resolve owned service').strip()
    if not cid or '\n' in cid:raise RuntimeError('non-unique owned service')
    obj=json.loads(require(command(['docker','inspect',cid],timeout=5),'inspect owned service'))[0]
    if obj['Config']['Labels']['com.docker.compose.project']!=project:raise RuntimeError('service ownership lost')
    networks=obj['NetworkSettings']['Networks']
    addresses=[n['IPAddress'] for n in networks.values() if n['IPAddress']]
    if not addresses:raise RuntimeError('owned service address missing')
    return addresses[0]

def json_http(url,headers=None,body=None,method=None,timeout=2):
    request=urllib.request.Request(url,body,headers or {},method=method)
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=timeout) as response:
        raw=response.read(8*1024*1024+1)
        if len(raw)>8*1024*1024:raise RuntimeError('probe response exceeds bound')
        return json.loads(raw)

def process_stats(pid):
    if not pid:return None
    try:
        raw=Path(f'/proc/{pid}/stat').read_text();fields=raw[raw.rindex(')')+2:].split()
        return {'cpu_ticks':int(fields[11])+int(fields[12]),'rss_bytes':int(fields[21])*os.sysconf('SC_PAGE_SIZE')}
    except (OSError,ValueError,IndexError):return None


def cgroup_stats(pid):
    """Read cgroup v2 counters for an owned container when the engine exposes them."""
    if not pid:
        return {}
    try:
        relative = next(line.split(':', 2)[2] for line in Path(f'/proc/{pid}/cgroup').read_text().splitlines() if line.startswith('0::'))
        root = Path('/sys/fs/cgroup') / relative.lstrip('/')
        result = {}
        for name, key in (('memory.current', 'memory_current_bytes'), ('memory.max', 'memory_limit_bytes')):
            value = (root / name).read_text().strip()
            if value != 'max':
                result[key] = int(value)
        cpu_stat = root / 'cpu.stat'
        if cpu_stat.is_file():
            values = dict(line.split() for line in cpu_stat.read_text().splitlines() if len(line.split()) == 2)
            if 'usage_usec' in values:
                result['cpu_usage_usec'] = int(values['usage_usec'])
            if 'throttled_usec' in values:
                result['cpu_throttled_usec'] = int(values['throttled_usec'])
            if 'nr_throttled' in values:
                result['cpu_throttled_count'] = int(values['nr_throttled'])
        return result
    except (OSError, StopIteration, ValueError, IndexError):
        return {}


def container_budget(item, current_bytes):
    """Return quota and memory facts without treating RSS as cgroup usage."""
    host = item.get('HostConfig', {}) or {}
    state = item.get('State', {}) or {}
    result = {
        'memory_current_bytes': int(current_bytes),
        'memory_limit_bytes': int(host.get('Memory') or 0) or None,
        'cpu_quota_cores': (int(host.get('NanoCpus') or 0) / 1_000_000_000) or None,
        'container_pid': int(state.get('Pid') or 0) or None,
    }
    result.update(cgroup_stats(result['container_pid']))
    # Docker stats is the portable current-memory signal.  When cgroup v2
    # exports a more direct current value, retain it as a separate fact.
    return result

def sampler_process_main(project, env_file, files, environment, profile, run_id, path, stop_event, load_pid=None):
    """Run the observer in a separate process so its CPU is measurable independently."""
    sampler = Sampler(project, env_file, files, environment, profile, run_id, path)
    sampler.set_load_pid(load_pid)
    sampler.start()
    while not stop_event.wait(0.2):
        if sampler.thread and not sampler.thread.is_alive():
            break
    sampler.stop()
    if sampler.failure:
        raise RuntimeError("sampler failed: " + str(sampler.failure))

def kafka_consumer(address,group_id=None):
    from kafka import KafkaConsumer, __version__
    if __version__!='2.2.15':raise RuntimeError('frozen Kafka observer version required')
    return KafkaConsumer(bootstrap_servers=[address+':19092'],group_id=group_id,
                         enable_auto_commit=False,allow_auto_create_topics=False,
                         request_timeout_ms=5000,session_timeout_ms=2000,heartbeat_interval_ms=1000,
                         api_version_auto_timeout_ms=5000,fetch_max_bytes=8*1024*1024,
                         max_poll_records=100,client_id='gopulse-phase20-observer')

def kafka_offsets(client,partitions,attempts=2):
    last=None
    for attempt in range(attempts):
        try:
            end=client.end_offsets(partitions)
            committed={p:client.committed(p) for p in partitions}
            return end,committed
        except Exception as error:
            last=error
            if attempt+1<attempts:time.sleep(0.25)
    raise last

class KafkaWaterline:
    MAX_BUFFERED_MESSAGES = 20000

    def __init__(self,address,topic='gopulse-observability-v1'):
        from kafka import TopicPartition
        self.consumer=kafka_consumer(address)
        partitions=self.consumer.partitions_for_topic(topic)
        if not partitions:raise RuntimeError('Kafka marker partitions missing')
        self.partitions=[TopicPartition(topic,p) for p in sorted(partitions)]
        self.consumer.assign(self.partitions)
        self.consumer.seek_to_end(*self.partitions)
        self.messages=deque(maxlen=self.MAX_BUFFERED_MESSAGES)
        self.dropped_messages=0
    def poll(self,timeout_ms=200):
        for partition,rows in self.consumer.poll(timeout_ms=timeout_ms).items():
            for row in rows:
                if len(self.messages) == self.MAX_BUFFERED_MESSAGES:
                    self.dropped_messages += 1
                self.messages.append({'topic':partition.topic,'partition':partition.partition,'offset':row.offset,'envelope':json.loads(row.value)})
        return self.messages
    def close(self):self.consumer.close()

class Sampler:
    def __init__(self,project,env_file,files,environment,profile,run_id,path):
        self.project,self.env_file,self.files=project,env_file,files
        self.environment,self.profile,self.run_id=environment,profile,run_id
        self.path=Path(path);self.stop_event=threading.Event();self.thread=None
        self.load_pid=None;self.failure=None;self.records=[]
        self.addresses={s:service_address(project,env_file,files,s) for s in profile['sampling']['required_components']}
        broker_address(self.addresses['kafka'])
    def set_load_pid(self,pid):self.load_pid=pid
    def start(self):
        self.thread=threading.Thread(target=self._run,name='phase20-sampler')
        self.thread.start()
    def stop(self):
        self.stop_event.set()
        if self.thread:self.thread.join(timeout=20)
        if self.thread and self.thread.is_alive():raise RuntimeError('sampler failed to join; next stage forbidden')
        if self.failure:raise RuntimeError('sampler failed: '+str(self.failure))
    def _run(self):
        client=None
        try:
            from kafka import TopicPartition
            address=self.addresses['kafka']
            client=kafka_consumer(address,self.environment.get('MARSHALLER_KAFKA_GROUP','gopulse-marshaller-metrics-v1'))
            partitions=[TopicPartition('gopulse-observability-v1',p) for p in sorted(client.partitions_for_topic('gopulse-observability-v1') or [])]
            if not partitions:raise RuntimeError('sampling partitions missing')
            client.assign(partitions)
            next_at=time.monotonic();interval=self.profile['sampling']['interval_seconds']
            previous_cpu=None
            with self.path.open('x') as stream:
                os.chmod(self.path,0o600)
                while not self.stop_event.wait(max(0,next_at-time.monotonic())):
                    started=time.monotonic();missing=[];failure=None;signals={};probe_times={}
                    try:
                        end,committed=kafka_offsets(client,partitions)
                        if any(v is None for v in committed.values()):raise RuntimeError('product Kafka group offsets missing')
                        signals['kafka_lag']=[{'partition':p.partition,'end_offset':end[p],'committed_offset':committed[p],'lag':max(0,end[p]-committed[p])} for p in partitions]
                        probe_times['kafka_seconds']=time.monotonic()-started
                        ids=require(compose(self.project,self.env_file,self.files,'ps','-q',timeout=5),'inspect sampler containers').split()
                        inspections=json.loads(require(command(['docker','inspect',*ids],timeout=5),'inspect sampler ownership'))
                        containers={i['Id']:i for i in inspections}
                        if any(i['Config']['Labels']['com.docker.compose.project']!=self.project for i in inspections):raise RuntimeError('ownership lost')
                        stats=require(command(['docker','stats','--no-stream','--format','{{json .}}',*ids],timeout=5),'sample Docker resources')
                        signals['containers']=[]
                        for line in stats.splitlines():
                            stat=json.loads(line);obj=next((i for cid,i in containers.items() if cid.startswith(stat['ID'])),None)
                            if obj:
                                current= parse_size(stat['MemUsage'].split('/')[0])
                                signals['containers'].append({'service':obj['Config']['Labels']['com.docker.compose.service'],'cpu_percent':parse_ratio(stat['CPUPerc']),'rss_bytes':current,'oom':obj['State']['OOMKilled'],'running':obj['State']['Running'],**container_budget(obj,current)})
                        present={c['service'] for c in signals['containers']}
                        missing+=['component:'+s for s in set(self.addresses)-present]
                        if any(c['oom'] for c in signals['containers']):raise RuntimeError('owned product OOM')
                        cpu=parse_cpu_stat(Path('/proc/stat').read_text());mem=parse_meminfo(Path('/proc/meminfo').read_text())
                        signals['host']={'cpu':cpu,'meminfo':mem,'disk_free_bytes':os.statvfs(self.path.parent).f_bavail*os.statvfs(self.path.parent).f_frsize}
                        auth=base64.b64encode((self.environment['RABBITMQ_USER']+':'+self.environment['RABBITMQ_PASSWORD']).encode()).decode()
                        queues=json_http('http://'+self.addresses['rabbitmq']+':15672/api/queues',{'Authorization':'Basic '+auth},timeout=1)
                        signals['rabbitmq']=[{'name':q['name'],'ready':q['messages_ready'],'unacked':q['messages_unacknowledged']} for q in queues]
                        signals['load_process']=process_stats(self.load_pid)
                        if self.load_pid and signals['load_process'] is None:missing.append('load_process')
                        signals['links']={}
                        for service,port,token in [('backend',19101,'BACKEND'),('business-worker',19102,'BUSINESS_WORKER'),('search-indexer',19103,'SEARCH_INDEXER'),('monitor',19104,'MONITOR'),('router',19105,'ROUTER'),('marshaller',19106,'MARSHALLER')]:
                            for name in [service]+([service+'-2'] if service!='monitor' else []):
                                origin=time.monotonic()
                                request=urllib.request.Request('http://'+self.addresses[name]+':'+str(port)+'/internal/v1/metrics',headers={'Authorization':'Bearer '+self.environment[token+'_METRICS_TOKEN']})
                                with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=1) as response:text=response.read(1024*1024).decode()
                                signals['links'][name]=text
                                probe_times[name+'_seconds']=time.monotonic()-origin
                        # The existing exporter families are queried by the
                        # product metrics scrape; broker lag is observed directly
                        # without repeatedly launching a JVM CLI.
                        if 'gopulse_backend_outbox_pending' not in signals['links']['backend']:missing.append('outbox')
                    except Exception as error:failure=type(error).__name__+': '+str(error)
                    ended=time.monotonic()
                    row={'schema':'gopulse.phase20.resources.v1','run_id':self.run_id,'sequence':len(self.records),'scheduled_monotonic':next_at,'started_monotonic':started,'finished_monotonic':ended,'sampling_schedule_lag_ms':max(0,started-next_at)*1000,'sampling_duration_seconds':ended-started,'probe_durations':probe_times,'missing_signals':missing,'failure':failure,'signals':signals}
                    self.records.append(row);stream.write(json.dumps(row)+'\n');stream.flush()
                    if failure:self.failure=failure;self.stop_event.set();break
                    next_at+=interval
        except Exception as error:self.failure=type(error).__name__+': '+str(error)
        finally:
            if client:client.close()

def overhead_comparison(api,make_sampler,path,contract):
    """Two real read workloads, identical arrivals/config, with sampling off/on.

    This measures finite observer cost; it is not a business capacity repeat.
    Raw samples and timings are retained, no Kafka CPU peak causal claim.
    """
    import concurrent.futures
    trials=[]
    for enabled in (False,True):
        rows=[];sampler=make_sampler() if enabled else None
        before_cpu=parse_cpu_stat(Path('/proc/stat').read_text());before_process=time.process_time()
        if sampler:sampler.start()
        started=time.monotonic()
        def call(slot,scheduled):
            sent=time.monotonic();response=api.call('/api/v1/users/me');finished=time.monotonic()
            return {'slot_id':slot,'scheduled_monotonic':scheduled,'sent_monotonic':sent,'completed_monotonic':finished,'latency_ms':(finished-scheduled)*1000,'status':200 if 'data' in response else None}
        with concurrent.futures.ThreadPoolExecutor(max_workers=contract["workers"]) as workers:
            futures=[]
            for slot in range(int(contract["target_rps"]*contract["seconds"])):
                scheduled=started+slot/contract["target_rps"]
                time.sleep(max(0,scheduled-time.monotonic()))
                futures.append(workers.submit(call,slot,scheduled))
            rows=[f.result() for f in futures]
        if sampler:sampler.stop()
        finished=time.monotonic();after_cpu=parse_cpu_stat(Path('/proc/stat').read_text())
        trials.append({'sampling_enabled':enabled,'route':'GET /api/v1/users/me','target_rps':contract['target_rps'],'arrival_seconds':contract['seconds'],'requests':rows,'elapsed_seconds':finished-started,'observer_process_cpu_seconds':time.process_time()-before_process,'host_cpu_before':before_cpu,'host_cpu_after':after_cpu,'sampler_records':sampler.records if sampler else []})
    result={'formal':False,'same_host':True,'trials':trials,'conclusion':'bounded_observer_cost_only'}
    Path(path).write_text(json.dumps(result));os.chmod(path,0o600)
    return result
