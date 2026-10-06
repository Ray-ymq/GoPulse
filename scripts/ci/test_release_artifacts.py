import json
import unittest
from unittest.mock import patch

from release_artifacts import COMPOSE_IMAGE_ALIASES, product_compose
from release_manifest import PRODUCTS, SOURCES, PLATFORMS


def image(name):
    return {
        'ref': 'registry.example/gopulse/' + name + '@sha256:' + '1' * 64,
        'platforms': dict(zip(PLATFORMS, ['sha256:' + '2' * 64, 'sha256:' + '3' * 64])),
    }


def candidate():
    return {
        'images': {name: image(name) for name in PRODUCTS},
        'third_party': {name: image(name) for name in SOURCES},
    }


def compose_services(extra=None):
    names = [
        'backend', 'backend-2', 'platform-api', 'migrate', 'search-init', 'admin-role',
        'business-worker', 'business-worker-2', 'search-indexer', 'search-indexer-2',
        'router', 'router-2', 'marshaller', 'marshaller-2', 'monitor', 'redis-exporter',
        'frontend', 'admin-frontend', 'redis', 'mysql', 'rabbitmq', 'kafka', 'kafka-init',
        'elasticsearch', 'observability-elasticsearch', 'victoriametrics',
    ]
    services = {name: {'build': {'context': '..'}, 'ports': ['1:1']} for name in names}
    services['acceptance'] = {'image': 'acceptance'}
    if extra:
        services.update(extra)
    return services


class ReleaseArtifactsTest(unittest.TestCase):
    def test_service_mapping_closes_current_compose(self):
        document = {'services': compose_services()}
        with patch('release_artifacts.run', return_value=json.dumps(document)):
            rendered = json.loads(product_compose(candidate()))

        self.assertIn('edge', rendered['services'])
        for name, service in rendered['services'].items():
            if name == 'edge':
                continue
            self.assertTrue(service['image'].startswith('registry.example/gopulse/'))
            self.assertEqual(service['pull_policy'], 'always')
        self.assertEqual(
            rendered['services']['business-worker-2']['image'],
            rendered['services']['business-worker']['image'],
        )
        self.assertEqual(
            rendered['services']['observability-elasticsearch']['image'],
            rendered['services']['elasticsearch']['image'],
        )

    def test_unknown_service_is_rejected(self):
        document = {'services': compose_services({'future-service': {}})}
        with patch('release_artifacts.run', return_value=json.dumps(document)):
            with self.assertRaisesRegex(ValueError, 'unmapped product service: future-service'):
                product_compose(candidate())

    def test_aliases_cover_each_new_split_service(self):
        expected = {
            'backend-2': 'backend',
            'platform-api': 'backend',
            'business-worker-2': 'business-worker',
            'search-indexer-2': 'search-indexer',
            'router-2': 'router',
            'marshaller-2': 'marshaller',
            'observability-elasticsearch': 'elasticsearch',
        }
        self.assertEqual({key: COMPOSE_IMAGE_ALIASES[key] for key in expected}, expected)
