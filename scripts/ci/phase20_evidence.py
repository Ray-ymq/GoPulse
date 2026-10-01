"""Recompute diagnostic gates from private observations, never summary booleans."""
from __future__ import annotations
import hashlib
import json
import math
import re
from pathlib import Path

DIMENSIONS = ('business', 'metrics', 'logs', 'events')
ROTATED_TRACE_NAME = re.compile(r'spans-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}\.jsonl$')


def tool_inputs():
    root=Path(__file__).resolve().parents[2]
    inputs=list((root/'loadtest').rglob('*.go'))
    inputs += [root/'loadtest'/name for name in ('go.mod','go.sum','capacity-profile.json','capacity-profile.schema.json','report.schema.json','phase20-capacity-profile.schema.json')]
    inputs += [root/'scripts/ci'/name for name in ('phase19_capacity.py','phase19_sampler.py','phase19_evidence.py','phase20_diagnostic.py','phase20_sampler.py','phase20_evidence.py','test_phase20_diagnostic.py','test_phase20_sampler.py','test_phase20_evidence.py')]
    inputs += [root/'scripts/verify-phase20-diagnostic.sh',root/'scripts/verify-phase20-evidence.py',root/'deploy/compose.yaml',root/'deploy/runtime-contracts.json']
    return sorted(inputs)

class Incomplete(ValueError):
    pass

def digest(path):
    return 'sha256:' + hashlib.sha256(Path(path).read_bytes()).hexdigest()

def content_digest(title, content):
    value = json.dumps([title, content], ensure_ascii=False, separators=(',', ':'))
    return 'sha256:' + hashlib.sha256(value.encode()).hexdigest()

def read_jsonl(path):
    rows = [json.loads(line) for line in Path(path).read_text().splitlines()]
    if not rows:
        raise Incomplete('empty raw evidence: ' + str(path))
    return rows

def ledger_requests(rows, run_id, repeat, stage):
    arrivals, terminals = {}, {}
    fields = ('run_id','repeat','stage','window','slot_id','operation_id','record','actor_id',
              'method','route_template','object_key','scheduled_at','sent_at','completed_at',
              'status','outcome','request_id','response_id','response_revision','content_digest')
    for row in rows:
        if any(key not in row for key in fields):
            raise Incomplete('ledger fields missing')
        if (row['run_id'],row['repeat'],row['stage']) != (run_id,repeat,stage):
            raise Incomplete('cross-stage ledger source')
        if row['operation_id'] != f"{run_id}/{row['slot_id']}":
            raise Incomplete('operation binding mismatch')
        target = arrivals if row['record']=='arrival' else terminals if row['record']=='terminal' else None
        if target is None or row['slot_id'] in target:
            raise Incomplete('unknown or duplicate ledger record')
        target[row['slot_id']] = row
    expected = {slot for slot,row in arrivals.items() if row['outcome']=='scheduled'}
    if set(terminals) != expected or set(arrivals) != set(range(len(arrivals))):
        raise Incomplete('missing request terminal or arrival')
    for slot,row in terminals.items():
        if row['window'] != arrivals[slot]['window'] or not row['sent_at'] or not row['completed_at']:
            raise Incomplete('request timing/window missing')
        if row['completed_at'] < row['sent_at'] or row['sent_at'] < row['scheduled_at']:
            raise Incomplete('request timing order invalid')
    return list(terminals.values())

