#!/usr/bin/env python3
"""Phase-14 focused acceptance. All mutable Docker resources belong to one run.

Requires the current backend/frontend/router/marshaller and monitor-acceptance
images tagged 1.11.1, and the proven Phase 13 1.10.6 business/acceptance images.
No application or release-verification bypass is enabled by this harness.
"""
import argparse
import datetime
import http.cookiejar
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
VERSION = '1.11.1'
PATTERN = re.compile(r'^gopulse-p1401-[a-f0-9]{12}$')


def command(args, *, data=None, check=True, timeout=240):
    result = subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if check and result.returncode:
        # Do not echo command arguments or arbitrary process output: they may
        # contain candidate credentials. Diagnostics are fixed and local.
        raise RuntimeError('command failed: ' + args[0] + ' ' + args[1] + '; exit ' + str(result.returncode))
    return result


def wait_until(fn, description, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            value = fn()
            if value:
                return value
        except (AssertionError, urllib.error.URLError, RuntimeError, KeyError, ValueError):
            pass
        time.sleep(2)
    raise RuntimeError('timed out: ' + description)


class Client:
    def __init__(self, base):
        self.base = base
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(self, path, method='GET', body=None, expected=200, headers=None):
        headers = dict(headers or {})
        headers['Origin'] = self.base
        if body is not None and not isinstance(body, bytes):
            body = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(self.base + '/api/v1/' + path, data=body, method=method, headers=headers)
        try:
            response = self.opener.open(req, timeout=40)
        except urllib.error.HTTPError as error:
            response = error
        raw = response.read()
        assert response.code == expected, f'{method} {path}: expected {expected}, got {response.code}'
        return json.loads(raw) if raw else None


class Acceptance:
    def __init__(self):
        self.token = uuid.uuid4().hex[:12]
        self.project = 'gopulse-p1401-' + self.token
        assert PATTERN.fullmatch(self.project)
        self.work = ROOT / '.run' / self.project
        self.work.mkdir(parents=True, mode=0o700)
        self.env_file = self.work / 'acceptance.env'
        self.override_file = self.work / 'override.json'
        self.secret = 'phase14-secret-canary-' + self.token
        self.auth_password = 'Acceptance-' + self.token + '-pass'
        self.admin_name = 'admin_' + self.token
        self.user_name = 'user_' + self.token
        self.started = False
        self.passed = []
        self.snapshot = {kind: set(command(['docker', *args]).stdout.decode().split()) for kind, args in {
            'container': ['ps', '-aq'], 'network': ['network', 'ls', '-q'], 'volume': ['volume', 'ls', '-q']}.items()}
        values = dict(APP_ENV='test', PUBLISHED_HOST='127.0.0.1', HTTP_PORT='0', FRONTEND_PORT='0',
            MYSQL_DATABASE='gopulse_'+self.token, MYSQL_USER='user_'+self.token, MYSQL_PASSWORD='mysql-'+self.token,
            MYSQL_ROOT_PASSWORD='root-'+self.token, REDIS_PASSWORD=self.secret, REDIS_DB='0',
            RABBITMQ_USER='rabbit_'+self.token, RABBITMQ_PASSWORD='rabbit-'+self.token,
            AUTH_JWT_SECRET='jwt-'+self.token+'-0123456789abcdef0123456789abcdef', AUTH_COOKIE_NAME='gopulse_'+self.token,
            AUTH_COOKIE_SECURE='false', MONITOR_API_TOKEN='monitor-'+self.token+'-012345678901234567890123456789',
            LOG_MONITOR_INGEST_TOKEN='logs-'+self.token+'-012345678901234567890123456789',
            ROUTER_API_TOKEN='router-'+self.token+'-012345678901234567890123456789',
            MARSHALLER_API_TOKEN='marshaller-'+self.token+'-012345678901234567890123456789',
            VICTORIAMETRICS_USERNAME='vm_'+self.token, VICTORIAMETRICS_PASSWORD='vm-'+self.token+'-012345678901234567890123456789',
            GOPULSE_VERSION=VERSION, GOPULSE_REVISION=command(['git','-C',str(ROOT),'rev-parse','HEAD']).stdout.decode().strip(),
            GOPULSE_IMAGE_TAG='1.10.6', GOPULSE_UPDATE_VERSION='1.11.3', MONITOR_SCRAPE_INTERVAL='2s', MONITOR_SCRAPE_TIMEOUT='1s')
        self.env_file.write_text(''.join(f'{k}={v}\n' for k,v in values.items()))
        self.env_file.chmod(0o600)
        self.override = {'services': {service: {'image': f'gopulse/{service}:{VERSION}'} for service in ['backend','frontend','router','marshaller']},
                         'volumes': {'p14_stopped': {}, 'p14_empty': {}}}
        self.switch('1.10.6', 'monitor_plugin_data', bootstrap=True)

    def compose(self, *args, data=None, check=True, timeout=240):
        return command(['docker','compose','--project-name',self.project,'--env-file',str(self.env_file),
                        '-f',str(ROOT/'deploy/compose.yaml'),'-f',str(self.override_file),*args],data=data,check=check,timeout=timeout)

    def switch(self, image, volume, bootstrap=False):
        self.override['services']['monitor'] = {'image': 'gopulse/monitor:'+image if image=='1.10.6' else 'gopulse/monitor-acceptance:'+VERSION,
            'environment': {'MONITOR_BOOTSTRAP_PACKAGE': '/opt/gopulse/packages/gopulse-redis-exporter.tar.gz' if bootstrap else ''},
            'volumes': [volume+':/var/lib/gopulse-monitor/plugins']}
        self.override_file.write_text(json.dumps(self.override))
        if self.started:
            self.compose('up','-d','--no-build','--no-deps','--force-recreate','monitor')
            self.healthy('monitor')

    def owned_id(self, service):
        cid = self.compose('ps','-q',service).stdout.decode().strip()
        assert cid and '\n' not in cid
        info = json.loads(command(['docker','inspect',cid]).stdout)[0]
        assert info['Config']['Labels']['com.docker.compose.project']==self.project
        assert info['Config']['Labels']['com.docker.compose.service']==service
        return cid

    def healthy(self, service):
        def ready():
            info=json.loads(command(['docker','inspect',self.owned_id(service)]).stdout)[0]
            return info['State']['Status']=='running' and info['State'].get('Health',{}).get('Status')=='healthy'
        wait_until(ready,service+' health',180)

    def internal(self, path='', method='GET'):
        self.owned_id('monitor')
        args=['exec','-T','monitor','sh','-c',
            'wget -q -O - --header "Authorization: Bearer $MONITOR_API_TOKEN" '+
            ('--post-data "" ' if method=='POST' else '')+'http://127.0.0.1:9090/internal/v1/exporter-plugins'+path]
        return json.loads(self.compose(*args).stdout)

    def file(self,path):
        self.owned_id('monitor')
        return self.compose('exec','-T','monitor','cat','/var/lib/gopulse-monitor/plugins/'+path).stdout

    def status(self):
        return self.internal('/redis-exporter')['data']

    def upload(self, filename, expected=200):
        raw=self.compose('exec','-T','monitor','cat','/opt/gopulse/packages/'+filename).stdout
        return self.upload_bytes(raw,expected)

    def upload_bytes(self,raw,expected):
        boundary='p14'+uuid.uuid4().hex
        data=(f'--{boundary}\r\nContent-Disposition: form-data; name="package"; filename="plugin.tar.gz"\r\nContent-Type: application/gzip\r\n\r\n'.encode()+raw+f'\r\n--{boundary}--\r\n'.encode())
        return self.admin.request('exporter-plugins/redis-exporter/update','POST',data,expected,{'Content-Type':'multipart/form-data; boundary='+boundary})

    def metric(self,name='gopulse_redis_up'):
        result=self.admin.request('observability/metrics?metric='+name+'&range=15m')['data']
        assert result['series'] and all(set(series['labels']) <= {'db','mode'} for series in result['series'])
        points=[p for series in result['series'] for p in series['points']]
        assert points
        return points

    def mark(self,message):
        print('PASS: '+message,flush=True)
        self.passed.append(message)
        (self.work/'results.json').write_text(json.dumps({'passed':self.passed},indent=2))

    def run(self):
        for image in [f'gopulse/{s}:{VERSION}' for s in ['backend','frontend','router','marshaller','monitor-acceptance']]+['gopulse/monitor:1.10.6','gopulse/acceptance:1.10.6']:
            command(['docker','image','inspect',image])
        self.started=True
        self.compose('up','-d','--no-build',timeout=300)
        for service in ['frontend','backend','monitor','marshaller']:
            self.healthy(service)
        port=self.compose('port','frontend','8080').stdout.decode().strip()
        assert re.fullmatch(r'127\.0\.0\.1:[0-9]+',port)
        base='http://'+port
        self.admin,self.user=Client(base),Client(base)
        for client,name in [(self.admin,self.admin_name),(self.user,self.user_name)]:
            client.request('auth/register','POST',{'username':name,'password':self.auth_password},201)
        self.compose('exec','-T','backend','/usr/local/bin/admin-role','promote','--username',self.admin_name)
        before=wait_until(lambda:self.status() if self.status()['last_success_at'] else None,'legacy scrape')
        old_points=wait_until(self.metric,'legacy Backend series')
        old_last=old_points[-1]['timestamp']
        legacy=self.file('registry.json')
        assert self.secret.encode() not in legacy
        (self.work/'legacy-running-registry.json').write_bytes(legacy)
        self.switch(VERSION,'monitor_plugin_data')
        migrated=self.status()
        for key in ['version','installed_at','updated_at','desired_state']:
            assert migrated[key]==before[key],key
        assert migrated['version']=='1.10.6' and migrated['observed_state']=='running'
        candidate={'config':{'host':'redis','port':6379,'database':0,'connect_timeout':'500ms','scrape_timeout':'1s'},'secrets':{'password':self.secret}}
        self.admin.request('exporter-plugins/redis-exporter/configuration','PUT',candidate,409)
        self.upload('gopulse-redis-exporter.tar.gz')
        assert self.status()['version']==VERSION
        points=wait_until(lambda:(p if p[-1]['timestamp']>old_last else None) if (p:=self.metric()) else None,'v1 and v2 same series range',90)
        assert any(p['timestamp']<=old_last for p in points) and any(p['timestamp']>old_last for p in points)
        self.mark('running legacy migration preserves version/times/state; explicit v2 upgrade; v1/v2 Backend series continuity')
        old_active=self.file('redis-exporter/active.json')
        self.upload('redis-failure.tar.gz',422)
        assert self.file('redis-exporter/active.json')==old_active and self.status()['observed_state']=='running'
        raw=self.compose('exec','-T','monitor','cat','/opt/gopulse/packages/gopulse-redis-exporter.tar.gz').stdout
        self.upload_bytes(raw+b'not-registered',400)
        assert self.file('redis-exporter/active.json')==old_active
        bad={'config':candidate['config'],'secrets':{'password':'wrong-secret-canary-'+self.token}}
        self.admin.request('exporter-plugins/redis-exporter/configuration','PUT',bad,422)
        assert self.file('redis-exporter/active.json')==old_active and self.status()['observed_state']=='running'
        self.mark('registered candidate/config failure restores old active and process; unregistered upload rejected')
        self.switch('1.10.6','p14_stopped',bootstrap=True)
        self.internal('/redis-exporter/stop','POST')
        stopped=self.status()
        (self.work/'legacy-stopped-registry.json').write_bytes(self.file('registry.json'))
        self.switch(VERSION,'p14_stopped')
        migrated=self.status()
        for key in ['version','installed_at','updated_at','desired_state']:
            assert migrated[key]==stopped[key],key
        assert migrated['observed_state']=='stopped' and migrated['started_at'] is None
        self.upload('gopulse-redis-exporter.tar.gz')
        self.upload('redis-update.tar.gz')
        assert self.status()['observed_state']=='stopped'
        assert self.compose('exec','-T','monitor','test','!','-e','/var/lib/gopulse-monitor/plugins/redis-exporter/runtime/process.json',check=False).returncode==0
        self.mark('stopped legacy migration preserves original state; stopped v2 updates leave no process')
        self.switch(VERSION,'p14_empty')
        assert self.internal()['data']==[]
        for request,expected in [(candidate,200),(bad,422)]:
            self.admin.request('exporter-plugins/redis-exporter/connection-test','POST',request,expected)
            assert self.internal()['data']==[]
            assert self.compose('exec','-T','monitor','test','!','-e','/var/lib/gopulse-monitor/plugins/redis-exporter',check=False).returncode==0
        self.mark('empty-volume successful/failed connection-test has no plugin persistence')
        # Browser performs the actual schema-based installation and clears Secret inputs.
        self.browser()
        self.admin.request('exporter-plugins/redis-exporter/install','POST',candidate,409)
        self.admin.request('exporter-plugins/redis-exporter/stop','POST')
        self.admin.request('exporter-plugins/redis-exporter/stop','POST')
        self.admin.request('exporter-plugins/redis-exporter/start','POST')
        self.admin.request('exporter-plugins/redis-exporter/start','POST')
        process=json.loads(self.file('redis-exporter/runtime/process.json'))
        self.compose('stop','redis')
        def down():
            raw=self.compose('exec','-T','monitor','wget','-q','-O','-','http://127.0.0.1:9121/metrics',check=False)
            # HTTP 503 is intentional; live process/health is the readiness fact.
            return self.compose('exec','-T','monitor','wget','-q','-O','-','http://127.0.0.1:9121/health',check=False).returncode==0
        assert down()
        wait_until(lambda:any(p['value']==0 for p in self.metric()),'target unavailable metric',75)
        assert json.loads(self.file('redis-exporter/runtime/process.json'))['pid']==process['pid']
        self.compose('start','redis');self.healthy('redis')
        wait_until(lambda:self.metric()[-1]['value']==1,'target recovery metric',75)
        assert json.loads(self.file('redis-exporter/runtime/process.json'))['pid']==process['pid']
        families=['up','uptime_seconds','connected_clients','used_memory_bytes','commands_processed_total','keyspace_hits_total','keyspace_misses_total','cpu_seconds_total','db_keys','db_expiring_keys']
        for family in families:
            wait_until(lambda:self.metric('gopulse_redis_'+family),'Redis family '+family)
        assert self.metric('gopulse_redis_used_memory_bytes')[-1]['value']>0
        self.mark('idempotent lifecycle; target failure/recovery keeps exporter PID; all ten real Redis families queried via Backend')
        routes=[('POST','exporter-plugins/install'),('GET','observability/metrics/catalog'),('GET','exporter-plugins/catalog'),('GET','exporter-plugins'),('GET','exporter-plugins/redis-exporter'),('POST','exporter-plugins/redis-exporter/install'),('POST','exporter-plugins/redis-exporter/connection-test'),('PUT','exporter-plugins/redis-exporter/configuration'),('POST','exporter-plugins/redis-exporter/start'),('POST','exporter-plugins/redis-exporter/stop'),('POST','exporter-plugins/redis-exporter/update'),('GET','observability/metrics?metric=gopulse_redis_up&range=15m')]
        for method,path in routes:self.user.request(path,method,expected=403)
        catalog=self.admin.request('observability/metrics/catalog')['data']
        assert len(catalog)==10 and all(item['source']=='redis' and item['producer_kind']=='exporter_plugin' and item['producer_id']=='redis-exporter' for item in catalog)
        post=self.admin.request('posts','POST',{'title':'phase14 '+self.token,'content':'Phase 13 social search regression '+self.token},201)['data']
        self.admin.request('posts/'+str(post['id']))
        wait_until(lambda:self.admin.request('search/posts?q='+self.token)['data'],'social search regression')
        wait_until(lambda:self.admin.request('observability/logs')['data'],'Logs regression')
        wait_until(lambda:self.admin.request('observability/events')['data'],'Events regression')
        self.mark('ordinary-user management/metrics authorization; representative social/search, Logs and Events')
        samples=[self.compose('exec','-T','monitor','wget','-q','-O','-','http://127.0.0.1:9121/metrics').stdout,json.dumps(self.admin.request('exporter-plugins/catalog')).encode(),json.dumps(self.internal()).encode(),
                 self.compose('logs','--no-color','monitor','router','marshaller','backend').stdout,
                 json.dumps(self.admin.request('observability/logs')).encode(),json.dumps(self.admin.request('observability/events')).encode()]
        active=json.loads(self.file('redis-exporter/active.json'))['revision']
        for name in ['revision.json','config.json']:samples.append(self.file('redis-exporter/revisions/'+active+'/'+name))
        for payload in samples:
            assert self.secret.encode() not in payload and ('wrong-secret-canary-'+self.token).encode() not in payload
        mode=self.compose('exec','-T','monitor','stat','-c','%a','/var/lib/gopulse-monitor/plugins/redis-exporter/revisions/'+active+'/secret.json').stdout.strip()
        assert mode==b'600'
        self.mark('Secret separation, file mode and API/log/Event/registry leak scans')

    def browser(self):
        result=command(['docker','run','--rm','--network',self.project+'_edge',
            '-e','GOPULSE_BASE_URL=http://frontend:8080','-e','GOPULSE_P14_ADMIN='+self.admin_name,
            '-e','GOPULSE_P14_PASSWORD='+self.auth_password,'-e','GOPULSE_P14_SECRET='+self.secret,
            '-v',str(ROOT/'frontend/e2e/phase14-plugin.spec.ts')+':/work/frontend/e2e/phase14-plugin.spec.ts:ro',
            'gopulse/acceptance:1.10.6','e2e/phase14-plugin.spec.ts'],timeout=180,check=False)
        output=(result.stdout+result.stderr).decode(errors='replace')
        for secret in [self.secret,self.auth_password,'bad-browser-secret-canary']:output=output.replace(secret,'[REDACTED]')
        (self.work/'browser.log').write_text(output)
        if result.returncode:raise RuntimeError('browser acceptance failed; redacted diagnostics: '+str(self.work/'browser.log'))
        self.mark('administrator schema/check/install/configure browser flow and six real availability cards; Secret DOM cleared')

    def cleanup(self):
        if self.started:
            for kind,args in [('container',['ps','-aq']),('network',['network','ls','-q']),('volume',['volume','ls','-q'])]:
                ids=command(['docker',*args,'--filter','label=com.docker.compose.project='+self.project]).stdout.decode().split()
                for ident in ids:
                    info=json.loads(command(['docker',*([] if kind=='container' else [kind]),'inspect',ident]).stdout)[0]
                    labels=info['Config']['Labels'] if kind=='container' else info['Labels']
                    assert labels['com.docker.compose.project']==self.project
            self.compose('down','--volumes','--remove-orphans',timeout=120)
        for kind,ids in self.snapshot.items():
            for ident in ids:command(['docker',*([] if kind=='container' else [kind]),'inspect',ident])
        self.env_file.unlink(missing_ok=True)
        self.override_file.unlink(missing_ok=True)
        print('Owned resources removed; pre-existing resources preserved. Evidence: '+str(self.work),flush=True)


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--self-test',action='store_true')
    parser.add_argument('--sources',choices=['redis','mysql,rabbitmq'])
    parser.add_argument('--migration',action='store_true')
    args=parser.parse_args()
    if args.self_test:
        assert PATTERN.fullmatch('gopulse-p1401-012345abcdef')
        for invalid in ['', 'gopulse', 'gopulse-p1401-../', 'gopulse-p1401-012345abcdeg']:
            assert not PATTERN.fullmatch(invalid)
        print('PASS: bounded source selection and strong project ownership validation (no Docker access)')
        return
    if args.sources=='mysql,rabbitmq':
        from verify_plugin_clusters import ClusterAcceptance
        run=ClusterAcceptance()
    else:
        if args.sources!='redis' or not args.migration:parser.error('use --sources redis --migration or --sources mysql,rabbitmq')
        run=Acceptance()
    def interrupted(signum,frame):raise RuntimeError('acceptance interrupted')
    signal.signal(signal.SIGINT,interrupted);signal.signal(signal.SIGTERM,interrupted)
    try:run.run()
    finally:run.cleanup()

if __name__=='__main__':main()
