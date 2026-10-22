"""按正式窗口核对业务写入：请求成功数、资源 ID、数据库行和窗口边界请求。"""
from datetime import datetime, timezone
import json
from pathlib import Path

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
    directory = Path(directory)
    corpus = json.loads((Path(preparation) / 'corpus.json').read_text())['users']
    requests, event_errors = parse_events(directory / 'requests.jsonl')
    begin = min((r['started'] for r in requests.values()), default=0)
    end = max((r.get('completed', r['started']) for r in requests.values()), default=begin) + .001
    finished = [request for request in requests.values() if 'completed' in request]
    in_window = [request for request in finished if begin <= request['started'] < end]
    writes = [request for request in requests.values() if request.get('category') in WRITE_CATEGORIES]
    write_window = [request for request in writes if begin <= request['started'] < end]
    boundary_requests = []
    for request in finished:
        if begin <= request['started'] < end and request['completed'] >= end:
            boundary_requests.append({key: request.get(key) for key in ('slot', 'route', 'started', 'completed', 'success')} | {'reason': 'completed_after_window'})
        elif request['started'] < begin and begin <= request['completed'] < end:
            boundary_requests.append({key: request.get(key) for key in ('slot', 'route', 'started', 'completed', 'success')} | {'reason': 'started_before_window'})
    for item in boundary_requests:
        item['started'] = _label(item['started'])
        item['completed'] = _label(item['completed'])

    def content():
        operations = [dict(resource(request, corpus), request=request) for request in write_window
                      if request['route'] in CONTENT_ROUTES and request.get('success')]
        creates = [item for item in operations if item['kind'] in ('post_create', 'comment_create')]
        # 窗口前 10 秒开始、窗口内提交的创建用于识别边界行，不计入窗口内请求。
        boundary_creates = [dict(resource(request, corpus), request=request) for request in writes
                            if request['route'] in ('POST /api/v1/posts', 'POST /api/v1/posts/:postId/comments')
                            and request.get('success') and begin - 10 <= request['started'] < begin]
        cohort = [item for item in creates if item.get('key') is not None and item['request']['completed'] < end]
        after = [item for item in creates if item.get('key') is not None and item['request']['completed'] >= end]
        post_ids = [item['key'] for item in cohort if item['kind'] == 'post_create']
        comment_ids = [item['key'] for item in cohort if item['kind'] == 'comment_create']
        posts = _post_rows(sql, post_ids)
        comments = _comment_rows(sql, comment_ids)
        post_verified = mismatched = missing = 0
        details = []
        for item in cohort:
            if item['kind'] == 'post_create':
                row = posts.get(item['key'])
                expected = item['expected'] or {}
                if row is None:
                    missing += 1
                    details.append({'kind': item['kind'], 'id': item['key'], 'error': 'row_missing'})
                elif row['author_id'] != item['user_id'] or row['title'] != expected.get('title') or row['content'] != expected.get('content'):
                    mismatched += 1
                    details.append({'kind': item['kind'], 'id': item['key'], 'error': 'content_mismatch'})
                else:
                    post_verified += 1
            else:
                row = comments.get(item['key'])
                expected = item['expected'] or {}
                if row is None:
                    missing += 1
                    details.append({'kind': item['kind'], 'id': item['key'], 'error': 'row_missing'})
                elif row['post_id'] != item['post_id'] or row['author_id'] != item['user_id'] or row['content'] != expected.get('content'):
                    mismatched += 1
                    details.append({'kind': item['kind'], 'id': item['key'], 'error': 'content_mismatch'})
                else:
                    post_verified += 1

        # 数据库窗口内新建的行必须能对应到某个请求；否则说明存在未记录写入或窗口错位。
        created = {}
        for table, ids in (('posts', post_ids), ('comments', comment_ids)):
            rows = _rows(sql,
                         'SELECT id FROM ' + table + " WHERE created_at >= '" + _time(begin) + "' AND created_at < '" + _time(end) + "';")
            created[table] = [int(row[0]) for row in rows]
        create_by_id = {'posts': {}, 'comments': {}}
        for item in creates + after + boundary_creates:
            if item.get('key') is None:
                continue
            table = 'posts' if item['kind'] == 'post_create' else 'comments'
            create_by_id[table].setdefault(item['key'], item)
        unmatched = {'posts': [], 'comments': []}
        for table, values in created.items():
            known = set(post_ids if table == 'posts' else comment_ids)
            for value in values:
                if value not in known and value not in create_by_id[table]:
                    unmatched[table].append(value)

        # 内容更新：同一 post 取窗口内最后完成的 PATCH 作为预期内容。
        updates = [item for item in operations if item['kind'] == 'post_update' and item.get('key') is not None]
        # 同一 post 可能被并发更新；最终内容必须属于该资源窗口内成功更新的集合，
        # 不假设 HTTP 完成顺序等于提交顺序。
        update_expected = {}
        boundary_ids = set()
        for item in updates:
            if item['request']['completed'] >= end:
                boundary_ids.add(item['key'])
            else:
                expected = item['expected'] or {}
                update_expected.setdefault(item['key'], set()).add((expected.get('title'), expected.get('content')))
        update_ids = [post_id for post_id in update_expected if post_id not in boundary_ids]
        update_rows = _post_rows(sql, update_ids)
        update_verified = update_mismatch = update_missing = 0
        for post_id in update_ids:
            row = update_rows.get(post_id)
            if row is None:
                update_missing += 1
            elif row['edited_at'] is None or row['content_revision'] < 2 or (row['title'], row['content']) not in update_expected[post_id]:
                update_mismatch += 1
            else:
                update_verified += 1

        # 删除：窗口内成功（含幂等 404）的删除，最终行应不存在。
        deletes = [item for item in operations if item['kind'] == 'post_delete' and item.get('key') is not None]
        delete_cohort = {item['key'] for item in deletes if item['request']['completed'] < end}
        delete_boundary = {item['key'] for item in deletes if item['request']['completed'] >= end}
        delete_rows = _post_rows(sql, delete_cohort - delete_boundary)
        delete_verified = sum(1 for post_id in delete_cohort - delete_boundary if post_id not in delete_rows)
        delete_mismatch = sorted(post_id for post_id in delete_cohort - delete_boundary if post_id in delete_rows)
        return {
            'creates': {'requests_started': len(creates), 'cohort_completed_in_window': len(cohort),
                        'boundary_completed_after_window': len(after), 'verified': post_verified,
                        'missing': missing, 'mismatched': mismatched, 'details': details[:50]},
            'database_created_in_window': {table: len(values) for table, values in created.items()},
            'database_created_unmatched': unmatched,
            'updates': {'requests_started': len(updates), 'cohort': len(update_expected),
                        'boundary_after_window': len(boundary_ids), 'verified': update_verified,
                        'missing': update_missing, 'mismatched': update_mismatch},
            'deletes': {'requests_started': len(deletes), 'cohort': len(delete_cohort - delete_boundary),
                        'boundary_after_window': len(delete_boundary), 'verified': delete_verified,
                        'mismatched': delete_mismatch[:50]},
        }

    def interactions():
        operations = [dict(resource(request, corpus), request=request) for request in writes
                      if request['route'] in INTERACTION_ROUTES and request.get('success') and request['started'] >= begin - 10]
        grouped = {}
        for item in operations:
            if item.get('key') is None or None in item['key']:
                continue
            grouped.setdefault((item['kind'], item['key']), []).append(item)
        expected = {'like': {}, 'bookmark': {}, 'follow': {}}
        boundary = 0
        for (kind, key), items in grouped.items():
            if not any(begin <= item['request']['started'] < end for item in items):
                continue
            if any(item['request']['completed'] >= end for item in items):
                boundary += 1
                continue
            before_end = [item for item in items if item['request']['completed'] < end]
            if any(item['request']['started'] < begin for item in before_end):
                boundary += 1
                continue
            if not before_end:
                boundary += 1
                continue
            latest_start = max(item['request']['started'] for item in before_end)
            candidates = [item for item in before_end if item['request']['completed'] >= latest_start]
            expected[kind][key] = {item['request']['route'].split()[0] == 'PUT' for item in candidates}
        liked = _interaction_rows(sql, 'post_likes', ('post_id', 'user_id'),
                                  list(expected['like']))
        bookmarked = _interaction_rows(sql, 'post_bookmarks', ('post_id', 'user_id'),
                                       list(expected['bookmark']))
        followed = _interaction_rows(sql, 'user_follows', ('follower_id', 'followed_id'),
                                     list(expected['follow']))
        present = {'like': liked, 'bookmark': bookmarked, 'follow': followed}
        result = {}
        for kind, values in expected.items():
            verified = mismatch = 0
            examples = []
            for key, should_exist in values.items():
                exists = key in present[kind]
                if exists in should_exist:
                    verified += 1
                else:
                    mismatch += 1
                    if len(examples) < 20:
                        examples.append({'key': list(key), 'expected': sorted(should_exist), 'present': exists})
            result[kind] = {'keys_in_window': len(values), 'expected_present': sum(1 for value in values.values() if value == {True}),
                            'expected_absent': sum(1 for value in values.values() if value == {False}),
                            'verified': verified, 'mismatched': mismatch, 'examples': examples}
        result['boundary_or_indeterminate_keys'] = boundary
        return result

    content_result = content()
    interaction_result = interactions()
    database = {}
    database['posts_created_in_window'] = int(sql(
        "SELECT COUNT(*) FROM posts WHERE created_at >= '" + _time(begin) + "' AND created_at < '" + _time(end) + "';").strip())
    database['comments_created_in_window'] = int(sql(
        "SELECT COUNT(*) FROM comments WHERE created_at >= '" + _time(begin) + "' AND created_at < '" + _time(end) + "';").strip())
    database['business_commands'] = {row[0]: {'status': row[1], 'count': int(row[2])} for row in _rows(
        sql,
        "SELECT command_type,status,COUNT(*) FROM business_commands WHERE created_at >= '" + _time(begin) +
        "' AND created_at < '" + _time(end) + "' GROUP BY command_type,status ORDER BY command_type,status;")}
    database['application_results'] = {row[0] + '/' + row[1]: int(row[2]) for row in _rows(
        sql,
        "SELECT consumer,result,COUNT(*) FROM business_application_results WHERE completed_at >= '" + _time(begin) +
        "' AND completed_at < '" + _time(end) + "' GROUP BY consumer,result ORDER BY consumer,result;")}
    database['application_failures'] = sum(value for key, value in database['application_results'].items() if not key.endswith('/applied'))
    database['requirements_closed_in_window'] = int(sql(
        "SELECT COUNT(*) FROM business_event_requirements WHERE closed_at >= '" + _time(begin) +
        "' AND closed_at < '" + _time(end) + "';").strip())
    database['budget'] = dict(zip(('open_requirements', 'retained_groups', 'retained_bytes', 'parked_messages'),
                                  (sql('SELECT open_requirements,retained_groups,retained_bytes,parked_messages FROM business_async_budget;').strip().split('\t'))))
    database['orphans'] = {}
    for name, query in (
            ('post_likes', 'SELECT COUNT(*) FROM post_likes l LEFT JOIN posts p ON p.id=l.post_id LEFT JOIN users u ON u.id=l.user_id WHERE p.id IS NULL OR u.id IS NULL;'),
            ('post_bookmarks', 'SELECT COUNT(*) FROM post_bookmarks b LEFT JOIN posts p ON p.id=b.post_id LEFT JOIN users u ON u.id=b.user_id WHERE p.id IS NULL OR u.id IS NULL;'),
            ('user_follows', 'SELECT COUNT(*) FROM user_follows f LEFT JOIN users a ON a.id=f.follower_id LEFT JOIN users b ON b.id=f.followed_id WHERE a.id IS NULL OR b.id IS NULL;')):
        database['orphans'][name] = int(sql(query).strip())
    write_slots = {request['slot'] for request in write_window}
    unfinished_writes = sum(1 for slot in write_slots if slot in requests and 'completed' not in requests[slot])
    unknown_successes = sum(1 for r in writes if r.get('success') and resource(r, corpus).get('key') is None)
    failures = unknown_successes + (content_result['creates']['missing'] + content_result['creates']['mismatched'] +
                content_result['updates']['missing'] + content_result['updates']['mismatched'] +
                len(content_result['deletes']['mismatched']) +
                sum(item['mismatched'] for name, item in interaction_result.items() if isinstance(item, dict)) +
                sum(len(values) for values in content_result['database_created_unmatched'].values()) +
                database['application_failures'] + sum(database['orphans'].values()))
    value = {
        'schema_version': 'gopulse.pt.audit.v1',
        'window': {'begin_epoch': begin, 'end_epoch': end, 'begin': _label(begin), 'end': _label(end)},
        'requests': {'started_in_window': len(in_window), 'write_requests_started_in_window': len(write_window),
                     'write_requests_unfinished': unfinished_writes,
                     'started_before_window_completed_in_window': sum(1 for item in boundary_requests if item['reason'] == 'started_before_window'),
                     'started_in_window_completed_after_window': sum(1 for item in boundary_requests if item['reason'] == 'completed_after_window')},
        'content_write': content_result,
        'interaction_write': interaction_result,
        'database': database,
        'boundary': {'count': len(boundary_requests), 'samples': boundary_requests[:100],
                     'by_reason': _summary(item['reason'] for item in boundary_requests)},
        'event_errors': event_errors,
        'failures': failures,
        'status': 'passed' if failures == 0 and unfinished_writes == 0 and not any(event_errors.values()) else 'failed',
        'acceptance': '按请求/资源 ID 核对窗口内成功写入；窗口边界单独记录，不计入窗口内差额',
    }
    atomic_json(directory / 'audit.json', value)
    return value
