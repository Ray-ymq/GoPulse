"""流式读取请求事件，用 SQLite 复算阶段、路由和分钟统计，并合并恢复证据。"""
import argparse
import csv
import json
import math
from pathlib import Path
import sqlite3
from common import atomic_json, parse_event


def percentile(db, condition, fraction, column='http_ms'):
    count = db.execute('SELECT COUNT(' + column + ') FROM requests WHERE ' + condition).fetchone()[0]
    if not count:
        return None
    position = (count - 1) * fraction
    lower, upper = math.floor(position), math.ceil(position)
    values = [row[0] for row in db.execute('SELECT ' + column + ' FROM requests WHERE ' + condition + ' AND ' + column + ' IS NOT NULL ORDER BY ' + column + ' LIMIT ? OFFSET ?', (upper - lower + 1, lower))]
    return values[0] + (values[-1] - values[0]) * (position - lower)


def stats(db, condition, seconds):
    count, outcomes, successes = db.execute('SELECT COUNT(*),COUNT(completed),COALESCE(SUM(success),0) FROM requests WHERE ' + condition).fetchone()
    return {'started': count, 'outcomes': outcomes, 'unfinished': count - outcomes, 'successful': successes,
            'error_rate': (count - successes) / count if count else None,
            'started_rps': count / seconds if seconds else None, 'successful_cohort_rps': successes / seconds if seconds else None,
            **{f'{prefix}p{int(q * 100)}_ms': percentile(db, condition + (' AND success=1' if prefix == 'success_' else ''), q, column)
               for prefix, column in (('', 'http_ms'), ('success_', 'http_ms'), ('end_to_end_', 'e2e_ms')) for q in (.5, .95, .99)}}


