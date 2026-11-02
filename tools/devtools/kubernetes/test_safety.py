"""验证模式、凭据与资源操作的边界。"""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import k8s
import cluster


class NodeAddressTests(unittest.TestCase):
    def test_vpn_outbound_route_does_not_replace_node_address(self):
        rows = [{'dst': 'default', 'dev': 'eth4', 'gateway': '10.11.40.1', 'metric': 50},
                {'dst': '0.0.0.0/5', 'dev': 'eth6', 'gateway': '172.18.0.2'}]
        with patch.object(cluster, 'run', side_effect=[json.dumps(rows), json.dumps([{'prefsrc': '10.11.40.164'}])]) as command:
            self.assertEqual(cluster.node_ip(), '10.11.40.164')
            self.assertEqual(command.call_args.args, ('ip', '-j', '-4', 'route', 'get', '10.11.40.1', 'oif', 'eth4'))

    def test_node_must_match_saved_config_and_local_address(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(cluster, 'PRIVATE', Path(directory)):
            (Path(directory) / 'kubeadm.yaml').write_text('localAPIEndpoint:\n  advertiseAddress: 10.11.40.164\n')
            local = [{'ifname': 'eth4', 'addr_info': [{'local': '10.11.40.164'}]},
                     {'ifname': 'eth6', 'addr_info': [{'local': '172.18.0.1'}]}]
            with patch.object(cluster, 'run', return_value=json.dumps(local)):
                cluster.check_node_address('10.11.40.164')
                with self.assertRaisesRegex(AssertionError, '配置不符'):
                    cluster.check_node_address('172.18.0.1')
            with patch.object(cluster, 'run', return_value=json.dumps(local[1:])):
                with self.assertRaisesRegex(AssertionError, '已不在本机'):
                    cluster.check_node_address('10.11.40.164')


class SafetyTests(unittest.TestCase):
    def test_foreign_namespace_is_never_stopped(self):
        foreign = json.dumps({'metadata': {'annotations': {'gopulse.io/workspace': '/other'}}})
        with patch.object(k8s, 'kube', return_value=foreign) as command, patch.object(k8s, 'stop_forwards') as stop:
            with self.assertRaisesRegex(ValueError, '归属不符'):
                k8s.down('business')
        self.assertEqual(command.call_count, 1)
        stop.assert_not_called()

    def test_budget_includes_other_workloads_in_same_namespace(self):
        items = [{'kind':'Deployment','metadata':{'name':'new'},'spec':{'template':{'spec':{'containers':[{'resources':{'requests':{'cpu':'1','memory':'1Mi'}}}]}}}}]
        def kube(*args):
            if args[1] == 'node':return json.dumps({'status':{'allocatable':{'cpu':'2','memory':'1Gi'}}})
            return json.dumps({'items':[{'metadata':{'namespace':'gopulse-business','labels':{'app.kubernetes.io/name':'existing'}},'status':{'phase':'Running'},'spec':{'containers':[{'resources':{'requests':{'cpu':'1.5'}}}]}}]})
        with patch.object(k8s, 'kube', side_effect=kube):
            with self.assertRaisesRegex(ValueError, 'cpu 预算不足'):
                k8s.resource_check(items, 'business')

    def test_other_mode_and_integration_pods_block_start(self):
        for ns in ('gopulse-all', 'gopulse-integration-abcd', 'gopulse-pt-business'):
            with patch.object(k8s, 'kube', return_value=json.dumps({'items':[{'metadata':{'namespace':ns},'status':{'phase':'Pending'}}]})):
                with self.assertRaisesRegex(ValueError, '先停止'):
                    k8s.assert_exclusive('business')

    def test_credentials_are_stable_and_mode_specific(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(k8s, 'PRIVATE', Path(tmp)), patch.object(k8s, 'apply'), patch.object(k8s, 'kube', return_value=''):
            k8s.credentials('business')
            original=(Path(tmp)/'business/credentials.json').read_bytes()
            k8s.credentials('business')
            self.assertEqual((Path(tmp)/'business/credentials.json').read_bytes(),original)
            k8s.credentials('all')
            self.assertNotEqual((Path(tmp)/'all/credentials.json').read_bytes(),original)
            self.assertEqual((Path(tmp)/'business/credentials.json').stat().st_mode & 0o777,0o600)

    def test_forward_cleanup_uses_birth_token_and_group(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(k8s, 'PRIVATE', Path(tmp)):
            directory=Path(tmp)/'business';directory.mkdir()
            (directory/'forwards-product.json').write_text(json.dumps([{'pid':123,'birth':'old'}]))
            other=directory/'forwards-selfmonitor.json';other.write_text(json.dumps([{'pid':456,'birth':'current'}]))
            with patch.object(k8s,'birth',return_value='current'),patch.object(k8s.os,'kill') as kill:
                k8s.stop_forwards('business','product')
                kill.assert_not_called()
            self.assertTrue(other.exists())

    def test_service_proxy_stops_its_owned_process_group(self):
        with tempfile.TemporaryDirectory() as tmp, patch.object(k8s, 'PRIVATE', Path(tmp)):
            directory = Path(tmp) / 'business'
            directory.mkdir()
            record = directory / 'forwards-product.json'
            record.write_text(json.dumps([{'pid': 123, 'birth': 'current', 'process_group': True}]))
            with patch.object(k8s, 'birth', side_effect=['current', None]), patch.object(k8s.os, 'killpg') as group, patch.object(k8s.os, 'kill') as process:
                k8s.stop_forwards('business')
                group.assert_called_once_with(123, k8s.signal.SIGTERM)
                process.assert_not_called()
            self.assertFalse(record.exists())


class RenderTests(unittest.TestCase):
    def test_trace_summary_excludes_history_and_incomplete_writes(self):
        from tracecheck import summarize
        def row(service, trace, started):
            return {'resourceSpans': [{'resource': {'attributes': [{'key': 'service.name', 'value': {'stringValue': service}}]},
                    'scopeSpans': [{'spans': [{'traceId': trace, 'startTimeUnixNano': str(started)}]}]}]}
        lines = [row('old-service', 'old', 1), row('backend', 'new', 10), row('business-worker', 'new', 11)]
        raw = '\n'.join(json.dumps(line) for line in lines) + '\n{"resourceSpans":'
        self.assertEqual(summarize(raw, 10), {'services': {'backend': 1, 'business-worker': 1},
                                           'trace_count': 1, 'cross_service_traces': 1})

    def test_reads_multiple_kubectl_json_documents_and_lists(self):
        self.assertEqual(k8s.decode_resources('{"kind":"Namespace","metadata":{"name":"a"}}\n{"kind":"List","items":[{"kind":"Service"}]}'),
                         [{'kind':'Namespace','metadata':{'name':'a'}},{'kind':'Service'}])

    def test_digest_image_import_preserves_digest_and_content(self):
        import io
        import tarfile
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory) / 'images.tar'
            digest = 'sha256:' + 'a' * 64
            index = {'schemaVersion': 2, 'manifests': [{'digest': digest, 'size': 3}]}
            with tarfile.open(archive, 'w') as target:
                for name, raw in [('index.json', json.dumps(index).encode()), ('blobs/sha256/example', b'xyz')]:
                    member = tarfile.TarInfo(name)
                    member.size = len(raw)
                    target.addfile(member, io.BytesIO(raw))
            k8s.name_archive(archive, ['prom/prometheus:v3.5.0@' + digest])
            with tarfile.open(archive) as result:
                descriptor = json.load(result.extractfile('index.json'))['manifests'][0]
                self.assertEqual(descriptor['digest'], digest)
                self.assertEqual(descriptor['annotations']['io.containerd.image.name'], 'docker.io/prom/prometheus@' + digest)
                self.assertEqual(result.extractfile('blobs/sha256/example').read(), b'xyz')


class BootstrapTests(unittest.TestCase):
    def test_single_node_services_can_start_before_readiness(self):
        rows = json.loads((k8s.BASE / 'observability/kafka.json').read_text())['items']
        service = next(row for row in rows if row['kind'] == 'Service')
        self.assertTrue(service['spec']['publishNotReadyAddresses'])
        rows = json.loads((k8s.BASE / 'observability/victoriametrics.json').read_text())['items']
        container = rows[1]['spec']['template']['spec']['containers'][0]
        self.assertNotIn('command', container)
        self.assertTrue(container['args'][0].startswith('-storageDataPath='))

    def test_standalone_observation_enables_its_auth_routes(self):
        overlay = json.loads((k8s.BASE / 'overlays/observability/kustomization.yaml').read_text())
        config = next(json.loads(patch['patch']) for patch in overlay['patches'] if patch['target'].get('name') == 'platform-api-env')
        self.assertEqual(config['data']['BACKEND_STANDALONE_OBSERVABILITY'], 'true')
        self.assertEqual(config['data']['BUSINESS_STAGE_ENDPOINTS'], '')


class MonitorStorageTests(unittest.TestCase):
    def test_recreation_does_not_rewrite_private_plugin_permissions(self):
        rows = json.loads((k8s.BASE / 'observability/monitor.json').read_text())['items']
        spec = next(row for row in rows if row['kind'] == 'Deployment')['spec']['template']['spec']
        self.assertEqual(spec['securityContext']['runAsUser'], 10005)
        self.assertNotIn('fsGroup', spec['securityContext'])
        prepare = spec['initContainers'][0]
        self.assertEqual(prepare['securityContext']['runAsUser'], 0)
        self.assertIn('chmod 0700', prepare['command'][-1])
        names = {volume['name'] for volume in spec['volumes']}
        self.assertTrue(all(mount['name'] in names for mount in prepare['volumeMounts']))

class CredentialRecoveryTests(unittest.TestCase):
    def test_missing_local_credentials_reuse_existing_secret(self):
        import base64
        secret = json.dumps({'data': {'key': base64.b64encode(b'existing').decode()}})
        with tempfile.TemporaryDirectory() as tmp, patch.object(k8s, 'PRIVATE', Path(tmp)), patch.object(k8s, 'kube', return_value=secret), patch.object(k8s, 'apply'):
            values = k8s.ensure_credentials('all', 'test', 'credentials.json', lambda: self.fail('不得重新生成凭据'))
            self.assertEqual(values, {'key': 'existing'})
            self.assertEqual(json.loads((Path(tmp) / 'all/credentials.json').read_text()), values)

    def test_mismatched_local_credentials_never_overwrite_secret(self):
        import base64
        with tempfile.TemporaryDirectory() as tmp, patch.object(k8s, 'PRIVATE', Path(tmp)):
            path = Path(tmp) / 'all'
            path.mkdir()
            (path / 'credentials.json').write_text(json.dumps({'key': 'local'}))
            secret = json.dumps({'data': {'key': base64.b64encode(b'cluster').decode()}})
            with patch.object(k8s, 'kube', return_value=secret), patch.object(k8s, 'apply') as apply:
                with self.assertRaisesRegex(ValueError, '不一致'):
                    k8s.ensure_credentials('all', 'test', 'credentials.json', lambda: {})
                apply.assert_not_called()

    def test_partial_job_failure_does_not_start_application(self):
        rows = [{'kind': 'Namespace', 'metadata': {'name': 'gopulse-all'}},
                {'kind': 'Job', 'metadata': {'name': 'migrate'}},
                {'kind': 'Deployment', 'metadata': {'name': 'backend'}}]
        with tempfile.TemporaryDirectory() as tmp, patch.object(k8s, 'PRIVATE', Path(tmp)), patch.object(k8s, 'assert_exclusive'), patch.object(k8s.deployment, 'check'), patch.object(k8s, 'owned_namespace'), patch.object(k8s, 'render', return_value=rows), patch.object(k8s, 'bind_config'), patch.object(k8s, 'resource_check'), patch.object(k8s, 'credentials'), patch.object(k8s, 'apply') as apply:
            def kube(*args, **kwargs):
                if 'wait' in args and 'job/migrate' in args:
                    raise RuntimeError('migration failed')
                return ''
            with patch.object(k8s, 'kube', side_effect=kube):
                with self.assertRaisesRegex(RuntimeError, 'migration failed'):
                    k8s._up('all')
            self.assertFalse(any(row['kind'] == 'Deployment' for call in apply.call_args_list for row in call.args[0]))
