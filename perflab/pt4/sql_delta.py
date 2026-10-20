"""按 schema 和 digest 计算 SQL 窗口增量，保留重置、丢失及截断限制。"""
import argparse
from datetime import datetime
import json
from pathlib import Path
from common import atomic_json, rows

COUNTERS = ('COUNT_STAR', 'SUM_TIMER_WAIT', 'SUM_LOCK_TIME', 'SUM_ROWS_EXAMINED', 'SUM_ROWS_SENT',
            'SUM_CREATED_TMP_TABLES', 'SUM_CREATED_TMP_DISK_TABLES', 'SUM_SORT_ROWS', 'SUM_ERRORS')


def delta(before, after):
    reasons = []
    if not before.get('ok') or not after.get('ok'):
        return {'valid': False, 'reasons': ['snapshot_incomplete'], 'digests': []}
    a, b = rows(before, 'identity')[0], rows(after, 'identity')[0]
    boot_changed = abs((int(b['server_epoch']) - int(b['uptime'])) - (int(a['server_epoch']) - int(a['uptime']))) > 2
    if a['server_uuid'] != b['server_uuid'] or int(b['uptime']) < int(a['uptime']) or boot_changed:
        return {'valid': False, 'reasons': ['server_restarted_or_changed'], 'digests': []}
    lost = lambda sample: int(rows(sample, 'lost')[0]['Value'])
    if lost(after) != lost(before):
        reasons.append('digest_lost_counter_changed')
    previous = {(row['SCHEMA_NAME'], row['DIGEST']): row for row in rows(before, 'digests')}
    current = {(row['SCHEMA_NAME'], row['DIGEST']): row for row in rows(after, 'digests')}
    if previous.keys() - current.keys():
        reasons.append('digest_disappeared')
    result = []
    for key, row in current.items():
        old = previous.get(key)
        if old and old.get('FIRST_SEEN') != row.get('FIRST_SEEN'):
            reasons.append('digest_reset'); continue
        values = {name: int(row[name]) - (int(old[name]) if old else 0) for name in COUNTERS}
        if min(values.values()) < 0:
            reasons.append('counter_reset'); continue
        result.append({'schema': key[0], 'digest': key[1], 'text': row['DIGEST_TEXT'], 'null_digest': key[1] is None,
                       'new_digest': old is None, **values})
    return {'valid': not reasons, 'reasons': sorted(set(reasons)), 'started_at': before['timestamp'],
            'ended_at': after['timestamp'], 'digests': sorted(result, key=lambda row: row['SUM_TIMER_WAIT'], reverse=True)}


def summarize(path, output, start=None, end=None):
    previous = first = last = None
    invalid = set(); found_after = end is None
    stamp = lambda value: datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()
    with path.open() as source, output.with_suffix('.jsonl').open('w') as stream:
        for line in source:
            row = json.loads(line)
            if start is not None and stamp(row['ended_at']) <= start:
                first = last = previous = row
                continue
            if start is not None and first is None:
                invalid.add('measurement_boundary_snapshot_missing')
            if previous:
                change = delta(previous, row)
                invalid.update(change['reasons'])
                stream.write(json.dumps(change) + '\n')
            if first is None:
                first = row
            last = row; previous = row
            if end is not None and stamp(row['timestamp']) >= end:
                found_after = True
                break
    if first is None:
        result = {'valid': False, 'reasons': ['no_snapshot'], 'digests': []}
    else:
        result = delta(first, last)
        invalid.update(result['reasons'])
        if not found_after:
            invalid.add('measurement_boundary_snapshot_missing')
        if start is not None:
            result['requested_window'] = {'start_epoch': start, 'end_epoch': end}
            result['boundary_extra_seconds'] = {'before': start - stamp(first['timestamp']), 'after': stamp(last['ended_at']) - end}
            result['window_note'] = 'SQL 快照覆盖边界外的少量时间，不能将全部增量称为精确业务窗口成本'
            if max(result['boundary_extra_seconds'].values()) > 12:
                invalid.add('boundary_alignment_exceeds_12_seconds')
        result['reasons'] = sorted(invalid)
        result['valid'] = not invalid
    atomic_json(output, result)
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('snapshots', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    summarize(args.snapshots, args.output)
