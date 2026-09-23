import json
import stat
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from phase18_capacity import first_bottleneck, generate_recipe, observability_progress, run_preflight


class CapacityRunnerTest(unittest.TestCase):
    def test_preflight_workspace_uses_owner_only_lock(self):
        host = {
            'platform': 'linux/amd64', 'host_os': 'Linux', 'kernel': '6.6-microsoft-standard-WSL2', 'cpu_count': 8,
            'memory_bytes': 12 * 1024 ** 3, 'swap_total_bytes': 8 * 1024 ** 3,
            'disk_available_bytes': 100 * 1024 ** 3, 'docker_server_os': 'linux',
            'docker_server_arch': 'amd64', 'docker_server_version': '1', 'compose_version': '2',
            'active_compose_projects': [],
        }
        with tempfile.TemporaryDirectory() as directory, mock.patch('phase18_capacity.host_inventory', return_value=host):
            document = run_preflight(Path(directory))
            self.assertEqual(document['problems'], [])
            self.assertEqual(stat.S_IMODE((Path(directory) / '.lock').stat().st_mode), 0o600)

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
        convergence = {'search_seconds': 1, 'notification_seconds': 1, 'metrics_logs_events_seconds': 1, 'recovery_seconds': 1}
        records = [{'links': {'backend': {'gopulse_backend_outbox_pending': 7}}, 'rabbitmq': None, 'kafka_lag': None,
                    'containers': [], 'host': {}}]
        bottleneck = first_bottleneck(report, resources, convergence, records)
        self.assertEqual((bottleneck['component'], bottleneck['reason_code']), ('backend', 'outbox_backlog'))

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


if __name__ == '__main__':
    unittest.main()
