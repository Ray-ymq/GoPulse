"""验证恢复验收不能把未发生的重启、地址变更或资源替换记为通过。"""
import copy
import json
from pathlib import Path
import tempfile
import unittest

import recovery


class RecoveryComparisonTests(unittest.TestCase):
    def setUp(self):
        self.before = {'schema': 1, 'workspace': '/workspace', 'mode': 'all',
                       'node_uid': 'node', 'namespace_uid': 'namespace',
                       'resources': {'pvc/mysql': {'uid': 'claim', 'volume': 'volume'},
                                     'secret/credentials': {'uid': 'secret', 'data_sha256': 'digest'}},
                       'credentials': {'credentials.json': 'digest'},
                       'data': {'business': {'post_id': 42, 'title': 'existing'}, 'configured_plugins': ['mysql-exporter']},
                       'healthy_targets': 16, 'host_boot_id': 'boot-before', 'address': '10.11.40.164'}
        self.after = copy.deepcopy(self.before)

    def test_requires_a_real_boot_change(self):
        with self.assertRaisesRegex(ValueError, 'boot ID 未变化'):
            recovery.compare(self.before, self.after, 'reboot')
        self.after['host_boot_id'] = 'boot-after'
        recovery.compare(self.before, self.after, 'reboot')

    def test_requires_a_real_address_change(self):
        with self.assertRaisesRegex(ValueError, '节点地址未变化'):
            recovery.compare(self.before, self.after, 'address')
        self.after['address'] = '192.0.2.10'
        recovery.compare(self.before, self.after, 'address')

    def test_rejects_replacement_or_data_changes_even_after_reboot(self):
        for key in ('schema', 'workspace', 'mode', 'node_uid', 'namespace_uid', 'resources', 'credentials', 'data', 'healthy_targets'):
            with self.subTest(key=key):
                after = copy.deepcopy(self.before)
                after['host_boot_id'] = 'boot-after'
                after[key] = 'changed'
                with self.assertRaisesRegex(ValueError, key):
                    recovery.compare(self.before, after, 'reboot')

    def test_accepts_unchanged_resources_without_claiming_a_reboot(self):
        recovery.compare(self.before, self.after)

    def test_record_is_private_and_cannot_overwrite_baseline(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'records/baseline.json'
            recovery.save(path, self.before)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                recovery.save(path, {'changed': True})
            self.assertEqual(json.loads(path.read_text()), self.before)
