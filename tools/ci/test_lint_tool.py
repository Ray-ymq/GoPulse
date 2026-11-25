from __future__ import annotations

import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

from lint_tool import ensure_lint


class LintToolTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.config = {
            'version': '2.14.0',
            'release': 'https://example.invalid',
            'archives': {'linux/amd64': {'name': 'lint.tar.gz', 'sha256': ''}},
        }
        (self.root / 'tools/ci').mkdir(parents=True)
        self.directory = self.root / '.run/tools/golangci-lint/2.14.0/linux-amd64'
        self.host = patch('lint_tool.subprocess.check_output', return_value='linux\namd64\n')
        self.host.start()
        self.addCleanup(self.host.stop)

    def archive(self, version='2.14.0', name='lint/golangci-lint'):
        content = f'#!/bin/sh\nprintf "golangci-lint has version {version}\\n"\n'.encode()
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode='w:gz') as archive:
            member = tarfile.TarInfo(name)
            member.size = len(content)
            archive.addfile(member, io.BytesIO(content))
        data = output.getvalue()
        self.config['archives']['linux/amd64']['sha256'] = hashlib.sha256(data).hexdigest()
        self.write_config()
        return data

    def write_config(self):
        (self.root / 'tools/ci/toolchains.json').write_text(json.dumps({'golangci_lint': self.config}))

    def test_download_and_cache_without_another_request(self):
        data = self.archive()
        with patch('lint_tool.urllib.request.urlopen', return_value=io.BytesIO(data)) as download:
            binary = ensure_lint(self.root)
            self.assertTrue(binary.stat().st_mode & 0o100)
            self.assertEqual(ensure_lint(self.root), binary)
            download.assert_called_once()

    def test_rejects_download_with_wrong_checksum(self):
        self.archive()
        with patch('lint_tool.urllib.request.urlopen', return_value=io.BytesIO(b'wrong')):
            with self.assertRaisesRegex(ValueError, 'SHA256'):
                ensure_lint(self.root)
        self.assertEqual(list(self.directory.iterdir()), [])

    def test_rejects_corrupted_archive_even_with_cached_binary(self):
        data = self.archive()
        with patch('lint_tool.urllib.request.urlopen', return_value=io.BytesIO(data)):
            ensure_lint(self.root)
        (self.directory / 'lint.tar.gz').write_bytes(b'corrupted')
        with self.assertRaisesRegex(ValueError, 'SHA256'):
            ensure_lint(self.root)

    def test_rejects_binary_with_wrong_version(self):
        data = self.archive(version='0.0.0')
        with patch('lint_tool.urllib.request.urlopen', return_value=io.BytesIO(data)):
            with self.assertRaisesRegex(ValueError, '版本'):
                ensure_lint(self.root)
        self.assertFalse((self.directory / 'golangci-lint').exists())

    def test_rejects_parent_path_member(self):
        data = self.archive(name='../golangci-lint')
        with patch('lint_tool.urllib.request.urlopen', return_value=io.BytesIO(data)):
            with self.assertRaisesRegex(ValueError, '缺少'):
                ensure_lint(self.root)
        self.assertFalse((self.directory.parent / 'golangci-lint').exists())


if __name__ == '__main__':
    unittest.main()
