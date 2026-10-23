"""核对成功写入、取消及响应不确定的写入，并保留业务失败统计。"""
from datetime import datetime, timezone
import json
from pathlib import Path
import time

from .runtime import write_json as atomic_json
from .common import sql

WRITE_CATEGORIES = ('content_write', 'interaction_write')
CONTENT_ROUTES = ('POST /api/v1/posts', 'POST /api/v1/posts/:postId/comments',
                  'PATCH /api/v1/posts/:postId', 'DELETE /api/v1/posts/:postId')
INTERACTION_ROUTES = ('PUT /api/v1/posts/:postId/like', 'DELETE /api/v1/posts/:postId/like',
                      'PUT /api/v1/users/:userId/follow', 'DELETE /api/v1/users/:userId/follow',
                      'PUT /api/v1/posts/:postId/bookmark')
CHUNK = 500


def _epoch(milliseconds):
    return milliseconds / 1000


def _time(epoch):
    return datetime.fromtimestamp(epoch, timezone.utc).strftime('%Y-%m-%d %H:%M:%S.%f')


def _label(epoch):
    return datetime.fromtimestamp(epoch, timezone.utc).isoformat()


def parse_events(path):
    requests = {}
    errors = {'invalid_event_json': 0, 'outcome_without_start': 0, 'duplicate_start': 0}
    with Path(path).open() as source:
        for line in source:
            try:
                event = json.loads(line)
                if event.get('schema_version') != 'gopulse.pt.request.v1':
                    raise ValueError('事件版本不符')
                key = event['request_id']
                if event['type'] == 'start':
                    if key in requests:
                        errors['duplicate_start'] += 1
                    requests[key] = {'slot': key, 'phase': event['phase'], 'route': event['route'],
                                     'category': event['category'], 'started': _epoch(event['started_at_ms'])}
                elif event['type'] == 'outcome':
                    if key not in requests:
                        errors['outcome_without_start'] += 1
                        continue
                    requests[key].update(completed=_epoch(event['completed_at_ms']), success=event['success'],
                                         sample=event.get('sample'), status=event.get('status'), http_ms=event.get('http_ms'))
                elif event['type'] == 'cancel' and key in requests:
                    requests[key]['canceled'] = True
                else:
                    raise ValueError('事件类型无效')
            except (ValueError, KeyError, TypeError):
                errors['invalid_event_json'] += 1
    return requests, errors


def _path_id(path, prefix, suffix=''):
    if not path or not path.startswith(prefix) or (suffix and not path.endswith(suffix)):
        return None
    value = path[len(prefix):]
    if suffix:
        value = value[:-len(suffix)]
    value = value.strip('/')
    return int(value) if value.isdigit() else None


def resource(request, corpus_users):
    """把写请求映射为可核对的资源键；无法映射时返回 slot 键并标记。"""
    sample = request.get('sample') or {}
    path = sample.get('path') or ''
    actor = sample.get('actor')
    user_id = corpus_users[actor]['id'] if actor is not None and 0 <= actor < len(corpus_users) else None
    route = request['route']
    response_id = sample.get('response_id')
    value = {'actor': actor, 'user_id': user_id, 'path': path, 'response_id': response_id}
    if route == 'POST /api/v1/posts':
        value.update(kind='post_create', table='posts', key=response_id, expected=sample.get('expected'))
    elif route == 'POST /api/v1/posts/:postId/comments':
        value.update(kind='comment_create', table='comments', key=response_id, expected=sample.get('expected'),
                     post_id=_path_id(path, '/api/v1/posts/', '/comments'))
    elif route == 'PATCH /api/v1/posts/:postId':
        value.update(kind='post_update', table='posts', key=_path_id(path, '/api/v1/posts/'), expected=sample.get('expected'))
    elif route == 'DELETE /api/v1/posts/:postId':
        value.update(kind='post_delete', table='posts', key=_path_id(path, '/api/v1/posts/'))
    elif route.endswith('/like'):
        value.update(kind='like', table='post_likes', key=(_path_id(path, '/api/v1/posts/', '/like'), user_id))
    elif route.endswith('/bookmark'):
        value.update(kind='bookmark', table='post_bookmarks', key=(_path_id(path, '/api/v1/posts/', '/bookmark'), user_id))
    elif route.endswith('/follow'):
        value.update(kind='follow', table='user_follows', key=(user_id, _path_id(path, '/api/v1/users/', '/follow')))
    else:
        value.update(kind='unknown', key=None)
    return value


