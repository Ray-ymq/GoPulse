import unittest
from flow.config import load, compose_resources, contract, validate


class ConfigTests(unittest.TestCase):
    def test_start(self):
        self.assertEqual(load()['start_rps'], 10)
        self.assertEqual(load('250')['start_rps'], 250)
        self.assertEqual(load('6400')['start_rps'], 6400)
        for value in ['0', '-1', '1.5', 'abc', '6401', '１０']:
            with self.subTest(value=value), self.assertRaises(ValueError):
                load(value)

    def test_budget(self):
        r = load()['resources']
        self.assertEqual(sum(v['cpus'] for v in r['services'].values()) + 1.25, 16)
        self.assertEqual(sum(v['memory_mib'] for v in r['services'].values()) + 2048, 12288)
        for row in compose_resources(r)['services'].values():
            self.assertEqual(row['mem_limit'], row['memswap_limit'])
            self.assertEqual(row['restart'], 'no')

    def test_integer_cpu_quotas_preserve_fixed_fractional_resources(self):
        resources = load()['resources']
        overlay = compose_resources(resources)['services']
        for name, expected in resources['services'].items():
            with self.subTest(name=name):
                row = overlay[name]
                self.assertEqual(row['cpus'], 0)
                self.assertEqual(row['cpu_quota'] / row['cpu_period'], expected['cpus'])
        self.assertEqual(overlay['frontend']['cpu_quota'], 13000)
        self.assertEqual(overlay['admin-frontend']['cpu_quota'], 12000)

    def test_contract(self):
        with self.assertRaises(ValueError):
            validate('task', contract('task'))