def associate(requests, before, after):
    """Object/revision association; outbox ID ranges only discover the event set."""
    for snapshot in (before, after):
        if any(key not in snapshot for key in ('posts','comments','relations','events')):
            raise Incomplete('consistent fact snapshot missing')
    posts = {int(row['id']):row for row in after['posts']}
    old_posts = {int(row['id']):row for row in before['posts']}
    comments = {int(row['id']):row for row in after['comments']}
    events = after['events']
    old_event_ids={o['event_id'] for o in before['events']}
    if len({e['event_id'] for e in events}) != len(events) or any(e['event_id'] in old_event_ids for e in events):
        raise Incomplete('duplicate or old outbox source')
    groups, used, violations = {}, set(), []
    for request in sorted(requests, key=lambda r:(r['sent_at'],r['slot_id'])):
        route = request['route_template']
        if request['method']=='GET' or route=='POST /api/v1/auth/login':
            continue
        if request['outcome'] not in ('accepted','timeout','transport_failure'):
            # Rejected writes still need reconciliation below if facts were produced.
            continue
        actor = request['actor_id']
        object_id = int(request['object_key'].split('/')[4]) if len(request['object_key'].split('/'))>4 else request['response_id']
        kind = ('post.created' if route=='POST /api/v1/posts' else
                'comment.created' if route.endswith('/comments') else
                'post.updated' if route.startswith('PATCH ') else
                'post.deleted' if route=='DELETE /api/v1/posts/:postId' else
                'post.liked' if route.endswith('/like') and request['method']=='PUT' else
                'user.followed' if route.endswith('/follow') and request['method']=='PUT' else None)
        if route=='POST /api/v1/posts':
            object_id=request['response_id']
            if not object_id:
                matches=[p for p in posts.values() if p['author_id']==actor and p['content_digest']==request['content_digest'] and int(p['id']) not in old_posts]
                if len(matches)>1: raise Incomplete('ambiguous timeout post reconciliation')
                object_id=int(matches[0]['id']) if matches else 0
        group_key=f"{actor}/{request['object_key']}/{object_id}"
        group=groups.setdefault(group_key, {'fact_group_id':group_key,'operation_ids':[], 'event_ids':[], 'object_id':object_id,'actor_id':actor,'routes':[]})
        group['operation_ids'].append(request['operation_id']);group['routes'].append(route)
        matched=[]
        for event in events:
            payload=event['payload']
            if payload['actor_id']!=actor or payload['event_type']!=kind: continue
            if kind=='user.followed': same=payload.get('recipient_id')==object_id
            elif kind=='comment.created': same=(payload.get('post_id')==object_id and (payload.get('comment_id')==request['response_id'] if request['response_id'] else payload.get('comment_id') in comments and comments[payload['comment_id']]['content_digest']==request['content_digest']))
            else: same=payload.get('post_id')==object_id
            if kind=='post.updated' and request['response_revision']: same = same and payload.get('content_revision')==request['response_revision']
            if same: matched.append(event)
        if kind in ('post.created','comment.created','post.updated','post.deleted') and request['outcome']=='accepted' and len(matched)!=1:
            violations.append({'operation_id':request['operation_id'],'reason':'accepted write event missing or duplicated'})
        for event in matched:
            used.add(event['event_id'])
            if event['event_id'] not in group['event_ids']:group['event_ids'].append(event['event_id'])
        if request['outcome']=='accepted' and kind=='post.created':
            fact=posts.get(object_id)
            if not fact or fact['author_id']!=actor or fact['content_digest']!=request['content_digest']:
                violations.append({'operation_id':request['operation_id'],'reason':'accepted post fact mismatch'})
        if request['outcome']=='accepted' and kind=='comment.created':
            fact=comments.get(request['response_id'])
            if not fact or fact['author_id']!=actor or fact['post_id']!=object_id or fact['content_digest']!=request['content_digest']:
                violations.append({'operation_id':request['operation_id'],'reason':'accepted comment fact mismatch'})
    if used!={e['event_id'] for e in events}:
        raise Incomplete('outbox event lacks unambiguous request group')
    # Per-actor requests run serially. Recompute relationship effects from actual
    # send order, before state and accepted responses, never response arrival order.
    relationships={tuple(r) for r in before['relations']}
    expected_events={}
    for r in sorted(requests,key=lambda x:(x['sent_at'],x['slot_id'])):
        if r['outcome']!='accepted' or not r['object_key']: continue
        path=r['object_key'].split('/')
        if path[-1] not in ('like','follow','bookmark'): continue
        target=int(path[4]);relation=(path[-1],r['actor_id'],target)
        if r['method']=='PUT':
            if relation not in relationships and path[-1]!='bookmark': expected_events[relation]=expected_events.get(relation,0)+1
            relationships.add(relation)
        else: relationships.discard(relation)
    affected={(r['object_key'].split('/')[-1],r['actor_id'],int(r['object_key'].split('/')[4])) for r in requests if r['object_key'].split('/')[-1] in ('like','follow','bookmark')}
    actual={tuple(r) for r in after['relations']}
    if relationships & affected != actual & affected:violations.append({'reason':'relationship final fact mismatch'})
    for relation in affected:
        kind,actor,target=relation
        event_type={'like':'post.liked','follow':'user.followed'}.get(kind)
        found=[e for e in events if e['payload']['event_type']==event_type and e['payload']['actor_id']==actor and e['payload'].get('recipient_id' if kind=='follow' else 'post_id')==target]
        if len(found)!=expected_events.get(relation,0): violations.append({'reason':'idempotent relationship event count mismatch','relation':list(relation)})
    for post_id in {int(r['object_key'].split('/')[4]) for r in requests if r['route_template'] in ('PATCH /api/v1/posts/:postId','DELETE /api/v1/posts/:postId')}:
        rows=[r for r in requests if r['object_key']==f'/api/v1/posts/{post_id}' and r['outcome']=='accepted']
        rows.sort(key=lambda r:r['sent_at'])
        if rows and rows[-1]['method']=='DELETE' and post_id in posts: violations.append({'reason':'deleted post fact remains','post_id':post_id})
        elif rows and rows[-1]['method']=='PATCH':
            final=posts.get(post_id)
            if not final or final['content_revision']!=rows[-1]['response_revision'] or final['content_digest']!=rows[-1]['content_digest']:violations.append({'reason':'latest edit fact mismatch','post_id':post_id})
    return {'groups':list(groups.values()),'violations':violations,'reconciliation':[{'operation_id':r['operation_id'],'group_ids':[g['fact_group_id'] for g in groups.values() if r['operation_id'] in g['operation_ids']]} for r in requests if r['outcome'] in ('timeout','transport_failure')]}

