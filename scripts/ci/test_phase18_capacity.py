import hashlib
import json
import stat
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from phase18_capacity import (
    compose_override, first_bottleneck, generate_recipe, observability_progress,
    preflight, run_capacity, run_preflight, wait_convergence, write_round_binding,
)


class CapacityRunnerTest(unittest.TestCase):
    def test_preflight_workspace_uses_owner_only_lock(self):
        host = {
            'platform': 'linux/amd64', 'host_os': 'Linux', 'kernel': '6.6-microsoft-standard-WSL2', 'cpu_count': 8,
            'memory_bytes': 12 * 1024 ** 3, 'swap_total_bytes': 16 * 1024 ** 3,
            'disk_available_bytes': 80 * 1024 ** 3, 'docker_server_os': 'linux',
            'docker_server_arch': 'amd64', 'docker_server_version': '1', 'compose_version': '2',
            'active_compose_projects': [],
        }
        with tempfile.TemporaryDirectory() as directory, mock.patch('phase18_capacity.host_inventory', return_value=host):
            document = run_preflight(Path(directory))
            self.assertEqual(document['problems'], [])
            self.assertEqual(stat.S_IMODE((Path(directory) / '.lock').stat().st_mode), 0o600)

    def test_preflight_refuses_to_reuse_failed_evidence_workspace(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'evidence').mkdir()
            (work / 'evidence' / 'capacity-failure.json').write_text('{}\n')
            with self.assertRaisesRegex(ValueError, 'refusing to overwrite'):
                run_preflight(work)

    def test_compose_override_uses_dedicated_acceptance_network_for_mysql(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'compose.override.yaml'
            compose_override(path, 18307)
            text = path.read_text()
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            self.assertIn('127.0.0.1:18307:3306', text)
            self.assertIn('      business:\n      acceptance:\n', text)
            self.assertIn('networks:\n  acceptance:\n', text)

    def test_preflight_treats_swap_as_a_minimum(self):
        host = {
            'platform': 'linux/amd64', 'host_os': 'Linux', 'kernel': '6.6-microsoft-standard-WSL2', 'cpu_count': 8,
            'memory_bytes': 12 * 1024 ** 3, 'swap_total_bytes': 8 * 1024 ** 3,
            'disk_available_bytes': 80 * 1024 ** 3, 'docker_server_os': 'linux',
            'docker_server_arch': 'amd64', 'docker_server_version': '1', 'compose_version': '2',
            'active_compose_projects': [],
        }
        self.assertNotIn('8 GiB swap is required', preflight(host))
        host['swap_total_bytes'] = 8 * 1024 ** 3 - 1
        self.assertIn('8 GiB swap is required', preflight(host))

    def test_preflight_requires_at_least_80_gib_free_disk(self):
        host = {
            'platform': 'linux/amd64', 'host_os': 'Linux', 'kernel': '6.6-microsoft-standard-WSL2', 'cpu_count': 8,
            'memory_bytes': 12 * 1024 ** 3, 'swap_total_bytes': 8 * 1024 ** 3,
            'disk_available_bytes': 80 * 1024 ** 3, 'docker_server_os': 'linux',
            'docker_server_arch': 'amd64', 'docker_server_version': '1', 'compose_version': '2',
            'active_compose_projects': [],
        }
        self.assertNotIn('at least 80 GiB free disk is required', preflight(host))
        host['disk_available_bytes'] -= 1
        self.assertIn('at least 80 GiB free disk is required', preflight(host))

    def test_observability_progress_requires_all_three_data_paths(self):
        baseline = {
            'logs_count': 10, 'events_count': 5,
            'marshaller_store_counts': {'metrics': 10, 'logs': 10, 'events': 5},
        }
        progressed = {
            'logs_count': 11, 'events_count': 6,
            'marshaller_store_counts': {'metrics': 11, 'logs': 11, 'events': 6},
        }
        self.assertTrue(observability_progress(progressed, baseline))
        progressed['marshaller_store_counts']['events'] = 5
        self.assertFalse(observability_progress(progressed, baseline))

    def test_first_bottleneck_reports_the_first_queue_signal(self):
        report = {
            'phases': [{'name': 'steady', 'scheduled_slots': 1, 'dropped_slots': 0, 'max_schedule_lag_ms': 0,
                        'latency_by_category': {'read': {'p95_ms': 1, 'p99_ms': 1}, 'content_write': {'p95_ms': 1, 'p99_ms': 1}, 'interaction_write': {'p95_ms': 1, 'p99_ms': 1}},
                        'counts': {'requests': 1, 'timeouts': 0, 'errors': 0}}],
            'load_process': {'rss_bytes': 1},
        }
        resources = {'oom_killed': 0, 'restart_count': 0, 'max_swap_delta_bytes': 0}
        convergence = {'converged': True, 'outbox_pending': 0, 'rabbit_ready': 0, 'rabbit_unacked': 0, 'kafka_lag': 0,
                       'search_count': 1, 'mysql_posts': 1, 'notifications': 500000,
                       'search_seconds': 1, 'notification_seconds': 1, 'metrics_logs_events_seconds': 1, 'recovery_seconds': 1}
        records = [{'links': {'backend': {'gopulse_backend_outbox_pending': 7}}, 'rabbitmq': None, 'kafka_lag': None,
                    'containers': [], 'host': {}}]
        bottleneck = first_bottleneck(report, resources, convergence, records)
        self.assertEqual((bottleneck['component'], bottleneck['reason_code']), ('backend', 'outbox_backlog'))

    def test_bounded_convergence_timeout_returns_truthful_partial_snapshot(self):
        def fake_compose(_env, _compose_file, _project, *args, **_kwargs):
            if 'mysql' in args:
                return SimpleNamespace(returncode=0, stdout='42 500000 50000\n', stderr='')
            if 'rabbitmq' in args:
                return SimpleNamespace(returncode=0, stdout='queue 0 0\n', stderr='')
            return SimpleNamespace(returncode=1, stdout='', stderr='')

        baseline = {
            'logs_count': 10, 'events_count': 5,
            'marshaller_store_counts': {'metrics': 10, 'logs': 10, 'events': 5},
        }
        progressed = {
            'logs_count': 11, 'events_count': 6,
            'marshaller_store_counts': {'metrics': 11, 'logs': 11, 'events': 6},
        }
        with mock.patch('phase18_capacity.compose', side_effect=fake_compose), \
             mock.patch('phase18_capacity.elasticsearch_count', return_value=49999), \
             mock.patch('phase18_capacity.kafka_group_lag', return_value=0), \
             mock.patch('phase18_capacity.observability_snapshot', return_value=progressed), \
             mock.patch('phase18_capacity.time.monotonic', side_effect=[0, 0, 1, 1, 1]), \
             mock.patch('phase18_capacity.time.sleep', return_value=None):
            snapshot = wait_convergence(
                Path('/candidate.env'), Path('/compose.yaml'), 'gopulse-p18-01-000000000000',
                timeout=1, baseline=baseline, allow_incomplete=True,
            )
        self.assertFalse(snapshot['converged'])
        self.assertEqual(snapshot['outbox_pending'], 42)
        self.assertEqual(snapshot['search_count'], 49999)
        self.assertEqual(snapshot['recovery_seconds'], 1.0)
        self.assertEqual(snapshot['logs_count_after'], 11)

    def test_recipe_credentials_use_owner_only_files_not_argv(self):
        captured = []
        secrets_seen = []

        def fake_run(args, timeout=300, env=None):
            captured.append((list(args), timeout, env))
            argv = ' '.join(args)
            self.assertNotIn('db-secret-value', argv)
            dsn_file = Path(args[args.index('--dsn-file') + 1])
            password_file = Path(args[args.index('--password-file') + 1])
            self.assertEqual(stat.S_IMODE(dsn_file.stat().st_mode), 0o600)
            self.assertEqual(stat.S_IMODE(password_file.stat().st_mode), 0o600)
            password = password_file.read_text().strip()
            secrets_seen.append(password)
            self.assertNotIn(password, argv)
            if len(captured) == 1:
                receipt = Path(args[args.index('--receipt') + 1])
                receipt.write_text(json.dumps({'schema_version': 'test'}) + '\n')
                return SimpleNamespace(returncode=0, stdout='', stderr='')
            return SimpleNamespace(returncode=3, stdout='', stderr='')

        with tempfile.TemporaryDirectory() as directory:
            round_dir = Path(directory)
            with mock.patch('phase18_capacity.run', side_effect=fake_run):
                receipt = generate_recipe(
                    Path('/private/recipe'),
                    {
                        'version': '2.0.1', 'revision': 'a' * 40,
                        'manifest_sha256': 'sha256:' + 'b' * 64,
                    },
                    {'MYSQL_USER': 'gopulse', 'MYSQL_PASSWORD': 'db-secret-value', 'MYSQL_DATABASE': 'gopulse'},
                    round_dir,
                    18307,
                )
        self.assertEqual(receipt, {'schema_version': 'test'})
        self.assertEqual(len(captured), 2)
        self.assertGreaterEqual(len(secrets_seen[0]), 32)
        self.assertEqual(secrets_seen[0], secrets_seen[1])

    def test_round_binding_records_corpus_and_load_version_before_load(self):
        with tempfile.TemporaryDirectory() as directory:
            round_dir = Path(directory)
            (round_dir / 'corpus.json').write_text('{"seed":18002005}\n')
            (round_dir / 'recipe').write_text('recipe-binary')
            (round_dir / 'load').write_text('load-binary')
            binding = {
                'version': '2.0.1', 'revision': 'a' * 40,
                'manifest_sha256': 'sha256:' + 'b' * 64,
                'bundle_sha256': 'sha256:' + 'c' * 64,
                'image_digests': {'backend': 'sha256:' + 'd' * 64},
            }
            receipt = {'digest': 'sha256:' + 'e' * 64}
            with mock.patch('phase18_capacity.repository_commit', return_value='f' * 40):
                document = write_round_binding(
                    round_dir, 1, '1' * 64, binding, round_dir / 'recipe', round_dir / 'load', receipt,
                )
            self.assertEqual(document['corpus']['sha256'], 'sha256:' + hashlib.sha256(b'{"seed":18002005}\n').hexdigest())
            self.assertEqual(document['load']['source_commit'], 'f' * 40)
            self.assertEqual(document['load']['binary_sha256'], 'sha256:' + hashlib.sha256(b'load-binary').hexdigest())
            self.assertEqual(json.loads((round_dir / 'load-binding.json').read_text()), document)

    def test_repeatability_failure_writes_complete_failure_evidence_without_capacity(self):
        host = {
            'platform': 'linux/amd64', 'host_os': 'Linux', 'kernel': '6.6-microsoft-standard-WSL2', 'cpu_count': 8,
            'memory_bytes': 12 * 1024 ** 3, 'swap_total_bytes': 16 * 1024 ** 3,
            'disk_available_bytes': 80 * 1024 ** 3, 'docker_server_os': 'linux',
            'docker_server_arch': 'amd64', 'docker_server_version': '1', 'compose_version': '2',
            'active_compose_projects': [],
        }
        binding = {
            'version': '2.0.1', 'revision': 'a' * 40,
            'manifest_sha256': 'sha256:' + 'b' * 64,
            'bundle_sha256': 'sha256:' + 'c' * 64,
            'image_digests': {'backend': 'sha256:' + 'd' * 64},
        }
        manifest = {'version': '2.0.1', 'revision': 'a' * 40, 'compose': {'path': 'deploy/product/compose.yaml'}}
        inspected = {
            'schema_version': 'gopulse.phase18.recipe.v1', 'seed': 18002005,
            'counts': {'posts': 50000}, 'id_ranges': {'posts': {'first': 1, 'last': 50000}},
            'digest': 'sha256:' + 'e' * 64,
        }
        results = [
            {'id': index, 'evidence': {'load_source_commit': 'f' * 40}}
            for index in range(1, 4)
        ]
        repeated = {'rps_percent': 6.0, 'p95_percent': 4.0, 'p99_percent': 3.0, 'rates_rps': [150, 160, 150]}
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            with mock.patch('phase18_capacity.run_preflight', return_value={'host': host, 'problems': []}), \
                 mock.patch('phase18_capacity.candidate_binding', return_value=(manifest, binding)), \
                 mock.patch('phase18_capacity.prepare_workspace', return_value=None), \
                 mock.patch('phase18_capacity.build_loadtest', return_value=(Path('/recipe'), Path('/load'))), \
                 mock.patch('phase18_capacity.inspect_recipe', side_effect=[inspected, inspected]), \
                 mock.patch('phase18_capacity.run_round', side_effect=[(item, str(item['id']) * 64) for item in results]), \
                 mock.patch('phase18_capacity.repeatability', return_value=repeated):
                with self.assertRaisesRegex(RuntimeError, 'repeatability'):
                    run_capacity(Path('/release-manifest.json'), 3, work)
            failure = json.loads((work / 'evidence' / 'capacity-failure.json').read_text())
            self.assertEqual(failure['schema'], 'gopulse.phase18.capacity-failure.v1')
            self.assertEqual(failure['execution_status'], 'failed')
            self.assertFalse(failure['complete'])
            self.assertEqual(failure['rounds'], results)
            self.assertEqual(failure['failure']['reason_code'], 'repeatability_gate_failed')
            self.assertFalse((work / 'evidence' / 'capacity.json').exists())


if __name__ == '__main__':
    unittest.main()
