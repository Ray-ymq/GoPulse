"""Regression for digest-only image snapshots and cleanup's masked exit status."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[2]

class ReleaseSnapshotTest(unittest.TestCase):
    def test_none_is_not_a_tag(self):
        source=(ROOT/'scripts/verify-compose-observability.sh').read_text()
        line=next(s for s in source.splitlines() if 'awk' in s and 'image-tags' in s)
        filter_command=line.split('| awk',1)[1].split(' >',1)[0]
        result=subprocess.run(['bash','-c','awk'+filter_command],input='registry/product:<none>|sha256:aaa\nregistry/product:1.13.1|sha256:bbb\n',text=True,capture_output=True,check=True)
        self.assertEqual(result.stdout,'registry/product:1.13.1|sha256:bbb\n')

    def test_snapshot_failure_is_not_masked_by_following_success(self):
        source=(ROOT/'scripts/verify-compose-observability.sh').read_text()
        function=source.split('assert_snapshot_preserved() {',1)[1].split('\ncleanup_acceptance_images()',1)[0]
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            (root/'git-status').write_text('')
            for kind in ('containers','networks','volumes','images'):(root/kind).write_text('')
            (root/'image-tags').write_text('bad|old\ngood|new\n')
            script='''fail() { echo "$*" >&2; return 1; }
git() { :; }
docker() { echo new; }
assert_snapshot_preserved() {'''+function+'''
if assert_snapshot_preserved; then exit 0; else exit 17; fi
'''
            result=subprocess.run(['bash','-c',script],env={**os.environ,'SNAPSHOT_DIR':directory,'REPO_ROOT':directory},capture_output=True,text=True)
            self.assertEqual(result.returncode,17)
            self.assertIn('mapping changed: bad',result.stderr)