def business_ready(after, association, observed):
    if association['violations']:return False
    if any(e['status']!='published' for e in observed['events']):return False
    expected_ids={e['event_id'] for e in after['events']}
    if {e['event_id'] for e in observed['events']}!=expected_ids:raise Incomplete('recovery event set drift')
    affected_posts={g['object_id'] for g in association['groups'] if any(route in ('POST /api/v1/posts','PATCH /api/v1/posts/:postId','DELETE /api/v1/posts/:postId') for route in g['routes'])}
    expected={int(p['id']):p for p in after['posts'] if int(p['id']) in affected_posts}
    projections={int(p['id']):p for p in observed['search']}
    if set(projections)!=set(expected):return False
    if any(p['content_revision']!=projections[k]['content_revision'] or p['content_digest']!=projections[k]['content_digest'] for k,p in expected.items()):return False
    notifications=observed['notifications']
    surviving_posts={x['id'] for x in after['posts']}
    for event in after['events']:
        p=event['payload']; found=[n for n in notifications if n['source_event_id']==event['event_id']]
        needs=p['event_type'] in ('comment.created','post.liked','user.followed') and p['actor_id']!=p.get('recipient_id')
        if len(found)!=(1 if needs else 0):return False
        if needs:
            n=found[0]
            if n['recipient_id']!=p['recipient_id'] or n['actor_id']!=p['actor_id'] or n['type']!=p['event_type']:return False
            expected_post=p.get('post_id') if p.get('post_id') in surviving_posts else None
            if n['post_id']!=expected_post or n['comment_id']!=(p.get('comment_id') if expected_post else None):return False
    return True

LOG_QUERY_FIELDS = ('timestamp','level','service','instance_id','module','message','request_id','trace_id','span_id',
    'event_id','event_type','user_id','post_id','content_revision','comment_id','notification_id','outbox_id',
    'method','route','status','duration_ms','response_bytes','error_code','reason','operation','resource','stage','result','attempt','batch_size',
    'document_count','panic_recovered','response_committed')

def management_payload(channel, payload):
    if channel=='logs':
        return {key:payload[key] for key in LOG_QUERY_FIELDS if key in payload and payload[key] is not None and (payload[key]!='' or key in ('timestamp','level','service','module','message'))}
    return {key:value for key,value in payload.items() if key!='event_schema_version'}


def marker_ready(channel, marker, observation, run_id):
    if marker.get('run_id')!=run_id or marker.get('channel')!=channel:raise Incomplete('cross-stage marker')
    required=('marker_id','t_origin','message_id','source','timestamp','topic','partition','offset','envelope','accepted')
    if any(k not in marker for k in required) or not marker['accepted']:raise Incomplete('marker generation/acceptance missing')
    if not isinstance(marker['partition'],int) or not isinstance(marker['offset'],int) or marker['partition']<0 or marker['offset']<0:raise Incomplete('partition waterline invalid')
    envelope=marker['envelope']
    if any(envelope.get(k)!=marker[k] for k in ('message_id','source','timestamp')) or envelope.get('type')!=channel:raise Incomplete('Envelope identity mismatch')
    payload=envelope['payload']
    if channel=='metrics':
        labels={'source':envelope['source'],'target_id':payload['target_id'],'producer_kind':'component','producer_id':payload['producer_id']}
        choices=[s for s in payload['samples'] if s['name']==marker['series']['name'] and {**labels,**s.get('labels',{})}==marker['series']['labels'] and s['value']==marker['value']]
        if len(choices)!=1:raise Incomplete('metric marker is not bound to raw snapshot series/value')
    elif channel=='logs':
        generation=marker['generation_evidence']
        if generation['method']!='GET' or generation['route']!='/api/v1/users/me' or generation['status']!=200 or generation['request_id']!=payload['request_id'] or generation['t_origin']!=marker['t_origin']:raise Incomplete('log marker generation contract mismatch')
    elif channel=='events':
        operation=marker['operation_evidence']
        if operation['before']['observed_state']!='running' or operation['stop_response']['observed_state']!='stopped' or operation['start_response']['observed_state']!='running' or operation['start_response']['version']!=payload['metadata']['plugin_version'] or payload['metadata']['from_state']!='stopped' or payload['metadata']['to_state']!='running':raise Incomplete('event marker real operation evidence missing')
    if not observation:return False
    if observation.get('message_id')!=marker['message_id']:raise Incomplete('query identity mismatch')
    if channel=='metrics':
        return observation['series']==marker['series'] and observation['value']==marker['value'] and abs(observation['timestamp_ms']-marker['timestamp_ms'])<=1
    if observation.get('stored_id')!=marker['message_id']:raise Incomplete('storage identity mismatch')
    stored=dict(observation['stored_source']);stored['timestamp']=stored.pop('@timestamp')
    stored.pop('event_schema_version',None);stored.pop('log_schema_version',None)
    raw=dict(envelope['payload']);raw.pop('event_schema_version',None);raw.pop('log_schema_version',None)
    if stored!=raw:raise Incomplete('storage payload differs from Envelope')
    expected=management_payload(channel,envelope['payload'])
    return observation['entry']==expected

