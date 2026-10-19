#!/usr/bin/env python3
"""Independent 1Hz metrics and resource evidence with explicit UTC windows."""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import threading
import time
import urllib.request

SAFE_CONFIG = (
    'MYSQL_MAX_OPEN_CONNS', 'OUTBOX_MYSQL_MAX_OPEN_CONNS',
    'OUTBOX_DATABASE_TIMEOUT', 'OUTBOX_PUBLISH_CONCURRENCY', 'OUTBOX_CLAIM_BATCH',
    'OUTBOX_LEASE_DURATION', 'OUTBOX_PUBLISH_TIMEOUT', 'REDIS_POST_DETAIL_TTL',
    'BACKEND_RESOURCE_REBUILD', 'BACKEND_RESOURCE_WAITER',
)


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def sample_metrics(url, token):
    request = urllib.request.Request(url)
    if token:
        request.add_header('Authorization', 'Bearer ' + token)
    try:
        with urllib.request.urlopen(request, timeout=.5) as response:
            return {'ok': True, 'body': response.read(1 << 20).decode()}
    except Exception as error:
        return {'ok': False, 'error': type(error).__name__}


def run_window(output, name, seconds, url, token, containers, redis_url=None):
    started = time.monotonic()
    # Separate worker and file: metrics failures never delay resource sampling.
    def resources():
        latest = {}
        lock = threading.Lock()
        process = subprocess.Popen(
            ['docker', 'stats', '--format', '{{json .}}', *containers],
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
        def collect():
            for line in process.stdout:
                try:
                    item = json.loads(line.lstrip('\x1b[2J\x1b[H'))
                    with lock:
                        latest[item['ID']] = (time.monotonic(), item)
                except (ValueError, KeyError):
                    continue
        reader = threading.Thread(target=collect, daemon=True)
        reader.start()
        try:
            with (output / (name + '-resources.jsonl')).open('w') as stream:
                slot = 0
                while time.monotonic() - started < seconds:
                    now = time.monotonic()
                    actual_slot = int(now - started)
                    with lock:
                        samples = [{'age_seconds': now - at, 'stats': item}
                                   for at, item in latest.values()]
                    record = {'utc': utc(), 'slot': actual_slot,
                              'gap_slots': max(0, actual_slot - slot),
                              'containers': samples, 'stream_alive': process.poll() is None}
                    if redis_url:
                        record['redis_metrics'] = sample_metrics(redis_url, None)
                    stream.write(json.dumps(record) + '\n')
                    stream.flush()
                    slot = actual_slot + 1
                    time.sleep(max(0, started + slot - time.monotonic()))
        finally:
            if process.poll() is None:
                process.terminate()
            try:
                process.wait(timeout=2)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
            reader.join(timeout=1)
    worker = threading.Thread(target=resources)
    worker.start()
    with (output / (name + '-metrics.jsonl')).open('w') as stream:
        slot = 0
        while time.monotonic() - started < seconds:
            now = time.monotonic()
            actual_slot = int(now - started)
            record = {'utc': utc(), 'slot': actual_slot, 'gap_slots': max(0, actual_slot - slot),
                      'lag_seconds': now - started - actual_slot}
            record.update(sample_metrics(url, token))
            stream.write(json.dumps(record) + '\n')
            stream.flush()
            slot = actual_slot + 1
            time.sleep(max(0, started + slot - time.monotonic()))
    worker.join()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--metrics-url', required=True)
    parser.add_argument('--token-env', default='BACKEND_METRICS_TOKEN')
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--stage', default='capacity')
    parser.add_argument('--snapshot-sha256', required=True)
    parser.add_argument('--sequence-sha256', required=True)
    parser.add_argument('--redis-metrics-url')
    parser.add_argument('--warmup', type=int, default=1800)
    parser.add_argument('--measure', type=int, default=1800)
    parser.add_argument('--recovery', type=int, default=120)
    parser.add_argument('--container', action='append', required=True)
    args = parser.parse_args()
    if min(args.warmup, args.measure, args.recovery) < 0:
        parser.error('window durations must be nonnegative')
    args.output.mkdir(parents=True, exist_ok=False)
    metadata = {'started_utc': utc(), 'stage': args.stage,
                'effective_config': {key: os.environ[key] for key in SAFE_CONFIG if key in os.environ},
                'snapshot_sha256': args.snapshot_sha256, 'sequence_sha256': args.sequence_sha256,
                'metrics_timeout_seconds': .5, 'sampling_hz': 1, 'windows': []}
    for name, duration in [('warmup', args.warmup), ('measure', args.measure), ('recovery', args.recovery)]:
        window = {'name': name, 'started_utc': utc(), 'duration_seconds': duration}
        run_window(args.output, name, duration, args.metrics_url, os.getenv(args.token_env), args.container, args.redis_metrics_url)
        window['ended_utc'] = utc()
        metadata['windows'].append(window)
        (args.output / 'windows.json').write_text(json.dumps(metadata, indent=2) + '\n')


if __name__ == '__main__':
    main()
