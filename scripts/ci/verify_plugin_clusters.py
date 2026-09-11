"""MySQL/RabbitMQ fixed completion gate; reuse the owned Compose harness."""
import hashlib
import json
from pathlib import Path
import subprocess
import time

from verify_plugin_metrics import Acceptance, Client, command, wait_until
from reconcile_plugin_accounts import Reconciler, SafeFailure, PermissionDenied, save, private_read

VERSION = '1.11.2'

class ClusterAcceptance(Acceptance):
    def __init__(self):
        super().__init__()
        self.override['services'] = {name: {'image': 'gopulse/'+name+':'+VERSION}
                                    for name in ['backend','frontend','router','marshaller','monitor']}
        self.override_file.write_text(json.dumps(self.override))
        self.admin_file = self.work/'deployment-admin.json'
        save(self.admin_file, {'mysql_root_password':'root-'+self.token, 'mysql_database':'gopulse_'+self.token,
            'rabbitmq_username':'rabbit_'+self.token, 'rabbitmq_password':'rabbit-'+self.token})
        self.reconciler = Reconciler(self.project, self.admin_file, self.work/'accounts')

    def status_for(self, source):
        return self.admin.request('exporter-plugins/'+source+'-exporter')['data']

    def metric_for(self, source, suffix='up'):
        result = self.admin.request('observability/metrics?metric=gopulse_'+source+'_'+suffix+'&range=15m')['data']
        points = [point for series in result['series'] for point in series['points']]
        return points if points else None

    def scrape(self, source):
        port = {'redis':'9121','mysql':'9122','rabbitmq':'9123'}[source]
        result = self.compose('exec','-T','monitor','sh','-c','{ cat; sleep 3; } | nc -w 4 127.0.0.1 "$1"','probe',port,
            data=b'GET /metrics HTTP/1.0\r\nHost: localhost\r\n\r\n').stdout
        head,body = result.split(b'\r\n\r\n',1)
        return int(head.split()[1]), body

    def account(self, source):
        return private_read(self.reconciler.directory/(source+'.json'))

    def config(self, source):
        cfg = {'host':source,'username':'gopulse_metrics','connect_timeout':'1s','scrape_timeout':'2s'}
        cfg.update({'port':3306,'database':'gopulse_'+self.token} if source=='mysql' else {'management_port':15672,'vhost':'/'})
        return {'config':cfg,'secrets':{'password':self.account(source)['password']}}

    def run(self):
        for source in ['backend','frontend','router','marshaller','monitor']:
            command(['docker','image','inspect','gopulse/'+source+':'+VERSION])
        self.started=True
        self.compose('up','-d','--no-build',timeout=360)
        for service in ['frontend','backend','monitor','marshaller']:
            self.healthy(service)
        base='http://'+self.compose('port','frontend','8080').stdout.decode().strip()
        self.admin,self.user=Client(base),Client(base)
        for client,name in [(self.admin,self.admin_name),(self.user,self.user_name)]:
            client.request('auth/register','POST',{'username':name,'password':self.auth_password},201)
        self.compose('exec','-T','backend','/usr/local/bin/admin-role','promote','--username',self.admin_name)
        redis={'config':{'host':'redis','port':6379,'database':0,'connect_timeout':'1s','scrape_timeout':'2s'},'secrets':{'password':self.secret}}
        if not any(item['id']=='redis-exporter' for item in self.admin.request('exporter-plugins')['data']):
            self.admin.request('exporter-plugins/redis-exporter/install','POST',redis,201)
        for source in ['mysql','rabbitmq']:
            self.reconciler.reconcile(source)
            assert self.status_for(source)['observed_state']=='running'
        self.mark('fresh volumes: dedicated USAGE/monitoring accounts, connection-test and independent install/start')
        post=self.admin.request('posts','POST',{'title':'phase1402 '+self.token,'content':'Phase 13 preserved data '+self.token},201)['data']
        wait_until(lambda:self.admin.request('search/posts?q='+self.token)['data'],'social search')
        # Simulate a Phase-13 volume upgrade lacking collector accounts, using
        # only this run's owned dedicated users and a fresh plugin volume.
        self.compose('stop','monitor')
        self.reconciler.sql('root',self.reconciler.admin['mysql_root_password'],"DROP USER 'gopulse_metrics'@'%';")
        self.reconciler.rabbit('users/gopulse_metrics','DELETE',expected=204)
        for source in ['mysql','rabbitmq']:(self.reconciler.directory/(source+'.json')).unlink()
        self.override['services']['monitor']['volumes']=['p14_empty:/var/lib/gopulse-monitor/plugins']
        self.override_file.write_text(json.dumps(self.override))
        self.compose('up','-d','--no-build','--no-deps','--force-recreate','monitor')
        self.healthy('monitor')
        for source in ['mysql','rabbitmq']:self.reconciler.reconcile(source)
        assert self.admin.request('posts/'+str(post['id']))['data']['id']==post['id']
        self.mark('Phase-13-compatible existing business volume: missing collector accounts provisioned without changing business data')
        # Existing business data + interrupted account verification: no account,
        # password, grant or business-data mutations on retry.
        for source in ['mysql','rabbitmq']:
            path=self.reconciler.directory/(source+'.json')
            state=self.account(source);state['active']=False;save(path,state)
            digest=hashlib.sha256(path.read_bytes()).hexdigest()
            original=self.reconciler.monitor
            def interrupted(*args,**kwargs):raise SafeFailure('injected activation interruption')
            self.reconciler.monitor=interrupted
            try:
                try:self.reconciler.reconcile(source)
                except SafeFailure:pass
                else:raise AssertionError('interruption not exercised')
            finally:self.reconciler.monitor=original
            assert hashlib.sha256(path.read_bytes()).hexdigest()==digest
            self.reconciler.reconcile(source)
            before=path.read_bytes();self.reconciler.reconcile(source);assert path.read_bytes()==before
        assert self.admin.request('posts/'+str(post['id']))['data']['id']==post['id']
        self.mark('existing business data: interrupted activation retries same Secret; repeat reconciliation is idempotent')
        # Permission-denial probes are confined to this run's owned resources.
        try:self.reconciler.sql('gopulse_metrics',self.account('mysql')['password'],'CREATE DATABASE gopulse_p1402_denied;')
        except PermissionDenied:pass
        else:raise AssertionError('MySQL collector can create objects')
        try:self.reconciler.sql('gopulse_metrics',self.account('mysql')['password'],"INSERT INTO `gopulse_"+self.token+"`.users (username) VALUES ('denied_probe');")
        except PermissionDenied:pass
        else:raise AssertionError('MySQL collector can write business data')
        self.reconciler.rabbit('queues/%2F/gopulse-p1402-denied','PUT',{'durable':False,'arguments':{}},
            credentials=('gopulse_metrics',self.account('rabbitmq')['password']),expected=401)
        self.mark('minimal grants: status reads allowed; owned object creation/business writes denied')
        families={'mysql':['up','uptime_seconds','connections','max_connections','threads_running','queries_total','slow_queries_total','transactions_total','buffer_pool_data_bytes','buffer_pool_dirty_bytes'],
                  'rabbitmq':['up','connections','channels','queues','consumers','messages','published_total','delivered_total','acked_total']}
        for source, names in families.items():
            status,body=self.scrape(source)
            assert status==200 and body.count(b'# TYPE ')==len(names), (source,status,body.count(b'# TYPE '),len(body),body[:140])
            assert len([line for line in body.splitlines() if line and not line.startswith(b'#')])==len(names)+1
            for suffix in names:wait_until(lambda s=source,f=suffix:self.metric_for(s,f),source+' Backend '+suffix)
            bad=self.config(source);bad['secrets']['password']='bad-secret-canary-'+self.token
            self.admin.request('exporter-plugins/'+source+'-exporter/connection-test','POST',bad,422)
            assert self.status_for(source)['observed_state']=='running'
            for method,path in [('GET',''),('POST','/connection-test'),('POST','/install'),('POST','/start'),('POST','/stop'),('PUT','/configuration')]:
                self.user.request('exporter-plugins/'+source+'-exporter'+path,method,expected=403)
        maximum=int(self.reconciler.sql('gopulse_metrics',self.account('mysql')['password'],'SELECT @@GLOBAL.max_connections;'))
        assert self.metric_for('mysql','max_connections')[-1]['value']==maximum
        self.mark('all 19 families/21 samples validated and queried; invalid credentials isolated; ordinary-user management denied')
        # A controlled queue/message makes the aggregate change, without names in labels.
        before=self.metric_for('rabbitmq','published_total')[-1]['value']
        self.reconciler.rabbit('queues/%2F/gopulse-p1402-probe','PUT',{'durable':False,'arguments':{}},expected=201)
        self.reconciler.rabbit('exchanges/%2F/amq.default/publish','POST',{'properties':{},'routing_key':'gopulse-p1402-probe','payload':'probe','payload_encoding':'string'})
        wait_until(lambda:self.metric_for('rabbitmq','published_total')[-1]['value']>before,'RabbitMQ message delta')
        self.mark('real RabbitMQ publish changes vhost aggregate and Backend query')
        for source in ['mysql','rabbitmq']:
            pid=self.file(source+'-exporter/runtime/process.json')
            self.compose('pause',source)
            try:
                code,body=self.scrape(source)
                assert code==503 and body==('# TYPE gopulse_'+source+'_up gauge\ngopulse_'+source+'_up 0\n').encode()
                other='rabbitmq' if source=='mysql' else 'mysql'
                assert self.scrape(other)[0]==200 and self.scrape('redis')[0]==200
            finally:self.compose('unpause',source)
            wait_until(lambda:self.scrape(source)[0]==200,source+' same-process recovery')
            assert self.file(source+'-exporter/runtime/process.json')==pid
        # Authentication faults alter only the owned collector account, not
        # the shared service or business credential. Restore it in finally.
        for source in ['mysql','rabbitmq']:
            password=self.account(source)['password']
            pid=self.file(source+'-exporter/runtime/process.json')
            try:
                if source=='mysql':
                    root=self.reconciler.admin['mysql_root_password']
                    self.reconciler.sql('root',root,"ALTER USER 'gopulse_metrics'@'%' IDENTIFIED BY 'temporary-denied';")
                    ids=self.reconciler.sql('root',root,"SELECT ID FROM information_schema.PROCESSLIST WHERE USER='gopulse_metrics';")
                    for connection in ids.splitlines():
                        assert connection.isdigit()
                        self.reconciler.sql('root',root,'KILL '+connection+';')
                else:self.reconciler.rabbit('users/gopulse_metrics','PUT',{'password':'temporary-denied','tags':'monitoring'},expected=204)
                code,body=self.scrape(source)
                assert code==503 and body==('# TYPE gopulse_'+source+'_up gauge\ngopulse_'+source+'_up 0\n').encode()
                assert self.scrape('redis')[0]==200
                assert self.admin.request('posts/'+str(post['id']))['data']['id']==post['id']
            finally:
                if source=='mysql':self.reconciler.sql('root',root,"ALTER USER 'gopulse_metrics'@'%' IDENTIFIED BY '"+password+"';")
                else:self.reconciler.rabbit('users/gopulse_metrics','PUT',{'password':password,'tags':'monitoring'},expected=204)
            wait_until(lambda:self.scrape(source)[0]==200,source+' credential recovery')
            assert self.file(source+'-exporter/runtime/process.json')==pid
        # Shared dependency stops intentionally affect their consumers; the
        # assertion is safe unavailability + process survival + recovery, not
        # uninterrupted business success while MySQL/RabbitMQ is stopped.
        for source in ['mysql','rabbitmq']:
            pid=self.file(source+'-exporter/runtime/process.json')
            self.compose('stop',source)
            try:
                code,body=self.scrape(source)
                assert code==503 and body==('# TYPE gopulse_'+source+'_up gauge\ngopulse_'+source+'_up 0\n').encode()
                assert next(item for item in self.internal()['data'] if item['id']==source+'-exporter')['observed_state']=='running'
                if source=='mysql':
                    self.admin.request('exporter-plugins/mysql-exporter',expected=500)
                assert self.scrape('redis')[0]==200
            finally:
                self.compose('start',source)
                self.healthy(source)
            wait_until(lambda:self.scrape(source)[0]==200,source+' stopped-target recovery')
            assert self.file(source+'-exporter/runtime/process.json')==pid
        self.mark('real target timeouts return sole safe up=0; sibling exporters continue; same-process recovery')
        self.admin.request('exporter-plugins/mysql-exporter/stop','POST')
        self.compose('up','-d','--no-build','--no-deps','--force-recreate','monitor')
        self.healthy('monitor')
        assert self.status_for('mysql')['desired_state']=='stopped'
        assert self.status_for('mysql')['observed_state']=='stopped'
        for source in ['redis','rabbitmq']:wait_until(lambda s=source:self.status_for(s)['observed_state']=='running',source+' restored')
        self.admin.request('exporter-plugins/mysql-exporter/start','POST')
        pointer=self.file('mysql-exporter/active.json')
        self.compose('exec','-T','monitor','sh','-c','cat > /var/lib/gopulse-monitor/plugins/mysql-exporter/active.json',data=b'{invalid')
        self.compose('up','-d','--no-build','--no-deps','--force-recreate','monitor')
        self.healthy('monitor')
        for source in ['redis','rabbitmq']:wait_until(lambda s=source:self.status_for(s)['observed_state']=='running',source+' damaged sibling isolation')
        assert not any(item['id']=='mysql-exporter' and item['observed_state']=='running' for item in self.internal()['data'])
        self.compose('exec','-T','monitor','sh','-c','cat > /var/lib/gopulse-monitor/plugins/mysql-exporter/active.json',data=pointer)
        self.compose('up','-d','--no-build','--no-deps','--force-recreate','monitor')
        self.healthy('monitor')
        wait_until(lambda:self.status_for('mysql')['observed_state']=='running','repaired owned record recovery')
        wait_until(lambda:self.metric_for('redis'),'Redis regression')
        wait_until(lambda:self.admin.request('observability/logs')['data'],'Logs regression')
        wait_until(lambda:self.admin.request('observability/events')['data'],'Events regression')
        assert self.admin.request('posts/'+str(post['id']))['data']['id']==post['id']
        wait_until(lambda:self.admin.request('search/posts?q='+self.token)['data'],'search regression')
        self.mark('Monitor replacement preserves desired states; Redis/Logs/Events/social/search regression')
        snapshots=[self.compose('logs','--no-color','monitor','backend','router','marshaller').stdout,
                   json.dumps(self.admin.request('exporter-plugins/catalog')).encode(),json.dumps(self.internal()).encode(),
                   json.dumps(self.admin.request('observability/events')).encode()]
        catalog=self.admin.request('exporter-plugins/catalog')['data']
        for source in ['mysql','rabbitmq']:
            item=next(item for item in catalog if item['id']==source+'-exporter')
            root='/var/lib/gopulse-monitor/plugins/'+source+'-exporter/revisions/'+item['revision']+'/'
            mode=self.compose('exec','-T','monitor','stat','-c','%a',root+'secret.json').stdout.strip()
            assert mode==b'600'
            assert self.reconciler.admin['mysql_root_password'].encode() not in self.file(source+'-exporter/revisions/'+item['revision']+'/secret.json')
        for source in ['mysql','rabbitmq']:
            active=json.loads(self.file(source+'-exporter/active.json'))['revision']
            for name in ['config.json','revision.json']:
                snapshots.append(self.file(source+'-exporter/revisions/'+active+'/'+name))
            for snapshot in snapshots:assert self.account(source)['password'].encode() not in snapshot
        self.mark('API/registry/events/logs credential leak scan')
        self.browser_clusters()

    def browser_clusters(self):
        args=['docker','run','--rm','--network',self.project+'_edge','-e','GOPULSE_BASE_URL=http://frontend:8080',
              '-e','GOPULSE_P14_ADMIN='+self.admin_name,'-e','GOPULSE_P14_PASSWORD='+self.auth_password]
        for source in ['mysql','rabbitmq']:args+=['-e','GOPULSE_P1402_'+source.upper()+'_SECRET='+self.account(source)['password']]
        args+=['-e','GOPULSE_P1402_DATABASE=gopulse_'+self.token,
               '-v',str(Path(__file__).resolve().parents[2]/'frontend/e2e/phase14-clusters.spec.ts')+':/work/frontend/e2e/phase14-clusters.spec.ts:ro',
               'gopulse/acceptance:1.10.6','e2e/phase14-clusters.spec.ts']
        result=command(args,timeout=180,check=False)
        output=(result.stdout+result.stderr).decode(errors='replace')
        for secret in [self.auth_password,self.account('mysql')['password'],self.account('rabbitmq')['password']]:output=output.replace(secret,'[REDACTED]')
        (self.work/'browser.log').write_text(output)
        assert result.returncode==0,'browser acceptance failed (redacted log in run directory)'
        self.mark('administrator MySQL/RabbitMQ browser operations, source switching, metrics and cleared Secret DOM')

    def cleanup(self):
        super().cleanup()
        self.admin_file.unlink(missing_ok=True)
        for path in self.reconciler.directory.glob('*.json'):path.unlink()
