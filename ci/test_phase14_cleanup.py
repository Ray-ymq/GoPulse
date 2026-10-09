"""Regression for real migration volumes left behind by Compose down."""
import json
import subprocess
import unittest
from unittest.mock import patch

from verify_phase14_closure import cleanup_orphan_volumes


class ClosureCleanupTests(unittest.TestCase):
    def exercise(self, preexisting):
        project = 'gopulse-p1401-012345abcdef'
        remaining = [project+'_monitor_plugin_data', project+'_p14_stopped']
        deleted = []

        def command(args):
            if args[1:3] == ['volume', 'ls']:
                body = '\n'.join(remaining).encode()
            elif args[1:3] == ['volume', 'inspect']:
                body = json.dumps([{'Labels': {'com.docker.compose.project': project}}]).encode()
            elif args[1:3] == ['volume', 'rm']:
                deleted.append(args[-1]); remaining.remove(args[-1]); body = b''
            else:
                self.fail('unexpected Docker operation')
            return subprocess.CompletedProcess(args, 0, body, b'')

        with patch('verify_phase14_closure.command', command):
            if preexisting:
                with self.assertRaisesRegex(AssertionError, 'pre-existing'):
                    cleanup_orphan_volumes(project, {remaining[-1]})
                self.assertEqual(deleted, [])
                self.assertEqual(len(remaining), 2)
            else:
                cleanup_orphan_volumes(project, set())
                self.assertEqual(remaining, [])
                self.assertEqual(len(deleted), 2)

    def test_removes_formerly_mounted_owned_volumes(self):
        self.exercise(False)

    def test_refuses_entire_deletion_if_any_volume_predates_run(self):
        self.exercise(True)


if __name__ == '__main__':
    unittest.main()
