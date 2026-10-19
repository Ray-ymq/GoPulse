#!/usr/bin/env python3
"""Conservative TTL gate over two independently recorded capacity runs."""
import argparse
import json
import re
from pathlib import Path

SAMPLE = re.compile(r'^(\w+)(?:\{([^}]*)\})?\s+([\d.eE+\-]+)$')


def metric(body, name, result=None):
    total = 0.
    for line in body.splitlines():
        match = SAMPLE.match(line)
        if match and match[1] == name and (result is None or f'result="{result}"' in (match[2] or '')):
            total += float(match[3])
    return total


def evidence(directory):
    meta = json.loads((directory / 'windows.json').read_text())
    windows = {window['name']: window for window in meta['windows']}
    if any(windows[name]['duration_seconds'] < 1800 for name in ['warmup', 'measure']):
        raise ValueError('both warmup and measure must last at least 1800 seconds')
    samples = [json.loads(line) for line in (directory / 'measure-metrics.jsonl').read_text().splitlines()]
    if len(samples) < 1800 or any(not row['ok'] or row['gap_slots'] for row in samples):
        raise ValueError('metrics sampling is incomplete')
    first, last = samples[0]['body'], samples[-1]['body']
    if any('gopulse_backend_cache_writes_total' not in body for body in [first, last]):
        raise ValueError('cache write failure counters are missing')
    def delta(name, result=None):
        value = metric(last, name, result) - metric(first, name, result)
        if value < 0:
            raise ValueError('counter reset during measurement')
        return value
    hit = delta('gopulse_backend_cache_requests_total', 'hit')
    miss = sum(delta('gopulse_backend_cache_requests_total', result) for result in ['miss', 'invalid', 'error'])
    if hit + miss == 0:
        raise ValueError('no cache requests were measured')
    memory = []
    resources = [json.loads(line) for line in (directory / 'measure-resources.jsonl').read_text().splitlines()]
    if len(resources) < 1800:
        raise ValueError('resource sampling is incomplete')
    for row in resources:
        redis = row.get('redis_metrics', {})
        if not redis.get('ok') or row.get('gap_slots'):
            raise ValueError('Redis memory evidence is incomplete')
        limit = metric(redis['body'], 'redis_memory_max_bytes')
        if limit <= 0:
            raise ValueError('Redis maxmemory must be configured')
        used = metric(redis['body'], 'redis_memory_used_bytes')
        if used <= 0:
            raise ValueError('Redis used memory is missing')
        memory.append(used / limit)
    return meta, {'hit_ratio': hit / (hit + miss),
                  'origin': delta('gopulse_backend_cache_origin_total'),
                  'write_failures': delta('gopulse_backend_cache_writes_total', 'failure'),
                  'redis_memory_peak_ratio': max(memory)}


def compare(baseline, candidate):
    a_meta, a = evidence(baseline)
    b_meta, b = evidence(candidate)
    for key in ['snapshot_sha256', 'sequence_sha256']:
        if not a_meta.get(key) or a_meta[key] != b_meta.get(key):
            raise ValueError('snapshot and request sequence must match')
    a_config = dict(a_meta['effective_config'])
    b_config = dict(b_meta['effective_config'])
    a_config.pop('REDIS_POST_DETAIL_TTL', None)
    b_config.pop('REDIS_POST_DETAIL_TTL', None)
    if a_config != b_config:
        raise ValueError('non-TTL effective configurations must match')
    if a_config.get('MYSQL_MAX_OPEN_CONNS') != '8' or a_config.get('BACKEND_RESOURCE_REBUILD') != '8':
        raise ValueError('the experiment must fix API pool and rebuild executors at 8')
    if a_meta['effective_config'].get('REDIS_POST_DETAIL_TTL') != '5m' or b_meta['effective_config'].get('REDIS_POST_DETAIL_TTL') != '30m':
        raise ValueError('expected 5m baseline and 30m candidate')
    adopt = (b['hit_ratio'] - a['hit_ratio'] >= .10 and
             a['origin'] > 0 and b['origin'] <= a['origin'] * .80 and
             b['redis_memory_peak_ratio'] < .75 and b['write_failures'] == 0)
    return {'selected_ttl': '30m' if adopt else '5m', 'baseline': a, 'candidate': b}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('baseline', type=Path)
    parser.add_argument('candidate', type=Path)
    args = parser.parse_args()
    try:
        result = compare(args.baseline, args.candidate)
    except (ValueError, KeyError, OSError) as error:
        result = {'selected_ttl': '5m', 'status': 'inconclusive', 'reason': str(error)}
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
