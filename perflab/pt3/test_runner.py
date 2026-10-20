"""用临时 HTTP 服务验证 k6 与 Go runner；不访问 GoPulse 业务服务。"""
from datetime import datetime,timezone
import http.server
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import time
import unittest
from environment import ROOT,STATE

class Fixture(http.server.BaseHTTPRequestHandler):
    calls=0
    delay=0
    def log_message(self,*_):pass
    def do_POST(self):
        self.rfile.read(int(self.headers.get('Content-Length','0')))
        time.sleep(.3)
        self.send_response(200);self.send_header('Set-Cookie','gopulse_session=fixture; Path=/');self.end_headers();self.wfile.write(b'{}')
    def do_GET(self):
        if '/api/' in self.path:
            Fixture.calls+=1;time.sleep(Fixture.delay)
        self.send_response(503 if '/api/' in self.path else 200);self.end_headers();self.wfile.write(b'{}')

@unittest.skipUnless((STATE/'bin/load').exists(),'先构建 perflab/cmd/load')
class RunnerIntegrationTest(unittest.TestCase):
    def setUp(self):
        Fixture.calls=0;Fixture.delay=0
        self.fixture=http.server.ThreadingHTTPServer(('127.0.0.1',0),Fixture)
        threading.Thread(target=self.fixture.serve_forever,daemon=True).start()
        self.temp=tempfile.TemporaryDirectory();self.path=Path(self.temp.name)
    def tearDown(self):self.fixture.shutdown();self.fixture.server_close();self.temp.cleanup()
    def execute(self,stop_after=None):
        p=self.path
        users=[{'id':1,'username':'fixture'}]
        corpus={'users':users,'read_post_ids':[1],'interaction_post_ids':[1],'editable_post_ids':[[1]],'delete_post_ids':[[1]]}
        (p/'corpus.json').write_text(json.dumps(corpus))
        (p/'credentials.json').write_text(json.dumps({'schema_version':'gopulse.phase18.credentials.v1','users':users,'password':'fixture'}))
        config=json.loads((ROOT/'perflab/k6/test3.json').read_text())
        config['target'].update(base_url=f'http://127.0.0.1:{self.fixture.server_port}',health_url=f'http://127.0.0.1:{self.fixture.server_port}/ready',corpus_path=str(p/'corpus.json'),credentials_path=str(p/'credentials.json'))
        config['workload'].update(virtual_users=2,profile='hot-repeat')
        config['boundary'].update(start_rps=2,max_rps=4,warmup_seconds=.5,measurement_seconds=1.5 if stop_after is None else 8)
        config.update(safety_file=str(p/'guard.json'),report_dir=str(p/'reports'))
        (p/'config.json').write_text(json.dumps(config))
        done=threading.Event();started=time.monotonic()
        def write_guard():
            while not done.is_set():
                stopped=stop_after is not None and time.monotonic()-started>=stop_after
                row={'timestamp':datetime.now(timezone.utc).isoformat(),'status':'stop' if stopped else 'ready','reason':'fixture_safety_stop' if stopped else None}
                temp=p/'guard.tmp';temp.write_text(json.dumps(row));temp.replace(p/'guard.json')
                done.wait(.05)
        thread=threading.Thread(target=write_guard);thread.start()
        while not (p/'guard.json').exists():time.sleep(.01)
        try:
            result=subprocess.run([str(STATE/'bin/load'),'--mode','boundary','--config',str(p/'config.json')],cwd=ROOT,capture_output=True,text=True,timeout=20)
        finally:done.set();thread.join()
        reports=list((p/'reports').glob('*-report.json'))
        self.assertEqual(len(reports),1,result.stderr)
        report=json.loads(reports[0].read_text())
        summary=json.loads(Path(report['stages'][0]['k6_summary_path']).read_text())
        return result,report,summary
    def test_business_503_does_not_stop_the_next_stage(self):
        result,report,summary=self.execute()
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(report['execution_status'],'complete')
        self.assertEqual(len(report['stages']),2)
        for stage in report['stages']:
            self.assertGreater(stage['measurement']['requests'],0)
            self.assertEqual(stage['measurement']['actual_seconds'],1.5)
            self.assertEqual(stage['measurement']['error_rate'],1)
        self.assertGreater(Fixture.calls,3)
        self.assertEqual(summary['timing']['windows'][0]['measurement'],1.5)
    def test_generator_drops_are_assigned_to_their_measurement_windows(self):
        Fixture.delay=.8
        result,report,_=self.execute()
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertGreater(sum(stage['measurement']['dropped_iterations'] for stage in report['stages']),0)
        self.assertTrue(any(stage['status']=='capacity_insufficient' for stage in report['stages']))
    def test_guard_stop_freezes_actual_duration_and_omits_future_stage_counts(self):
        result,report,summary=self.execute(stop_after=2)
        self.assertNotEqual(result.returncode,0)
        self.assertIn('fixture_safety_stop',report['stop_reason'])
        self.assertLess(report['stages'][0]['measurement']['actual_seconds'],2)
        self.assertGreater(report['stages'][0]['measurement']['actual_seconds'],0)
        self.assertEqual(report['stages'][1]['measurement']['requests'],0)
        self.assertIn('stop_requested_at',summary['timing'])

if __name__=='__main__':unittest.main()
