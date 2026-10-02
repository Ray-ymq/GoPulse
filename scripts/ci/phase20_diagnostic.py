#!/usr/bin/env python3
"""Owned, sequential Phase 20 diagnosis; separate clocks and raw facts."""
from __future__ import annotations
import argparse
import base64
import copy
import concurrent.futures
import datetime
import hashlib
import json
import os
import re
import secrets
import shutil
import socket
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from pathlib import Path
import phase19_capacity as legacy
from phase20_evidence import (DIMENSIONS, Incomplete, associate, business_ready,
    content_digest, digest, ledger_requests, marker_ready, verify_directory, tool_inputs, management_payload)
from phase20_sampler import Sampler, KafkaWaterline, broker_address, service_address, json_http, overhead_comparison

ROOT=Path(__file__).resolve().parents[2]
PROFILE=ROOT/'loadtest/phase20-capacity-profile.json'
TRACE_COMPOSE=ROOT/'deploy/phase20-trace.yaml'

def write_json(path,value):
    legacy.atomic_json(path,value)

def append(path,row):
    with Path(path).open('a') as f:f.write(json.dumps(row)+'\n')
    os.chmod(path,0o600)

def load_profile():
    profile=json.loads(PROFILE.read_text())
    import jsonschema
    jsonschema.validate(profile,json.loads((ROOT/'loadtest/phase20-capacity-profile.schema.json').read_text()))
    old=json.loads((ROOT/'loadtest/capacity-profile.json').read_text())
    for route in old['workload']['routes']:
        if route['template'] in ('PUT /api/v1/users/:userId/follow','DELETE /api/v1/users/:userId/follow'):route['allowed_statuses']=[200]
    for name in ('host','recipe','workload','stages','repetitions','gates','stop_conditions','sampling','statistics'):
        expected=old[name]
        if name=='host':
            # Phase 20 owns the explicit host disk safety gate.  All other
            # host facts remain frozen to the Phase 19 input.
            expected=dict(expected)
            expected['disk_free_bytes_min']=profile['host']['disk_free_bytes_min']
        if profile[name]!=expected:raise Incomplete('frozen Phase 19 input drift: '+name)
    return profile

def mysql(project,env_file,files,sql,timeout=15):
    # SQL travels over stdin: a single argv element is capped by Linux even
    # when the overall command is below ARG_MAX. One stream preserves the RR
    # snapshot across both exact event and notification queries.
    args=['docker','compose','--project-name',project,'--env-file',str(env_file)]
    for path in files:args.extend(['-f',str(path)])
    args.extend(['exec','-T','mysql','sh','-c',
        'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" --raw -N -B "$MYSQL_DATABASE"'])
    result=subprocess.run(args,input=sql,text=True,capture_output=True,timeout=timeout)
    return legacy.require(result,'read consistent business facts')

def snapshot(project,env_file,files,run_id,minimum_outbox=0):
    statements=["SET TRANSACTION ISOLATION LEVEL REPEATABLE READ",'START TRANSACTION WITH CONSISTENT SNAPSHOT']
    tables=[('posts',"JSON_OBJECT('id',id,'author_id',author_id,'title',title,'content',content,'content_revision',content_revision)",''),
            ('comments',"JSON_OBJECT('id',id,'author_id',author_id,'post_id',post_id,'content',content)",''),
            ('relations',"JSON_ARRAY('like',user_id,post_id)", 'post_likes'),
            ('relations',"JSON_ARRAY('follow',follower_id,followed_id)",'user_follows'),
            ('relations',"JSON_ARRAY('bookmark',user_id,post_id)",'post_bookmarks'),
            ('events',"JSON_OBJECT('outbox_id',id,'event_id',event_id,'status',status,'payload',payload)", 'business_outbox')]
    for key,expression,table in tables:
        table=table or key
        where=f' WHERE id>{int(minimum_outbox)}' if key=='events' else ''
        if not minimum_outbox and key=='events':expression="JSON_OBJECT('outbox_id',id,'event_id',event_id)"
        statements.append(f"SELECT JSON_OBJECT('table','{key}','row',{expression}) FROM {table}{where}")
    statements.append('COMMIT')
    value={'run_id':run_id,'consistent_read':True,'posts':[],'comments':[],'relations':[],'events':[]}
    for line in mysql(project,env_file,files,';'.join(statements),timeout=60).splitlines():
        row=json.loads(line);key=row['table'];item=row['row']
        if key in ('posts','comments'):
            item['content_digest']=content_digest(item.pop('title',''),item.pop('content'))
        value[key].append(item)
    return value

