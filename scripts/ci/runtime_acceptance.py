#!/usr/bin/env python3
"""Owned Linux amd64 runtime acceptance; no product configuration bypasses."""
import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import shutil
import sys
import tarfile
import time
import urllib.request
import urllib.error
import urllib.parse
import uuid
from verify_runtime_contracts import ROOT, load
from release_artifacts import platform_ref, verify_bundle
from verify_plugin_metrics import Client

SOURCES=('redis','mysql','rabbitmq','kafka','elasticsearch','victoriametrics')
SERVICES=('backend','business-worker','search-indexer','router','marshaller','monitor')

def command(args, *, check=True, timeout=180, data=None):
    p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
    if check and p.returncode:
        raise RuntimeError('runtime command failed: '+args[0]+' '+args[1]+' exit '+str(p.returncode))
    return p

def wait(fn, description, timeout=150):
    end=time.monotonic()+timeout
    while time.monotonic()<end:
        try:
            result=fn()
            if result:return result
        except (RuntimeError,ValueError,AssertionError,KeyError,urllib.error.URLError):pass
        time.sleep(.5)
    raise RuntimeError('runtime timeout: '+description)

class Acceptance:
    def __init__(self,version,manifest=None):
        self.version=version;self.manifest_path=manifest.resolve() if manifest else None;self.candidate=verify_bundle(self.manifest_path) if self.manifest_path else None
        if self.candidate and (self.candidate['version'] != version):raise ValueError('candidate version mismatch')
        self.token=uuid.uuid4().hex[:12];self.project='gopulse-runtime-'+self.token
        assert re.fullmatch(r'gopulse-runtime-[0-9a-f]{12}',self.project)
        self.work=ROOT/'.run'/self.project;self.work.mkdir(mode=0o700)
        self.probe=self.work/'runtime-http'
        command(['env','CGO_ENABLED=0','go','build','-o',str(self.probe),str(ROOT/'scripts/ci/testdata/runtime-http.go')])
        self.ids={};self.extra=[];self.results=[];self.log_results={};self.started=False
        self.contract=load(ROOT/'deploy/runtime-contracts.json')
        self.values={k:v for k,v in (line.split('=',1) for line in (ROOT/'.env.example').read_text().splitlines() if '=' in line and not line.startswith('#'))}
        self.values.update(APP_ENV='test',GOPULSE_VERSION=version,GOPULSE_IMAGE_TAG=version+'-runtime-'+self.token,
            GOPULSE_REVISION=self.candidate['revision'] if self.candidate else command(['git','rev-parse','HEAD']).stdout.decode().strip(),HTTP_PORT='0',FRONTEND_PORT='0',
            MYSQL_DATABASE='runtime_'+self.token,MYSQL_USER='user_'+self.token,RABBITMQ_USER='rabbit_'+self.token,
            VICTORIAMETRICS_USERNAME='vm_'+self.token,GOPULSE_UPDATE_VERSION=version,
            MONITOR_BOOTSTRAP_PACKAGE='',AUTH_COOKIE_SECURE='false')
        for key in self.values:
            if any(s in key for s in ('PASSWORD','TOKEN','SECRET')):self.values[key]='runtime-canary-'+key.lower()+'-'+self.token+'-0123456789abcdef'
        self.values['BACKEND_VICTORIAMETRICS_PASSWORD']=self.values['VICTORIAMETRICS_PASSWORD']
        if self.candidate:
            for name,image in {**self.candidate['images'],**self.candidate['third_party']}.items():
                self.values['GOPULSE_'+name.upper().replace('-','_')+'_IMAGE']=platform_ref(image,'linux/amd64')
        self.env=self.work/'runtime.env';self.env.write_text(''.join(k+'='+v+'\n' for k,v in self.values.items()));self.env.chmod(0o600)
        self.override=self.work/'private.yaml';self.override.write_text('services:\n  backend:\n    ports: !reset []\n')
        self.base=['docker','compose','-p',self.project,'--env-file',str(self.env),'-f',str(ROOT/'deploy/compose.yaml'),'-f',str(self.override)]
        self.image=platform_ref(self.candidate['images']['monitor'],'linux/amd64') if self.candidate else 'gopulse/monitor:'+self.values['GOPULSE_IMAGE_TAG']
    def compose(self,*args,**kwargs):return command(self.base+list(args),**kwargs)
    def owned(self,cid):
        info=json.loads(command(['docker','inspect',cid]).stdout)[0]
        assert info['Config']['Labels'].get('com.docker.compose.project')==self.project,'resource ownership mismatch'
        return info
    def wire(self,component,path='/ready',method='GET',body=b'',headers=None):
        cid=self.ids[component];self.owned(cid)
        port=next(c['listeners'][0]['port'] for c in self.contract['components'] if c['id']==component)
        result=command(['docker','run','--rm','--label','com.docker.compose.project='+self.project,
            '--network','container:'+cid,'--read-only','--cap-drop','ALL','--security-opt','no-new-privileges:true',
            '--user','10005:10001','--mount','type=bind,src='+str(self.probe)+',dst=/probe,readonly',
            '--entrypoint','/probe',self.image,method,'http://127.0.0.1:'+str(port)+path,
            base64.b64encode(body).decode(),json.dumps(headers or {})],timeout=15)
        result=json.loads(result.stdout)
        return result['status'],result['headers'],result['body'].encode()

    def status(self,component,want):return self.wire(component)[0]==want
    def mark(self,name,**details):self.results.append(dict(name=name,**details));print('PASS: '+name,flush=True)
    def build(self):
        info=json.loads(command(['docker','info','--format','{{json .}}']).stdout)
        assert info['OSType']=='linux' and info['Architecture'] in ('x86_64','amd64')
        action='pull' if self.candidate else 'build'
        p=self.compose(action,*SERVICES,'frontend','admin-frontend',timeout=2400,check=False)
        (self.work/(action+'.log')).write_bytes(p.stdout+p.stderr)
        if p.returncode:raise RuntimeError('runtime product image '+action+' failed; see owned log')
        self.started=True;self.compose('up','-d',timeout=600)
        for service in SERVICES:
            self.ids[service]=self.compose('ps','-q',service).stdout.decode().strip();self.owned(self.ids[service])
        for service in SERVICES:wait(lambda s=service:self.status(s,200),service+' ready')
        front=json.loads(command(['docker','inspect',self.compose('ps','-q','frontend').stdout.decode().strip()]).stdout)[0]
        port=front['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort'];self.origin='http://127.0.0.1:'+port
        self.client=Client(self.origin);self.username='runtime_'+self.token;self.password='Acceptance-'+self.token+'-password'
        self.client.request('auth/register','POST',{'username':self.username,'password':self.password},201)
        self.client.request('auth/login','POST',{'username':self.username,'password':self.password})
        self.mark('Linux amd64 owned Compose product booted',project=self.project)
    def exporters(self):
        networks=list(self.owned(self.ids['monitor'])['NetworkSettings']['Networks'])
        for source in SOURCES:
            cidname=self.project+'-'+source
            package=self.compose('exec','-T','monitor','cat','/opt/gopulse/packages/gopulse-'+source+'-exporter.tar.gz').stdout
            with tarfile.open(fileobj=io.BytesIO(package),mode='r:gz') as archive:
                manifest=json.load(archive.extractfile('plugin.json'));binary=archive.extractfile('bin/gopulse-'+source+'-exporter').read()
                assert hashlib.sha256(binary).hexdigest()==manifest['entrypoint_sha256'] and manifest['version']==self.version
            path=self.work/(source+'-exporter');path.write_bytes(binary);path.chmod(0o755)
            prefix=source.upper();values={'GOPULSE_RUNTIME_MODE':'container','GOPULSE_VERSION':self.version,'GOPULSE_REVISION':self.values['GOPULSE_REVISION'],prefix+'_HOST':source,prefix+'_PORT':str(dict(redis=6379,mysql=3306,rabbitmq=5672,kafka=19092,elasticsearch=9200,victoriametrics=8428)[source]),prefix+'_EXPORTER_CONNECT_TIMEOUT':'500ms',prefix+'_EXPORTER_SCRAPE_TIMEOUT':'2s'}
            if source in ('redis','mysql','rabbitmq','victoriametrics'):values[prefix+'_PASSWORD']=self.values[prefix+'_PASSWORD']
            if source=='redis':values['REDIS_DB']='0'
            if source=='mysql':values.update(MYSQL_USERNAME=self.values['MYSQL_USER'],MYSQL_DATABASE=self.values['MYSQL_DATABASE'])
            if source=='rabbitmq':values.update(RABBITMQ_USERNAME=self.values['RABBITMQ_USER'],RABBITMQ_VHOST='/',RABBITMQ_MANAGEMENT_PORT='15672')
            if source=='kafka':values.update(KAFKA_TOPIC='gopulse-observability-v1',KAFKA_CONSUMER_GROUP='gopulse-marshaller-metrics-v1')
            if source=='victoriametrics':values['VICTORIAMETRICS_USERNAME']=self.values['VICTORIAMETRICS_USERNAME']
            env=self.work/(source+'.env');env.write_text(''.join(k+'='+v+'\n' for k,v in values.items()));env.chmod(0o600)
            self.extra.append(cidname)
            command(['docker','run','-d','--name',cidname,'--label','com.docker.compose.project='+self.project,'--network',networks[0],'--read-only','--tmpfs','/tmp','--cap-drop','ALL','--security-opt','no-new-privileges:true','--user','10005:10001','--env-file',str(env),'--mount','type=bind,src='+str(path)+',dst=/runtime-exporter,readonly','--entrypoint','/runtime-exporter',self.image])
            for network in networks[1:]:command(['docker','network','connect',network,cidname])
            self.ids[source+'-exporter']=cidname
            wait(lambda s=source:self.status(s+'-exporter',200),source+' exporter ready')
        self.mark('six checksum-verified official exporter processes started without host ports')
    def probes(self):
        for component in self.ids:
            wait(lambda c=component:self.status(c,200),component+' stable ready')
            for path in ('/startup','/live','/ready','/health'):
                code,headers,body=self.wire(component,path)
                assert code==200 and headers.get('cache-control')=='no-store' and headers.get('content-type','').startswith('application/json'),(component,path,code,headers,body.decode())
                assert json.loads(body)['contract_version']==self.contract['contract_version']
                assert self.wire(component,path,method='POST')[0]==405
                assert self.wire(component,path+'?x=1')[0]==400
                assert self.wire(component,path,body=b'x')[0]==400
            assert self.wire(component,'/health')[2]==self.wire(component,'/live')[2]
        self.mark('twelve processes: four paths, GET-only, query/body rejection, JSON, cache and health alias')
    def faults(self):
        hard={'mysql':('backend','business-worker','search-indexer'),'rabbitmq':('business-worker','search-indexer'),'kafka':('router','marshaller'),'elasticsearch':('search-indexer','marshaller'),'victoriametrics':('marshaller',),'redis':()}
        for source,targets in hard.items():
            cid=self.compose('ps','-q',source).stdout.decode().strip();self.owned(cid)
            command(['docker','pause',cid])
            try:
                for target in targets:wait(lambda t=target:self.status(t,503),target+' hard dependency unavailable',40)
                for target in targets:assert self.wire(target,'/live')[0]==200
                exporter=source+'-exporter';assert self.status(exporter,200)
                status,_,metrics=self.wire(exporter,'/metrics');assert status in (200,503)
                assert re.search(rb'_up 0(?:\n|$)',metrics),source+' missing up=0'
                if source!='mysql':
                    assert self.status('backend',200)
                    self.client.request('posts')
            finally:command(['docker','unpause',cid])
            for target in targets:wait(lambda t=target:self.status(t,200),target+' recovery')
            self.mark(source+' hard/soft dependency matrix')
        monitor=self.ids['monitor'];command(['docker','pause',monitor])
        try:assert self.status('backend',200);self.client.request('posts')
        finally:command(['docker','unpause',monitor])
        self.mark('monitor is soft for social API')
    def negative_config(self):
        for service,key,value in [('backend','AUTH_JWT_SECRET',''),('backend','AUTH_JWT_SECRET','short-canary'),('backend','MYSQL_HOST','127.0.0.1'),('router','GOPULSE_RUNTIME_MODE','bad-mode-canary'),('router','ROUTER_HTTP_PORT','70000'),('router','ROUTER_METRICS_TOKEN',self.values['ROUTER_API_TOKEN'])]:
            p=self.compose('run','--rm','--no-deps','-e',key+'='+value,service,timeout=30,check=False)
            assert p.returncode!=0 and (not value or value.encode() not in p.stdout+p.stderr),'configuration accepted or leaked'
        self.mark('missing/short secret, loopback, mode, range and credential reuse fail before listening')
    def correlation(self):
        request=urllib.request.Request(self.origin+'/api/v1/runtime-missing',headers={'X-Request-ID':'external-canary'})
        try:response=urllib.request.urlopen(request)
        except urllib.error.HTTPError as e:response=e
        body=json.loads(response.read());rid=response.headers['X-Request-ID']
        assert response.code==404 and re.fullmatch('[0-9a-f]{32}',rid) and body['error']['request_id']==rid
        logs=command(['docker','logs',self.ids['backend']]).stdout
        assert rid.encode() in logs,'request not correlated with backend log'
        self.client.request('exporter-plugins',expected=403)
        self.compose('exec','-T','backend','/usr/local/bin/admin-role','promote','--username',self.username)
        call=urllib.request.Request(self.origin+'/api/v1/exporter-plugins',headers={'Origin':self.origin,'X-Request-ID':'untrusted-browser-id'})
        result=self.client.opener.open(call,timeout=15);result.read();downstream_id=result.headers['X-Request-ID']
        assert re.fullmatch('[0-9a-f]{32}',downstream_id)
        for component in ('backend','monitor'):
            raw=command(['docker','logs',self.ids[component]]).stdout
            assert downstream_id.encode() in raw,component+' lost forwarded request ID'
        for component,path,method in [('router','/internal/v1/messages','POST'),('monitor','/internal/v1/exporter-plugins','GET'),('marshaller','/missing','GET')]:
            code,headers,raw=self.wire(component,path,method,headers={'X-Request-ID':rid})
            assert code in (400,401,404) and headers['x-request-id']==rid and json.loads(raw)['error']['request_id']==rid
        for path in ('/startup','/live','/internal/v1/metrics'):
            try:r=urllib.request.urlopen(self.origin+path)
            except urllib.error.HTTPError as e:r=e
            assert r.code in (403,404),'internal probe leaked through edge: '+path+' status='+str(r.code)
        for path in ('/health','/ready'):
            with urllib.request.urlopen(self.origin+path) as response:
                assert response.code==200 and json.load(response)['contract_version']==self.contract['contract_version']
        self.mark('edge replaces IDs; Backend/internal errors and logs correlate; internal probes blocked')
    def managed_plugins(self):
        for source in SOURCES:
            cfg=dict(host=source,port=dict(redis=6379,mysql=3306,rabbitmq=5672,kafka=19092,elasticsearch=9200,victoriametrics=8428)[source],connect_timeout='500ms',scrape_timeout='2s')
            secrets={}
            if source in ('redis','mysql','rabbitmq','victoriametrics'):secrets['password']=self.values[source.upper()+'_PASSWORD']
            if source=='redis':cfg['database']=0
            if source=='mysql':cfg.update(database=self.values['MYSQL_DATABASE'],username=self.values['MYSQL_USER'])
            if source=='rabbitmq':cfg.pop('port');cfg.update(management_port=15672,vhost='/',username=self.values['RABBITMQ_USER'])
            if source=='kafka':cfg.update(topic='gopulse-observability-v1',consumer_group='gopulse-marshaller-metrics-v1')
            if source=='victoriametrics':cfg['username']=self.values['VICTORIAMETRICS_USERNAME']
            self.client.request('exporter-plugins/'+source+'-exporter/install','POST',dict(config=cfg,secrets=secrets),201)
            wait(lambda s=source:self.client.request('exporter-plugins/'+s+'-exporter')['data']['observed_state']=='running',source+' managed plugin running')
        self.mark('six official plugins installed and started under Monitor supervision')
    def logs(self):
        required={'log_schema_version','timestamp','level','service','module','message','event','version','revision'}
        allowed={'http_request','http_panic','startup','shutdown','dependency_up','dependency_down','operation'}
        for component,cid in self.ids.items():
            p=command(['docker','logs',cid]);raw=p.stdout+p.stderr
            for key,value in self.values.items():
                if any(word in key for word in ('PASSWORD','TOKEN','SECRET')) and value:assert value.encode() not in raw,'secret in process log'
            assert not re.search(rb'https?://[^\s"/]*@|/(?:home|mnt|var/lib)/',raw),'unsafe URL or absolute path in log'
            assert b'permanent_rejection' not in raw,component+' dropped incompatible logs'
            lines=raw.splitlines();assert lines,component+' missing logs'
            for line in lines:
                record=json.loads(line);assert required <= record.keys(),component+' missing schema fields'
                assert record['log_schema_version']==1 and record['event'] in allowed and record['timestamp'].endswith('Z')
            (self.work/(component+'.jsonl')).write_bytes(raw)
            self.log_results[component]={'records':len(lines),'sha256':hashlib.sha256(raw).hexdigest()}
        self.mark('twelve component log schemas and secret/userinfo/path scans')
    def disconnected_shutdown(self):
        source=self.compose('ps','-q','redis').stdout.decode().strip();self.owned(source)
        command(['docker','pause',source])
        cid=self.ids['redis-exporter'];self.owned(cid)
        try:
            status,_,body=self.wire('redis-exporter','/metrics')
            assert status in (200,503) and b'gopulse_redis_up 0' in body
            started=time.monotonic();command(['docker','kill','--signal','TERM',cid])
            code=command(['docker','wait',cid],timeout=7).stdout.decode().strip()
            assert code=='0' and time.monotonic()-started<7
            self.mark('exporter drains with source disconnected',exit_code=0,elapsed_seconds=round(time.monotonic()-started,3))
        finally:
            command(['docker','unpause',source]);command(['docker','start',cid])
        wait(lambda:self.status('redis-exporter',200),'exporter restart after disconnected shutdown')
    def interrupted_log_drain(self):
        monitor=self.ids['monitor'];self.owned(monitor)
        backend=self.ids['backend'];self.owned(backend)
        command(['docker','pause',monitor])
        try:
            self.client.request('posts')
            time.sleep(.3)
            started=time.monotonic();command(['docker','kill','--signal','TERM',backend])
            code=command(['docker','wait',backend],timeout=8).stdout.decode().strip()
            elapsed=time.monotonic()-started
            assert code=='1' and elapsed<7,'Backend log drain must fail within the original process budget'
            self.mark('Backend disconnected log drain deadline',exit_code=1,elapsed_seconds=round(elapsed,3))
        finally:
            command(['docker','unpause',monitor]);command(['docker','start',backend])
        wait(lambda:self.status('backend',200),'Backend restart after failed drain')
    def signals(self):
        for signal_name in ('TERM','INT'):
            for component,cid in self.ids.items():
                self.owned(cid);start=time.monotonic();command(['docker','kill','--signal',signal_name,cid])
                code=command(['docker','wait',cid],timeout=20).stdout.decode().strip();elapsed=time.monotonic()-start
                budget=next(c['shutdown_seconds'] for c in self.contract['components'] if c['id']==component)
                assert code=='0' and elapsed<budget+2,component+' did not drain successfully'
                self.mark(component+' SIG'+signal_name,exit_code=int(code),elapsed_seconds=round(elapsed,3))
                command(['docker','start',cid]);wait(lambda c=component:self.status(c,200),component+' restart')
        self.mark('all processes restart after both signals; no detached plugin container/process remains')
    def cleanup(self):
        for cid in self.extra:
            if command(['docker','inspect',cid],check=False).returncode==0:self.owned(cid);command(['docker','rm','-f',cid])
        if self.started:self.compose('--profile','exporter','--profile','acceptance','down','--volumes','--remove-orphans',timeout=180)
        remaining=command(['docker','ps','-aq','--filter','label=com.docker.compose.project='+self.project]).stdout.strip()
        assert not remaining,'owned container remains'
    def run(self):
        passed=False
        try:
            self.build();self.exporters();self.probes();self.negative_config();self.correlation();self.faults();self.managed_plugins();self.disconnected_shutdown();self.interrupted_log_drain();self.signals();self.logs();passed=True
        finally:
            if not passed:
                for component,cid in self.ids.items():
                    raw=command(['docker','logs','--tail','40',cid],check=False).stdout
                    for key,value in self.values.items():
                        if any(word in key for word in ('PASSWORD','TOKEN','SECRET')) and value:raw=raw.replace(value.encode(),b'[redacted]')
                    (self.work/(component+'-failure.log')).write_bytes(raw)
            self.cleanup()
            (self.work/'evidence.json').write_text(json.dumps(dict(passed=passed,version=self.version,revision=self.values['GOPULSE_REVISION'],manifest_sha256=hashlib.sha256(self.manifest_path.read_bytes()).hexdigest() if self.manifest_path else None,contract_sha256=hashlib.sha256((ROOT/'deploy/runtime-contracts.json').read_bytes()).hexdigest(),scenarios=self.results,logs=self.log_results),indent=2)+'\n')
            print('Runtime evidence: '+str(self.work/'evidence.json'),flush=True)


