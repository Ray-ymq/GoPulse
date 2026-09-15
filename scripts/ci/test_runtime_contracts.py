import copy
import unittest
from pathlib import Path
from verify_runtime_contracts import ROOT, load, compose_document, validate

class RuntimeContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.contract=load(ROOT/'deploy/runtime-contracts.json')
        cls.compose=compose_document(ROOT/'deploy/compose.yaml')
        cls.env=(ROOT/'.env.example').read_text()
    def test_current_inventory(self):
        validate(self.contract,self.compose,self.env,check_version=False)
    def test_required_rejections(self):
        def secret(d):
            next(f for f in d['components'][0]['environment'] if f['sensitive'])['sensitive']=False
        def alias(d):
            d['components'][0]['environment'][0]['aliases']=[{'key':'OLD_KEY','expires':''}]
        changes=[lambda d:d['components'].pop(),lambda d:d['components'][1].update(id=d['components'][0]['id']),lambda d:d['components'][1]['listeners'][0].update(port=d['components'][0]['listeners'][0]['port']),secret,alias,lambda d:d['components'][0]['probes'].update(ready='/live'),lambda d:d['components'][0].update(stop_grace_seconds=1),lambda d:d.update(contract_version='2')]
        for change in changes:
            with self.subTest(change=change):
                d=copy.deepcopy(self.contract);change(d)
                with self.assertRaises(ValueError):validate(d,self.compose,self.env,check_version=False)
        compose=copy.deepcopy(self.compose);compose['services']['backend']['environment']['UNREGISTERED_KEY']='value'
        with self.assertRaises(ValueError):validate(self.contract,compose,self.env,check_version=False)
