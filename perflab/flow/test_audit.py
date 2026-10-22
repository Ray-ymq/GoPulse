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
