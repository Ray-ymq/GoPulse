import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from runtime_acceptance import Acceptance
from verify_runtime_contracts import ROOT, sha256_file, validate_split_evidence, publish_split_evidence, verify_split_publication


CASES = ('S01', 'S02', 'S03', 'S04', 'S05', 'S06', 'S07')
ADMISSION = ('go', '-C', 'backend', 'test', './internal/http', '-run', '^TestServiceRoleAdmissionIsolation$', '-count=1', '-timeout=60s', '-v')


class RuntimeAcceptanceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        (ROOT / '.run').mkdir(parents=True, exist_ok=True)

    def raw_evidence(self, root, run=None, mode='preflight', manifest=None, receipt=None):
        run = run or root / '.run' / 'unit-runtime'
        run.mkdir(parents=True, exist_ok=True)
        raw = run / 'raw.log'
        raw.write_text('unit raw output\n')
        rel = str(raw.relative_to(root))
        command = {'logical_argv': list(ADMISSION), 'executed_argv': list(ADMISSION), 'exit_code': 0,
                   'stdout': rel, 'stderr': rel, 'started_at': 1, 'ended_at': 2}
        return {
            'schema': 'gopulse.phase21.service-split-evidence.v1', 'suite': 'service-split', 'mode': mode,
            'status': 'passed', 'failure_class': None, 'target_version': '2.3.2',
            'completed_version': (ROOT / 'VERSION').read_text().strip(), 'revision': 'HEAD',
            'source_hash': 'sha256:' + 'a' * 64, 'working_tree_hash': 'sha256:' + 'b' * 64,
            'compose_sha256': sha256_file(ROOT / 'deploy/compose.yaml'),
            'contract_sha256': sha256_file(ROOT / 'deploy/runtime-contracts.json'),
            'manifest_sha256': manifest, 'preflight_receipt': receipt,
            'project': 'gopulse-runtime-012345abcdef',
            'cases': {case: {'status': 'passed', 'raw': [rel]} for case in CASES},
            'commands': [command],
            'preflight': command if mode == 'preflight' else None,
            'resources': {'before': {'containers': [], 'networks': [], 'volumes': []}, 'after': {'containers': [], 'networks': [], 'volumes': []}},
            'cleanup': {'status': 'passed'},
            'instances': {'backend': ['backend-1', 'backend-2'], 'platform-api': ['platform-api-1']},
        }

    def test_cli_exposes_split_modes_and_rejects_mixed_default_mode(self):
        script = ROOT / 'scripts/ci/runtime_acceptance.py'
        help_result = subprocess.run([sys.executable, str(script), '--help'], capture_output=True, text=True)
        self.assertEqual(help_result.returncode, 0)
        self.assertIn('--suite', help_result.stdout)
        rejected = subprocess.run([sys.executable, str(script), '--candidate', '2.3.2', '--preflight'], capture_output=True, text=True)
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn('service-split', rejected.stderr)

    def test_preflight_evidence_requires_closed_cases(self):
        with tempfile.TemporaryDirectory(dir=ROOT / '.run') as directory:
            root = Path(directory)
            evidence = self.raw_evidence(root.parent.parent, run=root, mode='preflight')
            evidence['cases'].pop('S07')
            with self.assertRaises(ValueError):
                validate_split_evidence(evidence, root.parent.parent)

    def test_strict_preflight_evidence_binds_manifest_digest(self):
        with tempfile.TemporaryDirectory(dir=ROOT / '.run') as directory:
            root = Path(directory)
            evidence = self.raw_evidence(root.parent.parent, run=root, mode='preflight', manifest='sha256:' + 'c' * 64)
            validate_split_evidence(evidence, root.parent.parent)
            evidence['manifest_sha256'] = 'not-a-digest'
            with self.assertRaises(ValueError):
                validate_split_evidence(evidence, root.parent.parent)

    def test_clean_checkout_creates_private_run_root(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / '.env.example').write_text('APP_ENV=test\nVICTORIAMETRICS_PASSWORD=test-password\n')
            with patch('runtime_acceptance.ROOT', root), \
                 patch('runtime_acceptance.load', return_value={}), \
                 patch('runtime_acceptance.command', return_value=subprocess.CompletedProcess([], 0, stdout=b'HEAD\n', stderr=b'')):
                acceptance = Acceptance('2.3.2')
            self.assertTrue(acceptance.work.is_dir())
            self.assertEqual(acceptance.work.parent, root / '.run')

    def test_publication_binds_formal_evidence_and_raw_hashes(self):
        with tempfile.TemporaryDirectory(dir=ROOT / '.run') as directory:
            run = Path(directory)
            root = run.parent.parent
            preflight_path = run / 'preflight.json'
            preflight = self.raw_evidence(root, run=run, mode='preflight')
            preflight_path.write_text(json.dumps(preflight))
            formal = self.raw_evidence(root, run=run, mode='formal', manifest='sha256:' + 'c' * 64, receipt=str(preflight_path.relative_to(root)))
            evidence_path = run / 'formal.json'
            evidence_path.write_text(json.dumps(formal))
            publication_path = run / 'publication.json'
            publish_split_evidence(evidence_path, publication_path, root)
            verified = verify_split_publication(publication_path, root)
            self.assertEqual(verified['status'], 'passed')
            self.assertTrue((run / 'summary.json').is_file())


if __name__ == '__main__':
    unittest.main()
