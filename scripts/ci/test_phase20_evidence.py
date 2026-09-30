import copy
import unittest
from phase20_evidence import Incomplete, associate, business_ready, marker_ready, recovery_result, ledger_requests

def empty():return {'posts':[],'comments':[],'relations':[],'events':[]}

def request(method,route,key,slot=0,revision=0,response_id=0):
    return {'run_id':'r','repeat':1,'stage':'rps-50','window':'measurement','slot_id':slot,'operation_id':f'r/{slot}','record':'terminal','actor_id':1,'method':method,'route_template':route,'object_key':key,'scheduled_at':f'2026-09-30T00:00:0{slot}Z','sent_at':f'2026-09-30T00:00:0{slot}Z','completed_at':f'2026-09-30T00:00:0{slot}Z','status':204,'outcome':'accepted','request_id':'a'*32,'response_id':response_id,'response_revision':revision,'content_digest':'digest'}

def post_event(kind,revision=0,event_id='event'):
    return {'outbox_id':1,'event_id':event_id,'status':'published','payload':{'actor_id':1,'post_id':9,'event_type':kind,'content_revision':revision}}

def event_marker():
    payload={'event_schema_version':1,'timestamp':'2026-09-30T00:00:00Z','event_name':'exporter_plugin_started','source':'monitor','severity':'info','message':'exporter plugin started','metadata':{'plugin_id':'redis-exporter','plugin_version':'2.1.4','operation':'start','from_state':'stopped','to_state':'running'}}
    return {'run_id':'r','channel':'events','marker_id':'r/events','t_origin':1,'message_id':'a'*32,'source':'monitor','timestamp':payload['timestamp'],'topic':'gopulse-observability-v1','partition':0,'offset':7,'accepted':True,'operation_evidence':{'before':{'observed_state':'running'},'stop_response':{'observed_state':'stopped'},'start_response':{'observed_state':'running','version':'2.1.4'}},'envelope':{'message_id':'a'*32,'source':'monitor','timestamp':payload['timestamp'],'type':'events','payload':payload}}

