"""Cleanup must refuse foreign IDs even when the caller tolerates failure."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class CleanupTest(unittest.TestCase):
    def test_foreign_label_never_reaches_stop_or_remove(self):
        source = (ROOT/'scripts/verify-marshaller.sh').read_text()
        for name, variable in [('stop_router', 'ROUTER_CONTAINER'), ('stop_monitor', 'MONITOR_CONTAINER')]:
            start = source.index(name+'() {')
            function = source[start:source.index('\n}\n', start)+3]
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                log = Path(directory)/'calls'
                script = '''set -euo pipefail
PROJECT=owned
fail() { return 1; }
docker() { printf '%s\\n' "$*" >> "$LOG"; printf foreign; }
'''+variable+'=foreign-id\n'+function+'\n'+name+' || true\n'
                subprocess.run(['bash', '-c', script], env={'PATH':'/usr/bin:/bin', 'LOG':str(log)}, check=True)
                self.assertEqual(len(log.read_text().splitlines()), 1)
                self.assertTrue(log.read_text().startswith('inspect '))
