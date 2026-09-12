#!/usr/bin/env python3
"""Phase 15 acceptance contract: one owned Compose product, one browser origin.

No historical package checks are rerun. Runtime evidence stays in .run, and a
failed gate never advances VERSION or marks the phase complete.
"""
import json
import datetime
import signal
import re
import shutil
import time
import sys
import urllib.parse
import urllib.request
from verify_admin_frontend import AdminAcceptance
from verify_alert_sources import SourcesAcceptance
from verify_alerts import AlertsAcceptance, rule
from verify_plugin_metrics import ROOT, command, Client, wait_until
from verify_plugin_isolation import REPRESENTATIVE
from verify_component_metrics import QUERIES


class Phase15Acceptance(AdminAcceptance):
    # Reuse the proven real-source adapters and ownership-checked fault helpers.
    api = AlertsAcceptance.api
    get = AlertsAcceptance.get
    state = AlertsAcceptance.state
    incidents = AlertsAcceptance.incidents
    count = AlertsAcceptance.count
    sql = AlertsAcceptance.sql
    fault = AlertsAcceptance.fault
    record = AlertsAcceptance.record
    save = AlertsAcceptance.save
    available = SourcesAcceptance.available
    es = SourcesAcceptance.es
    redaction = SourcesAcceptance.redaction

    def __init__(self):
        super().__init__()
        self.values = dict(line.split('=', 1) for line in self.env_file.read_text().splitlines())
        self.version='1.12.6'
        self.values.update(GOPULSE_VERSION=self.version, ALERT_EVALUATION_ENABLED='true', MONITOR_SCRAPE_INTERVAL='15s',
                           GOPULSE_ACCEPTANCE_TOKEN=self.token)
        self.env_file.write_text(''.join(f'{k}={v}\n' for k,v in self.values.items()))
        self.evidence = []
        self.connections = False
        self.override['services']['backend'] = {}
        # Compose reset tags cannot be represented in JSON. Supply the tiny
        # port reset as an additional owned YAML override.
        self.port_override=self.work/'edge-only.yaml'
        self.port_override.write_text('services:\n  backend:\n    ports: !reset []\n')
        self.save()
        (self.work/'snapshot-before.json').write_text(json.dumps({k:sorted(v) for k,v in self.snapshot.items()}, indent=2))
        for filename, args in [('git-before.txt',['git','status','--porcelain=v1']),
                               ('ports-before.txt',['ss','-ltn']),
                               ('projects-before.json',['docker','compose','ls','--all','--format','json'])]:
            (self.work/filename).write_bytes(command(args).stdout)

    def compose(self, *args, **kwargs):
        if args and args[0]=='down':
            args=('--profile','exporter','--profile','acceptance',*args)
        if not hasattr(self,'port_override'):
            return super().compose(*args,**kwargs)
        check=kwargs.pop('check',True)
        result=command(['docker','compose','--project-name',self.project,'--env-file',str(self.env_file),
            '-f',str(ROOT/'deploy/compose.yaml'),'-f',str(self.override_file),'-f',str(self.port_override),*args],check=False,**kwargs)
        if check and result.returncode:
            text=(result.stdout+result.stderr).decode(errors='replace')
            for value in getattr(self,'values',{}).values():
                if len(value)>8:text=text.replace(value,'[REDACTED]')
            (self.work/'compose-failure.log').write_text(text)
            raise RuntimeError('Compose failed; redacted evidence: '+str(self.work/'compose-failure.log'))
        return result

    def browser(self, name, specs, **env):
        args = ['--profile','acceptance','run','--rm','--no-deps']
        for key,value in env.items():
            args += ['-e',key+'='+str(value)]
        result = self.compose(*args,'acceptance',*specs,check=False,timeout=600)
        output = result.stdout+result.stderr
        for value in self.values.values():
            if len(value)>8:
                output=output.replace(value.encode(),b'[REDACTED]')
        (self.work/(name+'.log')).write_bytes(output)
        assert result.returncode == 0, name+' browser gate failed'
        self.record(name+' container browser passed')

    def restart(self, enabled=True):
        self.owned_id('backend')
        self.override['services']['backend'].setdefault('environment',{})['ALERT_EVALUATION_ENABLED']=str(enabled).lower()
        self.save()
        self.compose('up','-d','--no-deps','--force-recreate','backend')
        self.healthy('backend')
        wait_until(lambda:self.admin.request('users/me')['data'],'edge resolves replacement Backend')
        # All clients retain the edge origin and their original Cookie jars.

    def build(self):
        self.services=['backend','business-worker','search-indexer','frontend','admin-frontend','monitor','router','marshaller']
        self.images=['gopulse/'+s+':'+self.tag for s in self.services+['acceptance']]
        for image in self.images:
            assert command(['docker','image','inspect',image],check=False).returncode!=0,'refuse pre-existing acceptance image'
        result=self.compose('--profile','acceptance','build',*self.services,'acceptance',check=False,timeout=1800)
        (self.work/'build.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode==0, 'current image build failed'
        self.record('current eight product images and browser image built; both Frontend tests/typecheck/build')

    def start(self):
        self.started=True
        result=self.compose('up','-d','--wait','--wait-timeout','420','frontend','monitor','marshaller','business-worker','search-indexer',check=False,timeout=500)
        (self.work/('startup-'+str(len(self.evidence))+'.log')).write_bytes(result.stdout+result.stderr)
        assert result.returncode==0,'Compose startup failed'
        base='http://'+self.compose('port','frontend','8080').stdout.decode().strip()
        self.admin,self.user,self.second=Client(base),Client(base),Client(base)
        self.record('owned Compose started', project=self.project, origin=base)

    def register(self):
        self.second_name='second_'+self.token
        for client,name in [(self.admin,self.admin_name),(self.user,self.user_name),(self.second,self.second_name)]:
            client.request('auth/register','POST',{'username':name,'password':self.auth_password},201)
        self.admin_id=self.admin.request('users/me')['data']['id']
        self.user_id=self.user.request('users/me')['data']['id']
        self.second_id=self.second.request('users/me')['data']['id']
        self.browser_env=dict(GOPULSE_ADMIN_USERNAME=self.admin_name,GOPULSE_USER_USERNAME=self.user_name,
            GOPULSE_DEMOTION_USERNAME=self.second_name,GOPULSE_ACCEPTANCE_PASSWORD=self.auth_password,
            GOPULSE_ADMIN_ID=self.admin_id,GOPULSE_USER_ID=self.user_id,GOPULSE_DEMOTION_ID=self.second_id)

    def clean_install(self):
        self.start();self.register()
        assert self.sql('SELECT COUNT(*) FROM bootstrap_super_admin')=='0'
        assert self.admin.request('users/me')['data']['role']=='user'
        self.admin.request('admin/overview?range=15m',expected=403)
        self.browser('clean-before-bootstrap',['e2e/phase15-closure.spec.ts','--grep','clean setup'],**self.browser_env)
        for _ in range(2):
            self.compose('exec','-T','backend','/usr/local/bin/admin-role','bootstrap','--user-id',str(self.admin_id))
        assert self.sql("SELECT COUNT(*) FROM management_audit_events WHERE action='bootstrap.declare'")=='1'
        assert self.compose('exec','-T','backend','/usr/local/bin/admin-role','bootstrap','--user-id',str(self.second_id),check=False).returncode!=0
        self.browser('clean-after-bootstrap',['e2e/phase15-closure.spec.ts','--grep','bootstrap login'],**self.browser_env)
        self.record('empty database registration, no implicit privilege, unique idempotent operations bootstrap; fresh plugin volume')
        # Validate the complete removal set before resetting this owned fixture.
        self.assert_owned_resources()
        self.compose('down','--volumes','--remove-orphans',timeout=120)
        self.started=False

    def assert_owned_resources(self):
        for kind,args in [('container',['ps','-aq']),('network',['network','ls','-q']),('volume',['volume','ls','-q'])]:
            for ident in command(['docker',*args,'--filter','label=com.docker.compose.project='+self.project]).stdout.decode().split():
                assert ident not in self.snapshot[kind], 'refuse pre-existing resource'
                obj=json.loads(command(['docker',*([] if kind=='container' else [kind]),'inspect',ident]).stdout)[0]
                labels=obj['Config']['Labels'] if kind=='container' else obj['Labels']
                assert labels.get('com.docker.compose.project')==self.project
                if kind=='volume':assert ident.startswith(self.project+'_')

    def install_plugins(self):
        from reconcile_plugin_accounts import Reconciler, save
        self.admin_file=self.work/'deployment-admin.json'
        save(self.admin_file,dict(mysql_root_password=self.values['MYSQL_ROOT_PASSWORD'],mysql_database=self.values['MYSQL_DATABASE'],
             rabbitmq_username=self.values['RABBITMQ_USER'],rabbitmq_password=self.values['RABBITMQ_PASSWORD']))
        reconciler=Reconciler(self.project,self.admin_file,self.work/'accounts')
        try:
            for source in ['mysql','rabbitmq']:reconciler.reconcile(source)
        finally:reconciler.lock.close()
        configs={
            'redis':dict(config=dict(host='redis',port=6379,database=0,connect_timeout='1s',scrape_timeout='2s'),secrets=dict(password=self.secret)),
            'kafka':dict(config=dict(host='kafka',port=19092,topic='gopulse-observability-v1',consumer_group='gopulse-marshaller-metrics-v1',connect_timeout='1s',scrape_timeout='3s'),secrets={}),
            'elasticsearch':dict(config=dict(host='elasticsearch',port=9200,connect_timeout='1s',scrape_timeout='3s'),secrets={}),
            'victoriametrics':dict(config=dict(host='victoriametrics',port=8428,username=self.values['VICTORIAMETRICS_USERNAME'],connect_timeout='1s',scrape_timeout='2s'),secrets=dict(password=self.values['VICTORIAMETRICS_PASSWORD']))}
        for source,cfg in configs.items():
            self.admin.request('exporter-plugins/'+source+'-exporter/install','POST',cfg,201)
            self.admin.request('exporter-plugins/'+source+'-exporter/start','POST')
        wait_until(lambda:len(self.admin.request('exporter-plugins')['data'])==6,'six installed plugins')

    def plugin_facts(self):
        return {p['id']:{k:p[k] for k in ['version','installed_at','updated_at','desired_state']} for p in self.admin.request('exporter-plugins')['data']}

    def upgrade(self):
        # A real Phase 14 runtime generates the legacy schema, roles, sessions,
        # business records, plugin registry and stored telemetry. No SQL grants.
        for service in ['backend','monitor']:
            image='gopulse/'+service+':1.11.5'
            command(['docker','image','inspect',image])
            self.override['services'].setdefault(service,{})['image']=image
        self.override['services']['migrate']={'image':'gopulse/backend:1.11.5'}
        self.override['services']['monitor']['environment']={'MONITOR_BOOTSTRAP_PACKAGE':''}
        self.save();self.start();self.register()
        for name in [self.admin_name,self.second_name]:
            self.compose('exec','-T','backend','/usr/local/bin/admin-role','promote','--username',name)
        assert self.admin.request('users/me')['data']['role']=='admin'
        assert self.second.request('users/me')['data']['role']=='admin'
        self.install_plugins()
        post=self.user.request('posts','POST',dict(title='upgrade-'+self.token,content='preserved Phase 14 business'),201)['data']
        wait_until(lambda:self.user.request('search/posts?q=upgrade-'+self.token)['data'],'legacy search')
        wait_until(self.metric,'legacy metric history',180)
        logs=wait_until(lambda:self.admin.request('observability/logs')['data'],'legacy logs',180)
        events=wait_until(lambda:self.admin.request('observability/events')['data'],'legacy events',180)
        facts=self.plugin_facts()
        users=self.sql('SELECT id,username,password_hash,created_at FROM users ORDER BY id')
        post_before=self.user.request('posts/'+str(post['id']))['data']
        self.compose('stop','backend','monitor')
        for service in ['backend','monitor']:
            self.override['services'][service]['image']='gopulse/'+service+':'+self.tag
        self.override['services']['migrate']['image']='gopulse/backend:'+self.tag
        self.save()
        self.compose('up','--no-deps','--force-recreate','migrate')
        self.compose('up','-d','--no-deps','--force-recreate','backend','monitor')
        for service in ['backend','monitor']:self.healthy(service)
        wait_until(lambda:self.admin.request('users/me')['data']['role']=='super_admin','edge resolves upgraded Backend')
        assert self.admin.request('users/me')['data']['role']=='super_admin'
        assert self.second.request('users/me')['data']['role']=='super_admin'
        assert self.user.request('users/me')['data']['role']=='user'
        assert self.sql('SELECT user_id FROM bootstrap_super_admin')==str(self.admin_id)
        assert users==self.sql('SELECT id,username,password_hash,created_at FROM users ORDER BY id')
        assert post_before==self.user.request('posts/'+str(post['id']))['data']
        assert facts==self.plugin_facts()
        wait_until(lambda:all(p['observed_state']=='running' for p in self.admin.request('exporter-plugins')['data']),'all retained Phase14 plugins recovered',180)
        baseline=self.sql('SELECT COUNT(*) FROM management_audit_events')
        for _ in range(2):self.compose('run','--rm','--no-deps','migrate')
        self.restart()
        assert baseline==self.sql('SELECT COUNT(*) FROM management_audit_events')
        assert self.sql('SELECT COUNT(*) FROM alert_rule_states')=='0'
        assert self.sql('SELECT user_id FROM bootstrap_super_admin')==str(self.admin_id)
        assert facts==self.plugin_facts()
        # The protected DELETE is the sole intentional SQL invariant violation.
        denied=self.compose('exec','-T','mysql','sh','-c','mysql -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE" -e "$1"','sql',f'DELETE FROM users WHERE id={self.admin_id}',check=False)
        assert denied.returncode!=0 and b'foreign key constraint fails' in denied.stderr
        self.admin.request(f'admin/users/{self.admin_id}/role','PUT',{'role':'user'},409)
        self.record('Phase 14 two-admin upgrade and rerun preserve original Cookies/users/post/plugin desired state; minimum-ID bootstrap protected',
                    legacy_version='1.11.5',bootstrap=self.admin_id,plugins=facts,legacy_log_rows=len(logs),legacy_event_rows=len(events))

    def preserved_observability(self):
        # Prove pre-upgrade storage survived, not merely that new samples exist.
        upgrade=next(e for e in self.evidence if e['step'].startswith('Phase 14 two-admin upgrade'))
        cutoff=max(p['installed_at'] for p in upgrade['plugins'].values())
        counts={}
        for source in ['logs','events']:
            status,body=self.es('gopulse-'+source+'-v1-read/_count',{'query':{'range':{'@timestamp':{'lt':cutoff}}}})
            assert status==200 and body['count']>0 and body['_shards']['failed']==0
            counts[source]=body['count']
        self.owned_id('victoriametrics');self.owned_id('backend')
        query=urllib.parse.urlencode({'match[]':'gopulse_redis_up{source="redis",target_id="redis-exporter-local"}',
                                     'end':str(datetime.datetime.fromisoformat(cutoff).timestamp()),'reduce_mem_usage':'1'})
        raw=self.compose('exec','-T','backend','sh','-c',
            'auth=$(printf "%s:%s" "$BACKEND_VICTORIAMETRICS_USERNAME" "$BACKEND_VICTORIAMETRICS_PASSWORD" | base64 | tr -d "\\n"); '
            'exec wget -q -O - --header "Authorization: Basic $auth" --post-data "$1" "$BACKEND_VICTORIAMETRICS_URL/prometheus/api/v1/export"','history',query).stdout
        rows=[json.loads(line) for line in raw.splitlines() if line]
        assert rows and any(row['timestamps'] for row in rows)
        counts['original_metric_points']=sum(len(row['timestamps']) for row in rows)
        (self.work/'preserved-observability.json').write_text(json.dumps(dict(before=cutoff,counts=counts),indent=2))
        print('PASS: pre-upgrade original metric points and stored Logs/Events preserved',flush=True)

    def three_sources(self):
        self.browser('three-source-create',['e2e/phase15-closure.spec.ts','--grep','create exact'],**self.browser_env)
        rules=[r for r in self.api('rules')['data'] if r['name'].startswith('closure-')]
        assert {r['source'] for r in rules}=={'metrics','logs','events'}
        # Generate a fresh accepted auth log and Monitor stop/start event.
        Client(self.admin.base).request('auth/register','POST',dict(username='log_'+self.token,password=self.auth_password),201)
        self.admin.request('exporter-plugins/redis-exporter/stop','POST')
        self.admin.request('exporter-plugins/redis-exporter/start','POST')
        self.fault(True)
        for r in rules:self.state(r,'firing','ok')
        before={r['id']:self.incidents(r)[0] for r in rules}
        self.restart()
        for r in rules:
            wait_until(lambda:self.incidents(r)[0]['evaluation_count']>=before[r['id']]['evaluation_count']+3,'three continuing rounds',180)
            h=self.incidents(r)
            assert len(h)==1 and h[0]['id']==before[r['id']]['id'] and self.count(r,'alert.trigger')==1
            assert h[0]['last_triggered_at']>before[r['id']]['last_triggered_at']
        self.record('browser-created three-source real firing/three continuing rounds/restart retain one incident and trigger',incidents=[self.incidents(r)[0] for r in rules])
        for service,affected,sections in [('victoriametrics',{'metrics'},['key_metrics','components','plugins']),('elasticsearch',{'logs','events'},['logs','events'])]:
            # Keep count windows populated through serial failures using only
            # fresh product operations, then stop generation before recovery.
            Client(self.admin.base).request('auth/register','POST',dict(username=service[:5]+'_'+self.token,password=self.auth_password),201)
            self.admin.request('exporter-plugins/redis-exporter/stop','POST')
            self.admin.request('exporter-plugins/redis-exporter/start','POST')
            time.sleep(20)  # let the normal log/event transport persist new inputs
            self.owned_id(service);self.compose('stop',service)
            try:
                for r in rules:self.state(r,'firing','stale' if r['source'] in affected else 'ok')
                for r in rules:assert self.count(r,'alert.recover')==0
                self.available()
                self.browser(service+'-partial',['e2e/dashboard-partial.spec.ts'],**self.browser_env,GOPULSE_PARTIAL_SECTIONS=','.join(sections))
                self.record(service+' source unknown without false recovery; other sources and social/MySQL remain available',states=[self.get(r)['evaluation'] for r in rules])
            finally:self.compose('start',service);self.healthy(service)
            for r in rules:self.state(r,'firing','ok')
        self.restart(False)
        query='SELECT rule_id,state,data_status,last_evaluated_at,active_incident_id FROM alert_rule_states ORDER BY rule_id'
        frozen=self.sql(query);counts=self.sql('SELECT SUM(evaluation_count) FROM alert_incidents')
        time.sleep(35)
        assert self.sql(query)==frozen and self.sql('SELECT SUM(evaluation_count) FROM alert_incidents')==counts
        self.available()
        with urllib.request.urlopen(self.admin.base+'/ready',timeout=5) as ready:
            assert ready.status==200
        self.browser('evaluator-disabled',['e2e/phase15-closure.spec.ts','--grep','bootstrap login'],**self.browser_env)
        self.restart(True);self.fault(False)
        for r in rules:
            self.state_recovered(r)
        assert not self.api('current')['data']
        self.record('evaluator disable/re-enable preserves firing; real values and count-window expiry recover original incidents',
                    incidents=[self.incidents(r)[0] for r in rules],audits={r['source']:{'trigger':self.count(r,'alert.trigger'),'recover':self.count(r,'alert.recover')} for r in rules})
        # Close reasons reuse one real metrics transition per business outcome.
        self.fault(True)
        close_rules=[self.api('rules','POST',rule('close-'+action),201)['data'] for action in ['update','disable','delete']]
        for r in close_rules:self.state(r,'firing')
        for r,action in zip(close_rules,['update','disable','delete']):
            current=self.get(r);body={'revision':current['revision']};path='rules/'+str(r['id'])
            if action=='update':
                body=rule('closed-update',revision=current['revision']);body.pop('enabled');self.api(path,'PUT',body)
            elif action=='disable':self.api(path+'/disable','POST',body)
            else:self.api(path,'DELETE',body,204)
            h=self.incidents(r)[0]
            assert h['status']=='closed' and h['resolution_reason']=='rule_'+{'update':'updated','disable':'disabled','delete':'deleted'}[action] and h['recovered_at'] is None
            assert self.count(r,'alert.close')==1
        self.fault(False)
        for r in self.api('rules')['data']:
            if r['enabled']:self.api('rules/'+str(r['id'])+'/disable','POST',{'revision':r['revision']})
        self.record('update/disable/delete firing produce distinct closed reasons, not recovered')

    def state_recovered(self,r):
        wait_until(lambda:self.get(r)['evaluation']['state']=='normal' and self.get(r)['evaluation']['data_status']=='ok','real window recovery',420)
        h=self.incidents(r)
        assert len(h)==1 and h[0]['status']=='recovered' and h[0]['recovered_at']
        assert self.count(r,'alert.trigger')==1 and self.count(r,'alert.recover')==1
        if r['source']!='metrics':assert h[0]['last_value']==0

    def runtime_contracts(self):
        revision=command(['git','rev-parse','HEAD']).stdout.decode().strip()
        for service in self.services:
            obj=json.loads(command(['docker','inspect',self.owned_id(service)]).stdout)[0]
            assert obj['Config']['Labels']['org.opencontainers.image.version']==self.version
            assert obj['Config']['Labels']['org.opencontainers.image.revision']==revision
            assert re.fullmatch(r'[1-9][0-9]*(?::[1-9][0-9]*)?',obj['Config']['User'])
            assert obj['HostConfig']['ReadonlyRootfs']
            ports=obj['HostConfig']['PortBindings']
            if service=='frontend':
                assert len(ports)==1 and all(b['HostIp']=='127.0.0.1' for rows in ports.values() for b in rows)
            else:assert not ports,service+' must not publish host ports'
            networks=set(obj['NetworkSettings']['Networks'])
            if service in ['frontend','admin-frontend']:assert networks=={self.project+'_edge'}
            image=json.loads(command(['docker','image','inspect',obj['Image']]).stdout)[0]
            for secret in [self.secret,self.auth_password]:assert secret not in json.dumps(image)
        for service in ['frontend','admin-frontend']:
            self.compose('exec','-T',service,'sh','-c',"! command -v node; ! command -v npm; test -z \"$(find /usr/share/nginx/html -name '*.map')\"")
        self.record('eight runtime images version/revision/numeric UID/read-only/network/only edge port; separate bundles without source maps/toolchains')

    def regression(self):
        self.browser('roles-apps',['e2e/admin-frontend.spec.ts'],**self.browser_env)
        # Existing matrix demotes its second admin. Restore using the public API.
        self.admin.request(f'admin/users/{self.second_id}/role','PUT',{'role':'super_admin'})
        # Dashboard browser assumes exactly its own three active rules.
        self.browser('dashboard',['e2e/dashboard.spec.ts'],**self.browser_env)
        for r in self.api('rules')['data']:
            if r['enabled']:self.api('rules/'+str(r['id'])+'/disable','POST',{'revision':r['revision']})
        self.browser('phase13',['e2e/compose-business.spec.ts','e2e/profile.spec.ts','e2e/follow.spec.ts','e2e/bookmark.spec.ts','e2e/edit.spec.ts','e2e/delete.spec.ts'])
        for source,suffix in REPRESENTATIVE.items():
            wait_until(lambda:self.admin.request('observability/metrics?metric=gopulse_'+source+'_'+suffix+'&range=15m')['data']['series'],source+' metric',180)
        for source,queries in QUERIES.items():
            wait_until(lambda:self.admin.request('observability/metrics?metric=gopulse_'+source.replace('-','_')+'_'+queries[0]+'&range=15m')['data']['series'],source+' component metric',180)
        wait_until(lambda:self.admin.request('observability/logs')['data'],'logs')
        wait_until(lambda:self.admin.request('observability/events')['data'],'events')
        # Unexpected exit of a non-storage Exporter, not an intentional stop.
        self.owned_id('monitor')
        pid=json.loads(self.file('rabbitmq-exporter/runtime/process.json'))['pid']
        executable=self.compose('exec','-T','monitor','readlink',f'/proc/{pid}/exe').stdout.decode().strip()
        assert executable.endswith('/bin/gopulse-rabbitmq-exporter')
        self.compose('exec','-T','monitor','kill','-KILL',str(pid))
        wait_until(lambda:self.admin.request('exporter-plugins/rabbitmq-exporter')['data']['observed_state']=='failed','unexpected exporter exit')
        self.available()
        assert self.admin.request('exporter-plugins/redis-exporter')['data']['observed_state']=='running'
        self.admin.request('exporter-plugins/rabbitmq-exporter/start','POST')
        wait_until(lambda:self.admin.request('exporter-plugins/rabbitmq-exporter')['data']['observed_state']=='running','exporter recovery')
        snapshot=wait_until(lambda:self.admin.request('admin/overview?range=15m')['data'] if all(v['value'] is not None for v in self.admin.request('admin/overview?range=15m')['data']['key_metrics']['items']) else None,'six key metrics')
        (self.work/'overview.json').write_text(json.dumps(snapshot,indent=2))
        self.record('Phase13 browser social matrix; six plugin/six component real metrics/logs/events; isolated Exporter unexpected exit/recovery')

    def audit_redaction(self):
        bad=dict(config=dict(host='redis',port=6379,database=0,connect_timeout='500ms',scrape_timeout='1s'),secrets=dict(password='wrong-'+self.secret))
        self.admin.request('exporter-plugins/redis-exporter/connection-test','POST',bad,422)
        self.admin.request('exporter-plugins/redis-exporter/configuration','PUT',bad,422)
        self.admin.request('exporter-plugins/redis-exporter/install','POST',bad,409)
        self.admin.request('exporter-plugins/redis-exporter/update','POST',b'invalid',400,{'Content-Type':'multipart/form-data; boundary=invalid'})
        # The previous batch already proved failed completion persistence; reuse
        # its fixture only to prove the integrated audit/DOM redaction boundary.
        self.sql("DELIMITER $$\nCREATE TRIGGER acceptance_audit_failure BEFORE INSERT ON management_audit_events FOR EACH ROW BEGIN IF NEW.action='plugin.stop' AND NEW.phase='completed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='acceptance completion unavailable'; END IF; END$$\nDELIMITER ;")
        try:
            self.admin.request('exporter-plugins/redis-exporter/stop','POST')
            row=self.admin.request('admin/audit-events?action=plugin.stop')['data'][0]
            assert row['phase']=='requested' and row['outcome']=='unknown'
        finally:
            self.sql('DROP TRIGGER acceptance_audit_failure')
            self.admin.request('exporter-plugins/redis-exporter/start','POST')
        self.browser('audit-redaction',['e2e/dashboard-audit.spec.ts'],**self.browser_env,GOPULSE_SECRET_CANARY=self.secret)
        for path in ['admin/overview?range=15m','admin/audit-events','alerts/current','alerts/history','observability/logs','observability/events','exporter-plugins']:
            assert self.secret not in json.dumps(self.admin.request(path))
        self.redaction()
        page=self.admin.request('admin/audit-events?limit=20');rows=list(page['data']);cursor=page['meta']['next_cursor']
        while cursor:
            page=self.admin.request('admin/audit-events?cursor='+urllib.parse.quote(cursor,safe=''));rows+=page['data'];cursor=page['meta']['next_cursor']
        assert len({r['id'] for r in rows})==len(rows)
        assert {'plugin.'+a for a in ['install','connection-test','configuration','start','stop','update']} <= {r['action'] for r in rows}
        self.record('stable audit cursor traverses actual role/rule/alert/six plugin operations',rows=len(rows))
        self.record('safe public DTO/log/event/audit/browser bundles and DOM; real requested/completed/unknown plugin audit')

    def standalone_exporter(self):
        # The standalone image remains a shipped Phase 12/14 contract even
        # though the full product normally uses Monitor-managed processes.
        image='gopulse/redis-exporter:'+self.tag
        assert command(['docker','image','inspect',image],check=False).returncode!=0
        self.images.append(image)
        result=self.compose('--profile','exporter','build','redis-exporter',check=False,timeout=600)
        (self.work/'standalone-build.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode==0
        self.started=True
        self.compose('--profile','exporter','up','-d','--wait','--wait-timeout','120','redis-exporter')
        cid=self.owned_id('redis-exporter')
        obj=json.loads(command(['docker','inspect',cid]).stdout)[0]
        assert obj['Config']['Labels']['org.opencontainers.image.version']==self.version
        assert obj['Config']['Labels']['org.opencontainers.image.revision']==self.values['GOPULSE_REVISION']
        assert obj['Config']['User']=='10004:10001' and obj['HostConfig']['ReadonlyRootfs']
        assert not obj['HostConfig']['PortBindings'] and set(obj['NetworkSettings']['Networks'])=={self.project+'_business'}
        raw=self.compose('exec','-T','redis-exporter','wget','-q','-O','-','http://127.0.0.1:9121/metrics').stdout
        assert b'gopulse_redis_up 1' in raw and raw.count(b'# TYPE gopulse_redis_')==10
        assert self.secret.encode() not in raw
        self.compose('kill','--signal','SIGTERM','redis-exporter')
        wait_until(lambda:json.loads(command(['docker','inspect',cid]).stdout)[0]['State']['Status']=='exited','standalone SIGTERM',20)
        assert json.loads(command(['docker','inspect',cid]).stdout)[0]['State']['ExitCode']==0
        self.record('standalone ninth product image: labels/nonroot/read-only/internal-only/real ten metric families/SIGTERM')

    def run(self):
        self.build()
        self.clean_install()
        self.upgrade()
        self.preserved_observability()
        self.runtime_contracts()
        for service in ['backend','business-worker','search-indexer','monitor','router','marshaller','frontend','admin-frontend']:
            cid=self.owned_id(service)
            self.compose('kill','--signal','SIGTERM',service)
            wait_until(lambda:json.loads(command(['docker','inspect',cid]).stdout)[0]['State']['Status']=='exited',service+' SIGTERM',30)
            assert json.loads(command(['docker','inspect',cid]).stdout)[0]['State']['ExitCode']==0,service+' SIGTERM'
            self.compose('up','-d','--no-deps',service)
            if service in ['business-worker','search-indexer']:
                wait_until(lambda:json.loads(command(['docker','inspect',cid]).stdout)[0]['State']['Running'],service+' restarted')
            else:self.healthy(service)
        origin='http://'+self.compose('port','frontend','8080').stdout.decode().strip()
        for client in [self.admin,self.user,self.second]:client.base=origin
        wait_until(lambda:self.admin.request('users/me')['data'],'edge after signal restart')
        self.record('eight runtime PID1 bounded SIGTERM and restart passed',origin=origin)
        self.standalone_exporter()
        for endpoint in ['admin/overview?range=15m','alerts/catalog','alerts/rules','alerts/current','alerts/history',f'admin/users/{self.user_id}','admin/audit-events','observability/logs','observability/events','observability/metrics/catalog','exporter-plugins']:
            Client(self.admin.base).request(endpoint,expected=401)
            self.user.request(endpoint,expected=403)
        self.record('anonymous and ordinary user API denial across all management families')
        self.regression()
        self.three_sources()
        self.audit_redaction()
        self.record('Phase 15 runtime gates passed; cleanup remains mandatory before completion')

    def cleanup(self):
        self.assert_owned_resources()
        super().cleanup()
        for kind,args in [('container',['ps','-aq']),('network',['network','ls','-q']),('volume',['volume','ls','-q'])]:
            assert not command(['docker',*args,'--filter','label=com.docker.compose.project='+self.project]).stdout.strip()
        for path in [self.work/'deployment-admin.json',self.work/'accounts',self.work/'edge-only.yaml']:
            if path.is_dir():shutil.rmtree(path)
            else:path.unlink(missing_ok=True)
        after={kind:sorted(command(['docker',*args]).stdout.decode().split()) for kind,args in {'container':['ps','-aq'],'network':['network','ls','-q'],'volume':['volume','ls','-q']}.items()}
        assert all(set(after[k])==v for k,v in self.snapshot.items()),'resource snapshot differs'
        (self.work/'snapshot-after.json').write_text(json.dumps(after,indent=2))
        (self.work/'ports-after.txt').write_bytes(command(['ss','-ltn']).stdout)
        def ports(path):return sorted(line.split()[3] for line in path.read_text().splitlines()[1:])
        assert ports(self.work/'ports-before.txt')==ports(self.work/'ports-after.txt'),'host listening ports changed'
        self.record('strong ownership cleanup passed; all pre-existing containers/networks/volumes preserved')



def main():
    if sys.argv[1:]:
        raise SystemExit('usage: verify_phase15_closure.py')
    run=Phase15Acceptance()
    print('Phase 15 evidence: '+str(run.work),flush=True)
    def interrupted(signum, frame):
        raise KeyboardInterrupt('acceptance interrupted')
    signal.signal(signal.SIGTERM,interrupted)
    try:
        run.run()
    finally:
        run.cleanup()


if __name__=='__main__':
    main()