def recovery_result(channel, origin, observations, deadline=120):
    if not math.isfinite(origin) or deadline!=120:raise Incomplete('recovery timing contract invalid')
    previous=origin
    for row in observations:
        at=row['observed_monotonic']
        if not math.isfinite(at) or at<previous:raise Incomplete('recovery observation ordering invalid')
        previous=at
        if row['ready'] and at<=origin+deadline:
            return {'first_success':at,'elapsed_seconds':at-origin,'timed_out':False,'classification':None}
    return {'first_success':None,'elapsed_to_deadline':deadline,'timed_out':True,'classification':'unresolved'}

def verify_directory(directory, formal=True):
    root=Path(directory)
    document=json.loads((root/'diagnostic.json').read_text())
    if document['schema']!='gopulse.phase20.diagnostic.v1' or document['formal']!=formal:raise Incomplete('diagnostic mode/schema mismatch')
    profile=json.loads((root/'profile.json').read_text())
    if digest(root/'profile.json')!=document['profile_sha256']:raise Incomplete('profile digest mismatch')
    candidate=json.loads((root/'candidate-manifest.json').read_text())
    binding={'version':candidate['version'],'revision':candidate['revision'],'manifest_sha256':digest(root/'candidate-manifest.json')}
    if document['candidate']!=binding or candidate['version']!=profile['target_candidate_version']:raise Incomplete('candidate reference mismatch')
    if document['execution_status']!='complete':raise Incomplete('execution not complete')
    if formal:
        preflight=document['preflight'];path=Path(preflight['path'])
        if digest(path)!=preflight['sha256']:raise Incomplete('selected preflight changed')
        checked=json.loads(path.read_text())
        if checked['candidate']!=binding or checked['tools']!=document['tools'] or checked['profile_sha256']!=document['profile_sha256'] or checked['host_identity']!=document['host_identity']:raise Incomplete('preflight candidate contract drift')
        verify_directory(path.parent,formal=False)
    expected={(r,s['name']) for r in range(1,4) for s in profile['stages']} if formal else {(1,profile['stages'][0]['name']),(1,profile['stages'][1]['name'])}
    cells=document['cells']
    if {(c['repeat'],c['stage']) for c in cells}!=expected or len(cells)!=len(expected):raise Incomplete('required isolated cells missing')
    if document['tool_dependencies']!={'kafka-python':'2.2.15','python-snappy':'0.7.3','cramjam':'2.11.0'}:raise Incomplete('observer dependency binding drift')
    tools=document['tools']
    project_root=Path(__file__).resolve().parents[2]
    if set(tools)!={str(p.relative_to(project_root)) for p in tool_inputs()}:raise Incomplete('tool input binding set incomplete')
    for name,bound_digest in tools.items():
        path=project_root/name
        if not path.resolve().is_relative_to(project_root) or digest(path)!=bound_digest:raise Incomplete('diagnostic tool candidate changed')
    tests=document['self_tests'];test_path=root/tests['path']
    if digest(test_path)!=tests['sha256']:raise Incomplete('self-test receipt digest mismatch')
    cases=json.loads(test_path.read_text())
    if cases['formal'] or {c['case_id'] for c in cases['cases']}!={f'D{i:02d}' for i in range(1,7)} or any(c['result']!='pass' for c in cases['cases']):raise Incomplete('required D01-D06 self-tests missing/failed')
    if digest(root/'self-tests.txt')!=cases['raw_sha256']:raise Incomplete('self-test raw execution changed')
    results=[];previous_cleanup=None
    for cell in cells:
        if cell['candidate']!=binding:raise Incomplete('cell candidate drift')
        files={}
        for name,ref in cell['raw'].items():
            path=root/ref['path']
            if path.resolve().is_relative_to(root.resolve()) is False or not path.is_file() or digest(path)!=ref['sha256']:raise Incomplete('raw source/digest mismatch')
            files[name]=path
        required={'ledger','load','before','after','recovery','markers','lifecycle','resources','cli_overhead','growth'}
        if set(files)-{'overhead'}!=required:raise Incomplete('raw artifact set incomplete')
        if cell['repeat']==1 and cell['stage']=='rps-50':
            if 'overhead' not in files:raise Incomplete('same-configuration observer comparison missing')
            verify_overhead(json.loads(files['overhead'].read_text()),profile['diagnostic']['observer_comparison'])
        load=json.loads(files['load'].read_text())
        if load['candidate']!=binding or load['profile']['sha256']!=document['profile_sha256'] or load['schema_version']!='gopulse.phase20.load.v1' or load['execution_status']!='complete':raise Incomplete('load binding/status mismatch')
        requests=ledger_requests(read_jsonl(files['ledger']),cell['run_id'],cell['repeat'],cell['stage'])
        if load['ledger']['sha256']!=digest(files['ledger']):raise Incomplete('load ledger binding mismatch')
        before=json.loads(files['before'].read_text());after=json.loads(files['after'].read_text())
        for fact in (before,after):
            if fact['run_id']!=cell['run_id'] or not fact['consistent_read']:raise Incomplete('fact snapshot consistency/binding missing')
        association=associate(requests,before,after)
        markers=json.loads(files['markers'].read_text())
        observations=read_jsonl(files['recovery'])
        dimensions={}
        for channel in DIMENSIONS:
            rows=[]
            origin=cell['origins'][channel]
            for o in observations:
                if o['run_id']!=cell['run_id']:raise Incomplete('cross-stage recovery source')
                if o['channel']!=channel:continue
                if channel=='business':ready=business_ready(after,association,o['facts'])
                else:
                    if channel not in markers:raise Incomplete(channel+' marker not generated')
                    if markers[channel]['t_origin']!=origin:raise Incomplete('marker origin was reset')
                    ready=marker_ready(channel,markers[channel],o['query'],cell['run_id'])
                rows.append({'observed_monotonic':o['observed_monotonic'],'ready':ready})
            if not rows or rows[-1]['observed_monotonic']<origin+120 and not any(r['ready'] for r in rows):raise Incomplete('recovery deadline evidence missing')
            dimensions[channel]=recovery_result(channel,origin,rows)
        lifecycle=read_jsonl(files['lifecycle'])
        names=[x['event'] for x in lifecycle]
        if names!=['project_started','load_stopped','requests_drained','recovery_finished','workers_joined','project_cleaned']:raise Incomplete('stage causal lifecycle incomplete')
        times=[x['monotonic'] for x in lifecycle]
        if times!=sorted(times) or any(x['run_id']!=cell['run_id'] for x in lifecycle):raise Incomplete('stage lifecycle order/source invalid')
        if previous_cleanup is not None and times[0]<=previous_cleanup:raise Incomplete('next project started before cleanup')
        previous_cleanup=times[-1]
        receipt=lifecycle[-1]['receipt']
        if receipt['inventory_before']!=receipt['inventory_after']:raise Incomplete('cleanup inventory drift')
        if lifecycle[-1]['receipt']['status']!='passed' or lifecycle[-1]['receipt']['global_prune'] or not lifecycle[-1]['receipt']['owned']:raise Incomplete('unsafe ownership cleanup receipt')
        cli=json.loads(files['cli_overhead'].read_text())
        if cli['formal_load_window'] or cli['duration_seconds']<0:raise Incomplete('CLI probe timing invalid')
        growth=json.loads(files['growth'].read_text())
        for side in ('before','after'):
            if growth[side]['_shards']['failed'] or growth[side]['_all']['primaries']['docs']['count']<0 or growth[side]['_all']['primaries']['store']['size_in_bytes']<0:raise Incomplete('growth observation incomplete')
        resource_rows=read_jsonl(files['resources'])
        if any(r['run_id']!=cell['run_id'] or r['missing_signals'] or r['failure'] for r in resource_rows):raise Incomplete('sampling failure/missing resource evidence')
        synchronous=recompute_load(read_jsonl(files['ledger']),load,profile)
        passed=synchronous and not association['violations'] and all(not d['timed_out'] for d in dimensions.values())
        classification='product_failure' if association['violations'] else 'unresolved' if not passed else None
        results.append({'repeat':cell['repeat'],'stage':cell['stage'],'dimensions':dimensions,'synchronous_pass':synchronous,'passed':passed,'classification':classification,'association':association})
    measurements=[]
    for cell in cells:
        load_ref=cell['raw']['load'];load=json.loads((root/load_ref['path']).read_text())
        measurements.append({'repeat':cell['repeat'],'stage':cell['stage'],'achieved_rps':load['measurement']['achieved_rps'],'p95_ms':load['measurement']['latency']['p95_ms'],'p99_ms':load['measurement']['latency']['p99_ms']})
    return {'execution_status':'complete','capability_status':'target_met' if all(r['passed'] for r in results) else 'boundary_found','cells':results,'measurements':measurements,'aggregates':aggregate_measurements(measurements) if formal else None}

