import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from flow.baseline import smoke_api_replicas, verify_replicas


class ReplicaSmokeTests(unittest.TestCase):
    def test_each_api_replica_logs_in_and_reads(self):
        rows = {role: {'Id': role + '-id', 'NetworkSettings': {'Networks': {
                    'gopulse-pt-business_edge': {'IPAddress': address}, 'gopulse-pt-business_business': {'IPAddress': '172.26.0.2'}}}}
                for role, address in (('backend', '172.25.0.2'), ('backend-2', '172.25.0.3'))}
        credentials = {'users': [{'username': 'load-user'}], 'password': 'private'}
        with patch('flow.baseline.containers', return_value=rows), patch('flow.baseline.http', return_value=(200, {})) as request:
            evidence = smoke_api_replicas(credentials)
        self.assertEqual(set(evidence), set(rows))
        self.assertEqual([(call.args[0], call.args[1]) for call in request.call_args_list],
                         [('http://172.25.0.2:8080', 'POST'), ('http://172.25.0.2:8080', 'GET'),
                          ('http://172.25.0.3:8080', 'POST'), ('http://172.25.0.3:8080', 'GET')])
        self.assertNotIn('private', json.dumps(evidence))
        with patch('flow.baseline.containers', return_value=rows), patch('flow.baseline.http', side_effect=[(200, {}), (200, {}), (503, {})]):
            with self.assertRaisesRegex(AssertionError, 'backend-2 登录失败'):
                smoke_api_replicas(credentials)

    def test_only_current_smoke_logs_count(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            record = {'started_at': '2026-10-22T01:00:00Z', 'finished_at': '2026-10-22T01:01:00Z'}
            (directory / 'smoke.json').write_text(json.dumps(record))
            def output(args):
                self.assertEqual(args[2:6], ['--since', record['started_at'], '--until', record['finished_at']])
                if 'backend' in args[-1]:
                    return json.dumps({'message': 'http request completed', 'route': '/api/v1/posts', 'status': 200})
                return json.dumps({'message': 'event processed', 'result': 'success'})
            with patch('flow.baseline.command', side_effect=output):
                verify_replicas(directory)
            self.assertEqual(len(json.loads((directory / 'replicas.json').read_text())), 6)
            with patch('flow.baseline.command', return_value=''):
                with self.assertRaisesRegex(AssertionError, '尚无业务处理成功记录'):
                    verify_replicas(directory)
