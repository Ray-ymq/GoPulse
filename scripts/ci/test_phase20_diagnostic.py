import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch
from phase20_diagnostic import independent_recovery, load_profile
from phase20_evidence import recovery_result

class DiagnosticTests(unittest.TestCase):
    def test_D05_other_timeout_does_not_override_first_success(self):
        fast=recovery_result('business',10,[{'observed_monotonic':11,'ready':True}])
        slow=recovery_result('events',10,[{'observed_monotonic':133,'ready':False}])
        self.assertEqual(fast['first_success'],11);self.assertEqual(fast['elapsed_seconds'],1)
        self.assertIsNone(slow['first_success']);self.assertEqual(slow['elapsed_to_deadline'],120)
    def test_D06_recovery_workers_join_before_return_to_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            active=set();completed=[]
            def probe(channel,remaining):
                active.add(channel);time.sleep(0.005);active.remove(channel);completed.append(channel)
                return {'ready':True,'facts':{'channel':channel}}
            origins={c:time.monotonic() for c in ('business','metrics','logs','events')}
            result=independent_recovery(origins,probe,Path(directory)/'raw.jsonl','r')
            self.assertEqual(active,set());self.assertEqual(len(completed),4)
            self.assertTrue(all(not r['timed_out'] for r in result.values()))
            rows=[json.loads(line) for line in (Path(directory)/'raw.jsonl').read_text().splitlines()]
            self.assertEqual(len(rows),4)
    def test_profile_preserves_frozen_recipe_and_load(self):
        profile=load_profile();self.assertEqual(profile['repetitions'],3);self.assertEqual([s['target_rps'] for s in profile['stages']],[50,100,150,200])
        self.assertEqual(profile['host']['disk_free_bytes_min'],50_000_000_000)
        self.assertEqual(profile['diagnostic']['drain_seconds'],30);self.assertEqual(profile['diagnostic']['recovery_seconds'],120)

    def test_business_search_uses_public_post_id_mapping(self):
        from phase20_diagnostic import search_posts
        response={'_shards':{'failed':0},'hits':{'hits':[{'_source':{'post_id':9,'title':'t','content':'c','content_revision':2}}]}}
        with patch('phase20_diagnostic.json_http',return_value=response) as call:
            result=search_posts('127.0.0.1',{9})
            body=json.loads(call.call_args.args[2]);self.assertEqual(body['query'],{'terms':{'post_id':[9]}})
            self.assertEqual(result[0]['id'],9);self.assertEqual(result[0]['content_revision'],2)

    def test_plugin_action_is_empty_post_and_header_lookup_is_case_insensitive(self):
        from phase20_diagnostic import ProductAPI
        from email.message import Message
        from unittest.mock import MagicMock
        api=ProductAPI.__new__(ProductAPI);api.url='http://127.0.0.1';api.cookie='session=private'
        headers=Message();headers['X-Request-Id']='a'*32
        response=MagicMock();response.status=200;response.headers=headers;response.read.return_value=b'{"data":{"observed_state":"stopped"}}'
        opener=MagicMock();opener.open.return_value.__enter__.return_value=response
        with patch('urllib.request.build_opener',return_value=opener):
            _,received=api.call('/api/v1/exporter-plugins/redis-exporter/stop',return_headers=True,method='POST')
            request=opener.open.call_args.args[0]
            self.assertEqual(request.get_method(),'POST');self.assertIsNone(request.data)
            self.assertEqual(received.get('X-Request-ID'),'a'*32)

    def test_large_consistent_sql_uses_stdin_without_argv_limit(self):
        from phase20_diagnostic import mysql
        from subprocess import CompletedProcess
        sql='START TRANSACTION WITH CONSISTENT SNAPSHOT;'+('SELECT 1;'*30000)+'COMMIT;'
        with patch('phase20_diagnostic.subprocess.run',return_value=CompletedProcess([],0,stdout='ok',stderr='')) as run:
            self.assertEqual(mysql('owned','private.env',['compose.yaml','override.yaml'],sql),'ok')
            self.assertNotIn(sql,run.call_args.args[0])
            self.assertEqual(run.call_args.kwargs['input'],sql)
            self.assertIn('-T',run.call_args.args[0]);self.assertEqual(run.call_args.kwargs['timeout'],15)
