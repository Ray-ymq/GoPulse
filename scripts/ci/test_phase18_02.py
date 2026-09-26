import unittest

from phase18_02 import evaluate_window, parse_outbox_state, sample_outbox

BASE = 1790380800


def sample(at, pending, oldest):
    return {
        "observed_at": at,
        "mysql": {"status": {"pending": {"count": pending}, "leased": {"count": 0}}},
        "links": {"backend": {"gopulse_backend_outbox_oldest_age_seconds": oldest}},
    }


def report():
    return {
        "started_at": "2026-09-26T00:00:00Z",
        "phases": [
            {
                "name": "warmup",
                "duration_seconds": 60,
                "counts": {"requests": 60, "succeeded": 60, "explicit_rejects": 0, "timeouts": 0, "errors": 0},
                "scheduled_slots": 60,
                "dropped_slots": 0,
            },
            {
                "name": "steady",
                "duration_seconds": 300,
                "counts": {"requests": 300, "succeeded": 300, "explicit_rejects": 0, "timeouts": 0, "errors": 0},
                "scheduled_slots": 300,
                "dropped_slots": 0,
            },
            {
                "name": "burst",
                "duration_seconds": 60,
                "counts": {"requests": 300, "succeeded": 290, "explicit_rejects": 10, "timeouts": 0, "errors": 0},
                "scheduled_slots": 300,
                "dropped_slots": 0,
            },
        ],
        "routes": {},
    }


class Phase1802EvaluationTest(unittest.TestCase):
    def test_rejects_duplicate_event_id_across_outbox_statuses(self):
        result = parse_outbox_state(
            [
                "status\tpublished\t1\t1\t0",
                "status\tpending\t1\t1\t1",
                "total\t2\t1\t1",
            ]
        )
        self.assertEqual(result["duplicate_event_ids"], 1)
        self.assertFalse(result["state_check_passed"])

    def test_accepts_bounded_pending_and_recovery(self):
        records = [
            sample(BASE + 60, 5, 10),
            sample(BASE + 240, 6, 20),
            sample(BASE + 350, 15, 30),
            sample(BASE + 365, 15, 0),
            sample(BASE + 500, 4, 0),
        ]
        result = evaluate_window(
            report(),
            records,
            {"oom_killed": 0, "max_swap_delta_bytes": 0},
            {"state_check_passed": True, "total": 10, "unique_event_ids": 10},
            10,
            BASE + 480,
        )
        self.assertTrue(result["passed"], result)

    def test_rejects_steady_pending_growth_above_claim_batch(self):
        records = [
            sample(BASE + 60, 5, 10),
            sample(BASE + 240, 6, 20),
            sample(BASE + 350, 16, 30),
            sample(BASE + 365, 16, 0),
            sample(BASE + 500, 4, 0),
        ]
        result = evaluate_window(
            report(),
            records,
            {"oom_killed": 0, "max_swap_delta_bytes": 0},
            {"state_check_passed": True, "total": 10, "unique_event_ids": 10},
            10,
            BASE + 480,
        )
        self.assertFalse(result["checks"]["steady_pending_bound"])
        self.assertFalse(result["passed"])

    def test_uses_backend_metric_sample_when_mysql_status_is_unavailable(self):
        result = sample_outbox({
            "observed_at": BASE,
            "mysql": {},
            "links": {"backend": {
                "gopulse_backend_outbox_pending": 7,
                "gopulse_backend_outbox_oldest_age_seconds": 2.5,
            }},
        })
        self.assertEqual(result, {"observed_at": BASE, "pending": 7.0, "oldest_age_seconds": 2.5})


if __name__ == "__main__":
    unittest.main()