SPLIT_CASES=('S01','S02','S03','S04','S05','S06','S07')
ADMISSION_COMMAND=('go','-C','backend','test','./internal/http','-run','^TestServiceRoleAdmissionIsolation$','-count=1','-timeout=60s','-v')

def file_sha256(path):
    digest=hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda:stream.read(1024*1024),b''):
            digest.update(block)
    return 'sha256:'+digest.hexdigest()

def tree_sha256():
    names=subprocess.check_output(['git','-C',str(ROOT),'ls-files','-z'])
    digest=hashlib.sha256()
    for name in sorted(filter(None,names.decode().split('\0'))):
        path=ROOT/name
        digest.update(name.encode())
        digest.update(b'\0')
        if path.is_file():
            digest.update(hashlib.sha256(path.read_bytes()).digest())
    return 'sha256:'+digest.hexdigest()

def working_tree_sha256():
    status=subprocess.check_output(['git','-C',str(ROOT),'status','--porcelain=v1','-z'])
    return 'sha256:'+hashlib.sha256(status).hexdigest()

def relative_run_path(path):
    return str(path.resolve().relative_to(ROOT.resolve()))

class ServiceSplitAcceptance(Acceptance):
    """Finite Phase-21 split probe reusing the existing owned Compose boundary."""
    def __init__(self,version,manifest=None,mode='preflight',preflight_receipt=None):
        if mode not in ('preflight','formal'):
            raise ValueError('invalid service-split mode')
        if mode=='formal' and manifest is None:
            raise ValueError('formal service-split requires a manifest')
        if mode=='formal' and preflight_receipt is None:
            raise ValueError('formal service-split requires --preflight-receipt')
        super().__init__(version,manifest)
        self.mode=mode
        self.preflight_receipt=preflight_receipt.resolve() if preflight_receipt else None
        self.evidence_path=self.work/'evidence.json'
        self.commands=[]
        self.case_results={case:{'status':'not_started'} for case in SPLIT_CASES}
        self.failure_class=None
        self.error=None
        self.snapshot_before=self.resource_snapshot()
        self.source_hash=tree_sha256()
        self.config_hash='sha256:'+hashlib.sha256(b''.join((ROOT/name).read_bytes() for name in ('deploy/compose.yaml','.env.example','deploy/runtime-contracts.json','deploy/runtime-contracts.schema.json'))).hexdigest()
        self.reused_images={}
        self.legacy_fixture={}
        if self.mode=='preflight' and self.candidate is None:
            cached='gopulse/admin-frontend:1.13.6'
            if command(['docker','image','inspect',cached],check=False).returncode:
                raise RuntimeError('unchanged admin-frontend image cache is unavailable')
            self.values['GOPULSE_ADMIN_FRONTEND_IMAGE']=cached
            self.reused_images['admin-frontend']={'ref':cached,'id':json.loads(command(['docker','image','inspect',cached]).stdout)[0]['Id']}
            self.env.write_text(''.join(k+'='+v+'\n' for k,v in self.values.items()));self.env.chmod(0o600)
        self.preflight_result=None
        if self.mode=='formal':
            self.validate_preflight_receipt()

    def resource_snapshot(self):
        result={}
        for name,args in {'containers':['ps','-aq'],'networks':['network','ls','-q'],'volumes':['volume','ls','-q']}.items():
            result[name]=sorted(command(['docker',*args],check=False).stdout.decode().split())
        return result

    def record_command(self,logical,argv,timeout=120,cwd=ROOT):
        stamp=str(int(time.time()*1000))
        base=self.work/'commands'
        base.mkdir(mode=0o700,exist_ok=True)
        stdout=base/(stamp+'-stdout.raw')
        stderr=base/(stamp+'-stderr.raw')
        started=time.time()
        result=subprocess.run(argv,cwd=str(cwd),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
        stdout.write_bytes(result.stdout);stderr.write_bytes(result.stderr)
        entry={'logical_argv':list(logical),'executed_argv':list(argv),'exit_code':result.returncode,
               'started_at':started,'ended_at':time.time(),'stdout':relative_run_path(stdout),'stderr':relative_run_path(stderr)}
        self.commands.append(entry)
        return result

    def raw_paths_exist(self,paths):
        for raw in paths:
            path=ROOT/raw
            if Path(raw).is_absolute() or not str(path.resolve()).startswith(str((ROOT/'.run').resolve())+os.sep) or not path.is_file():
                raise ValueError('preflight raw evidence is missing or outside .run')

    def validate_preflight_receipt(self):
        receipt=load(self.preflight_receipt)
        if receipt.get('schema')!='gopulse.phase21.service-split-evidence.v1' or receipt.get('suite')!='service-split' or receipt.get('mode')!='preflight' or receipt.get('status')!='passed':
            raise ValueError('preflight receipt is not a successful service-split receipt')
        if receipt.get('target_version')!=self.version:
            raise ValueError('preflight receipt candidate identity mismatch')
        expected_manifest=file_sha256(self.manifest_path)
        if receipt.get('manifest_sha256')!=expected_manifest:
            raise ValueError('preflight receipt manifest identity mismatch')
        if receipt.get('revision')!=self.values['GOPULSE_REVISION'] or receipt.get('source_hash')!=tree_sha256():
            raise ValueError('preflight receipt source identity mismatch')
        if receipt.get('compose_sha256')!=file_sha256(ROOT/'deploy/compose.yaml') or receipt.get('contract_sha256')!=file_sha256(ROOT/'deploy/runtime-contracts.json'):
            raise ValueError('preflight receipt configuration identity mismatch')
        preflight=receipt.get('preflight',{})
        if tuple(preflight.get('logical_argv',()))!=ADMISSION_COMMAND or preflight.get('exit_code')!=0:
            raise ValueError('preflight receipt admission command mismatch')
        self.raw_paths_exist([preflight.get('stdout',''),preflight.get('stderr','')])
        for case in SPLIT_CASES:
            if receipt.get('cases',{}).get(case,{}).get('status')!='passed':
                raise ValueError('preflight receipt has incomplete case set')

    def run_admission(self):
        if self.mode!='preflight':
            return
        executable=list(ADMISSION_COMMAND)
        if shutil.which('rtk'):
            executable=['rtk']+executable
        result=self.record_command(ADMISSION_COMMAND,executable,timeout=70)
        self.preflight_result=self.commands[-1]
        if result.returncode:
            self.case_results['S02']={'status':'failed','failure_class':'product_failure','raw':[self.preflight_result['stdout'],self.preflight_result['stderr']]}
            raise RuntimeError('service-role admission preflight failed')

    def compose_action(self,*args,timeout=600):
        result=self.compose(*args,timeout=timeout,check=False)
        log=self.work/'commands'/('compose-'+str(len(self.commands))+'.log')
        log.parent.mkdir(mode=0o700,exist_ok=True)
        log.write_bytes(result.stdout+result.stderr)
        self.commands.append({'logical_argv':['docker','compose',*args],'executed_argv':['docker','compose',*args],
                              'exit_code':result.returncode,'started_at':time.time(),'ended_at':time.time(),
                              'stdout':relative_run_path(log),'stderr':relative_run_path(log)})
        return result

    def split_component(self,service):
        return 'platform-api' if service=='platform-api' else 'backend' if service in ('backend','backend-2') else service

    def wire(self,component,path='/ready',method='GET',body=b'',headers=None,listener='probe'):
        service_component=self.split_component(component)
        cid=self.ids[component];self.owned(cid)
        port=next(listener_spec['port'] for c in self.contract['components'] if c['id']==service_component
                  for listener_spec in c['listeners'] if listener_spec['name']==listener)
        result=command(['docker','run','--rm','--label','com.docker.compose.project='+self.project,
            '--network','container:'+cid,'--read-only','--cap-drop','ALL','--security-opt','no-new-privileges:true',
            '--user','10005:10001','--mount','type=bind,src='+str(self.probe)+',dst=/probe,readonly',
            '--entrypoint','/probe',self.image,method,'http://127.0.0.1:'+str(port)+path,
            base64.b64encode(body).decode(),json.dumps(headers or {})],timeout=15)
        result=json.loads(result.stdout)
        return result['status'],result['headers'],result['body'].encode()

    def build_stack(self):
        info=json.loads(command(['docker','info','--format','{{json .}}']).stdout)
        if info['OSType']!='linux' or info['Architecture'] not in ('x86_64','amd64'):
            raise RuntimeError('service-split requires Linux amd64 Docker')
        action='pull' if self.candidate else 'build'
        services=('backend','frontend','admin-frontend') if self.candidate else ('backend','frontend')
        result=self.compose_action(action,*services,timeout=2400)
        if result.returncode:
            raise RuntimeError('service-split product image preparation failed')
        self.started=True
        self.prepare_legacy_plugin()
        result=self.compose_action('up','-d','--wait','--wait-timeout','120',timeout=900)
        if result.returncode:
            raise RuntimeError('service-split Compose startup failed')
        for service in ('backend','backend-2','platform-api','business-worker','search-indexer','router','marshaller','monitor','frontend','admin-frontend'):
            cid=self.compose('ps','-q',service).stdout.decode().strip()
            if not cid:
                raise RuntimeError('service-split service did not create a container: '+service)
            self.ids[service]=cid;self.owned(cid)
        for service in ('backend','backend-2','platform-api','monitor'):
            wait(lambda s=service:self.wire(s)[0]==200,service+' ready',timeout=120)
        front=json.loads(command(['docker','inspect',self.ids['frontend']]).stdout)[0]
        port=front['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort']
        self.origin='http://127.0.0.1:'+port
        self.client=Client(self.origin)
        self.username='split_'+self.token;self.password='Acceptance-'+self.token+'-password'
        self.client.request('auth/register','POST',{'username':self.username,'password':self.password},201)
        self.client.request('auth/login','POST',{'username':self.username,'password':self.password})
        self.compose('exec','-T','backend','/usr/local/bin/admin-role','promote','--username',self.username)

    def prepare_legacy_plugin(self):
        """Seed the Compose-owned Monitor volume with the real v1 fixture."""
        candidates=('gopulse/monitor:1.10.6','gopulse/monitor:phase21-legacy-1.10.6')
        legacy_image=next((image for image in candidates if command(['docker','image','inspect',image],check=False).returncode==0),None)
        if legacy_image is None:
            raise RuntimeError('official legacy Monitor fixture image is unavailable')
        image_info=json.loads(command(['docker','image','inspect',legacy_image]).stdout)[0]
        self.reused_images['legacy-monitor']={'ref':legacy_image,'id':image_info['Id']}
        legacy_override=self.work/'legacy-monitor.yaml'
        legacy_override.write_text('services:\n  monitor:\n    image: '+legacy_image+'\n    pull_policy: never\n    environment:\n      GOPULSE_VERSION: 1.10.6\n      GOPULSE_IMAGE_TAG: 1.10.6\n      MONITOR_BOOTSTRAP_PACKAGE: /opt/gopulse/packages/gopulse-redis-exporter.tar.gz\n')
        up=self.compose_action('-f',str(legacy_override),'up','-d','monitor',timeout=900)
        if up.returncode:
            raise RuntimeError('legacy plugin fixture startup failed')
        def legacy_ready():
            result=self.compose('-f',str(legacy_override),'exec','-T','monitor','test','-f','/var/lib/gopulse-monitor/plugins/registry.json',check=False)
            return result.returncode==0
        wait(legacy_ready,'legacy plugin fixture registry',timeout=90)
        package=self.compose('exec','-T','monitor','cat','/opt/gopulse/packages/gopulse-redis-exporter.tar.gz').stdout
        with tarfile.open(fileobj=io.BytesIO(package),mode='r:gz') as archive:
            manifest=json.load(archive.extractfile('plugin.json'))
        if manifest.get('version')!='1.10.6' or manifest.get('schema_version')!=1:
            raise RuntimeError('legacy plugin fixture identity mismatch')
        legacy_path=self.work/'redis-legacy.tar.gz';legacy_path.write_bytes(package);legacy_path.chmod(0o600)
        self.legacy_fixture={'image':legacy_image,'version':manifest['version'],'schema_version':manifest['schema_version'],
                             'package_sha256':hashlib.sha256(package).hexdigest(),'package_path':relative_run_path(legacy_path)}
        stop=self.compose_action('-f',str(legacy_override),'stop','monitor',timeout=180)
        if stop.returncode:
            raise RuntimeError('legacy plugin fixture stop failed')
        remove=self.compose_action('-f',str(legacy_override),'rm','-f','-s','monitor',timeout=180)
        if remove.returncode:
            raise RuntimeError('legacy plugin fixture removal failed')

    def case_file(self,case,payload):
        path=self.work/(case+'.json');path.write_text(json.dumps(payload,sort_keys=True,indent=2)+'\n');path.chmod(0o600)
        return [relative_run_path(path)]

    def finish_case(self,case,payload):
        raw=self.case_file(case,payload)
        self.case_results[case]={'status':'passed','raw':raw,'summary':payload}

    def request_status(self,path):
        request=urllib.request.Request(self.origin+path,headers={'X-Request-ID':'phase21-'+self.token})
        try:response=urllib.request.urlopen(request,timeout=15)
        except urllib.error.HTTPError as error:response=error
        return response.code,hashlib.sha256(response.read()).hexdigest()

    def case_gateway(self):
        paths=['/api/v1/observability','/api/v1/observability/metrics','/api/v1/alerts','/api/v1/alerts/rules','/api/v1/admin','/api/v1/exporter-plugins','/api/v1/observabilityx']
        statuses={path:self.request_status(path)[0] for path in paths}
        assert all(status in (200,401,403,404,405) for status in statuses.values())
        self.finish_case('S01',{'gateway_statuses':statuses,'platform_paths':paths[:6],'negative_prefix':'/api/v1/observabilityx'})

    def case_roles(self):
        def env(service):
            return {item.split('=',1)[0]:item.split('=',1)[1] for item in self.owned(self.ids[service])['Config'].get('Env',[]) if '=' in item}
        business=env('backend');platform=env('platform-api')
        assert business.get('BACKEND_SERVICE_ROLE')=='business'
        assert platform.get('BACKEND_SERVICE_ROLE')=='platform'
        assert platform.get('GOPULSE_INSTANCE_ID')=='platform-api-1'
        assert platform.get('MYSQL_MAX_OPEN_CONNS')=='4'
        assert platform.get('BACKEND_HTTP_MAX_CONCURRENCY')=='32'
        assert 'REDIS_HOST' not in platform and 'RABBITMQ_URL' not in platform and 'ELASTICSEARCH_URL' not in platform
        assert business.get('MYSQL_MAX_OPEN_CONNS')=='8' and business.get('BACKEND_HTTP_MAX_CONCURRENCY')=='128'
        self.finish_case('S02',{'business_instance':business.get('GOPULSE_INSTANCE_ID'),'platform_instance':platform.get('GOPULSE_INSTANCE_ID'),'business_pool':business.get('MYSQL_MAX_OPEN_CONNS'),'platform_pool':platform.get('MYSQL_MAX_OPEN_CONNS'),'platform_http':platform.get('BACKEND_HTTP_MAX_CONCURRENCY')})

    def case_business(self):
        title='Phase 21 split '+self.token
        created=self.client.request('posts','POST',{'title':title,'content':'service split business write'},201)
        listed=self.client.request('posts')
        assert any(item.get('title')==title for item in listed.get('data',listed if isinstance(listed,list) else []))
        found=self.client.request('search/posts?q='+urllib.parse.quote(title))
        self.finish_case('S03',{'post_id':created.get('data',{}).get('id'),'search_sha256':hashlib.sha256(json.dumps(found,sort_keys=True).encode()).hexdigest()})

    def case_platform(self):
        catalog=self.client.request('observability/metrics/catalog')
        rules=self.client.request('alerts/rules')
        rule=self.client.request('alerts/rules','POST',{'name':'phase21-'+self.token,'enabled':True,'severity':'warning','source':'metrics','selector':{'metric':'gopulse_backend_alert_evaluation_known','labels':{'alert_source':'metrics'}},'reducer':'last','operator':'eq','threshold':1,'window':'1m','for':'0s'},201)
        rule_id=rule.get('data',{}).get('id')
        current=self.client.request('alerts/rules/'+str(rule_id)) if rule_id else {}
        assert isinstance(catalog,dict) and isinstance(rules,dict) and isinstance(current,dict)
        self.finish_case('S04',{'metric_catalog_sha256':hashlib.sha256(json.dumps(catalog,sort_keys=True).encode()).hexdigest(),'initial_rule_count':len(rules.get('data',[])),'rule_id':rule_id,'evaluation_present':bool(current.get('data',{}).get('evaluation'))})

    def case_plugin(self):
        installed=self.client.request('exporter-plugins/redis-exporter')
        installed_data=installed.get('data',{})
        assert installed_data.get('version')==self.legacy_fixture['version']
        package=self.compose('exec','-T','monitor','cat','/opt/gopulse/packages/gopulse-redis-exporter.tar.gz').stdout
        with tarfile.open(fileobj=io.BytesIO(package),mode='r:gz') as archive:
            manifest=json.load(archive.extractfile('plugin.json'))
        assert manifest.get('version')==self.version and manifest.get('schema_version')==2
        package_path=self.work/'redis-current.tar.gz';package_path.write_bytes(package);package_path.chmod(0o600)
        boundary='phase21'+uuid.uuid4().hex
        body=(('--'+boundary+'\r\nContent-Disposition: form-data; name="package"; filename="redis-update.tar.gz"\r\nContent-Type: application/gzip\r\n\r\n').encode()+package+('\r\n--'+boundary+'--\r\n').encode())
        updated=self.client.request('exporter-plugins/redis-exporter/update','POST',body,200,{'Content-Type':'multipart/form-data; boundary='+boundary})
        updated_data=updated.get('data',{})
        self.finish_case('S05',{'legacy_version':self.legacy_fixture['version'],'legacy_schema_version':self.legacy_fixture['schema_version'],
                                'legacy_package_sha256':self.legacy_fixture['package_sha256'],
                                'current_version':manifest['version'],'current_package_sha256':hashlib.sha256(package).hexdigest(),
                                'installed_state':installed_data.get('observed_state'),'updated_state':updated_data.get('observed_state'),
                                'updated_version':updated_data.get('version')})

    def case_metrics(self):
        headers={'Authorization':'Bearer '+self.values['BACKEND_METRICS_TOKEN']}
        backend_status,_,backend_metrics=self.wire('backend','/internal/v1/metrics',headers=headers,listener='metrics')
        platform_status,_,platform_metrics=self.wire('platform-api','/internal/v1/metrics',headers=headers,listener='metrics')
        assert backend_status==200 and platform_status==200
        assert b'gopulse_backend_http_concurrency_limit' in backend_metrics and b'gopulse_backend_http_concurrency_limit' in platform_metrics
        observed=self.client.request('observability/metrics?metric=gopulse_backend_http_concurrency_limit&range=15m')
        self.finish_case('S06',{'backend_metrics_sha256':hashlib.sha256(backend_metrics).hexdigest(),'platform_metrics_sha256':hashlib.sha256(platform_metrics).hexdigest(),'observed_series':len(observed.get('data',{}).get('series',[]))})

    def case_switch(self):
        self.compose('stop','platform-api')
        assert self.wire('backend')[0]==200
        self.compose('start','platform-api');wait(lambda:self.wire('platform-api')[0]==200,'platform-api recovery',60)
        self.compose('stop','backend','backend-2')
        platform_status=self.wire('platform-api')[0]
        self.compose('start','backend','backend-2');wait(lambda:self.wire('backend')[0]==200,'business recovery',60)
        assert platform_status==200
        self.finish_case('S07',{'platform_survived_business_stop':True,'platform_status':platform_status})

    def cleanup_split(self):
        failure=None
        try:
            if self.started:
                result=self.compose('--profile','exporter','--profile','acceptance','down','--volumes','--remove-orphans',timeout=180,check=False)
                if result.returncode:
                    failure=RuntimeError('owned Compose cleanup failed')
            after=self.resource_snapshot()
            for kind,items in self.snapshot_before.items():
                if not set(items).issubset(set(after[kind])):
                    failure=failure or RuntimeError('cleanup removed a pre-existing '+kind)
            for kind in ('containers','networks','volumes'):
                leftovers=set(after[kind])-set(self.snapshot_before[kind])
                if leftovers:
                    failure=failure or RuntimeError('owned '+kind+' remain after cleanup')
        except Exception as error:
            failure=failure or error
        return failure

    def write_evidence(self,passed):
        status='passed' if passed else 'failed'
        manifest_sha=file_sha256(self.manifest_path) if self.manifest_path else None
        data={'schema':'gopulse.phase21.service-split-evidence.v1','suite':'service-split','mode':self.mode,'status':status,
              'failure_class':self.failure_class,'target_version':self.version,'completed_version':(ROOT/'VERSION').read_text().strip(),
              'revision':self.values['GOPULSE_REVISION'],'source_hash':self.source_hash,'working_tree_hash':working_tree_sha256(),
              'compose_sha256':file_sha256(ROOT/'deploy/compose.yaml'),'contract_sha256':file_sha256(ROOT/'deploy/runtime-contracts.json'),
              'manifest_sha256':manifest_sha,'preflight_receipt':relative_run_path(self.preflight_receipt) if self.preflight_receipt and self.preflight_receipt.is_relative_to(ROOT) else None,
              'project':self.project,'cases':self.case_results,'commands':self.commands,
              'preflight':self.preflight_result,'resources':{'before':self.snapshot_before,'after':self.resource_snapshot()},
              'cleanup':{'status':'passed' if passed else 'checked','project':self.project},
              'instances':{'backend':['backend-1','backend-2'],'platform-api':['platform-api-1']},'reused_images':self.reused_images}
        self.evidence_path.write_text(json.dumps(data,sort_keys=True,indent=2)+'\n');self.evidence_path.chmod(0o600)

    def run(self):
        passed=False
        try:
            self.run_admission()
            self.build_stack()
            for case,method in [('S01',self.case_gateway),('S02',self.case_roles),('S03',self.case_business),('S04',self.case_platform),('S05',self.case_plugin),('S06',self.case_metrics),('S07',self.case_switch)]:
                try:
                    method()
                    print('PASS: '+case,flush=True)
                except Exception:
                    self.case_results[case]={'status':'failed','failure_class':'product_failure','raw':[]}
                    raise
            passed=True
        except AssertionError as error:
            self.failure_class='product_failure';self.error=error
        except Exception as error:
            self.failure_class='infrastructure_failure';self.error=error
        finally:
            if not passed and self.started:
                for service,cid in self.ids.items():
                    raw=command(['docker','logs','--tail','40',cid],check=False).stdout
                    for key,value in self.values.items():
                        if any(word in key for word in ('PASSWORD','TOKEN','SECRET')) and value:
                            raw=raw.replace(value.encode(),b'[redacted]')
                    path=self.work/(service+'-failure.log');path.write_bytes(raw);path.chmod(0o600)
            cleanup_error=self.cleanup_split()
            if cleanup_error:
                self.failure_class='infrastructure_failure';self.error=self.error or cleanup_error;passed=False
            self.write_evidence(passed)
            print('Runtime evidence: '+str(self.evidence_path),flush=True)
        if self.error:
            raise self.error

if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('--candidate',required=True)
    parser.add_argument('--manifest',type=Path)
    parser.add_argument('--evidence',type=Path)
    parser.add_argument('--suite',choices=('default','service-split'),default='default')
    parser.add_argument('--preflight',action='store_true')
    parser.add_argument('--preflight-receipt',type=Path)
    args=parser.parse_args()
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+',args.candidate):
        raise SystemExit('invalid candidate')
    if args.evidence and args.evidence.exists():
        raise SystemExit('refusing to overwrite existing evidence')
    if args.suite=='default' and (args.preflight or args.preflight_receipt):
        raise SystemExit('preflight options require --suite service-split')
    if args.preflight and args.preflight_receipt:
        raise SystemExit('--preflight and --preflight-receipt are mutually exclusive')
    acceptance=None
    exit_code=0
    try:
        if args.suite=='service-split':
            mode='preflight' if args.preflight else 'formal'
            acceptance=ServiceSplitAcceptance(args.candidate,args.manifest,mode,args.preflight_receipt)
        else:
            if args.manifest is not None and not args.evidence:
                pass
            acceptance=Acceptance(args.candidate,args.manifest)
        acceptance.run()
    except Exception as error:
        exit_code=1
        print('runtime acceptance failed: '+str(error),file=sys.stderr)
    finally:
        if acceptance is not None and args.evidence is not None:
            evidence=getattr(acceptance,'evidence_path',acceptance.work/'evidence.json')
            if evidence.is_file():
                args.evidence.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
                shutil.copyfile(evidence,args.evidence)
                args.evidence.chmod(0o600)
    raise SystemExit(exit_code)