def _chunks(values, size=CHUNK):
    values = list(values)
    for offset in range(0, len(values), size):
        yield values[offset:offset + size]


def _in(values):
    return ','.join(str(int(value)) for value in values)


def _rows(execute, query):
    return [line.split('\t') for line in execute(query).splitlines() if line]


def _post_rows(execute, ids):
    rows = {}
    for chunk in _chunks(ids):
        for row in _rows(execute, 'SELECT id,author_id,title,content,edited_at,content_revision FROM posts WHERE id IN (' + _in(chunk) + ');'):
            rows[int(row[0])] = {'author_id': int(row[1]), 'title': row[2], 'content': row[3],
                                 'edited_at': row[4], 'content_revision': int(row[5])}
    return rows


def capture_baseline(directory, preparation):
    corpus = json.loads((Path(preparation) / 'corpus.json').read_text())
    ids = {key for row in corpus['editable_post_ids'] for key in row}
    atomic_json(Path(directory) / 'audit-baseline.json', _post_rows(sql, ids))


def _text(value):
    if not isinstance(value, str) or not value:
        raise ValueError('写入预期内容缺失')
    return "CONVERT(0x" + value.encode().hex() + " USING utf8mb4)"


def _comment_rows(execute, ids):
    rows = {}
    for chunk in _chunks(ids):
        for row in _rows(execute, 'SELECT id,post_id,author_id,content FROM comments WHERE id IN (' + _in(chunk) + ');'):
            rows[int(row[0])] = {'post_id': int(row[1]), 'author_id': int(row[2]), 'content': row[3]}
    return rows


def _interaction_rows(execute, table, columns, pairs):
    rows = set()
    for chunk in _chunks(pairs):
        values = ','.join('(%d,%d)' % (int(first), int(second)) for first, second in chunk)
        for row in _rows(execute, 'SELECT ' + ','.join(columns) + ' FROM ' + table + ' WHERE (' + ','.join(columns) + ') IN (' + values + ');'):
            rows.add(tuple(int(value) for value in row))
    return rows


def _summary(values):
    counts = {}
    for value in values:
        counts[value] = counts.get(value, 0) + 1
    return counts


