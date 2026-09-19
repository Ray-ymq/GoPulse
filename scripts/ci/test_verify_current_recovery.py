"""Acceptance contract: compare content, not just matching row counts."""
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path

from verify_current_recovery import CurrentRecovery


class FactsTest(unittest.TestCase):
    def test_container_snapshot_uses_supported_positive_label_filter(self):
        def command(*args):
            if '--filter' in args:
                self.assertIn(args[-1], {
                    'label=io.gopulse.phase16.runner=true',
                    'label=com.docker.volume.anonymous'})
                return 'runner' if args[0] == 'ps' else ''
            return 'runner\nforeign' if args[0] == 'ps' else 'owned-before'
        with patch.dict('os.environ', {'GOPULSE_PHASE16_MATRIX':'1'}), patch('verify_current_recovery.docker', side_effect=command):
            self.assertEqual(CurrentRecovery.resources()['containers'], ['foreign'])

    def test_restored_content_allows_new_rows_but_rejects_changed_original(self):
        with tempfile.TemporaryDirectory() as directory:
            recovery = object.__new__(CurrentRecovery)
            recovery.work = Path(directory)
            recovery.receipt = recovery.work/'acceptance.json'
            recovery.data = {'schema': 1, 'completed': [], 'password': 'must-not-be-public'}
            recovery.facts = lambda _: {'posts': ['1\toriginal', '2\tnew']}
            recovery.compare('target', {'posts': ['1\toriginal']})
            self.assertNotIn('must-not-be-public', (recovery.work/'evidence.json').read_text())
            self.assertEqual((recovery.work/'evidence.json').stat().st_mode & 0o777, 0o600)
            recovery.facts = lambda _: {'posts': ['1\tcorrupted', '2\tnew']}
            with self.assertRaisesRegex(RuntimeError, 'content mismatch: posts'):
                recovery.compare('target', {'posts': ['1\toriginal']})


if __name__ == '__main__':
    unittest.main()
