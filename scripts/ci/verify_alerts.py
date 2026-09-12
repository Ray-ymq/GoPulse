#!/usr/bin/env python3
"""Phase-15-02: owned Compose chain, real Redis samples, no metric/incident fixtures."""
import argparse
import urllib.parse
import concurrent.futures
import datetime
import json
import os
import time
import urllib.request
from verify_plugin_metrics import Acceptance, Client, ROOT, command, wait_until, PATTERN


def rule(name, hold='0s', **changes):
    value = dict(name=name, enabled=True, severity='warning', source='metrics',
        selector={'metric': 'gopulse_redis_connected_clients', 'labels': {}},
        reducer='last', operator='gt', threshold=10, window='5m', **{'for': hold})
    value.update(changes)
    return value


class AlertsAcceptance(Acceptance):
    def __init__(self):
        super().__init__()
        values = dict(line.split('=', 1) for line in self.env_file.read_text().splitlines() if '=' in line)
        # Existing unchanged Phase 14 runtime chain; Backend/migrations/operations
        # are built from the current checkout and bind-mounted, never an old binary.
        values.update(GOPULSE_IMAGE_TAG='1.11.5', GOPULSE_VERSION='1.12.3',
                      ALERT_EVALUATION_ENABLED='true', MONITOR_SCRAPE_INTERVAL='15s')
        self.env_file.write_text(''.join(f'{k}={v}\n' for k, v in values.items()))
        self.values = values
        self.override = {'services': {s: {'image': 'gopulse/'+s+':1.11.5'} for s in
            ['backend', 'frontend', 'monitor', 'router', 'marshaller']}}
        self.override['services']['monitor']['environment'] = {'MONITOR_BOOTSTRAP_PACKAGE': ''}
        self.evidence = []
        self.connections = False
        self.save()

    def compose(self, *args, **kwargs):
        check=kwargs.pop("check",True)
        result=super().compose(*args,check=False,**kwargs)
        if check and result.returncode:
            text=(result.stdout+result.stderr).decode(errors="replace")
            if args and args[0]=="up":
                logs=super().compose("logs","--tail","25","migrate","backend",check=False)
                text+=(logs.stdout+logs.stderr).decode(errors="replace")
            for value in getattr(self,"values",{}).values():
                if len(value)>8:text=text.replace(value,"[REDACTED]")
            (self.work/"failure.log").write_text(text)
            raise RuntimeError("Compose failed; redacted evidence: "+str(self.work/"failure.log"))
        return result

    def save(self):
        self.override_file.write_text(json.dumps(self.override))

    def record(self, step, **values):
        self.evidence.append(dict(step=step, at=datetime.datetime.now(datetime.timezone.utc).isoformat(), **values))
        (self.work/'alert-evidence.json').write_text(json.dumps(self.evidence, indent=2))
        print('PASS: '+step, flush=True)

    def sql(self, query):
        self.owned_id('mysql')
        return self.compose('exec', '-T', 'mysql', 'sh', '-c',
            'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" --batch --skip-column-names "$MYSQL_DATABASE" -e "$1"',
            'sql', query).stdout.decode().strip()

    def build(self):
        bins = self.work/'bin'
        bins.mkdir()
        for name, package in [('server', 'server'), ('migrate', 'migrate'), ('admin-role', 'admin-role')]:
            command(['env', 'CGO_ENABLED=0', 'go', '-C', str(ROOT/'backend'), 'build', '-o', str(bins/name), './cmd/'+package])
        command(['env', 'CGO_ENABLED=0', 'go', '-C', str(ROOT/'backend'), 'test', '-c', '-o', str(bins/'alert-test'), './internal/alert'])
        mounts = [str(bins/name)+':/usr/local/bin/'+name+':ro' for name in ['server','migrate','admin-role','alert-test']]
        self.override['services']['backend']['volumes'] = mounts
        self.override['services']['migrate'] = {'volumes': mounts}
        self.save()

    def api(self, path, method='GET', body=None, expected=200, client=None):
        value = (client or self.admin).request('alerts/'+path, method, body, expected)
        text = json.dumps(value)
        for secret in [self.secret, self.values['VICTORIAMETRICS_PASSWORD'], 'http://victoriametrics', 'PromQL', 'SELECT ', 'source="redis"', 'http://elasticsearch', 'gopulse-logs-v1', 'gopulse-events-v1', '/_count', 'query DSL']:
            assert secret not in text, 'unsafe public alert response'
        return value

    def get(self, r):
        return self.api('rules/'+str(r['id']))['data']

    def state(self, r, state, data=None):
        def check():
            value = self.get(r)
            return value if value['evaluation']['state'] == state and (data is None or value['evaluation']['data_status'] == data) else None
        return wait_until(check, state+('/'+data if data else ''), 180)

    def incidents(self, r):
        return self.api('history?rule='+str(r['id']))['data']

    def count(self, r, action):
        resource = 'rule' if action.startswith('rule.') else 'alert'
        condition = f"resource_id='{r['id']}'" if resource == 'rule' else f"resource_id IN (SELECT CAST(id AS CHAR) FROM alert_incidents WHERE rule_id={r['id']})"
        return int(self.sql(f"SELECT COUNT(*) FROM management_audit_events WHERE action='{action}' AND resource_type='{resource}' AND {condition}"))

    def restart(self, enabled=True):
        self.owned_id('backend')
        self.override['services']['backend']['environment'] = {'ALERT_EVALUATION_ENABLED': str(enabled).lower()}
        self.save()
        self.compose('up', '-d', '--no-deps', '--force-recreate', 'backend')
        self.healthy('backend')
        # Docker may allocate a new published port on container replacement.
        host=self.compose('port','backend','8080').stdout.decode().strip()
        self.admin.base=self.user.base='http://'+host

    def fault(self, active):
        self.owned_id('redis')
        if active:
            # Twenty genuine authenticated TCP clients, not a changed rule or
            # manually imported samples. Redis and the social dependency stay up.
            self.compose('exec', '-T', '-d', 'redis', 'sh', '-c',
                r'echo $$ > /tmp/alert-fault.pid; trap "kill \$(jobs -p) 2>/dev/null || true; rm -f /tmp/alert-fault.pid; exit" TERM INT; '
                'i=0; while [ "$i" -lt 20 ]; do redis-cli -a "$REDIS_PASSWORD" BLPOP alert-owned-list 900 >/dev/null 2>&1 & i=$((i+1)); done; wait')
        else:
            self.compose('exec', '-T', 'redis', 'sh', '-c', 'test ! -f /tmp/alert-fault.pid || kill -TERM "$(cat /tmp/alert-fault.pid)"; '
                "redis-cli --no-auth-warning CLIENT LIST | awk '/cmd=blpop/ {sub(/^id=/,\"\",$1); print $1}' | "
                'while read -r id; do redis-cli --no-auth-warning CLIENT UNBLOCK "$id" ERROR >/dev/null; done')
        self.connections = active

    def samples(self, metric, labels=None):
        self.owned_id('victoriametrics')
        expression=metric+'{source="redis",target_id="redis-exporter-local"'+''.join(','+k+'='+json.dumps(v) for k,v in (labels or {}).items())+'}'
        body=urllib.parse.urlencode({'match[]':expression,'start':str(time.time()-300),'end':str(time.time()),'reduce_mem_usage':'1'})
        raw=self.compose('exec','-T','backend','sh','-c',
            'auth=$(printf "%s:%s" "$BACKEND_VICTORIAMETRICS_USERNAME" "$BACKEND_VICTORIAMETRICS_PASSWORD" | base64 | tr -d "\\n"); '
            'exec wget -q -O - --header "Authorization: Basic $auth" --post-data "$1" "$BACKEND_VICTORIAMETRICS_URL/prometheus/api/v1/export"', 'probe', body).stdout
        rows=[json.loads(line) for line in raw.splitlines() if line]
        return rows

    def probe(self):
        # Real collected counter before/after a target reset, never VM writes.
        wait_until(lambda:self.samples('gopulse_redis_commands_processed_total'),'original counter points')
        self.owned_id('redis')
        self.compose('exec','-T','redis','sh','-c', 'i=0; while [ "$i" -lt 100 ]; do redis-cli --no-auth-warning PING >/dev/null; i=$((i+1)); done')
        time.sleep(20)
        self.compose('exec','-T','redis','redis-cli','--no-auth-warning','CONFIG','RESETSTAT')
        def reset():
            rows=self.samples('gopulse_redis_commands_processed_total')
            points=sorted({t:v for row in rows for t,v in zip(row['timestamps'],row['values'])}.items())
            return points if any(b[1]<a[1] for a,b in zip(points,points[1:])) else None
        points=wait_until(reset,'real counter reset samples',120)
        increase=sum(b[1]-a[1] if b[1]>=a[1] else b[1] for a,b in zip(points,points[1:]))
        assert increase>=0 and self.samples('gopulse_redis_cpu_seconds_total',{'mode':'user'})
        assert self.samples('gopulse_redis_db_keys',{'db':'0'})
        assert self.samples('gopulse_redis_db_keys',{'db':'15'})==[]
        self.record('VictoriaMetrics locked export API: original samples, empty series, exact mode/db tuples, real CONFIG RESETSTAT',counter_points=points, reset_aware_increase=increase)

    def run(self):
        self.build()
        self.started = True
        self.compose('up', '-d', '--no-build', 'backend', 'monitor', 'router', 'marshaller', timeout=420)
        for service in ['backend','monitor','router','marshaller']:
            self.healthy(service)
        host = self.compose('port', 'backend', '8080').stdout.decode().strip()
        self.admin, self.user = Client('http://'+host), Client('http://'+host)
        for client, name in [(self.admin,self.admin_name),(self.user,self.user_name)]:
            client.request('auth/register','POST',{'username':name,'password':self.auth_password},201)
        self.compose('exec','-T','backend','/usr/local/bin/admin-role','promote','--username',self.admin_name)
        # Verify migration down/up touches only alert entities before creating rules.
        before = self.sql('SELECT COUNT(*) FROM management_audit_events')
        self.compose('exec','-T','backend','/usr/local/bin/migrate','down')
        assert self.sql('SELECT COUNT(*) FROM bootstrap_super_admin') == '1'
        assert self.sql('SELECT COUNT(*) FROM management_audit_events') == before
        self.compose('exec','-T','backend','/usr/local/bin/migrate','up')
        self.record('migration 13 down/up preserves bootstrap and audit')
        self.compose('exec','-T','-e','ALERT_TEST_PROJECT='+self.project,'backend','sh','-c',
            'ALERT_TEST_DSN="$MYSQL_USER:$MYSQL_PASSWORD@tcp(mysql:3306)/$MYSQL_DATABASE?parseTime=true&loc=UTC" /usr/local/bin/alert-test -test.run TestOwnedMySQLStateAndLease -test.v')
        self.record('owned MySQL controlled-clock state transitions, expired/replayed lease rejection, unique active constraint')
        cfg={'config':{'host':'redis','port':6379,'database':0,'connect_timeout':'1s','scrape_timeout':'2s'},'secrets':{'password':self.secret}}
        self.admin.request('exporter-plugins/redis-exporter/install','POST',cfg,201)
        self.admin.request('exporter-plugins/redis-exporter/start','POST',None,200)
        wait_until(self.metric, 'real Phase 14 Redis samples', 180)
        catalog=self.api('catalog')['data']
        assert catalog['creatable_sources'] == ['metrics', 'logs', 'events']
        assert next(m for m in catalog['metrics'] if m['metric']=='gopulse_redis_connected_clients')['allowed_tuples']==[[]]
        self.record('Monitor exporter -> Router -> Kafka -> Marshaller -> VictoriaMetrics -> Backend catalog', catalog_count=len(catalog['metrics']), metric='gopulse_redis_connected_clients', reducer='last')
        self.probe()
        self.api_matrix()
        immediate=self.api('rules','POST',rule('immediate'),201)['data']
        pending=self.api('rules','POST',rule('pending','1m'),201)['data']
        no_data=self.api('rules','POST',rule('empty',selector={'metric':'gopulse_mysql_up','labels':{}}),201)['data']
        self.state(immediate,'normal','ok'); self.state(no_data,'normal','unknown')
        self.fault(True)
        self.state(immediate,'firing')
        p=self.state(pending,'pending'); pending_since=p['evaluation']['pending_since']
        self.restart()
        self.state(pending,'firing')
        self.record('real connections trigger for=0 and pending for=1m survives Backend replacement', pending_since=pending_since,
                    incidents=[self.incidents(immediate),self.incidents(pending)])
        self.incident_pages()
        original=self.incidents(immediate)[0]['id']
        start_count=self.incidents(immediate)[0]['evaluation_count']
        self.restart()
        wait_until(lambda:self.incidents(immediate)[0]['evaluation_count']>=start_count+3,'three continued true rounds',180)
        assert len(self.incidents(immediate))==1 and self.incidents(immediate)[0]['id']==original
        assert self.count(immediate,'alert.trigger')==1
        self.record('firing restart and three true rounds suppress duplicate incident/audit', incident=self.incidents(immediate), triggers=self.count(immediate,'alert.trigger'))
        # Stop upstream, not Backend; current remains firing/stale, pending breaks.
        extra=self.api('rules','POST',rule('unknown-pending','1m'),201)['data'];self.state(extra,'pending')
        self.owned_id('victoriametrics');self.compose('stop','victoriametrics')
        self.state(immediate,'firing','stale');self.state(extra,'normal','unknown')
        assert self.count(immediate,'alert.recover')==0
        self.record('unavailable upstream is stale/unknown, never zero or recovered')
        self.compose('start','victoriametrics');self.healthy('victoriametrics');self.state(immediate,'firing','ok')
        self.fault(False)
        self.state(immediate,'normal','ok');self.state(pending,'normal','ok')
        recovered=self.incidents(immediate)[0]
        assert recovered['status']=='recovered' and recovered['recovered_at'] and recovered['id']==original
        assert self.count(immediate,'alert.recover')==1
        assert all(i['rule_id']!=immediate['id'] for i in self.api('current')['data'])
        self.record('real client removal recovers original incident; history retained', incident=recovered)
        self.close_and_pause()
        self.record('completed Metrics-only fixed runtime acceptance')

    def api_matrix(self):
        anon=Client(self.admin.base)
        for client,status in [(anon,401),(self.user,403)]:
            for method,path,body in [('GET','catalog',None),('GET','rules',None),('GET','rules/1',None),('POST','rules',{}),('PUT','rules/1',{}),('POST','rules/1/enable',{}),('POST','rules/1/disable',{}),('DELETE','rules/1',{}),('GET','current',None),('GET','history',None)]:
                self.api(path,method,body,status,client)
        for change in [dict(source='logs'),dict(selector={'metric':'unknown','labels':{}}),dict(selector={'metric':'gopulse_redis_up','labels':{'x':'x'}}),dict(selector={'metric':'gopulse_redis_cpu_seconds_total','labels':{}}),dict(reducer='increase'),dict(window='2m'),{'for':'5m','window':'1m'},dict(threshold=1e16),dict(unexpected=True)]:
            self.api('rules','POST',rule('invalid',**change),400)
        self.api('rules','POST',b'{"name":"x","name":"y"}',400)
        self.api('rules','POST',b'{"name":"\xff"}',400)
        r=self.api('rules','POST',rule('race',enabled=False),201)['data']
        candidate=rule('race-edited',revision=r['revision']);candidate.pop('enabled')
        # Capture exact status; a server error must not masquerade as a conflict.
        import urllib.error
        second_id=self.user.request('users/me')['data']['id']
        self.admin.request('admin/users/'+str(second_id)+'/role','PUT',{'role':'super_admin'})
        def race(index):
            req=urllib.request.Request(self.admin.base+'/api/v1/alerts/rules/'+str(r['id']),data=json.dumps(candidate).encode(),method='PUT',headers={'Content-Type':'application/json','Origin':self.admin.base})
            client=self.admin if index==0 else self.user
            try: result=client.opener.open(req,timeout=10)
            except urllib.error.HTTPError as e: result=e
            raw=json.loads(result.read());return result.code,raw
        with concurrent.futures.ThreadPoolExecutor(2) as pool: results=list(pool.map(race,range(2)))
        assert sorted(code for code,_ in results)==[200,409]
        assert next(raw for code,raw in results if code==409)['error']['code']=='alert_revision_conflict'
        assert self.count(r,'rule.update')==1
        self.admin.request('admin/users/'+str(second_id)+'/role','PUT',{'role':'user'})
        r=self.get(r);self.api('rules/'+str(r['id']),'DELETE',{'revision':r['revision']},204)
        # Limit test has no evaluations: all rules disabled, removed afterward.
        created=[self.api('rules','POST',rule('limit-'+str(i),enabled=False),201)['data'] for i in range(32)]
        self.api('rules','POST',rule('limit-33',enabled=False),409)
        first=self.api('rules?limit=7');seen=list(first['data']);cursor=first['meta']['next_cursor']
        self.api('rules?cursor='+cursor+'x',expected=400)
        while cursor:
            page=self.api('rules?cursor='+cursor);seen+=page['data'];cursor=page['meta']['next_cursor']
        assert len(seen)==32 and len({v['id'] for v in seen})==32
        for r in created:self.api('rules/'+str(r['id']),'DELETE',{'revision':r['revision']},204)
        self.record('401/403 all routes, strict rule validation, signed pagination, limit32, exact 200/409 revision race', distinct_admin_ids=[self.admin.request('users/me')['data']['id'],second_id])

    def incident_pages(self):
        evidence={}
        for kind in ['current','history']:
            page=self.api(kind+'?limit=1');rows=page['data'];cursor=page['meta']['next_cursor']
            assert cursor, 'pagination requires multiple real incidents'
            self.api(kind+'?cursor='+cursor+'x',expected=400)
            while cursor:
                page=self.api(kind+'?cursor='+cursor);rows+=page['data'];cursor=page['meta']['next_cursor']
            assert len({r['id'] for r in rows})==len(rows)
            evidence[kind]=[dict(id=r['id'],status=r['status'],severity=r['severity']) for r in rows]
        self.record('current/history signed keyset pages preserve real incident order and reject tampering',pages=evidence)

    def close_and_pause(self):
        self.fault(True)
        rules=[self.api('rules','POST',rule('close-'+s),201)['data'] for s in ['update','disable','delete']]
        for r in rules:self.state(r,'firing')
        # Readiness and social API remain available while evaluator is disabled.
        self.restart(False)
        before=self.sql('SELECT rule_id,state,last_evaluated_at,active_incident_id FROM alert_rule_states ORDER BY rule_id')
        time.sleep(35)
        assert self.sql('SELECT rule_id,state,last_evaluated_at,active_incident_id FROM alert_rule_states ORDER BY rule_id')==before
        self.admin.request('posts?limit=1');self.api('rules');self.api('history')
        with urllib.request.urlopen(self.admin.base+'/ready',timeout=5) as r:assert r.status==200
        self.record('ALERT_EVALUATION_ENABLED=false freezes state, APIs/social/readiness remain available')
        for r,action in zip(rules,['update','disable','delete']):
            latest=self.get(r);body={'revision':latest['revision']}
            if action=='update':
                body=rule('updated',revision=latest['revision']);body.pop('enabled');self.api('rules/'+str(r['id']),'PUT',body)
            elif action=='disable':self.api('rules/'+str(r['id'])+'/disable','POST',body)
            else:self.api('rules/'+str(r['id']),'DELETE',body,204)
            inc=self.incidents(r)[0]
            reason={'update':'rule_updated','disable':'rule_disabled','delete':'rule_deleted'}[action]
            assert inc['status']=='closed' and inc['resolution_reason']==reason and inc['recovered_at'] is None
            assert self.count(r,'alert.close')==1
            self.record(action+' closes firing without recovered',incident=inc)
        disabled=self.get(rules[1]);self.api('rules/'+str(disabled['id'])+'/enable','POST',{'revision':disabled['revision']})
        self.restart(True);self.state(rules[1],'firing')
        self.fault(False)


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--self-test',action='store_true')
    parser.add_argument('--sources',choices=['metrics','metrics,logs,events'])
    parser.add_argument('--fault-isolation',action='store_true')
    args=parser.parse_args()
    if args.self_test:
        assert PATTERN.fullmatch('gopulse-p1401-012345abcdef')
        assert not PATTERN.fullmatch('gopulse')
        assert rule('x')['selector']=={'metric':'gopulse_redis_connected_clients','labels':{}}
        assert rule('x','1m')['for']=='1m'
        from verify_alert_sources import count_rule
        assert count_rule('logs')['selector']['labels']['message']=='user registered'
        assert count_rule('events')['selector']['labels']['event_name']=='exporter_plugin_installed'
        print('PASS alerts verifier self-test: three bounded sources, owned chain and serial isolation (no Docker)')
        return
    if args.fault_isolation != (args.sources=='metrics,logs,events'):parser.error('three sources require --fault-isolation')
    if not args.sources:parser.error('--sources is required')
    if args.fault_isolation:
        from verify_alert_sources import SourcesAcceptance
        acceptance=SourcesAcceptance()
    else:acceptance=AlertsAcceptance()
    print('Evidence directory: '+str(acceptance.work),flush=True)
    failed=False
    try:acceptance.run()
    except Exception as error:
        failed=True
        print("FAIL: alerts acceptance ("+type(error).__name__+"); inspect owned evidence directory",flush=True)
    finally:acceptance.cleanup()
    if failed:raise SystemExit(1)

if __name__=='__main__':main()
