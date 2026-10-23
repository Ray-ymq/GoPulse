import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from flow.audit import build


class AuditTests(unittest.TestCase):
    def exercise(self, mismatch=False, unfinished=False, async_pending=False):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            (directory / 'corpus.json').write_text(json.dumps({'users':[{'id':7}]}))
            event = {'schema_version':'gopulse.pt.request.v1','type':'start','request_id':'fixture:1',
                     'route':'POST /api/v1/posts','category':'content_write','phase':'measurement','started_at_ms':1000}
            events = [event]
            if not unfinished:
                events.append(dict(event, type='outcome',completed_at_ms=2000,success=True,
                                   sample={'actor':0,'path':'/api/v1/posts','response_id':12,'expected':{'title':'title','content':'content'}}))
            (directory / 'requests.jsonl').write_text(''.join(json.dumps(row)+'\n' for row in events))
            def sql(query):
                if query.startswith('SELECT id,author_id,title,content'):
                    return f'12\t7\ttitle\t{"wrong" if mismatch else "content"}\tNULL\t1\n'
                if query.startswith('SELECT COUNT(*) FROM posts WHERE created_at'): return '1\n'
                if query.startswith('SELECT open_requirements,parked_messages'): return '1\t0\n' if async_pending else '0\t0\n'
                if query.startswith('SELECT COUNT(*)'): return '0\n'
                raise AssertionError(query)
            with patch('flow.audit.sql', side_effect=sql):
                return build(directory, directory)

    def test_confirmed_and_mismatched_write(self):
        self.assertEqual(self.exercise()['status'], 'passed')
        self.assertEqual(self.exercise(mismatch=True)['status'], 'failed')

    def test_unfinished_and_async_pending(self):
        self.assertEqual(self.exercise(unfinished=True)['status'], 'failed')
        self.assertEqual(self.exercise(async_pending=True)['status'], 'failed')

    def canceled(self, route='POST /api/v1/posts', applied=False, duplicate=False,
                 baseline=True, wrong=False, previous=False, known=True, failed_status=None):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            (directory / 'corpus.json').write_text(json.dumps({'users': [{'id': 7}]}))
            initial = {'author_id': 7, 'title': 'before', 'content': 'before',
                       'edited_at': 'NULL', 'content_revision': 1}
            if baseline:
                (directory / 'audit-baseline.json').write_text(json.dumps({'12': initial}))
            sample = {'actor': 0, 'path': '/api/v1/posts' if route == 'POST /api/v1/posts' else '/api/v1/posts/12',
                      'expected': {'title': 'title', 'content': 'content'}}
            event = {'schema_version': 'gopulse.pt.request.v1', 'type': 'start', 'request_id': 'cancel:1',
                     'route': route, 'category': 'content_write', 'phase': 'measurement',
                     'started_at_ms': 2000, 'sample': sample}
            events = []
            if previous:
                prior = dict(event, request_id='previous', started_at_ms=1000,
                             sample=dict(sample, expected={'title': 'before', 'content': 'before'}))
                events += [prior, dict(prior, type='outcome', completed_at_ms=1500, success=True)]
            outcome = dict(event, type='cancel', completed_at_ms=3000,
                           error='stop_cancellation' if known else 'unknown')
            if failed_status is not None:
                outcome.update(type='outcome', success=False, status=failed_status)
            events += [event, outcome]
            (directory / 'requests.jsonl').write_text(''.join(json.dumps(row)+'\n' for row in events))
            def sql(query):
                if query.startswith('SELECT id FROM posts WHERE'):
                    return '12\n13\n' if duplicate else ('12\n' if applied else '')
                if query.startswith('SELECT id,author_id,title,content'):
                    if route.startswith('DELETE') and applied:
                        return ''
                    content = 'wrong' if wrong else ('content' if applied else 'before')
                    title = 'title' if applied else 'before'
                    return f'12\t7\t{title}\t{content}\tNULL\t{2 if applied or previous else 1}\n'
                if query.startswith('SELECT COUNT(*) FROM posts WHERE created_at'):
                    return '1\n' if applied and route.startswith('POST') else '0\n'
                if query.startswith('SELECT open_requirements,parked_messages'):
                    return '0\t0\n'
                if query.startswith('SELECT COUNT(*)'):
                    return '0\n'
                raise AssertionError(query)
            with patch('flow.audit.sql', side_effect=sql):
                return build(directory, directory)

    def test_canceled_create_reconciles_applied_and_absent_rows(self):
        for applied in (False, True):
            with self.subTest(applied=applied):
                result = self.canceled(applied=applied)
                self.assertEqual(result['status'], 'passed', result)
                self.assertEqual(result['canceled_writes']['writes'], 1)
                self.assertEqual(result['canceled_writes']['resolved'], 1)
                self.assertEqual(result['canceled_writes']['applied_creates'], int(applied))
        self.assertEqual(self.canceled(applied=True, duplicate=True)['status'], 'failed')

    def test_network_or_server_failure_can_have_an_atomic_commit(self):
        for status in (0, 500, 504):
            for applied in (False, True):
                with self.subTest(status=status, applied=applied):
                    result = self.canceled(applied=applied, failed_status=status)
                    self.assertEqual(result['status'], 'passed', result)
                    self.assertEqual(result['uncertain_outcomes']['writes'], 1)
                    self.assertEqual(result['uncertain_outcomes']['applied_creates'], int(applied))
                    self.assertEqual(result['canceled_writes']['writes'], 0)
        self.assertEqual(self.canceled(applied=True, failed_status=429)['status'], 'failed')

    def test_canceled_update_allows_baseline_or_intent_but_rejects_other_content(self):
        route = 'PATCH /api/v1/posts/:postId'
        for applied in (False, True):
            for previous in (False, True):
                result = self.canceled(route, applied=applied, previous=previous)
                self.assertEqual(result['status'], 'passed', result)
        self.assertEqual(self.canceled(route, wrong=True)['status'], 'failed')
        self.assertEqual(self.canceled(route, baseline=False)['status'], 'failed')

    def test_canceled_delete_and_missing_cancellation_proof(self):
        for applied in (False, True):
            result = self.canceled('DELETE /api/v1/posts/:postId', applied=applied)
            self.assertEqual(result['status'], 'passed', result)
        self.assertEqual(self.canceled(known=False)['status'], 'failed')

    def test_canceled_interaction_keeps_confirmed_state_and_possible_commit(self):
        for present in (False, True):
            for cancel_method in ('PUT', 'DELETE'):
                with self.subTest(present=present, cancel_method=cancel_method), tempfile.TemporaryDirectory() as tmp:
                    directory = Path(tmp)
                    (directory / 'corpus.json').write_text(json.dumps({'users': [{'id': 7}]}))
                    start = {'schema_version': 'gopulse.pt.request.v1', 'type': 'start',
                             'request_id': 'confirmed', 'route': 'PUT /api/v1/posts/:postId/like',
                             'category': 'interaction_write', 'phase': 'measurement', 'started_at_ms': 1000,
                             'sample': {'actor': 0, 'path': '/api/v1/posts/12/like'}}
                    canceled = dict(start, request_id='canceled', started_at_ms=3000,
                                    route=f'{cancel_method} /api/v1/posts/:postId/like')
                    events = [start, dict(start, type='outcome', completed_at_ms=2000, success=True),
                              canceled, dict(canceled, type='cancel', error='stop_cancellation')]
                    (directory / 'requests.jsonl').write_text(''.join(json.dumps(row)+'\n' for row in events))
                    def sql(query):
                        if query.startswith('SELECT post_id,user_id FROM post_likes'):
                            return '12\t7\n' if present else ''
                        if query.startswith('SELECT open_requirements,parked_messages'):
                            return '0\t0\n'
                        if query.startswith('SELECT COUNT(*)'):
                            return '0\n'
                        raise AssertionError(query)
                    with patch('flow.audit.sql', side_effect=sql):
                        result = build(directory, directory)
                    expected = present or cancel_method == 'DELETE'
                    self.assertEqual(result['status'] == 'passed', expected, result)
