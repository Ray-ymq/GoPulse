import json
import stat
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from phase19_sampler import Sampler, load_samples, summarize, summarize_samples, validate_sample_intervals


class SamplerTest(unittest.TestCase):
    def test_private_link_metrics_are_kept_separate_from_sut_resources(self):
        with tempfile.TemporaryDirectory() as directory:
            raw = Path(directory) / "resources.raw.jsonl"
            sampler = Sampler("gopulse-p19-aaaaaaaaaaaa", Path("compose.yaml"), Path("env"), interval=3600, raw_path=raw)
            with mock.patch.object(sampler, "_containers", return_value=([], 0, 0)), \
                mock.patch.object(sampler, "_links", return_value={"backend": {"gopulse_backend_http_rejected_total": 2}}), \
                mock.patch.object(sampler, "_rabbitmq", return_value={"ready": 0, "unacked": 0}), \
                mock.patch.object(sampler, "_mysql", return_value={}), \
                mock.patch.object(sampler, "_kafka_lag", return_value={"lag": 0}):
                sampler.start()
                sampler.stop()
            records = load_samples(raw, expected_interval=3600)
            self.assertEqual(len(records), 2)
            self.assertIn("load_process", records[0])
            self.assertIn("sut", records[0])
            self.assertEqual(records[0]["sut"]["saturation"]["backend"]["gopulse_backend_http_rejected_total"], 2)
            self.assertEqual(stat.S_IMODE(raw.stat().st_mode), 0o600)

    def test_interval_and_sequence_drift_is_rejected(self):
        records = [
            {"schema": "gopulse.phase19.resources.v1", "sequence": 0, "observed_at": 1, "interval_seconds": 5},
            {"schema": "gopulse.phase19.resources.v1", "sequence": 2, "observed_at": 2, "interval_seconds": 5},
        ]
        with self.assertRaisesRegex(ValueError, "sequence"):
            validate_sample_intervals(records, 5)

    def test_boundary_samples_do_not_change_scheduled_interval_validation(self):
        records = [
            {"schema": "gopulse.phase19.resources.v1", "sequence": 0, "observed_at": 1, "interval_seconds": 5, "sample_kind": "initial"},
            {"schema": "gopulse.phase19.resources.v1", "sequence": 1, "observed_at": 1.1, "interval_seconds": 5, "sample_kind": "boundary"},
            {"schema": "gopulse.phase19.resources.v1", "sequence": 2, "observed_at": 6, "interval_seconds": 5, "sample_kind": "scheduled"},
            {"schema": "gopulse.phase19.resources.v1", "sequence": 3, "observed_at": 6.1, "interval_seconds": 5, "sample_kind": "boundary"},
            {"schema": "gopulse.phase19.resources.v1", "sequence": 4, "observed_at": 11, "interval_seconds": 5, "sample_kind": "scheduled"},
        ]
        validate_sample_intervals(records, 5)

    def test_scheduled_deadlines_allow_slow_resource_collection(self):
        records = [
            {"schema": "gopulse.phase19.resources.v1", "sequence": 0, "observed_at": 1, "interval_seconds": 5, "sample_kind": "initial"},
            {"schema": "gopulse.phase19.resources.v1", "sequence": 1, "observed_at": 10, "interval_seconds": 5, "sample_kind": "scheduled", "scheduled_at": 100},
            {"schema": "gopulse.phase19.resources.v1", "sequence": 2, "observed_at": 20, "interval_seconds": 5, "sample_kind": "scheduled", "scheduled_at": 105},
        ]
        validate_sample_intervals(records, 5)

    def test_summary_reports_load_and_sut_peaks_independently(self):
        records = [
            {
                "schema": "gopulse.phase19.resources.v1", "sequence": 0, "observed_at": 1, "interval_seconds": 5,
                "host": {"swap_free_bytes": 100}, "load_process": {"rss_bytes": 10, "cpu_ticks": 2, "scheduler_lag_ms": 1},
                "sut": {"rss_bytes": 20, "cpu_percent": 30}, "containers": [{"cpu_percent": 40}], "oom_killed": 0, "restart_count": 0,
                "links": {"backend": {"gopulse_backend_outbox_pending": 3}}, "rabbitmq": {"ready": 1, "unacked": 2}, "kafka_lag": {"lag": 4},
            },
            {
                "schema": "gopulse.phase19.resources.v1", "sequence": 1, "observed_at": 6, "interval_seconds": 5,
                "host": {"swap_free_bytes": 90}, "load_process": {"rss_bytes": 11, "cpu_ticks": 3, "scheduler_lag_ms": 2},
                "sut": {"rss_bytes": 21, "cpu_percent": 31}, "containers": [{"cpu_percent": 50}], "oom_killed": 0, "restart_count": 1,
                "links": {"backend": {"gopulse_backend_outbox_pending": 4}}, "rabbitmq": {"ready": 2, "unacked": 3}, "kafka_lag": {"lag": 5},
            },
        ]
        result = summarize(records)
        self.assertEqual(result["load_process_peak_rss_bytes"], 11)
        self.assertEqual(result["sut_peak_rss_bytes"], 21)
        self.assertEqual(result["peak_container_cpu_percent"], 50)
        self.assertEqual(result["max_outbox_pending"], 4)
        self.assertEqual(result["max_kafka_lag"], 5)

    def test_summary_failure_does_not_replace_raw_samples(self):
        with tempfile.TemporaryDirectory() as directory:
            raw = Path(directory) / "raw.jsonl"
            output = Path(directory) / "summary.json"
            raw.write_text(json.dumps({"schema": "gopulse.phase19.resources.v1", "sequence": 0, "observed_at": 1, "interval_seconds": 5}) + "\n")
            with mock.patch("phase19_sampler.summarize", side_effect=RuntimeError("summary failed")):
                with self.assertRaisesRegex(RuntimeError, "summary failed"):
                    summarize_samples(raw, output)
            self.assertTrue(raw.exists())
            self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
