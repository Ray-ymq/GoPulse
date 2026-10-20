"""用离线样本验证故障判据，不干预真实数据库。"""
from datetime import datetime,timezone,timedelta
from pathlib import Path
import tempfile
import unittest
from observe import Observation

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
