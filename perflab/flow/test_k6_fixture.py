"""用独立 HTTP fixture 验证实际 k6 脚本，不访问业务环境。"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest

from flow.config import ROOT, load


@unittest.skipUnless(os.environ.get('GOPULSE_PT_K6_FIXTURE') == '1', '显式启用隔离 k6 fixture')
class K6FixtureTests(unittest.TestCase):
    def test_real_script_records_start_and_outcome_without_credentials(self):
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_POST(self):
                self.rfile.read(int(self.headers.get('Content-Length', 0)))
                self.send_response(200)
                self.send_header('Set-Cookie', 'gopulse_session=fixture-session; Path=/')
                self.end_headers()
                self.wfile.write(b'{"data":{"id":1}}')
            def do_GET(self):
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'{"data":{"id":1}}')
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        server.daemon_threads = True
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp)
            (path / 'input').mkdir()
            (path / 'output').mkdir()
            users = [{'id': i+1, 'username': f'fixture{i}'} for i in range(512)]
            corpus = {'users': users, 'read_post_ids': [1], 'interaction_post_ids': [1],
                      'editable_post_ids': [[1]] * 512, 'delete_post_ids': [[1, 2]] * 512}
            (path / 'input/corpus.json').write_text(json.dumps(corpus))
            (path / 'input/credentials.json').write_text(json.dumps({'users': users, 'password': 'fixture-private-password'}))
            command = ['docker', 'run', '--rm', '--pull', 'never', '--network', 'host', '--cpus', '1.5', '--memory', '1536m',
                       '--memory-swap', '1536m', '--restart', 'no', '--user', f'{os.getuid()}:{os.getgid()}',
                       '-v', f'{path / "input"}:/input:ro', '-v', f'{path / "output"}:/output',
                       '-v', f'{ROOT / "perflab"}:/perflab:ro']
            env = {'GOPULSE_BASE_URL': f'http://127.0.0.1:{server.server_port}', 'LOAD_TARGET_RPS': '20',
                   'LOAD_WARMUP_SECONDS': '1', 'LOAD_MEASUREMENT_SECONDS': '2', 'LOAD_STAGE_ID': 'isolated',
                   'LOAD_MIX_JSON': json.dumps(load()['mix']), 'LOAD_ROUTE': '0'}
            for key, value in env.items(): command += ['-e', key + '=' + value]
            command += ['grafana/k6:0.56.0', 'run', '--quiet', '--log-format', 'raw', '/perflab/flow/workload.js']
            result = subprocess.run(command, capture_output=True, text=True, timeout=45)
            self.assertEqual(result.returncode, 0, result.stderr)
            combined = result.stdout + result.stderr
            events = [json.loads(line[line.index('PT {')+3:]) for line in combined.splitlines() if 'PT {' in line]
            starts = {e['request_id'] for e in events if e['type'] == 'start'}
            outcomes = {e['request_id'] for e in events if e['type'] == 'outcome'}
            self.assertGreaterEqual(len(outcomes), 57)
            self.assertEqual(starts, outcomes)
            self.assertTrue(all(e['success'] for e in events if e['type'] == 'outcome'))
            self.assertNotIn('fixture-private-password', combined)
            self.assertNotIn('fixture-session', combined)
            self.assertTrue((path / 'output/summary.json').exists())