def build(directory, preparation):
    """把成功写入索引到 SQLite，按 500 行核对，避免观测进程占用全部内存。"""
    import sqlite3
    directory = Path(directory)
    corpus = json.loads((Path(preparation) / 'corpus.json').read_text())['users']
    pending = {}
    canceled = []
    uncertain = []
    canceled_results = {'writes': 0, 'resolved': 0, 'applied_creates': 0}
    uncertain_results = {'writes': 0, 'resolved': 0, 'applied_creates': 0}
    errors = {'invalid_event': 0, 'orphan_outcome': 0, 'unknown_resource': 0, 'unfinished_write': 0}
    begin, end = float('inf'), 0
    count = failures = 0
    examples = []
    db = sqlite3.connect(directory / 'audit-index.sqlite')
    try:
        db.executescript('DROP TABLE IF EXISTS writes; CREATE TABLE writes(kind TEXT,k1 INTEGER,k2 INTEGER,started REAL,completed REAL,method TEXT,payload TEXT,uncertain INTEGER);')
        def insert(item, original, completed, uncertain=False):
            resource_key = item.get('key')
            if resource_key is None or (isinstance(resource_key, tuple) and None in resource_key):
                errors['unknown_resource'] += 1
                return False
            first, second = resource_key if isinstance(resource_key, tuple) else (resource_key, 0)
            db.execute('INSERT INTO writes VALUES (?,?,?,?,?,?,?,?)',
                       (item['kind'], int(first), int(second), original['started_at_ms']/1000,
                        completed, original['route'].split()[0], json.dumps(item), int(uncertain)))
            return True
        with (directory / 'requests.jsonl').open() as source:
            for line in source:
                try:
                    event = json.loads(line)
                    if event['schema_version'] != 'gopulse.pt.request.v1':
                        raise ValueError('事件版本无效')
                    key = event['request_id']
                    if event['type'] == 'start':
                        if key in pending:
                            errors['invalid_event'] += 1
                        pending[key] = event
                        begin = min(begin, event['started_at_ms'] / 1000)
                    elif event['type'] == 'outcome':
                        original = pending.pop(key, None)
                        if original is None:
                            errors['orphan_outcome'] += 1
                            continue
                        end = max(end, event['completed_at_ms'] / 1000)
                        count += 1
                        if event['success'] and event.get('category') in WRITE_CATEGORIES:
                            request = {'route': event['route'], 'sample': event.get('sample')}
                            item = resource(request, corpus)
                            insert(item, original, event['completed_at_ms']/1000)
                        elif event.get('category') in WRITE_CATEGORIES and (not event.get('status') or event['status'] >= 500):
                            uncertain.append(original)
                    elif event['type'] == 'cancel':
                        original = pending.pop(key, None)
                        if original is None:
                            errors['orphan_outcome'] += 1
                        elif original.get('category') in WRITE_CATEGORIES:
                            if event.get('error') == 'stop_cancellation':
                                canceled.append(original)
                            else:
                                errors['unfinished_write'] += 1
                    else:
                        errors['invalid_event'] += 1
                except (ValueError, KeyError, TypeError):
                    errors['invalid_event'] += 1
        errors['unfinished_write'] += sum(1 for item in pending.values() if item.get('category') in WRITE_CATEGORIES)
        canceled_results['writes'] = len(canceled)
        uncertain_results['writes'] = len(uncertain)
        if canceled or uncertain:
            end = max(end, time.time())
        pending_writes = [(original, canceled_results) for original in canceled]
        pending_writes += [(original, uncertain_results) for original in uncertain]
        for original, totals in pending_writes:
            item = resource(original, corpus)
            if item['kind'] in ('post_create', 'comment_create'):
                expected = item.get('expected') or {}
                try:
                    filters = [f"author_id={int(item['user_id'])}",
                               "BINARY content=BINARY " + _text(expected.get('content')),
                               f"created_at >= '{_time(begin)}'", f"created_at <= '{_time(end)}'"]
                    if item['kind'] == 'post_create':
                        filters.append('BINARY title=BINARY ' + _text(expected.get('title')))
                    else:
                        filters.append(f"post_id={int(item['post_id'])}")
                    matches = _rows(sql, f"SELECT id FROM {item['table']} WHERE " + ' AND '.join(filters) + ' LIMIT 2;')
                    if len(matches) > 1:
                        raise ValueError('取消请求匹配到重复写入')
                    if not matches:
                        totals['resolved'] += 1
                        continue
                    item['key'] = int(matches[0][0])
                    totals['applied_creates'] += 1
                except (ValueError, TypeError):
                    errors['unfinished_write'] += 1
                    continue
            if insert(item, original, end, uncertain=True):
                totals['resolved'] += 1
        db.commit()
        db.execute('CREATE INDEX write_resource ON writes(kind,k1,k2,completed)')
        def mismatch(kind, key):
            nonlocal failures
            failures += 1
            if len(examples) < 50:
                examples.append({'kind': kind, 'key': key})
        verified = 0
        cursor = db.execute("SELECT kind,k1,payload,uncertain FROM writes WHERE kind IN ('post_create','comment_create','post_delete')")
        while batch := cursor.fetchmany(CHUNK):
            posts = _post_rows(sql, [row[1] for row in batch if row[0] != 'comment_create'])
            comments = _comment_rows(sql, [row[1] for row in batch if row[0] == 'comment_create'])
            for kind, key, payload, uncertain in batch:
                item = json.loads(payload)
                row = (comments if kind == 'comment_create' else posts).get(key)
                if kind == 'post_delete':
                    valid = row is None or (uncertain and row['author_id'] == item['user_id'])
                else:
                    expected = item.get('expected') or {}
                    valid = row is not None and row['author_id'] == item['user_id'] and row['content'] == expected.get('content')
                    valid = valid and (row['post_id'] == item['post_id'] if kind == 'comment_create' else row['title'] == expected.get('title'))
                if valid: verified += 1
                else: mismatch(kind, key)
        baseline_path = directory / 'audit-baseline.json'
        baseline = json.loads(baseline_path.read_text()) if baseline_path.exists() else {}
        cursor = db.execute("SELECT k1,COALESCE(MAX(CASE WHEN uncertain=0 THEN started END),0) FROM writes WHERE kind='post_update' GROUP BY k1")
        while batch := cursor.fetchmany(CHUNK):
            posts = _post_rows(sql, [row[0] for row in batch])
            for key, latest_start in batch:
                candidates = [json.loads(row[0])['expected'] for row in db.execute(
                    "SELECT payload FROM writes WHERE kind='post_update' AND k1=? AND completed>=?", (key, latest_start))]
                row = posts.get(key)
                previous = baseline.get(str(key))
                unchanged = latest_start == 0 and previous is not None and row == previous
                if unchanged or (row and row['content_revision'] >= 2 and any(row['title'] == item.get('title') and row['content'] == item.get('content') for item in candidates)):
                    verified += 1
                else: mismatch('post_update', key)
        cursor = db.execute("SELECT kind,k1,k2,COALESCE(MAX(CASE WHEN uncertain=0 THEN started END),0) FROM writes WHERE kind IN ('like','bookmark','follow') GROUP BY kind,k1,k2")
        while batch := cursor.fetchmany(CHUNK):
            tables = {'like': ('post_likes', ('post_id','user_id')), 'bookmark': ('post_bookmarks', ('post_id','user_id')),
                      'follow': ('user_follows', ('follower_id','followed_id'))}
            present = {kind: _interaction_rows(sql, table, columns, [(r[1],r[2]) for r in batch if r[0] == kind])
                       for kind, (table, columns) in tables.items()}
            for kind, first, second, latest_start in batch:
                candidates = {row[0] == 'PUT' for row in db.execute(
                    'SELECT DISTINCT method FROM writes WHERE kind=? AND k1=? AND k2=? AND completed>=?', (kind,first,second,latest_start))}
                if latest_start == 0:
                    candidates.update((False, True))
                if ((first,second) in present[kind]) in candidates: verified += 1
                else: mismatch(kind, [first,second])
        if begin == float('inf') or end <= 0:
            errors['invalid_event'] += 1
            begin, end = 0, 0
        extra = {}
        if count or canceled:
            for table, kind in [('posts','post_create'),('comments','comment_create')]:
                actual = int(sql(f"SELECT COUNT(*) FROM {table} WHERE created_at >= '{_time(begin)}' AND created_at <= '{_time(end)}';").strip())
                observed = db.execute('SELECT COUNT(DISTINCT k1) FROM writes WHERE kind=?', (kind,)).fetchone()[0]
                extra[table] = actual - observed
                if actual != observed: mismatch('unmatched_create', table)
        async_result = sql("SELECT open_requirements,parked_messages FROM business_async_budget;").strip().split('\t')
        async_ok = len(async_result) == 2 and all(int(v) == 0 for v in async_result)
        if not async_ok: mismatch('async_incomplete', 'budget')
        if count or canceled:
            asynchronous_failures = int(sql("SELECT COUNT(*) FROM business_application_results WHERE completed_at >= '" + _time(begin) + "' AND result <> 'applied';").strip())
            failures += asynchronous_failures
        else:
            asynchronous_failures = None
        orphans = {}
        for table, query in [
            ('post_likes','SELECT COUNT(*) FROM post_likes l LEFT JOIN posts p ON p.id=l.post_id LEFT JOIN users u ON u.id=l.user_id WHERE p.id IS NULL OR u.id IS NULL;'),
            ('post_bookmarks','SELECT COUNT(*) FROM post_bookmarks b LEFT JOIN posts p ON p.id=b.post_id LEFT JOIN users u ON u.id=b.user_id WHERE p.id IS NULL OR u.id IS NULL;'),
            ('user_follows','SELECT COUNT(*) FROM user_follows f LEFT JOIN users a ON a.id=f.follower_id LEFT JOIN users b ON b.id=f.followed_id WHERE a.id IS NULL OR b.id IS NULL;')]:
            orphans[table] = int(sql(query).strip())
        failures += sum(orphans.values()) + sum(errors.values())
        value = {'schema_version':'gopulse.pt.audit.v1', 'status':'passed' if failures == 0 else 'failed',
                 'failures':failures, 'verified':verified, 'completed_requests':count, 'event_errors':errors,
                 'window':{'begin_epoch':begin,'end_epoch':end}, 'unmatched_creates':extra,
                 'canceled_writes':canceled_results,
                 'uncertain_outcomes':uncertain_results,
                 'async_recovered':async_ok, 'async_failures':asynchronous_failures, 'orphans':orphans, 'examples':examples}
        atomic_json(directory / 'audit.json', value)
        return value
    finally:
        db.close()