def recompute_load(rows,load,profile):
    arrivals=[r for r in rows if r['record']=='arrival' and r['window']=='measurement']
    terminals=[r for r in rows if r['record']=='terminal' and r['window']=='measurement']
    stage=next(s for s in profile['stages'] if s['name']==load['stage'])
    report=load['measurement'];allowed={r['template']:r['allowed_statuses'] for r in profile['workload']['routes']}
    if len(arrivals)!=round(stage['target_rps']*stage['measurement_seconds']):raise Incomplete('arrival recipe drift')
    for r in terminals:
        if r['route_template'] not in allowed:raise Incomplete('unregistered workload route')
        if r['outcome']=='accepted' and r['status'] not in allowed[r['route_template']]:raise Incomplete('accepted response violates route contract')
        if not math.isfinite(r['latency_ms']) or r['latency_ms']<0:raise Incomplete('invalid raw request duration')
    counts={'requests':len(terminals),'succeeded':0,'explicit_rejects':0,'rejected_429':0,'rejected_503':0,'timeouts':0,'transport_errors':0,'unexpected_errors':0}
    for r in terminals:
        key={'accepted':'succeeded','explicit_reject':'explicit_rejects','timeout':'timeouts','transport_failure':'transport_errors','unexpected_response':'unexpected_errors'}.get(r['outcome'])
        if key is None:raise Incomplete('invalid request terminal classification')
        counts[key]+=1
        if key=='explicit_rejects':counts['rejected_'+str(r['status'])]+=1
    values=sorted(r['latency_ms'] for r in terminals)
    latency={k:values[math.ceil(f*(len(values)-1))] if values else 0 for k,f in [('p50_ms',.5),('p95_ms',.95),('p99_ms',.99)]};latency['max_ms']=max(values,default=0)
    scheduled=sum(r['outcome']=='scheduled' for r in arrivals);dropped=len(arrivals)-scheduled
    if counts!=report['outcomes'] or scheduled!=report['scheduled_slots'] or dropped!=report['dropped_slots'] or len(terminals)!=report['completed_requests']:raise Incomplete('load counters disagree with ledger')
    if any(abs(report['latency'][k]-v)>1e-6 for k,v in latency.items()):raise Incomplete('load quantiles disagree with raw request durations')
    if report['achieved_rps']!=len(terminals)/stage['measurement_seconds'] or report['duration_seconds']!=stage['measurement_seconds'] or report['target_rps']!=stage['target_rps']:raise Incomplete('load throughput/window drift')
    max_lag=max((r['load_schedule_lag_ms'] for r in arrivals),default=0)
    if abs(max_lag-report['max_schedule_lag_ms'])>1:raise Incomplete('load scheduling evidence mismatch')
    g=profile['gates']['synchronous']
    return (report['achieved_rps']>=stage['target_rps']*g['min_achieved_rps_ratio'] and latency['p95_ms']<=g['max_p95_ms'] and latency['p99_ms']<=g['max_p99_ms'] and dropped==0 and max_lag<=g['max_schedule_lag_ms'] and all(counts[k]==0 for k in ('timeouts','transport_errors','unexpected_errors','explicit_rejects')))

