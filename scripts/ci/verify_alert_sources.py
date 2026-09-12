"""Phase-15-03: real three-source loop on a uniquely owned Compose stack."""
import json
import time
from verify_alerts import AlertsAcceptance, rule
from verify_plugin_metrics import Client, wait_until


def count_rule(source):
    labels = ({'service': 'backend', 'module': 'auth', 'message': 'user registered'} if source == 'logs'
              else {'source': 'monitor', 'event_name': 'exporter_plugin_installed', 'plugin_id': 'redis-exporter', 'operation': 'install'})
    return rule(source, source=source, selector={'labels': labels}, reducer='count', threshold=0)


class SourcesAcceptance(AlertsAcceptance):
    def es(self, path, body=None):
        self.owned_id('elasticsearch')
        args = ['exec', '-T', 'elasticsearch', 'curl', '--silent', '--show-error', '-w', '\n%{http_code}',
                '-H', 'Content-Type: application/json']
        if body is not None:
            args += ['-X', 'POST', '--data-binary', json.dumps(body)]
        raw = self.compose(*args, 'http://127.0.0.1:9200/'+path).stdout.decode()
        payload, status = raw.rsplit('\n', 1)
        return int(status), json.loads(payload)

    def probe_counts(self):
        # Read only timestamps for a bounded probe; never persist matching documents.
        for source in ['logs', 'events']:
            alias = 'gopulse-'+source+'-v1-read'
            labels = count_rule(source)['selector']['labels']
            terms = [{'term': {('metadata.'+k if source == 'events' and k in ['plugin_id','operation','error_code'] else k):v}}
                     for k,v in labels.items()]
            def document():
                status, body = self.es(alias+'/_search', {'size':1, '_source':['@timestamp'], 'query':{'bool':{'filter':terms}}})
                return body['hits']['hits'] if status == 200 else None
            hits = wait_until(document, source+' transported document', 120)
            timestamp = hits[0]['_source']['@timestamp']
            status, body = self.es(alias+'/_count', {'query':{'bool':{'filter':terms+[{'range':{'@timestamp':{'gte':timestamp,'lte':timestamp}}}]}}})
            assert status == 200 and body['count'] >= 1 and body['_shards']['failed'] == 0
            status, empty = self.es(alias+'/_count', {'query':{'range':{'@timestamp':{'gte':'2000-01-01T00:00:00Z','lte':'2000-01-01T00:01:00Z'}}}})
            assert status == 200 and empty['count'] == 0
            self.record(source+' locked Elasticsearch count: inclusive UTC nanosecond boundary and real empty window',
                        boundary_count=body['count'], empty_count=empty['count'])
        status, body = self.es('gopulse-acceptance-missing-'+self.token+'/_count?ignore_unavailable=false&allow_no_indices=false', {'query':{'match_all':{}}})
        assert status == 404 and body['error']['type']=='index_not_found_exception'
        self.record('missing index is distinguishable from successful empty count; conservatively unknown')

    def available(self):
        self.api('rules');self.api('history');self.api('current')
        user_id=self.admin.request('users/me')['data']['id']
        self.admin.request('admin/users/'+str(user_id))
        self.admin.request('posts?limit=1')
        self.user.request('posts?limit=1')

    def redaction(self):
        audit=self.sql("SELECT details_json FROM management_audit_events WHERE resource_type IN ('rule','alert') ORDER BY id")
        logs=self.compose('logs','--no-color','backend').stdout.decode(errors='replace')
        for value in [self.secret,self.values['VICTORIAMETRICS_PASSWORD'],'http://elasticsearch','http://victoriametrics','gopulse-logs-v1','gopulse-events-v1','/_count','_field_caps']:
            assert value not in audit and value not in logs, 'unsafe audit or Backend log output'
        self.record('alert audit and Backend logs contain no upstream secrets, addresses, aliases or count queries')

    def run(self):
        self.build();self.started=True
        self.compose('up','-d','--no-build','backend','monitor','router','marshaller',timeout=420)
        for service in ['backend','monitor','router','marshaller']:self.healthy(service)
        host=self.compose('port','backend','8080').stdout.decode().strip()
        self.admin,self.user=Client('http://'+host),Client('http://'+host)
        for client,name in [(self.admin,self.admin_name),(self.user,self.user_name)]:
            client.request('auth/register','POST',{'username':name,'password':self.auth_password},201)
        self.compose('exec','-T','backend','/usr/local/bin/admin-role','promote','--username',self.admin_name)
        catalog=self.api('catalog')['data']
        assert catalog['creatable_sources']==['metrics','logs','events']
        self.record('shared three-source catalog', vocabulary={s:{'values':{k:len(v) for k,v in catalog[s]['fields'].items()},'combinations':len(catalog[s]['allowed_combinations'])} for s in ['logs','events']})
        for client,status in [(Client(self.admin.base),401),(self.user,403)]:
            for method,path,body in [('GET','catalog',None),('GET','rules',None),('GET','current',None),('GET','history',None),('POST','rules',count_rule('logs')),('POST','rules',count_rule('events'))]:
                self.api(path,method,body,status,client)
        for source in ['logs','events']:
            for labels in [{'request_id':'0'*32},{'message':'*'},{'index':'private'},{'source':'audit'},
                           {'event_name':'exporter_plugin_started','severity':'error'} if source=='events' else {'service':'backend','module':'auth','message':'post created'}]:
                bad=count_rule(source);bad['selector']={'labels':labels};self.api('rules','POST',bad,400)
        self.record('three-source role denial and selector negative contracts')
        cfg={'config':{'host':'redis','port':6379,'database':0,'connect_timeout':'1s','scrape_timeout':'2s'},'secrets':{'password':self.secret}}
        self.admin.request('exporter-plugins/redis-exporter/install','POST',cfg,201)
        self.admin.request('exporter-plugins/redis-exporter/start','POST',None,200)
        wait_until(self.metric,'real Redis metrics',180)
        self.probe_counts()
        rules=[self.api('rules','POST',r,201)['data'] for r in [rule('metrics'),count_rule('logs'),count_rule('events')]]
        self.fault(True)
        for r in rules:self.state(r,'firing','ok')
        self.restart()
        for r in rules:
            wait_until(lambda:self.incidents(r)[0]['evaluation_count']>=3,'three true rounds',180)
            assert len(self.incidents(r))==1 and self.count(r,'alert.trigger')==1
        self.record('real three-source firing, three true rounds and Backend replacement preserve one incident/trigger',incidents=[self.incidents(r)[0] for r in rules])
        for service,affected in [('victoriametrics',{'metrics'}),('elasticsearch',{'logs','events'})]:
            self.owned_id(service);before={r['id']:self.get(r)['evaluation']['last_evaluated_at'] for r in rules}
            self.compose('stop',service)
            try:
                for r in rules:
                    expected='stale' if r['source'] in affected else 'ok'
                    wait_until(lambda:self.get(r)['evaluation']['last_evaluated_at']!=before[r['id']],'new fault-round evaluation',100)
                    value=self.state(r,'firing',expected)
                    assert value['evaluation']['error_code']==(r['source']+'_unknown' if expected=='stale' else '')
                    assert self.count(r,'alert.recover')==0
                self.available()
                self.record(service+' stopped: only its source adapters stale; MySQL management/social reads remain available',states=[self.get(r)['evaluation'] for r in rules])
            finally:
                self.compose('start',service);self.healthy(service)
            for r in rules:self.state(r,'firing','ok')
        self.restart(False)
        query='SELECT rule_id,state,data_status,last_evaluated_at,active_incident_id FROM alert_rule_states ORDER BY rule_id'
        before=self.sql(query);audits=self.sql('SELECT COUNT(*) FROM management_audit_events');incidents=self.sql('SELECT SUM(evaluation_count) FROM alert_incidents')
        time.sleep(35)
        assert before==self.sql(query) and audits==self.sql('SELECT COUNT(*) FROM management_audit_events') and incidents==self.sql('SELECT SUM(evaluation_count) FROM alert_incidents')
        self.available();self.record('disabled evaluator: state/incident/audit frozen, Backend ready and social/management APIs available')
        self.restart(True)
        self.fault(False)
        for r in rules:
            wait_until(lambda:self.get(r)['evaluation']['state']=='normal' and self.get(r)['evaluation']['data_status']=='ok','real window recovery',420)
            history=self.incidents(r)
            assert len(history)==1 and history[0]['status']=='recovered' and history[0]['recovered_at']
            assert self.count(r,'alert.trigger')==1 and self.count(r,'alert.recover')==1
            if r['source']!='metrics':assert history[0]['last_value']==0
        assert not self.api('current')['data']
        self.record('real metric normalization and document window expiry recover original incidents after evaluator resumes',incidents=[self.incidents(r)[0] for r in rules],audits={r['source']:{'trigger':self.count(r,'alert.trigger'),'recover':self.count(r,'alert.recover')} for r in rules})
        self.redaction()
        self.record('completed Phase-15-03 three-source fixed runtime acceptance')