def event_sql(ids):
    if any(not re.fullmatch(r'[0-9a-f-]{36}',x) for x in ids):raise Incomplete('invalid raw event identity')
    return ','.join("'"+x+"'" for x in ids) or "''"

def search_posts(address,ids):
    if not ids:return []
    response=json_http('http://'+address+':9200/gopulse-post-search-v1/_search',{'Content-Type':'application/json'},
        json.dumps({'size':10000,'query':{'terms':{'post_id':sorted(ids)}}}).encode(),method='POST',timeout=2)
    if response.get('timed_out') or response['_shards']['failed']:raise RuntimeError('business search incomplete')
    hits=response['hits']['hits']
    if len(hits)==10000:raise Incomplete('business search result bound exceeded')
    result=[]
    for hit in hits:
        p=hit['_source'];result.append({'id':p['post_id'],'content_revision':p['content_revision'],'content_digest':content_digest(p['title'],p['content'])})
    return result

def business_observation(project,env_file,files,after,association,search_address):
    ids=event_sql([e['event_id'] for e in after['events']])
    sql=f"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ;START TRANSACTION WITH CONSISTENT SNAPSHOT;SELECT JSON_OBJECT('table','events','row',JSON_OBJECT('event_id',event_id,'status',status)) FROM business_outbox WHERE event_id IN ({ids});SELECT JSON_OBJECT('table','notifications','row',JSON_OBJECT('source_event_id',source_event_id,'recipient_id',recipient_id,'actor_id',actor_id,'type',type,'post_id',post_id,'comment_id',comment_id)) FROM notifications WHERE source_event_id IN ({ids});COMMIT"
    result={'events':[],'notifications':[]}
    for line in mysql(project,env_file,files,sql,timeout=4).splitlines():
        row=json.loads(line);result[row['table']].append(row['row'])
    post_ids={g['object_id'] for g in association['groups'] if any(route in ('POST /api/v1/posts','PATCH /api/v1/posts/:postId','DELETE /api/v1/posts/:postId') for route in g['routes'])}
    result['search']=search_posts(search_address,post_ids)
    return result

class ProductAPI:
    def __init__(self,url,credentials):
        self.url=url;self.cookie=None
        user=credentials['users'][0]
        _,headers=self.call('/api/v1/auth/login',{'username':user['username'],'password':credentials['password']},return_headers=True)
        self.cookie=headers['Set-Cookie'].split(';')[0]
    def call(self,path,body=None,return_headers=False,method=None):
        headers={'Content-Type':'application/json'}
        if self.cookie:headers['Cookie']=self.cookie
        request=urllib.request.Request(self.url+path,json.dumps(body).encode() if body is not None else None,headers,method=method)
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=3) as response:
            if response.status!=200:raise Incomplete('acceptance API success status violates route contract')
            value=json.loads(response.read(8*1024*1024));response_headers=response.headers
        return (value,response_headers) if return_headers else value

def marker(channel,row,run_id,origin,extra=None):
    e=row['envelope']
    result={'run_id':run_id,'channel':channel,'marker_id':run_id+'/'+channel,'t_origin':origin,
            'message_id':e['message_id'],'source':e['source'],'timestamp':e['timestamp'],
            'topic':row['topic'],'partition':row['partition'],'offset':row['offset'],
            'envelope':e,'accepted':True}
    result.update(extra or {});return result