def verify_overhead(value,contract):
    if value['formal'] or [t['sampling_enabled'] for t in value['trials']]!=[False,True]:raise Incomplete('observer trial modes invalid')
    count=round(contract['target_rps']*contract['seconds'])
    for trial in value['trials']:
        if trial['route']!=contract['route'] or trial['target_rps']!=contract['target_rps'] or trial['arrival_seconds']!=contract['seconds']:raise Incomplete('observer workload configuration drift')
        rows=trial['requests']
        if len(rows)!=count or [r['slot_id'] for r in rows]!=list(range(count)):raise Incomplete('observer trial arrivals missing')
        for row in rows:
            if row['status']!=200 or row['scheduled_monotonic']>row['sent_monotonic'] or row['sent_monotonic']>row['completed_monotonic']:raise Incomplete('observer trial request failed')
        if trial['sampling_enabled']:
            if not trial['sampler_records'] or any(r['failure'] or r['missing_signals'] for r in trial['sampler_records']):raise Incomplete('observer enabled trial has missing sampling evidence')
        elif trial['sampler_records']:raise Incomplete('observer disabled trial sampled')


def aggregate_measurements(rows):
    import statistics
    result={}
    for stage in ('rps-50','rps-100','rps-150','rps-200'):
        selected=[r for r in rows if r['stage']==stage]
        if len(selected)!=3:raise Incomplete('raw repetition statistics missing')
        result[stage]={}
        for field in ('achieved_rps','p95_ms','p99_ms'):
            values=[r[field] for r in selected];mean=statistics.mean(values)
            result[stage][field]={'raw':values,'median':statistics.median(values),'min':min(values),'max':max(values),'cv':statistics.pstdev(values)/mean*100 if mean else 0}
    return result


def publication_projection(root):
    """Allowlisted aggregate projection; private object/actor/request IDs never leave root."""
    root=Path(root)
    result=verify_directory(root)
    document=json.loads((root/'diagnostic.json').read_text())
    cells=[]
    for source,checked in zip(document['cells'],result['cells']):
        files={k:root/v['path'] for k,v in source['raw'].items()}
        load=json.loads(files['load'].read_text())
        lifecycle=read_jsonl(files['lifecycle'])
        growth=json.loads(files['growth'].read_text())
        def sizes(value):
            return {'documents':value['_all']['primaries']['docs']['count'],'store_bytes':value['_all']['primaries']['store']['size_in_bytes']}
        resources=read_jsonl(files['resources'])
        cells.append({'repeat':source['repeat'],'stage':source['stage'],
            'dimensions':checked['dimensions'],'synchronous_pass':checked['synchronous_pass'],
            'passed':checked['passed'],'classification':checked['classification'],
            'association_counts':{'groups':len(checked['association']['groups']),'events':len({event for group in checked['association']['groups'] for event in group['event_ids']}),'violations':len(checked['association']['violations'])},
            'measurement':load['measurement'],'drain_elapsed_seconds':load['drain_elapsed_seconds'],
            'cleanup':{'status':lifecycle[-1]['receipt']['status'],'owned':True,'global_prune':False,'inventory_restored':lifecycle[-1]['receipt']['inventory_before']==lifecycle[-1]['receipt']['inventory_after']},
            'source_digests':{k:v['sha256'] for k,v in source['raw'].items()},
            'sampler':{'records':len(resources),'missing':sum(len(r['missing_signals']) for r in resources),'failures':sum(bool(r['failure']) for r in resources)},
            'cli_probe_seconds':json.loads(files['cli_overhead'].read_text())['duration_seconds'],
            'observability_growth':{'before':sizes(growth['before']),'after':sizes(growth['after'])}})
    overhead=json.loads((root/document['cells'][0]['raw']['overhead']['path']).read_text())
    observer=[]
    for trial in overhead['trials']:
        latencies=sorted((r['completed_monotonic']-r['sent_monotonic'])*1000 for r in trial['requests'])
        observer.append({'sampling_enabled':trial['sampling_enabled'],'requests':len(latencies),'p95_ms':latencies[math.ceil(.95*(len(latencies)-1))],'max_ms':max(latencies),'sampler_records':len(trial['sampler_records']),'observer_process_cpu_seconds':trial['observer_process_cpu_seconds'],'host_cpu_before':trial['host_cpu_before'],'host_cpu_after':trial['host_cpu_after']})
    return {'schema':'gopulse.phase20.publication.v1','candidate':document['candidate'],
        'profile_sha256':document['profile_sha256'],'tools':document['tools'],
        'tool_dependencies':document['tool_dependencies'],'diagnostic_sha256':digest(root/'diagnostic.json'),
        'execution_status':result['execution_status'],'capability_status':result['capability_status'],
        'aggregates':result['aggregates'],'observer_comparison':observer,'cells':cells}


