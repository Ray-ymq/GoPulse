import copy
import json
import unittest
from release_manifest import PRODUCTS, SOURCES, PLATFORMS, unique, validate


def fixture():
    image = {'ref': 'registry.example/gopulse@sha256:'+'1'*64,
             'platforms': dict(zip(PLATFORMS, ['sha256:'+'2'*64, 'sha256:'+'3'*64]))}
    plugins = [dict(id=s+'-exporter', version='1.13.1', os='linux', arch=a,
                    purpose='current', archive_sha256='sha256:'+'4'*64,
                    entrypoint_sha256='sha256:'+'5'*64, schema_sha256='sha256:'+'6'*64)
               for s in SOURCES for a in ('amd64', 'arm64')]
    plugins.append(dict(plugins[0], version='1.9.4', purpose='upgrade-only', schema_sha256=''))
    return dict(schema_version=1, version='1.13.1', revision='a'*40,
                compose={'path': 'deploy/product/compose.yaml', 'sha256': 'sha256:'+'7'*64},
                bundle_sha256='sha256:'+'8'*64, images={p:copy.deepcopy(image) for p in PRODUCTS},
                third_party={p:copy.deepcopy(image) for p in SOURCES}, lifecycle=copy.deepcopy(image),
                plugins=plugins, supported_upgrade_sources=[])


class ManifestTest(unittest.TestCase):
    def test_valid(self):
        validate(fixture(), '1.13.1', 'a'*40)

    def test_closed_contract(self):
        changes = [lambda m:m.update(unknown_required={}),
                   lambda m:m['images']['monitor']['platforms'].pop('linux/arm64'),
                   lambda m:m['images']['monitor'].update(ref='gopulse/monitor:latest'),
                   lambda m:m['plugins'].__setitem__(1, m['plugins'][0]),
                   lambda m:m['plugins'][0].update(version='1.13.0'),
                   lambda m:m.update(revision='bad'),
                   lambda m:m['compose'].update(sha256='bad'),
                   lambda m:m['plugins'][-1].update(arch='arm64')]
        for change in changes:
            m=fixture();change(m)
            with self.subTest(change=change), self.assertRaises(ValueError):validate(m)
        with self.assertRaises(ValueError):validate(fixture(), '1.13.2')
        with self.assertRaises(ValueError):validate(fixture(), revision='b'*40)
        with self.assertRaises(ValueError):json.loads('{"a":1,"a":2}', object_pairs_hook=unique)

    def test_amd64_only_candidate(self):
        m=fixture()
        for image in [*m['images'].values(), m['lifecycle']]:
            del image['platforms']['linux/arm64']
        m['plugins']=[p for p in m['plugins'] if p['arch']=='amd64']
        validate(m)