class EvidenceTests(unittest.TestCase):
    def test_D01_business_complete_event_not_generated_is_incomplete(self):
        state=empty();association=associate([],state,state)
        self.assertTrue(business_ready(state,association,{'events':[],'notifications':[],'search':[]}))
        with self.assertRaises(Incomplete):marker_ready('events',{'run_id':'r','channel':'events'},None,'r')
    def test_D02_accepted_marker_not_queryable_is_boundary(self):
        self.assertFalse(marker_ready('events',event_marker(),None,'r'))
        result=recovery_result('events',1,[{'observed_monotonic':121,'ready':False}])
        self.assertTrue(result['timed_out']);self.assertIsNone(result['first_success']);self.assertEqual(result['elapsed_to_deadline'],120)
    def test_D03_selected_partition_marker_closes_despite_continuous_production(self):
        marker=event_marker();entry=copy.deepcopy(marker['envelope']['payload']);entry.pop('event_schema_version')
        stored=copy.deepcopy(entry);stored['@timestamp']=stored.pop('timestamp')
        query={'message_id':'a'*32,'stored_id':'a'*32,'stored_source':stored,'entry':entry,'global_lag':5000}
        self.assertTrue(marker_ready('events',marker,query,'r'))
        marker['partition']=[0,1]
        with self.assertRaises(Incomplete):marker_ready('events',marker,query,'r')
    def test_D04_edit_delete_and_idempotent_fact_groups(self):
        before=empty();before['posts']=[{'id':9,'author_id':1,'content_revision':1,'content_digest':'old'}]
        rows=[request('PATCH','PATCH /api/v1/posts/:postId','/api/v1/posts/9',revision=2,response_id=9),request('DELETE','DELETE /api/v1/posts/:postId','/api/v1/posts/9',slot=1)]
        after=empty();after['events']=[post_event('post.updated',2,'update'),post_event('post.deleted',0,'delete')]
        linked=associate(rows,before,after);self.assertEqual(linked['violations'],[])
        observed={'events':[{'event_id':e['event_id'],'status':'published'} for e in after['events']],'notifications':[],'search':[]}
        self.assertTrue(business_ready(after,linked,observed))
        observed['search']=[{'id':9,'content_revision':1,'content_digest':'old'}];self.assertFalse(business_ready(after,linked,observed))
        before=empty();before['relations']=[['like',1,9]];after=copy.deepcopy(before)
        rows=[request('PUT','PUT /api/v1/posts/:postId/like','/api/v1/posts/9/like'),request('PUT','PUT /api/v1/posts/:postId/like','/api/v1/posts/9/like',slot=1)]
        self.assertEqual(associate(rows,before,after)['violations'],[])
    def test_revision_mismatch_and_duplicate_notification_fail(self):
        after=empty();after['posts']=[{'id':9,'author_id':1,'content_revision':2,'content_digest':'digest'}];after['events']=[post_event('post.updated',2)]
        linked=associate([request('PATCH','PATCH /api/v1/posts/:postId','/api/v1/posts/9',revision=2,response_id=9)],empty(),after)
        self.assertFalse(business_ready(after,linked,{'events':[{'event_id':'event','status':'published'}],'notifications':[],'search':[{'id':9,'content_revision':1,'content_digest':'digest'}]}))
        after['events']=[{'event_id':'like','status':'published','payload':{'event_type':'post.liked','post_id':9,'actor_id':1,'recipient_id':2}}]
        linked={'violations':[],'groups':[]}
        n={'source_event_id':'like','recipient_id':2,'actor_id':1,'type':'post.liked','post_id':9,'comment_id':None}
        self.assertFalse(business_ready(after,linked,{'events':[{'event_id':'like','status':'published'}],'notifications':[n,n],'search':[]}))
    def test_cross_stage_and_missing_terminal_rejected(self):
        terminal=request('GET','GET /api/v1/users/me','');arrival=copy.deepcopy(terminal);arrival.update(record='arrival',outcome='scheduled')
        with self.assertRaises(Incomplete):ledger_requests([arrival],'r',1,'rps-50')
        with self.assertRaises(Incomplete):ledger_requests([arrival,terminal],'other',1,'rps-50')

    def test_log_public_projection_preserves_identity_without_runtime_fields(self):
        from phase20_evidence import management_payload
        payload={'timestamp':'2026-09-30T00:00:00Z','level':'info','service':'backend','module':'http','message':'http request completed','request_id':'a'*32,'method':'GET','route':'/api/v1/users/me','status':200,'duration_ms':0,'response_bytes':143,'log_schema_version':1,'version':'2.1.4','revision':'private-build','runtime_mode':'container','event':'http_request'}
        entry=management_payload('logs',payload)
        self.assertNotIn('revision',entry);self.assertEqual(entry['duration_ms'],0)
        marker={'run_id':'r','channel':'logs','marker_id':'r/logs','t_origin':1,'message_id':'b'*32,'source':'backend','timestamp':payload['timestamp'],'topic':'gopulse-observability-v1','partition':0,'offset':3,'accepted':True,'generation_evidence':{'method':'GET','route':'/api/v1/users/me','status':200,'request_id':'a'*32,'t_origin':1},'envelope':{'message_id':'b'*32,'type':'logs','source':'backend','timestamp':payload['timestamp'],'payload':payload}}
        stored=copy.deepcopy(payload);stored['@timestamp']=stored.pop('timestamp')
        self.assertTrue(marker_ready('logs',marker,{'message_id':'b'*32,'stored_id':'b'*32,'stored_source':stored,'entry':entry},'r'))
        entry['status']=500
        self.assertFalse(marker_ready('logs',marker,{'message_id':'b'*32,'stored_id':'b'*32,'stored_source':stored,'entry':entry},'r'))
