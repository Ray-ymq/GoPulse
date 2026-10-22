import copy
import unittest
from unittest.mock import patch
from flow.environment import snapshot
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

    def test_effective_buffer_heap_budget_and_platform_role(self):
        rows = self.rows()
        for name, row in rows.items():
            row['Config']['Env'] = ['MYSQL_MAX_OPEN_CONNS=4', 'MYSQL_TOTAL_MAX_OPEN_CONNS=60']
            if name in ('backend', 'backend-2'):
                row['Config']['Env'] += ['REDIS_HOST=redis', 'BACKEND_REDIS_DISABLED=false']
        config = load()
        for buffer_pool, heap, valid in [(2*1024**3, 1024**3, True), (1, 1024**3, False), (2*1024**3, 1, False)]:
            with self.subTest(buffer_pool=buffer_pool, heap=heap), \
                 patch('flow.environment.containers', return_value=rows), \
                 patch('flow.environment.sql', side_effect=lambda query, **kwargs: str(buffer_pool) if 'innodb_buffer_pool_size' in query else 'identity'), \
                 patch('flow.environment.es', return_value={'nodes':{'one':{'jvm':{'mem':{'heap_max_in_bytes':heap, 'heap_init_in_bytes':heap}}}}}), \
                 patch('flow.environment.command', return_value='sha256:fixture'):
                if valid:
                    self.assertIn('platform-api', snapshot(config)['application_settings'])
                else:
                    with self.assertRaises(ValueError): snapshot(config)