def query_marker(channel,selected,api,addresses,environment):
    if selected is None:return None
    e=selected['envelope']
    if channel=='metrics':
        series=selected['series'];labels=','.join(k+'='+json.dumps(v) for k,v in series['labels'].items())
        expression=series['name']+('{'+labels+'}' if labels else '')
        params=urllib.parse.urlencode({'match[]':expression,'start':selected['timestamp_ms']/1000-1,'end':selected['timestamp_ms']/1000+1})
        auth=base64.b64encode((environment['VICTORIAMETRICS_USERNAME']+':'+environment['VICTORIAMETRICS_PASSWORD']).encode()).decode()
        request=urllib.request.Request('http://'+addresses['victoriametrics']+':8428/api/v1/export?'+params,headers={'Authorization':'Basic '+auth})
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=2) as response:
            rows=[json.loads(line) for line in response.read(1024*1024).decode().splitlines()]
        for row in rows:
            if row['metric']=={'__name__':series['name'],**series['labels']}:
                for timestamp,value in zip(row['timestamps'],row['values']):
                    if abs(timestamp-selected['timestamp_ms'])<=1 and value==selected['value']:
                        return {'message_id':e['message_id'],'series':series,'timestamp_ms':timestamp,'value':value}
        return None
    index='gopulse-'+channel+'-v1-'+e['timestamp'][:10].replace('-','.')
    try:stored=json_http('http://'+addresses['observability-elasticsearch']+':9200/'+index+'/_doc/'+e['message_id'],timeout=2)
    except urllib.error.HTTPError as error:
        if error.code==404:return None
        raise
    if not stored.get('found'):return None
    payload=dict(e['payload']);payload.pop('event_schema_version',None);payload.pop('log_schema_version',None)
    stored_source=dict(stored['_source']);stored_source['timestamp']=stored_source.pop('@timestamp');stored_source.pop('event_schema_version',None);stored_source.pop('log_schema_version',None)
    if stored['_id']!=e['message_id'] or stored_source!=payload:raise Incomplete('stored marker payload/identity mismatch')
    params={'from':e['timestamp'],'to':datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00','Z'),'limit':100}
    if channel=='logs':params['request_id']=payload['request_id']
    else:params.update({'plugin_id':payload['metadata']['plugin_id'],'event_name':payload['event_name'],'operation':payload['metadata']['operation']})
    page=api.call('/api/v1/observability/'+channel+'?'+urllib.parse.urlencode(params))
    entries=[entry for entry in page['data'] if entry==management_payload(channel,e['payload'])]
    if len(entries)>1:raise Incomplete('management marker association ambiguous')
    if len(entries)==1:return {'message_id':e['message_id'],'stored_id':stored['_id'],'entry':entries[0],'stored_source':stored['_source']}
    return None

def independent_recovery(origins,probe,path,run_id,deadline_seconds=120,poll_seconds=1):
    """Every probe has its own worker/deadline; join all workers before cleanup."""
    def worker(channel):
        origin=origins[channel];deadline=origin+deadline_seconds;first=None
        while True:
            started=time.monotonic();facts=probe(channel,max(0,deadline-started))
            ended=time.monotonic()
            row={'run_id':run_id,'channel':channel,'started_monotonic':started,'observed_monotonic':ended,**facts}
            # Each worker writes its own raw stream, merged only after joining.
            append(str(path)+'.'+channel,row)
            if facts['ready'] and ended<=deadline:first=ended;break
            if ended>=deadline:break
            time.sleep(min(poll_seconds,deadline-ended))
        return {'origin':origin,'first_success':first,'timed_out':first is None,'elapsed_to_deadline':deadline_seconds if first is None else None}
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as workers:
        futures={c:workers.submit(worker,c) for c in DIMENSIONS}
        results={c:f.result() for c,f in futures.items()}
    rows=[]
    for c in DIMENSIONS:
        part=Path(str(path)+'.'+c);rows.extend(json.loads(line) for line in part.read_text().splitlines())
    for row in sorted(rows,key=lambda r:r['observed_monotonic']):append(path,row)
    return results

def run_cell(profile,binding,candidate,recipe_binary,load_binary,work,repeat,index,*,trace=False,include_overhead=True,independent_sampler=False,fault=None,max_execution_seconds=None):
    stage=profile['stages'][index];run_id=f'r{repeat}-{stage["name"]}-{secrets.token_hex(4)}'
    cell_dir=work/run_id;cell_dir.mkdir(mode=0o700)
    inventory_before=legacy.resource_inventory()
    project=legacy.project_name();env_file=cell_dir/'candidate.env'
    environment=legacy._candidate_env(candidate,env_file,repeat)
    if trace:
        environment.update({'GOPULSE_TRACE_ENABLED':'true','GOPULSE_TRACE_ENDPOINT':'phase20-collector:4317','GOPULSE_TRACE_SAMPLE_RATIO':'1.0'})
    else:
        environment.update({'GOPULSE_TRACE_ENABLED':'false','GOPULSE_TRACE_ENDPOINT':'','GOPULSE_TRACE_SAMPLE_RATIO':'0.10'})
    environment['GOPULSE_BOOTSTRAP_USER_ID']='1';legacy.write_env(env_file,environment)
    override=legacy.compose_override(cell_dir/'compose.override.yaml',int(environment['MYSQL_PORT']))
    files=[legacy.COMPOSE_PATH,override]
    if trace:files.insert(1,TRACE_COMPOSE)
    sampler=None;waterline=None;process=None;started=False;fault_record=None;load_started=None;sampler_facts={};marker_observer={};cleanup=None
    lifecycle=cell_dir/'lifecycle.jsonl'
    def event(name,**extra):append(lifecycle,{'event':name,'run_id':run_id,'monotonic':time.monotonic(),**extra})
    try:
        legacy.require(legacy.compose(project,env_file,files,'up','-d','--wait','--wait-timeout','900',timeout=1200),'start owned empty diagnostic project')
        legacy.ensure_owned_project(project,env_file,files);started=True;event('project_started',project=project)
        mysql_address=legacy.mysql_service_address(project,env_file,files)
        receipt,corpus,credentials,rejection=legacy.generate_recipe(recipe_binary,binding,environment,cell_dir,3306,mysql_address)
        legacy.require(legacy.compose(project,env_file,files,'run','--rm','--no-deps','--entrypoint','/usr/local/bin/search-reindex','search-init',timeout=900),'reindex identical recipe')
        legacy.wait_initial_convergence(project,env_file,files,timeout=900)
        legacy.require(legacy.compose(project,env_file,files,'run','--rm','--no-deps','admin-role',timeout=30),'bootstrap acceptance operator')
        api=ProductAPI('http://127.0.0.1:'+environment['FRONTEND_PORT'],json.loads(credentials.read_text()))
        addresses={s:service_address(project,env_file,files,s) for s in ('kafka','elasticsearch','observability-elasticsearch','victoriametrics')}
        broker_address(addresses['kafka']);waterline=KafkaWaterline(addresses['kafka'])
        if include_overhead and repeat==1 and index==0:
            overhead_comparison(api,lambda:Sampler(project,env_file,files,environment,profile,run_id,cell_dir/'overhead-samples.jsonl'),cell_dir/'overhead.json',profile['diagnostic']['observer_comparison'])
        growth_before=json_http('http://'+addresses['observability-elasticsearch']+':9200/_stats/store,docs')
        before=snapshot(project,env_file,files,run_id);write_json(cell_dir/'before.json',before)
        baseline_outbox=max(e['outbox_id'] for e in before['events'])
        # Quantify the previous expensive JVM probe once, outside measured load.
        probe_origin=time.monotonic();old_lag=legacy._kafka_lag(project,env_file,files)
        write_json(cell_dir/'cli-overhead.json',{'duration_seconds':time.monotonic()-probe_origin,'lag':old_lag,'formal_load_window':False})
        if independent_sampler:
            from phase20_budget import ProcessSampler
            sampler=ProcessSampler(project,env_file,files,environment,profile,run_id,cell_dir/'resources.jsonl')
        else:
            sampler=Sampler(project,env_file,files,environment,profile,run_id,cell_dir/'resources.jsonl')
        args=[str(load_binary),'--profile',str(work/'profile.json'),'--base-url',api.url,'--corpus',str(corpus),'--credentials',str(credentials),'--candidate-manifest',str(work/'candidate-manifest.json'),'--workdir',str(cell_dir),'--repeat',str(repeat),'--stage',str(index),'--run-id',run_id]
        with (cell_dir/'load.stdout').open('x') as out,(cell_dir/'load.stderr').open('x') as err:
            load_started=time.monotonic()
            process=subprocess.Popen(args,stdout=out,stderr=err)
            sampler.set_load_pid(process.pid);sampler.start()
            start=time.monotonic()
            fault_started=False
            while process.poll() is None:
                waterline.poll(200)
                if sampler.failure:raise RuntimeError('sampling failure: '+str(sampler.failure))
                if fault and not fault_started and time.monotonic()-start >= float(fault['at_seconds']):
                    fault_started=True
                    target=fault['target']
                    container_id=legacy.compose(project,env_file,files,'ps','-q',target,timeout=30).stdout.strip()
                    if not container_id:raise RuntimeError('bounded diagnostic fault target has no container')
                    stop_requested=time.time()
                    legacy.require(legacy.compose(project,env_file,files,'stop',target,timeout=60),'inject bounded diagnostic fault')
                    stopped_at=time.time()
                    stopped=json.loads(legacy.require(legacy.command(['docker','inspect',container_id],timeout=30),'inspect bounded diagnostic fault'))[0]
                    if stopped.get('State',{}).get('Running'):
                        raise RuntimeError('bounded diagnostic fault did not stop its target')
                    requested_duration=float(fault['duration_seconds'])
                    remaining=max(0.0,requested_duration-(time.time()-stopped_at))
                    if remaining:time.sleep(remaining)
                    restart_requested=time.time()
                    legacy.require(legacy.compose(project,env_file,files,'start',target,timeout=60),'recover bounded diagnostic fault')
                    running_deadline=time.monotonic()+120;running=False
                    container_id=legacy.compose(project,env_file,files,'ps','-q',target,timeout=30).stdout.strip()
                    while time.monotonic()<running_deadline:
                        state=json.loads(legacy.require(legacy.command(['docker','inspect',container_id],timeout=30),'inspect recovered diagnostic fault'))[0].get('State',{})
                        if state.get('Running'):
                            running=True;break
                        time.sleep(1)
                    if not running:raise RuntimeError('bounded diagnostic fault did not recover within 120 seconds')
                    fault_record={'target':target,'at_seconds':float(fault['at_seconds']),'requested_duration_seconds':requested_duration,'stop_requested_at':stop_requested,'stopped_at':stopped_at,'restart_requested_at':restart_requested,'recovered_at':time.time(),'effective':True,'duration_seconds':time.time()-stopped_at}
                    write_json(cell_dir/'fault.json',fault_record)
                limit=max_execution_seconds
                if limit is None:
                    limit=float(stage['warmup_seconds'])+float(stage['measurement_seconds'])+180.0
                if time.monotonic()-start>limit:raise RuntimeError('load exceeded fixed windows/session/drain bound')
            if process.returncode:raise RuntimeError('load execution incomplete; private stderr retained')
        load_dir=cell_dir/f'repeat-{repeat:02d}'
        load=json.loads((load_dir/'load-report.json').read_text())
        now_mono=time.monotonic();now_wall=time.time()
        drain_wall=datetime.datetime.fromisoformat(load['t_drain'].replace('Z','+00:00')).timestamp()
        stop_wall=datetime.datetime.fromisoformat(load['t_stop'].replace('Z','+00:00')).timestamp()
        drain_origin=now_mono-(now_wall-drain_wall)
        append(lifecycle,{'event':'load_stopped','run_id':run_id,'monotonic':now_mono-(now_wall-stop_wall)})
        append(lifecycle,{'event':'requests_drained','run_id':run_id,'monotonic':drain_origin})
        sampler.set_load_pid(None);sampler.stop()
        sampler_facts={'cpu_peak_cores':float(getattr(sampler,'cpu_peak_cores',0.0)),'cpu_seconds':float(getattr(sampler,'cpu_seconds',0.0)),'sample_count':len(getattr(sampler,'records',[]))}
        sampler=None
        after=snapshot(project,env_file,files,run_id,baseline_outbox);write_json(cell_dir/'after.json',after)
        requests=ledger_requests([json.loads(line) for line in (load_dir/'ledger.jsonl').read_text().splitlines()],run_id,repeat,stage['name'])
        association=associate(requests,before,after)
        origins={'business':drain_origin,'metrics':time.monotonic()};markers={}
        origins['logs']=time.monotonic()
        _,headers=api.call('/api/v1/users/me',return_headers=True);request_id=headers.get('X-Request-ID')
        if not request_id:raise Incomplete('log marker request identity missing')
        origins['events']=time.monotonic()
        prior=api.call('/api/v1/exporter-plugins/redis-exporter')['data']
        stopped=api.call('/api/v1/exporter-plugins/redis-exporter/stop',method='POST')['data']
        restored=api.call('/api/v1/exporter-plugins/redis-exporter/start',method='POST')['data']
        if prior['observed_state']!='running' or stopped['observed_state']!='stopped' or restored['observed_state']!='running':raise Incomplete('real plugin transition not accepted/restored')
        operations={'before':prior,'stop_response':stopped,'start_response':restored}
        while len(markers)<3 and time.monotonic()<min(origins[c]+120 for c in ('metrics','logs','events') if c not in markers):
            messages=waterline.poll(200)
            for channel in ('metrics','logs','events'):
                if channel in markers:continue
                matches=[]
                for row in messages:
                    e=row['envelope'];p=e['payload']
                    timestamp=datetime.datetime.fromisoformat(e['timestamp'].replace('Z','+00:00')).timestamp()
                    if e['type']!=channel or timestamp<drain_wall:continue
                    if channel=='metrics' and e['source']=='backend':
                        samples=[s for s in p.get('samples',[]) if s['name']=='gopulse_backend_outbox_pending']
                        if samples:
                            s=samples[0];matches.append((row,{'series':{'name':s['name'],'labels':{'source':e['source'],'target_id':p['target_id'],'producer_kind':'component','producer_id':p['producer_id'],**s.get('labels',{})}},'value':s['value'],'timestamp_ms':int(timestamp*1000)}))
                    elif channel=='logs' and p.get('request_id')==request_id and p.get('message')=='http request completed':matches.append((row,{'generation_evidence':{'method':'GET','route':'/api/v1/users/me','status':200,'request_id':request_id,'t_origin':origins['logs']}}))
                    elif channel=='events' and p.get('event_name')=='exporter_plugin_started' and p.get('metadata',{}).get('plugin_id')=='redis-exporter' and p['metadata'].get('plugin_version')==restored['version'] and p['metadata'].get('from_state')=='stopped' and p['metadata'].get('to_state')=='running':matches.append((row,{'operation_evidence':operations}))
                if len(matches)>1 and channel!='metrics':raise Incomplete('real marker association ambiguous')
                if matches:
                    row,extra=matches[0];markers[channel]=marker(channel,row,run_id,origins[channel],extra)
        write_json(cell_dir/'markers.json',markers)
        if set(markers)!={'metrics','logs','events'}:raise Incomplete('event_not_generated' if 'events' not in markers else 'accepted observability marker missing')
        marker_observer={'buffer_limit':waterline.MAX_BUFFERED_MESSAGES,'buffered_messages':len(waterline.messages),'dropped_messages':waterline.dropped_messages}
        def probe(channel,remaining):
            if channel=='business':
                facts=business_observation(project,env_file,files,after,association,addresses['elasticsearch']);return {'facts':facts,'ready':business_ready(after,association,facts)}
            query=query_marker(channel,markers[channel],api,addresses,environment)
            return {'query':query,'ready':marker_ready(channel,markers[channel],query,run_id)}
        recovery=independent_recovery(origins,probe,cell_dir/'recovery.jsonl',run_id)
        event('recovery_finished',dimensions=recovery)
        growth_after=json_http('http://'+addresses['observability-elasticsearch']+':9200/_stats/store,docs')
        write_json(cell_dir/'growth.json',{'scope':'owned observability Elasticsearch, finite cell window','before':growth_before,'after':growth_after})
        waterline.close();waterline=None
        event('workers_joined')
        cleanup=legacy.cleanup_project(project,env_file,files);started=False
        inventory_after=legacy.resource_inventory()
        cleanup.update(inventory_before=inventory_before,inventory_after=inventory_after)
        if inventory_after!=inventory_before:raise Incomplete('cleanup resource inventory changed; raw ownership evidence retained')
        event('project_cleaned',receipt=cleanup)
        for path in (corpus,credentials,env_file,override):
            Path(path).unlink(missing_ok=True)
        raw_paths={'ledger':load_dir/'ledger.jsonl','load':load_dir/'load-report.json','before':cell_dir/'before.json','after':cell_dir/'after.json','markers':cell_dir/'markers.json','recovery':cell_dir/'recovery.jsonl','lifecycle':lifecycle,'resources':cell_dir/'resources.jsonl','cli_overhead':cell_dir/'cli-overhead.json','growth':cell_dir/'growth.json'}
        if (cell_dir/'overhead.json').exists():raw_paths['overhead']=cell_dir/'overhead.json'
        if (cell_dir/'fault.json').exists():raw_paths['fault']=cell_dir/'fault.json'
        measurement_start=float(load_started)+float(stage['warmup_seconds'])
        measurement_end=measurement_start+float(stage['measurement_seconds'])
        return {'run_id':run_id,'repeat':repeat,'stage':stage['name'],'candidate':binding,'origins':origins,'measurement_window':{'start_monotonic':measurement_start,'end_monotonic':measurement_end,'warmup_seconds':stage['warmup_seconds'],'measurement_seconds':stage['measurement_seconds']},'sampler':sampler_facts,'marker_observer':marker_observer,'fault':fault_record,'cleanup':cleanup,'raw':{k:{'path':str(v.relative_to(work)),'sha256':digest(v)} for k,v in raw_paths.items()},'recipe':{'receipt_sha256':digest(cell_dir/'recipe-receipt.json'),'rejection':rejection},'project':project,'execution_status':'complete'}
    finally:
        if process and process.poll() is None:
            process.terminate()
            try:process.wait(timeout=30)
            except subprocess.TimeoutExpired:process.kill();process.wait(timeout=5)
        stop_error=None
        if sampler:
            try:sampler.stop()
            except Exception as error:stop_error=error
            sampler_thread=getattr(sampler,'thread',None) or getattr(sampler,'monitor_thread',None)
            if sampler_thread and sampler_thread.is_alive():raise Incomplete('sampler not joined; ownership retained')
        if waterline:waterline.close()
        if started:
            cleanup=legacy.cleanup_project(project,env_file,files)
            write_json(cell_dir/'failure-cleanup.json',cleanup)
        for path in (locals().get('corpus'),locals().get('credentials'),env_file,override):
            if path:Path(path).unlink(missing_ok=True)
        if stop_error:raise stop_error

def main(arguments=None):
    parser=argparse.ArgumentParser(description=__doc__)
    modes=parser.add_mutually_exclusive_group(required=True);modes.add_argument('--preflight',action='store_true');modes.add_argument('--baseline',action='store_true')
    parser.add_argument('--manifest',required=True,type=Path);parser.add_argument('--work',required=True,type=Path)
    args=parser.parse_args(arguments);work=args.work.resolve();profile=load_profile()
    work.mkdir(mode=0o700,parents=True,exist_ok=False)
    if work.stat().st_mode&0o077:raise Incomplete('work directory not private')
    binding,candidate=legacy.candidate_binding(args.manifest,profile)
    shutil.copyfile(args.manifest,work/'candidate-manifest.json');os.chmod(work/'candidate-manifest.json',0o600)
    shutil.copyfile(PROFILE,work/'profile.json');os.chmod(work/'profile.json',0o600)
    # Bind exact source bytes, not a commit that omits uncommitted tool fixes.
    tool_files=tool_inputs()
    document={'schema':'gopulse.phase20.diagnostic.v1','formal':args.baseline,'candidate':binding,'profile_sha256':digest(work/'profile.json'),'tools':{str(p.relative_to(ROOT)):digest(p) for p in tool_files},'execution_status':'incomplete','cells':[],'stop':None}
    write_json(work/'diagnostic.json',document)
    try:
        document['host_identity']='sha256:'+hashlib.sha256(Path('/proc/sys/kernel/random/boot_id').read_bytes()).hexdigest()
        document['tool_dependencies']=validate_dependencies()
        document['self_tests']=run_self_tests(work);write_json(work/'diagnostic.json',document)
        if args.baseline:
            document['preflight']=select_preflight(work,binding,document['tools'],document['profile_sha256'],document['host_identity']);write_json(work/'diagnostic.json',document)
        host=legacy.host_inventory();legacy.validate_host(host,profile);write_json(work/'host.json',host)
        recipe_binary,load_binary=legacy.build_loadtest(work)
        first=legacy.inspect_recipe(recipe_binary,binding,work/'recipe-inspect-1.json');second=legacy.inspect_recipe(recipe_binary,binding,work/'recipe-inspect-2.json')
        if any(first[k]!=second[k] for k in ('digest','counts','id_ranges')):raise Incomplete('deterministic recipe inspection drift')
        schedule=[(r,i) for r in range(1,4) for i in range(4)] if args.baseline else [(1,0),(1,1)]
        for repeat,index in schedule:
            print(json.dumps({'event':'cell_started','repeat':repeat,'stage':profile['stages'][index]['name']}),flush=True)
            cell=run_cell(profile,binding,candidate,recipe_binary,load_binary,work,repeat,index)
            document['cells'].append(cell);write_json(work/'diagnostic.json',document)
        document['execution_status']='complete';write_json(work/'diagnostic.json',document)
        result=verify_directory(work,formal=args.baseline);write_json(work/'verification.json',result)
        print(json.dumps({'execution_status':result['execution_status'],'capability_status':result['capability_status']}),flush=True)
        return 0
    except Exception as error:
        document['execution_status']='incomplete';document['stop']={'classification':'event_not_generated' if str(error)=='event_not_generated' else 'acceptance_failure','reason':type(error).__name__+': '+str(error)}
        write_json(work/'diagnostic.json',document);print(json.dumps(document['stop']),file=sys.stderr,flush=True);return 1

def validate_dependencies():
    from importlib.metadata import version
    expected={'kafka-python':'2.2.15','python-snappy':'0.7.3','cramjam':'2.11.0'}
    actual={name:version(name) for name in expected}
    if actual!=expected:raise Incomplete('acceptance observer dependency drift')
    return actual


def select_preflight(work,binding,tools,profile_digest,host_identity):
    matches=[]
    for path in sorted(work.parent.glob('preflight*/diagnostic.json')):
        value=json.loads(path.read_text())
        if value.get('formal') is False and value.get('execution_status')=='complete' and value.get('candidate')==binding and value.get('tools')==tools and value.get('profile_sha256')==profile_digest and value.get('host_identity')==host_identity:
            verify_directory(path.parent,formal=False)
            matches.append(path)
    if not matches:raise Incomplete('same frozen candidate has no complete verified preflight')
    selected=matches[-1]
    return {'path':str(selected),'sha256':digest(selected)}


def run_self_tests(work):
    import io
    import unittest
    loader=unittest.TestLoader()
    suite=loader.loadTestsFromNames(['test_phase20_diagnostic','test_phase20_sampler','test_phase20_evidence'])
    output=io.StringIO();executed=[]
    class Results(unittest.TextTestResult):
        def startTest(self,test):
            self.started=time.monotonic();super().startTest(test)
        def addSuccess(self,test):
            executed.append({'test_id':test.id(),'elapsed_seconds':time.monotonic()-self.started,'result':'pass'})
            super().addSuccess(test)
    result=unittest.TextTestRunner(stream=output,resultclass=Results,verbosity=2).run(suite)
    (work/'self-tests.txt').write_text(output.getvalue());os.chmod(work/'self-tests.txt',0o600)
    if not result.wasSuccessful() or result.skipped:raise Incomplete('deterministic self-tests failed/skipped')
    cases=[]
    for row in executed:
        found=re.search(r'test_(D[0-9]{2})_',row['test_id'])
        if found:cases.append({'case_id':found.group(1),'preconditions':'controlled raw fixture, product dependencies excluded','operation':row['test_id'],'expected':'assertions in bound test source','timeout_seconds':5,'result':row['result'],'elapsed_seconds':row['elapsed_seconds'],'raw_evidence_path':'self-tests.txt','failure_classification':'acceptance_failure'})
    if {c['case_id'] for c in cases}!={f'D{i:02d}' for i in range(1,7)}:raise Incomplete('D01-D06 fixture executions missing')
    write_json(work/'self-tests.json',{'schema':'gopulse.phase20.self-tests.v1','formal':False,'cases':cases,'executed':executed,'raw_sha256':digest(work/'self-tests.txt')})
    return {'path':'self-tests.json','sha256':digest(work/'self-tests.json')}

if __name__=='__main__':sys.exit(main())
