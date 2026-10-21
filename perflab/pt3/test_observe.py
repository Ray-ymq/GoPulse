"""用离线样本验证故障判据，不干预真实数据库。"""
from datetime import datetime,timezone,timedelta
from pathlib import Path
import tempfile
import unittest
from observe import Observation
from unittest.mock import patch
from exporters import parse_metrics, scrape
import http.server
import threading
from environment import api_pool_override

class ApiPoolOverrideTest(unittest.TestCase):
    def test_absent_override_keeps_baseline(self):
        self.assertIsNone(api_pool_override({'MYSQL_TOTAL_MAX_OPEN_CONNS':'60'}))
    def test_doubling_within_budget_is_allowed(self):
        self.assertEqual(api_pool_override({'MYSQL_TOTAL_MAX_OPEN_CONNS':'60','PT3_API_MYSQL_MAX_OPEN_CONNS':'16'}),'16')
    def test_override_above_total_budget_is_rejected(self):
        with self.assertRaises(RuntimeError):
            api_pool_override({'MYSQL_TOTAL_MAX_OPEN_CONNS':'30','PT3_API_MYSQL_MAX_OPEN_CONNS':'40'})
    def test_invalid_override_is_rejected(self):
        with self.assertRaises(RuntimeError):
            api_pool_override({'MYSQL_TOTAL_MAX_OPEN_CONNS':'60','PT3_API_MYSQL_MAX_OPEN_CONNS':'invalid'})

class DatabaseEvidenceTest(unittest.TestCase):
    def sample(self,observer,second,ok):
        observer.database_sample({'timestamp':(self.start+timedelta(seconds=second)).isoformat(),'ok':ok},False)
    def setUp(self):
        self.start=datetime.now(timezone.utc)
        self.patcher=patch('observe.environment',return_value={'MYSQL_DATABASE':'fixture'})
        self.patcher.start()
        self.addCleanup(self.patcher.stop)
    def test_30_seconds_without_success_is_required(self):
        with tempfile.TemporaryDirectory() as directory:
            observer=Observation(Path(directory))
            for second in range(30):self.sample(observer,second,False)
            self.assertIsNone(observer.reason)
            self.sample(observer,30,False)
            self.assertEqual(observer.reason,'database_continuously_unavailable_30s')
    def test_success_resets_the_failure_interval(self):
        with tempfile.TemporaryDirectory() as directory:
            observer=Observation(Path(directory))
            for second in range(50):self.sample(observer,second,second==25)
            self.assertIsNone(observer.reason)
    def test_sampling_gap_is_not_database_outage_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            observer=Observation(Path(directory))
            self.sample(observer,0,False);self.sample(observer,31,False)
            self.assertEqual(observer.reason,'database_probe_sampling_gap')

class ExporterTest(unittest.TestCase):
    def test_http_scrape_checks_collector_success(self):
        class Handler(http.server.BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_GET(self):
                self.send_response(503 if self.path == '/unavailable' else 200)
                self.end_headers()
                success = 0 if self.path == '/failed' else 1
                self.wfile.write(('gopulse_mysql_up 1\ngopulse_mysql_collector_enabled{collector="transactions"} 1\n'
                                  'gopulse_mysql_collector_success{collector="transactions"} ' + str(success) + '\n').encode())
        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            url = 'http://127.0.0.1:' + str(server.server_port)
            self.assertIn('body', scrape('mysql', {'url': url + '/ok'}))
            failed = scrape('mysql', {'url': url + '/failed'})
            self.assertEqual(failed['error'], 'collector_failed')
            self.assertIn('collector_success', failed['body'])
            unavailable = scrape('mysql', {'url': url + '/unavailable'})
            self.assertEqual(unavailable['error'], 'HTTP_503')
            self.assertIn('gopulse_mysql_up', unavailable['body'])
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

    def test_invalid_metrics_never_become_zero(self):
        for body in ('', 'broken{', 'value NaN\n', 'value 1\nvalue 2\n'):
            with self.assertRaises(ValueError):
                parse_metrics(body)

    def test_database_identity_and_migration_change_stop_load(self):
        with tempfile.TemporaryDirectory() as directory, patch('observe.environment', return_value={'MYSQL_DATABASE':'fixture'}):
            observer = Observation(Path(directory))
            row = {'timestamp':datetime.now(timezone.utc).isoformat(), 'ok':True,
                   'identity':[{'server_uuid':'one','database_name':'fixture'}],
                   'environment':[{'data_epoch':'one','status':'ready','mode':'v2'}],
                   'migration':[{'version':'1','dirty':'0'}], 'backlog':[{'open_requirements':'0'}]}
            observer.database_sample(row, True)
            self.assertIsNone(observer.reason)
            row['migration'][0]['version'] = '2'
            observer.database_sample(row, True)
            self.assertEqual(observer.reason, 'business_state_or_schema_changed')

if __name__=='__main__':unittest.main()
