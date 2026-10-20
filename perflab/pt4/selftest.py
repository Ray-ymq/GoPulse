"""对临时 HTTP 服务验证负载与计数；不连接 GoPulse、MySQL 或消息队列。"""
import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import signal
import subprocess
import threading
import time
from common import ROOT, STATE, atomic_json, utc
from prepare import K6
from report import build_report
from run import load_environment


class Handler(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *_):
        pass

    def respond(self):
        if int(self.headers.get('Content-Length', 0)):
            self.rfile.read(int(self.headers['Content-Length']))
        login = self.path == '/api/v1/auth/login'
        if not login and self.path != '/ready':
            time.sleep(self.server.delay)
        status = 200
        if self.command == 'POST' and not login:
            status = 201
        if self.command in ('PUT', 'DELETE') and not self.path.endswith('/follow'):
            status = 204
        payload = b'' if status == 204 else json.dumps({'data': {'id': 1000000, 'content': 'mock'}}).encode()
        self.send_response(status)
        if login:
            self.send_header('Set-Cookie', 'gopulse_session=fixture; Path=/')
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        try:
            self.wfile.write(payload)
        except (BrokenPipeError, ConnectionResetError):
            pass

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = respond


def fixture(directory, users):
    corpus = {'read_post_ids': list(range(1, 10001)), 'interaction_post_ids': list(range(10001, 20001)),
              'editable_post_ids': [[20001 + user * 10 + i for i in range(10)] for user in range(users)],
              'delete_post_ids': [[30001 + user * 128 + i for i in range(128)] for user in range(users)],
              'users': [{'id': user + 1, 'username': f'fixture{user}'} for user in range(users)]}
    atomic_json(directory / 'corpus.json', corpus)
    atomic_json(directory / 'credentials.json', {'users': corpus['users'], 'password': 'fixture-only'})


def run_fixture(seconds=3, rps=100, vus=16, warmup=1, case='normal'):
    directory = STATE / 'selftest' / (case + '-' + str(time.time_ns()))
    directory.mkdir(mode=0o700, parents=True)
    fixture(directory, vus)
    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    server.daemon_threads = True
    server.delay = {'normal': 0, 'slow': 1.2, 'timeout': .4, 'interrupt': 2}[case]
    threading.Thread(target=server.serve_forever, daemon=True).start()
    config = json.loads((ROOT / 'perflab/k6/test4.json').read_text())
    config.update(base_url=f'http://127.0.0.1:{server.server_port}', run_id=directory.name, group='fixture',
                  rps=rps, virtual_users=vus, warmup_seconds=warmup, measurement_seconds=seconds, route_set='mixed',
                  request_timeout_seconds=.1 if case == 'timeout' else 5,
                  corpus_path=str(directory / 'corpus.json'), credentials_path=str(directory / 'credentials.json'))
    with (directory / 'load.log').open('wb') as log:
        process = subprocess.Popen([str(K6), 'run', '--address', '', '--quiet', '--log-format', 'json',
                                    '--log-output', 'file=' + str(directory / 'events.log'), '--out', 'json=' + str(directory / 'k6-samples.json'),
                                    str(ROOT / 'perflab/k6/test4.js')], cwd=ROOT, env=load_environment(config, directory), stdout=log, stderr=log)
        config['load_started_epoch'] = time.time()
        samples = []; kill_at = None
        with (directory / 'resources.jsonl').open('w') as resource:
            while process.poll() is None:
                status = Path(f'/proc/{process.pid}/status')
                try:
                    rss = next(int(line.split()[1]) * 1024 for line in status.read_text().splitlines() if line.startswith('VmRSS:'))
                    record = {'timestamp': utc(), 'rss_bytes': rss, 'elapsed_seconds': time.time() - config['load_started_epoch'],
                              'files_bytes': sum(path.stat().st_size for path in directory.iterdir() if path.is_file())}
                    resource.write(json.dumps(record) + '\n'); resource.flush(); samples.append(record)
                except (OSError, StopIteration):
                    pass
                event_path = directory / 'events.log'
                if case == 'interrupt' and kill_at is None and event_path.exists() and 'PT4 ' in event_path.read_text():
                    kill_at = time.time() + warmup + .3
                if kill_at is not None and time.time() >= kill_at:
                    config['stop_requested_epoch'] = time.time(); process.kill(); process.wait(); break
                time.sleep(.5)
        config.setdefault('stop_requested_epoch', time.time())
        config['load_exit_code'] = process.wait()
    server.shutdown(); server.server_close()
    atomic_json(directory / 'run.json', config)
    atomic_json(directory / 'recovery-summary.json', {'status': 'fixture_not_applicable'})
    if not (directory / 'events.log').exists():
        raise RuntimeError('k6 未生成事件，请检查 ' + str(directory / 'load.log'))
    report = build_report(directory)
    measurement = report['measurement']
    expected_truncation = case == 'interrupt' and report['evidence_errors'] == ['invalid_event_json']
    if not measurement['started'] or (report['evidence_errors'] and not expected_truncation):
        raise AssertionError('模拟负载没有有效测量证据：' + str(directory))
    if case == 'normal' and (measurement['unfinished'] or measurement['error_rate']):
        raise AssertionError('正常模拟请求计数不守恒：' + str(directory))
    if case == 'timeout' and not measurement['error_rate']:
        raise AssertionError('超时没有计入业务错误')
    if case == 'interrupt' and not measurement['unfinished']:
        raise AssertionError('强制中断没有保留未完成请求')
    result = {'directory': str(directory), 'case': case, 'seconds': seconds, 'rps': rps, 'vus': vus,
              'started': measurement['started'], 'outcomes': measurement['outcomes'], 'unfinished': measurement['unfinished'],
              'error_rate': measurement['error_rate'], 'rss_max_bytes': max(sample['rss_bytes'] for sample in samples),
              'event_log_truncated': expected_truncation,
              'evidence_bytes': sum(path.stat().st_size for path in directory.iterdir() if path.is_file())}
    atomic_json(directory / 'selftest-result.json', result)
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--seconds', type=int, default=3)
    parser.add_argument('--rps', type=float, default=100)
    parser.add_argument('--vus', type=int, default=16)
    parser.add_argument('--warmup-seconds', type=int, default=1)
    parser.add_argument('--case', choices=('normal', 'slow', 'timeout', 'interrupt'), default='normal')
    args = parser.parse_args()
    print(json.dumps(run_fixture(args.seconds, args.rps, args.vus, args.warmup_seconds, args.case), ensure_ascii=False))