def verify_publication(root, published):
    root=Path(root);published=Path(published)
    names={'baseline.json','candidate-manifest.json','profile.json','self-tests.json','self-tests.txt','publication-manifest.json'}
    if {p.name for p in published.iterdir()}!=names or any(not p.is_file() for p in published.iterdir()):
        raise Incomplete('publication file allowlist mismatch')
    if json.loads((published/'baseline.json').read_text())!=publication_projection(root):
        raise Incomplete('published projection differs from verified private facts')
    for name in ('candidate-manifest.json','profile.json','self-tests.json','self-tests.txt'):
        if (published/name).read_bytes()!=(root/name).read_bytes():raise Incomplete('published source differs: '+name)
    expected={name:digest(published/name) for name in names-{'publication-manifest.json'}}
    if json.loads((published/'publication-manifest.json').read_text())!={'schema':'gopulse.phase20.publication-manifest.v1','files':expected}:
        raise Incomplete('published file digests differ')
    return {'execution_status':'complete','publication_status':'verified','files':expected}


def verify_retention_directory(directory):
    """Recompute the lifecycle cases from the private retention receipts."""
    root = Path(directory).resolve()
    document = json.loads((root / 'retention.json').read_text(encoding='utf-8'))
    if document.get('schema') != 'gopulse.phase20.retention.v1' or document.get('execution_status') != 'complete':
        raise Incomplete('retention schema or execution status is incomplete')
    manifest_path = root / 'candidate-manifest.json'
    candidate = json.loads(manifest_path.read_text(encoding='utf-8'))
    candidate_value = candidate.get('candidate', candidate)
    if candidate_value.get('version') not in {'2.2.4', '2.2.5'} or not isinstance(candidate_value.get('revision'), str) or len(candidate_value['revision']) != 40:
        raise Incomplete('retention candidate version/revision is invalid')
    binding = {'version': candidate_value['version'], 'revision': candidate_value['revision'], 'manifest_sha256': digest(manifest_path)}
    if document.get('candidate') != binding:
        raise Incomplete('retention candidate binding drift')
    expected_cases = {f'R0{i}' for i in range(1, 9)}
    cases = document.get('cases')
    if not isinstance(cases, list) or {case.get('case_id') for case in cases} != expected_cases or len(cases) != 8:
        raise Incomplete('R01-R08 cases are incomplete')
    if any(case.get('status') != 'pass' for case in cases):
        raise Incomplete('a retention case is not passed')
    for case in cases:
        relative = case.get('path')
        if not isinstance(relative, str) or Path(relative).is_absolute() or '..' in Path(relative).parts:
            raise Incomplete('retention case path escapes evidence root')
        path = root / relative
        if not path.is_file() or digest(path) != case.get('sha256'):
            raise Incomplete('retention case raw digest mismatch')
        raw = json.loads(path.read_text(encoding='utf-8'))
        if raw.get('case_id') != case['case_id'] or raw.get('status') != 'pass':
            raise Incomplete('retention case raw receipt is incomplete')

    implementation = document.get('implementation')
    if implementation != {
        'go_integration': 'raw/elasticsearch-go-test.txt',
        'config': {
            'logs_days': 7, 'events_days': 7, 'cycle_seconds': 60, 'batch_indices': 16,
            'request_timeout_seconds': 3, 'round_timeout_seconds': 15,
            'retry_min_seconds': 0.25, 'retry_max_seconds': 5, 'max_retries': 3,
            'catchup_deadline_seconds': 60,
        },
        'prefixes': ['gopulse-logs-v1-', 'gopulse-events-v1-'],
        'aliases': ['gopulse-logs-v1-read', 'gopulse-events-v1-read'],
    }:
        raise Incomplete('retention implementation contract drift')
    integration = root / implementation['go_integration']
    if not integration.is_file() or 'ok  ' not in integration.read_text(encoding='utf-8'):
        raise Incomplete('real Elasticsearch integration output is missing')
    r01 = json.loads((root / 'raw/R01.json').read_text(encoding='utf-8'))
    if r01['cutoff'] == '' or set(r01['ownership_proof']) != {'cluster_uuid', 'strict_mapping', '_meta', 'fixed_alias'} or not any('R01 boundary' in item for item in r01['sequence']):
        raise Incomplete('R01 did not prove date/ownership boundaries')
    r02 = json.loads((root / 'raw/R02.json').read_text(encoding='utf-8'))
    old = r02['facts']
    current = r02['current']
    if old['logs_after']['exists'] or old['events_after']['exists'] or not current['logs_after']['exists'] or not current['events_after']['exists'] or r02['query_aliases'] != ['gopulse-logs-v1-read', 'gopulse-events-v1-read']:
        raise Incomplete('R02 deletion/current query facts are inconsistent')
    r03 = json.loads((root / 'raw/R03.json').read_text(encoding='utf-8'))
    r03_output = (root / 'raw' / r03['go_test']).read_text(encoding='utf-8')
    if 'ok  ' not in r03_output or set(r03['expired_codes']) != {'expired_log_retention', 'expired_event_retention'} or not r03['permanent_commit'] or not r03['no_index_revival']:
        raise Incomplete('R03 late-record/retry evidence is incomplete')
    r04 = json.loads((root / 'raw/R04.json').read_text(encoding='utf-8'))
    r04_output = (root / 'raw' / r04['go_test']).read_text(encoding='utf-8')
    if 'ok  ' not in r04_output:
        raise Incomplete('R04 real writer/cleanup integration output is missing')
    sequence = r04['sequence']
    positions = {needle: next((index for index, item in enumerate(sequence) if needle in item), -1) for needle in ('R04 writer precheck passed', 'R04 cleanup deleted before in-flight write release', 'R04 in-flight write released and final storage queried')}
    if any(value < 0 for value in positions.values()) or not (positions['R04 writer precheck passed'] < positions['R04 cleanup deleted before in-flight write release'] < positions['R04 in-flight write released and final storage queried']):
        raise Incomplete('R04 barrier order is not proven')
    r05 = json.loads((root / 'raw/R05.json').read_text(encoding='utf-8'))
    if r05['catchup_deadline_seconds'] != 60 or not r05['permission_failure_not_hidden'] or not any('R05 transient' in item for item in r05['sequence']):
        raise Incomplete('R05 failure recovery evidence is incomplete')
    r06 = json.loads((root / 'raw/R06.json').read_text(encoding='utf-8'))
    if not r06['idempotent_404_allowed'] or not any('R06 two runners' in item for item in r06['sequence']):
        raise Incomplete('R06 concurrent idempotency evidence is incomplete')
    r07 = json.loads((root / 'raw/R07.json').read_text(encoding='utf-8'))
    if r07['aliases'] != ['gopulse-logs-v1-read', 'gopulse-events-v1-read'] or not r07['expired_indices_empty'] or not r07['business_fixture_preserved']:
        raise Incomplete('R07 query compatibility evidence is incomplete')
    r08 = json.loads((root / 'raw/R08.json').read_text(encoding='utf-8'))
    vm = json.loads((root / 'raw' / r08['vm']).read_text(encoding='utf-8'))
    trace = json.loads((root / 'raw' / r08['trace']).read_text(encoding='utf-8'))
    if vm['retention_period'] != '30d' or not vm['current_query'] or not vm['within_window_submitted'] or not vm['outside_window_submitted'] or vm['observed_physical_reclaim']:
        raise Incomplete('R08 VictoriaMetrics evidence misstates the native retention limitation')
    inventory = trace['inventory']
    if len(inventory) > 4 or sum(item['bytes'] for item in inventory) > 64 * 1024 * 1024 or any(item['bytes'] > 16 * 1024 * 1024 for item in inventory) or not trace['rotation_observed']:
        raise Incomplete('R08 Collector file budget is exceeded or rotation was not observed')
    if any(item['path'] != '/var/lib/gopulse/trace/' + item['name'] or not (item['name'] == 'spans.jsonl' or item['name'].startswith('spans.jsonl.') or ROTATED_TRACE_NAME.fullmatch(item['name'])) for item in inventory):
        raise Incomplete('R08 Collector artifact ownership path is invalid')
    artifacts = document.get('dependency_fixtures', {})
    for key in ('elasticsearch', 'victoriametrics', 'collector'):
        if not isinstance(artifacts.get(key, {}).get('id'), str) or not artifacts[key]['id'].startswith('sha256:'):
            raise Incomplete('dependency artifact identity is missing: ' + key)
    return {'execution_status': 'complete', 'case_status': {case['case_id']: 'pass' for case in cases}, 'candidate': binding, 'trace_files': len(inventory), 'trace_bytes': sum(item['bytes'] for item in inventory), 'vm_physical_reclaim_observed': False}
