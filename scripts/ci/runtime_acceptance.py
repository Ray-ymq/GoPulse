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
import tarfile
import time
import urllib.request
import urllib.error
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
                assert json.loads(body)['contract_version']=='1'
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
                assert response.code==200 and json.load(response)['contract_version']=='1'
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

if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--candidate',required=True);parser.add_argument('--manifest',type=Path);parser.add_argument('--evidence',type=Path);args=parser.parse_args()
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+',args.candidate):raise SystemExit('invalid candidate')
    acceptance=Acceptance(args.candidate,args.manifest);acceptance.run()
    if args.evidence:
        args.evidence.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
        shutil.copyfile(acceptance.work/'evidence.json',args.evidence);args.evidence.chmod(0o600)
