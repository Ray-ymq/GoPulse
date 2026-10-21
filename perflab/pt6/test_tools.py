"""使用本地模拟 API 验证 k6 统计窗口和停止条件，不访问 PT6 业务系统。"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import time
import unittest

from common import ROOT, STATE
from run import evaluate


class LoadControlTest(unittest.TestCase):
    def exercise(self, mode):
        class Handler(BaseHTTPRequestHandler):
            protocol_version = 'HTTP/1.1'
            requests_seen = 0

            def log_message(self, *args):
                pass

            def do_POST(self):
                self.rfile.read(int(self.headers.get('Content-Length', 0)))
                self.respond(503 if mode == 'setup_failure' else 200, True)

            def do_GET(self):
                Handler.requests_seen += 1
                if mode == 'dropped':
                    time.sleep(.1)
                self.respond(503 if mode == 'business_failure' else 200)

            def respond(self, status, login=False):
                self.send_response(status)
                if login:
                    self.send_header('Set-Cookie', 'gopulse_session=mock; Path=/')
                self.send_header('Content-Length', '2')
                self.end_headers()
                try:
                    self.wfile.write(b'{}')
                except (BrokenPipeError, ConnectionResetError):
                    pass

        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        server.daemon_threads = True
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        STATE.mkdir(parents=True, exist_ok=True)
        try:
            with tempfile.TemporaryDirectory(prefix='selftest-', dir=STATE) as temp:
                directory = Path(temp)
                users = [{'id': i + 1, 'username': f'mock{i}'} for i in range(8)]
                corpus = {'users': users, 'read_post_ids': [1], 'interaction_post_ids': [1],
                          'editable_post_ids': [[1]] * 8, 'delete_post_ids': [[2]] * 8}
                (directory / 'corpus.json').write_text(json.dumps(corpus))
                (directory / 'credentials.json').write_text(json.dumps({'users': users, 'password': 'mock'}))
                variables = {'GOPULSE_BASE_URL': f'http://127.0.0.1:{server.server_port}',
                             'LOAD_VUS': '1' if mode == 'dropped' else '8',
                             'LOAD_TARGET_RPS': '500' if mode == 'dropped' else '20',
                             'LOAD_WARMUP_SECONDS': '1', 'LOAD_MEASUREMENT_SECONDS': '2',
                             'LOAD_REQUEST_TIMEOUT_SECONDS': '1',
                             'LOAD_MIX_JSON': '[{"category":"read","percent":100}]',
                             'LOAD_CORPUS_PATH': '/input/corpus.json',
                             'LOAD_CREDENTIALS_PATH': '/input/credentials.json',
                             'LOAD_SUMMARY_PATH': '/input/summary.json'}
                if mode == 'latency':
                    variables['LOAD_MAX_P95_MS'] = '0'
                args = ['docker', 'run', '--rm', '--network', 'host', '--cpus', '1.5', '--memory', '1536m',
                        '--user', f'{os.getuid()}:{os.getgid()}',
                        '-v', f'{ROOT / "perflab"}:/perflab:ro', '-v', f'{directory}:/input']
                for key, value in variables.items():
                    args += ['-e', key + '=' + value]
                result = subprocess.run(args + ['grafana/k6:0.56.0', 'run', '--quiet', '/perflab/pt6/workload.js'],
                                        capture_output=True, text=True, timeout=30)
                summary = json.loads((directory / 'summary.json').read_text()) if (directory / 'summary.json').exists() else {}
                return result, evaluate(summary, 20, 2, result.returncode), summary
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

    def test_windows_and_stop_conditions(self):
        for mode in ('success', 'setup_failure', 'business_failure', 'latency', 'dropped'):
            with self.subTest(mode=mode):
                result, report, summary = self.exercise(mode)
                if mode == 'success':
                    self.assertTrue(report['passed'], (report, result.stderr))
                    self.assertIn(report['completed'], (59, 60, 61))
                    measured = summary['metrics']['pt6_completed{phase:measurement}']['values']['count']
                    self.assertIn(measured, (39, 40, 41))
                else:
                    self.assertFalse(report['passed'])
                    self.assertNotEqual(result.returncode, 0)
                    if mode in ('setup_failure', 'business_failure'):
                        self.assertIn('first_failure', result.stderr)
                    if mode == 'dropped':
                        self.assertIn('dropped_iterations', report['reasons'])
                    if mode == 'latency':
                        self.assertIn('k6_failed', report['reasons'])

    def test_missing_and_incomplete_results_fail(self):
        self.assertFalse(evaluate({}, 250, 300, 0)['passed'])
        metrics = {name: {'values': {'count': value}} for name, value in
                   [('pt6_started', 100), ('pt6_completed', 99),
                    ('pt6_completed{phase:measurement}', 99), ('pt6_successes{phase:measurement}', 99)]}
        metrics['pt6_latency{phase:measurement}'] = {'values': {'p(95)': 1, 'p(99)': 2}}
        self.assertIn('unfinished_requests', evaluate({'metrics': metrics}, 250, 300, 0)['reasons'])


if __name__ == '__main__':
    unittest.main()
