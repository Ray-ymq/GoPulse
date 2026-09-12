#!/usr/bin/env python3
"""Phase 15 dashboard acceptance, isolated Compose and single browser origin."""
import json
import os
import subprocess
import time
from verify_admin_frontend import AdminAcceptance
from verify_plugin_metrics import Client, ROOT, wait_until

class DashboardAcceptance(AdminAcceptance):
    def sql(self,query):
        self.owned_id('mysql')
        return self.compose('exec','-T','mysql','sh','-c','exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" --batch --skip-column-names "$MYSQL_DATABASE" -e "$1"','sql',query).stdout.decode().strip()

    def run(self):
        services=['backend','business-worker','search-indexer','frontend','admin-frontend','monitor','router','marshaller']
        self.images=['gopulse/'+s+':'+self.tag for s in services]
        result=self.compose('build',*services,check=False,timeout=1800)
        (self.work/'build.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode==0,'build failed'
        self.started=True
        result=self.compose('up','-d','--wait','--wait-timeout','420','frontend','monitor','marshaller','business-worker','search-indexer',check=False,timeout=500)
        (self.work/'startup.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode==0,'startup failed'
        base='http://'+self.compose('port','frontend','8080').stdout.decode().strip()
        self.admin,self.user=Client(base),Client(base)
        demoted=Client(base); demoted_name='demote_'+self.token
        for client,name in [(self.admin,self.admin_name),(self.user,self.user_name),(demoted,demoted_name)]:
            client.request('auth/register','POST',{'username':name,'password':self.auth_password},201)
        admin_id=self.admin.request('users/me')['data']['id'];user_id=self.user.request('users/me')['data']['id'];demoted_id=demoted.request('users/me')['data']['id']
        self.compose('exec','-T','backend','/usr/local/bin/admin-role','bootstrap','--user-id',str(admin_id))
        self.admin.request(f'admin/users/{demoted_id}/role','PUT',{'role':'super_admin'})
        for endpoint in ['admin/overview?range=15m','alerts/catalog','alerts/rules','alerts/current','alerts/history',f'admin/users/{user_id}','admin/audit-events']:
            Client(base).request(endpoint,expected=401);self.user.request(endpoint,expected=403)
        self.admin.request(f'admin/users/{admin_id}/role','PUT',{'role':'user'},409)
        # Produce real HTTP logs and real Monitor plugin lifecycle events.
        for _ in range(3): self.user.request('observability/logs',expected=403)
        wait_until(lambda:self.admin.request('exporter-plugins')['data'],'plugin bootstrap')
        self.admin.request('exporter-plugins/redis-exporter/stop','POST')
        self.admin.request('exporter-plugins/redis-exporter/start','POST')
        def snapshot_ready():
            d=self.admin.request('admin/overview?range=15m')['data']
            return d if all(x['value'] is not None for x in d['key_metrics']['items']) and d['logs']['status']=='healthy' and d['events']['status']=='healthy' else None
        snapshot=wait_until(snapshot_ready,'real six metric/count snapshot',180)
        (self.work/'overview.json').write_text(json.dumps(snapshot,indent=2))
        env=dict(os.environ,GOPULSE_BASE_URL=base,GOPULSE_ADMIN_USERNAME=self.admin_name,GOPULSE_USER_USERNAME=self.user_name,GOPULSE_DEMOTION_USERNAME=demoted_name,GOPULSE_ACCEPTANCE_PASSWORD=self.auth_password,GOPULSE_USER_ID=str(user_id),GOPULSE_DEMOTION_ID=str(demoted_id),GOPULSE_ADMIN_ID=str(admin_id))
        result=subprocess.run(['npm','run','test:e2e','--','dashboard.spec.ts'],cwd=ROOT/'frontend',env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=400)
        (self.work/'browser.log').write_bytes(result.stdout.replace(self.auth_password.encode(),b'[REDACTED]'))
        assert result.returncode==0,'browser failed'
        result=subprocess.run(['npm','run','test:e2e','--','admin-frontend.spec.ts','--grep','existing real'],cwd=ROOT/'frontend',env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
        (self.work/'existing-browser.log').write_bytes(result.stdout)
        assert result.returncode==0,'existing management browser failed'
        for service,sections in [('victoriametrics',['key_metrics','components','plugins']),('elasticsearch',['logs','events']),('monitor',['plugins'])]:
            self.owned_id(service);self.compose('stop',service)
            start=time.monotonic();d=self.admin.request('admin/overview?range=15m')['data'];elapsed=time.monotonic()-start
            assert elapsed<3.5 and d['alerts']['status']=='healthy'
            assert all(d[k]['status']!='healthy' for k in sections)
            self.admin.request(f'admin/users/{user_id}')
            (self.work/(service+'-partial.json')).write_text(json.dumps(d,indent=2))
            result=subprocess.run(['npm','run','test:e2e','--','dashboard-partial.spec.ts'],cwd=ROOT/'frontend',env=dict(env,GOPULSE_PARTIAL_SECTIONS=','.join(sections)),stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=90)
            (self.work/(service+'-browser.log')).write_bytes(result.stdout)
            assert result.returncode==0,'partial browser failed'
            self.compose('start',service);self.healthy(service)
        candidate={'config':{'host':'redis','port':6379,'database':0,'connect_timeout':'500ms','scrape_timeout':'1s'},'secrets':{'password':'wrong-'+self.secret}}
        self.admin.request('exporter-plugins/redis-exporter/connection-test','POST',candidate,422)
        self.admin.request('exporter-plugins/redis-exporter/configuration','PUT',candidate,422)
        self.admin.request('exporter-plugins/redis-exporter/install','POST',candidate,409)
        # A database trigger on this uniquely owned database rejects only the
        # completion append; remote success must remain requested/unknown.
        self.sql("DELIMITER $$\nCREATE TRIGGER acceptance_audit_failure BEFORE INSERT ON management_audit_events FOR EACH ROW BEGIN IF NEW.action='plugin.stop' AND NEW.phase='completed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='acceptance completion unavailable'; END IF; END$$\nDELIMITER ;")
        try:
            self.admin.request('exporter-plugins/redis-exporter/stop','POST')
            intent=self.admin.request('admin/audit-events?action=plugin.stop')['data'][0]
            assert intent['phase']=='requested' and intent['outcome']=='unknown'
            operation=intent['operation_id']
            assert self.sql("SELECT COUNT(*) FROM management_audit_events WHERE operation_id='"+operation+"'")=='1'
        finally:
            self.sql('DROP TRIGGER acceptance_audit_failure')
            self.admin.request('exporter-plugins/redis-exporter/start','POST')
        # Legacy update is still a supported managed mutation and is audited.
        self.admin.request('exporter-plugins/redis-exporter/update','POST',b'invalid',400,{'Content-Type':'multipart/form-data; boundary=invalid'})
        rows=self.admin.request('admin/audit-events')['data']
        assert any(r['action']=='plugin.configuration' and r['phase']=='completed' and r['outcome']=='failed' for r in rows)
        assert set(['plugin.'+a for a in ['connection-test','install','configuration','start','stop','update']]).issubset({r['action'] for r in rows})
        for path in ['admin/overview?range=15m','admin/audit-events','alerts/current','alerts/history']:
            assert self.secret not in json.dumps(self.admin.request(path))
        for service in ['frontend','admin-frontend']:
            output=self.compose('exec','-T',service,'cat','/usr/share/nginx/html/index.html').stdout
            assert self.secret.encode() not in output
            self.compose('exec','-T',service,'sh','-c',"! grep -rE 'http://(backend|monitor|router|elasticsearch|victoriametrics):|MONITOR_API_TOKEN|gopulse-(logs|events)-v1-read' /usr/share/nginx/html")
        result=subprocess.run(['npm','run','test:e2e','--','dashboard-audit.spec.ts'],cwd=ROOT/'frontend',env=dict(env,GOPULSE_SECRET_CANARY=self.secret),stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=90)
        (self.work/'audit-browser.log').write_bytes(result.stdout)
        assert result.returncode==0,'audit/redaction browser failed'
        logs=self.compose('logs','--no-color','backend','monitor').stdout
        assert ('wrong-'+self.secret).encode() not in logs
        self.mark('dashboard six real sections; three pages; authorization; revision; self-demotion; plugin audit; VM/ES/Monitor isolation; redaction')

if __name__=='__main__':
    run=DashboardAcceptance();print('Evidence: '+str(run.work),flush=True)
    try:run.run()
    finally:run.cleanup()
