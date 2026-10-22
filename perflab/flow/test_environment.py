import copy
import unittest
from flow.config import load
from flow.environment import inspect_rows


class EnvironmentTests(unittest.TestCase):
    def rows(self):
        return {name: {'Id': name, 'Image': 'image', 'RestartCount': 0,
                       'State': {'Running': True, 'StartedAt': 'start', 'Health': {'Status': 'healthy'}},
                       'Config': {'Env': []},
                       'HostConfig': {'NanoCpus': int(row['cpus'] * 1e9), 'Memory': row['memory_mib'] * 1024**2,
                                      'MemorySwap': row['memory_mib'] * 1024**2, 'RestartPolicy': {'Name': 'no'}}}
                for name, row in load()['resources']['services'].items() if name != 'k6'}

    def test_resources(self):
        rows = self.rows()
        inspect_rows(rows, load()['resources'])
        for key, bad in [('NanoCpus', 1), ('Memory', 1), ('MemorySwap', -1), ('RestartPolicy', {'Name': 'always'})]:
            changed = copy.deepcopy(rows)
            changed['mysql']['HostConfig'][key] = bad
            with self.subTest(key=key), self.assertRaises(ValueError):
                inspect_rows(changed, load()['resources'])
        for change in ['missing', 'extra', 'restart', 'oom']:
            changed = copy.deepcopy(rows)
            if change == 'missing': del changed['backend-2']
            if change == 'extra': changed['unknown'] = rows['backend']
            if change == 'restart': changed['mysql']['RestartCount'] = 1
            if change == 'oom': changed['mysql']['State']['OOMKilled'] = True
            with self.subTest(change=change), self.assertRaises(ValueError):
                inspect_rows(changed, load()['resources'])
