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

    def test_contract(self):
        with self.assertRaises(ValueError):
            validate('task', contract('task'))