def build_report(directory):
    run = json.loads((directory / 'run.json').read_text())
    path = directory / 'requests.sqlite'
    path.unlink(missing_ok=True)
    db = sqlite3.connect(path)
    db.execute('CREATE TABLE requests(slot INTEGER PRIMARY KEY,phase TEXT,route TEXT,category TEXT,started REAL,completed REAL,success INTEGER,http_ms REAL,e2e_ms REAL,status INTEGER,code TEXT,client_error TEXT)')
    scenario_start = None
    errors = []; sampled = {}; sample_counts = {}
    with (directory / 'events.log').open() as source, (directory / 'write-samples.jsonl').open('w') as samples:
        for line in source:
            try:
                event = parse_event(line)
            except ValueError:
                errors.append('invalid_event_json'); continue
            if not event:
                continue
            kind = event['type']
            if kind in ('start', 'clock'):
                scenario_start = event['scenario_started_at_ms'] / 1000
            if kind == 'start':
                db.execute('INSERT INTO requests(slot,phase,route,category,started) VALUES(?,?,?,?,?)',
                           (event['slot'], event['phase'], event['route'], event['category'], event['started_at_ms'] / 1000))
            elif kind == 'outcome':
                cursor = db.execute('UPDATE requests SET completed=?,success=?,http_ms=?,e2e_ms=?,status=?,code=?,client_error=? WHERE slot=?',
                                    (event['completed_at_ms'] / 1000, int(event['success']), event['http_ms'], event['end_to_end_ms'], event['status'], event['business_code'], event['client_error'], event['slot']))
                if not cursor.rowcount:
                    errors.append('outcome_without_start')
                if event.get('sample'):
                    # 用固定 slot 哈希选择样本，避免总是只保留窗口最初的写入。
                    key = event['route']; sample_counts[key] = sample_counts.get(key, 0) + 1
                    rank = (event['slot'] * 2654435761) % 2**32
                    bucket = sampled.setdefault(key + ('/success' if event['success'] else '/uncertain'), [])
                    bucket.append((rank, event)); bucket.sort(key=lambda item: item[0])
                    if len(bucket) > 100:
                        bucket.pop()
            elif kind == 'invalid_corpus':
                errors.append('invalid_corpus')
        for bucket in sampled.values():
            for _, event in bucket:
                samples.write(json.dumps(event) + '\n')
    db.commit()
    db.execute('CREATE INDEX request_phase_time ON requests(phase,started)')
    db.execute('CREATE INDEX request_phase_route ON requests(phase,route)')
    db.execute('CREATE INDEX request_completed ON requests(completed)')
    if scenario_start is None:
        errors.append('scenario_clock_missing')
        scenario_start = run['load_started_epoch']
    begin = scenario_start + run['warmup_seconds']
    end = min(begin + run['measurement_seconds'], run['stop_requested_epoch'])
    seconds = max(0, end - begin)
    condition = 'phase="measurement" AND started >= ' + str(begin) + ' AND started < ' + str(end)
    result = stats(db, condition, seconds)
    completed, succeeded = db.execute('SELECT COUNT(*),COALESCE(SUM(success),0) FROM requests WHERE completed>=? AND completed<?', (begin, end)).fetchone()
    result.update(wall_clock_completed=completed, wall_clock_successful=succeeded,
                  completed_rps=completed / seconds if seconds else None, actual_seconds=seconds,
                  started_at_epoch=begin, ended_at_epoch=end)
    routes = {}
    for (route,) in db.execute('SELECT DISTINCT route FROM requests'):
        quoted = "'" + route.replace("'", "''") + "'"
        routes[route] = stats(db, condition + ' AND route=' + quoted, seconds)
    minutes = []
    for offset in range(0, math.ceil(seconds), 60):
        span = min(60, seconds - offset)
        row = stats(db, f'phase="measurement" AND started >= {begin + offset} AND started < {begin + offset + span}', span)
        minutes.append({'offset_seconds': offset, 'seconds': span, **row})
    with (directory / 'requests-per-second.csv').open('w', newline='') as stream:
        writer = csv.DictWriter(stream, fieldnames=['offset_seconds', 'seconds', 'started', 'outcomes', 'unfinished', 'successful',
                                                   'started_rps', 'successful_cohort_rps', 'error_rate', 'p95_ms', 'p99_ms'])
        writer.writeheader()
        for offset in range(math.ceil(seconds)):
            span = min(1, seconds - offset)
            row = stats(db, f'phase="measurement" AND started >= {begin + offset} AND started < {begin + offset + span}', span)
            writer.writerow({'offset_seconds': offset, 'seconds': span, **{key: row[key] for key in writer.fieldnames[2:]}})
    dropped = 0
    sample_path = directory / 'k6-samples.json'
    if sample_path.exists():
        with sample_path.open() as source:
            for line in source:
                record = json.loads(line)
                if record.get('type') == 'Point' and record.get('metric') == 'dropped_iterations' and record['data'].get('tags', {}).get('scenario') == 'business':
                    from datetime import datetime
                    stamp = datetime.fromisoformat(record['data']['time'].replace('Z', '+00:00')).timestamp()
                    if begin <= stamp < end:
                        dropped += record['data']['value']
    result['dropped_iterations'] = dropped
    result['dropped_ratio'] = dropped / (dropped + result['started']) if dropped + result['started'] else None
    result['error_classes'] = [list(row) for row in db.execute('SELECT status,code,client_error,COUNT(*) FROM requests WHERE ' + condition + ' GROUP BY status,code,client_error')]
    recovery = json.loads((directory / 'recovery-summary.json').read_text()) if (directory / 'recovery-summary.json').exists() else {'status': 'missing'}
    full = seconds >= run['measurement_seconds'] - .1
    report = {'schema_version': 'gopulse.pt4.report.v1', 'run_id': run['run_id'], 'group': run['group'],
              'execution': 'complete' if full and run.get('load_exit_code') == 0 else 'incomplete',
              'measurement': result, 'routes': routes, 'minutes': minutes, 'evidence_errors': sorted(set(errors)),
              'count_conservation': 'observed_records_only' if errors else 'started_equals_outcomes_plus_unfinished',
              'route_set': run.get('route_set'), 'profile_role': run.get('profile_role'), 'config_sha256': run.get('config_sha256'),
              'load_exit_code': run.get('load_exit_code'), 'stop_reason': run.get('stop_reason'), 'recovery': recovery,
              'business_short_window_pass': bool(full and run.get('load_exit_code') == 0 and not run.get('stop_reason') and not errors and result['started'] and result['error_rate'] <= .01
                                               and result['p95_ms'] <= 1000 and result['p99_ms'] <= 2000
                                               and result['started_rps'] >= run['rps'] * .95 and result['dropped_ratio'] <= .01),
              'acceptance': '业务窗口自动复算；一致性、异步积压、恢复基线和持续测试判据仍须按方案逐项验收'}
    atomic_json(directory / 'report.json', report)
    db.close()
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    args = parser.parse_args()
    value = build_report(args.directory)
    print(json.dumps({'run_id': value['run_id'], 'execution': value['execution'], 'report': str(args.directory / 'report.json')}, ensure_ascii=False))
