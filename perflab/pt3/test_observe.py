"""用离线样本验证故障判据，不干预真实数据库。"""
from datetime import datetime,timezone,timedelta
from pathlib import Path
import tempfile
import unittest
from observe import Observation
from environment import api_pool_override

class ApiPoolOverrideTest(unittest.TestCase):
    def test_absent_override_keeps_baseline(self):
        self.assertIsNone(api_pool_override({'MYSQL_TOTAL_MAX_OPEN_CONNS':'60'}))
    def test_doubling_within_budget_is_allowed(self):
        self.assertEqual(api_pool_override({'MYSQL_TOTAL_MAX_OPEN_CONNS':'60','PT3_API_MYSQL_MAX_OPEN_CONNS':'16'}),'16')
    def test_override_above_total_budget_is_rejected(self):
        with self.assertRaises(RuntimeError):
            api_pool_override({'MYSQL_TOTAL_MAX_OPEN_CONNS':'30','PT3_API_MYSQL_MAX_OPEN_CONNS':'40'})
    def test_invalid_override_is_rejected(self):
        with self.assertRaises(RuntimeError):
            api_pool_override({'MYSQL_TOTAL_MAX_OPEN_CONNS':'60','PT3_API_MYSQL_MAX_OPEN_CONNS':'invalid'})

class DatabaseEvidenceTest(unittest.TestCase):
    def sample(self,observer,second,ok):
        observer.database_sample({'timestamp':(self.start+timedelta(seconds=second)).isoformat(),'ok':ok},False)
    def setUp(self):self.start=datetime.now(timezone.utc)
    def test_30_seconds_without_success_is_required(self):
        with tempfile.TemporaryDirectory() as directory:
            observer=Observation(Path(directory))
            for second in range(30):self.sample(observer,second,False)
            self.assertIsNone(observer.reason)
            self.sample(observer,30,False)
            self.assertEqual(observer.reason,'database_continuously_unavailable_30s')
    def test_success_resets_the_failure_interval(self):
        with tempfile.TemporaryDirectory() as directory:
            observer=Observation(Path(directory))
            for second in range(50):self.sample(observer,second,second==25)
            self.assertIsNone(observer.reason)
    def test_sampling_gap_is_not_database_outage_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            observer=Observation(Path(directory))
            self.sample(observer,0,False);self.sample(observer,31,False)
            self.assertEqual(observer.reason,'database_probe_sampling_gap')

if __name__=='__main__':unittest.main()
