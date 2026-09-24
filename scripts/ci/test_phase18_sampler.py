import unittest

from phase18_sampler import LINK_METRICS_SCRIPTS, metric_sum, metric_value, parse_cpu_stat, parse_meminfo, parse_ratio, parse_size, summarize


class SamplerTest(unittest.TestCase):
    def test_link_metrics_use_the_private_component_boundary(self):
        # Metrics are served only on the private per-process listener; the
        # application ports return 404 and would silently zero every link sample.
        expected = {
            'backend': 19101, 'business-worker': 19102, 'search-indexer': 19103,
            'router': 19105, 'marshaller': 19106,
        }
        self.assertEqual(set(LINK_METRICS_SCRIPTS), set(expected))
        for service, port in expected.items():
            script = LINK_METRICS_SCRIPTS[service]
            self.assertIn('http://127.0.0.1:%d/internal/v1/metrics' % port, script)
            self.assertIn('Authorization: Bearer $', script)

    def test_parse_host_facts(self):
        memory = parse_meminfo("MemTotal:       12582912 kB\nSwapTotal:       8388608 kB\nSwapFree:        8388608 kB\n")
        self.assertEqual(memory["MemTotal"], 12 * 1024 ** 3)
        cpu = parse_cpu_stat("cpu  100 0 50 850 0 0 0 0 0 0\n")
        self.assertEqual(cpu, {"total": 1000, "idle": 850})

    def test_parse_docker_values(self):
        self.assertEqual(parse_size("1.5GiB / 2GiB"), int(1.5 * 1024 ** 3))
        self.assertEqual(parse_ratio("123.50%"), 123.5)

    def test_metric_value_accepts_labels(self):
        text = "gopulse_backend_outbox_pending 4\n# TYPE x\n"
        self.assertEqual(metric_value(text, "gopulse_backend_outbox_pending"), 4)

    def test_metric_sum_filters_exact_label_set(self):
        text = (
            'gopulse_marshaller_records_total{type="logs",message_source="backend",stage="store",result="stored"} 4\n'
            'gopulse_marshaller_records_total{type="events",message_source="monitor",stage="store",result="stored"} 3\n'
            'gopulse_marshaller_records_total{type="logs",message_source="backend",stage="consume",result="consumed"} 9\n'
        )
        self.assertEqual(metric_sum(text, "gopulse_marshaller_records_total", {
            "type": "logs", "stage": "store", "result": "stored",
        }), 4)

    def test_summary_uses_swap_and_process_peaks(self):
        records = [
            {"host": {"swap_free_bytes": 8 * 1024 ** 3}, "load_process": {"rss_bytes": 10}, "containers": [{"cpu_percent": 50}], "oom_killed": 0, "restart_count": 0},
            {"host": {"swap_free_bytes": 8 * 1024 ** 3 - 2 * 1024 ** 2}, "load_process": {"rss_bytes": 20}, "containers": [{"cpu_percent": 80}], "oom_killed": False, "restart_count": 0},
        ]
        summary = summarize(records)
        self.assertEqual(summary["max_swap_delta_bytes"], 2 * 1024 ** 2)
        self.assertEqual(summary["load_process_peak_rss_bytes"], 20)
        self.assertEqual(summary["peak_container_cpu_percent"], 80)

    def test_summary_aggregates_oom_and_restarts_across_samples(self):
        records = [
            {"host": {"swap_free_bytes": 1}, "load_process": None, "containers": [], "oom_killed": 1, "restart_count": 2},
            {"host": {"swap_free_bytes": 1}, "load_process": None, "containers": [], "oom_killed": 0, "restart_count": 1},
        ]
        summary = summarize(records)
        self.assertEqual(summary["oom_killed"], 1)
        self.assertEqual(summary["restart_count"], 2)


if __name__ == "__main__":
    unittest.main()
