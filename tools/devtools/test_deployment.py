"""验证跨部署互斥、工作区锁和状态记录。"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import deployment


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.parse_docker = deployment.docker_resources
        self.docker = patch.object(deployment, 'docker_resources', return_value=[]).start()
        self.kube = patch.object(deployment, 'kubernetes_resources', return_value=[]).start()
        self.addCleanup(patch.stopall)

    def test_conflicts_work_in_both_directions(self):
        for running, requested in [('kubernetes', 'compose'), ('compose', 'kubernetes')]:
            row = {'runtime': running, 'mode': 'all', 'owner': 'gopulse', 'purpose': 'product'}
            self.docker.return_value = [row] if running == 'compose' else []
            self.kube.return_value = [row] if running == 'kubernetes' else []
            with self.assertRaisesRegex(ValueError, '请先执行'):
                deployment.check(requested, root=self.root)

    def test_same_mode_requires_same_owner(self):
        self.docker.return_value = [{'runtime': 'compose', 'mode': 'business', 'owner': 'owned', 'purpose': 'product'}]
        deployment.check('compose', 'business', owner='owned', root=self.root)
        with self.assertRaises(ValueError):
            deployment.check('compose', 'business', owner='other', root=self.root)
        with self.assertRaises(ValueError):
            deployment.check('compose', 'all', owner='owned', root=self.root)

    def test_source_and_integration_cannot_start_with_product(self):
        self.kube.return_value = [{'runtime': 'kubernetes', 'mode': 'all', 'owner': 'gopulse-all', 'purpose': 'product'}]
        for purpose in ['source', 'integration', 'pt']:
            with self.assertRaises(ValueError):
                deployment.check('compose', purpose=purpose, root=self.root)

    def test_source_dependencies_can_be_reused_only_by_source(self):
        self.docker.return_value = [{'runtime': 'compose', 'mode': 'all', 'owner': 'dev', 'purpose': 'source'}]
        deployment.check('compose', purpose='source', root=self.root)
        with self.assertRaises(ValueError):
            deployment.check('compose', purpose='integration', root=self.root)

    def test_stale_product_record_does_not_override_actual_resources(self):
        deployment.record('kubernetes', 'all', 'gopulse-all', 'running', self.root)
        deployment.check('compose', root=self.root)

    def test_incomplete_pt_requires_explicit_stop(self):
        directory = self.root / '.run/perflab/test'
        directory.mkdir(parents=True)
        (directory.parent / 'active.json').write_text(json.dumps({'run_id': 'test'}))
        (directory / 'state.json').write_text(json.dumps({'status': 'running'}))
        with self.assertRaisesRegex(ValueError, 'pt-stop'):
            deployment.check('kubernetes', root=self.root)
        (directory / 'state.json').write_text(json.dumps({'status': 'completed'}))
        deployment.check('kubernetes', root=self.root)

    def test_flock_rejects_an_independent_process(self):
        with deployment.lock(self.root):
            result = subprocess.run(['python3', '-c', 'import fcntl,sys; f=open(sys.argv[1],"a"); fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)',
                                     str(self.root / '.run/deploy/operation.lock')], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
        with deployment.lock(self.root):
            pass

    def test_state_records_runtime_mode_and_ownership(self):
        deployment.record('compose', 'observability', 'owned', 'running', self.root)
        path = self.root / '.run/deploy/state.json'
        value = json.loads(path.read_text())
        self.assertEqual((value['runtime'], value['mode'], value['owner']), ('compose', 'observability', 'owned'))
        self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_root_selfmonitor_still_blocks_switch_after_product_stops(self):
        container = {'Config': {'Labels': {'com.docker.compose.project.working_dir': '/opt/gopulse-selfmonitor',
                                          'com.docker.compose.project': 'gopulse-selfmonitor'}},
                     'NetworkSettings': {'Networks': {'business': {'NetworkID': 'network'}}}}
        network = {'Labels': {'io.gopulse.workspace': str(self.root)}}
        with patch.object(deployment, 'capture', side_effect=['id', json.dumps([container]), json.dumps([network])]), patch.object(deployment.shutil, 'which', return_value='/bin/docker'):
            rows = self.parse_docker(self.root)
        self.docker.return_value = rows
        deployment.check('compose', 'all', owner='gopulse', root=self.root)
        with self.assertRaisesRegex(ValueError, 'selfmonitor-down'):
            deployment.check('kubernetes', root=self.root)

    def test_darwin_live_source_process_blocks_without_proc(self):
        with patch.object(deployment.sys, 'platform', 'darwin'), patch.object(deployment.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, 'S', '')):
            self.assertTrue(deployment.process_alive({'pid': 123, 'birth': '123.000001'}))
        with patch.object(deployment.sys, 'platform', 'darwin'), patch.object(deployment.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, 'Z', '')):
            self.assertFalse(deployment.process_alive({'pid': 123, 'birth': '123.000001'}))

    def test_actual_foreign_compose_resources_are_ignored(self):
        foreign = {'Config': {'Labels': {'com.docker.compose.project.working_dir': '/foreign/deploy/compose',
                                        'com.docker.compose.project': 'foreign'}}}
        with patch.object(deployment, 'capture', side_effect=['id', json.dumps([foreign])]), patch.object(deployment.shutil, 'which', return_value='/bin/docker'):
            self.assertEqual(self.parse_docker(self.root), [])


if __name__ == '__main__':
    unittest.main()
