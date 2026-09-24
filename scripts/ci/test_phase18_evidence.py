import copy
import unittest

from phase18_evidence import (
    CATEGORIES,
    EXPECTED_COUNTS,
    EXPECTED_RANGES,
    RECIPE_SCHEMA,
    evaluate_slo,
    repeatability,
    validate_capacity,
)


def counts(requests, succeeded=None, explicit=0, timeouts=0, errors=0):
    if succeeded is None:
        succeeded = requests - explicit - timeouts - errors
    return {
        "requests": requests,
        "succeeded": succeeded,
        "explicit_rejects": explicit,
        "timeouts": timeouts,
        "errors": errors,
    }


def category_counts(request_count):
    values = {}
    base = request_count // len(CATEGORIES)
    remainder = request_count % len(CATEGORIES)
    for index, category in enumerate(sorted(CATEGORIES)):
        values[category] = counts(base + (index < remainder))
    return values


def latency(value=10):
    return {"p50_ms": value / 2, "p95_ms": value, "p99_ms": value * 1.1, "max_ms": value * 1.2}


def phase(name, duration, rate, requests=60):
    category = category_counts(requests)
    return {
        "name": name,
        "target_rps": rate,
        "duration_seconds": duration,
        "scheduled_slots": requests,
        "dropped_slots": 0,
        "max_schedule_lag_ms": 2,
        "completed_requests": requests,
        "counts": counts(requests),
        "latency_by_category": {key: latency() for key in CATEGORIES},
        "counts_by_category": category,
    }


def load_report(multiplier=1):
    phases = [
        phase("warmup", 300, 150, 60),
        phase("steady", 900, 150, 60 * multiplier),
        phase("burst", 120, 300, 60 * multiplier),
    ]
    total_requests = sum(item["completed_requests"] for item in phases)
    route_counts = counts(total_requests)
    return {
        "schema_version": "gopulse.phase18.load.v1",
        "seed": 18002005,
        "started_at": "2026-09-22T00:00:00+00:00",
        "finished_at": "2026-09-22T00:22:00+00:00",
        "steady_target_rps": 150,
        "burst_target_rps": 300,
        "virtual_users": 1024,
        "phases": phases,
        "routes": {
            "GET /api/v1/posts": {
                "category": "read", "method": "GET", "template": "GET /api/v1/posts",
                "counts": route_counts, "statuses": {"200": total_requests}, "latency": latency(),
            }
        },
        "total": counts(total_requests),
        "load_process": {"rss_bytes": 100 * 1024 ** 2, "goroutines": 1040, "heap_alloc_bytes": 50 * 1024 ** 2},
    }


def capacity():
    reports = [load_report(1), load_report(1), load_report(1)]
    rounds = []
    digest = "sha256:" + "a" * 64
    for index, report in enumerate(reports, 1):
        rounds.append({
            "id": index,
            "project_sha256": str(index) * 64,
            "recipe_receipt": {
                "schema_version": RECIPE_SCHEMA, "seed": 18002005,
                "candidate": {"version": "2.0.1", "revision": "b" * 40, "manifest_sha256": "sha256:" + "c" * 64},
                "counts": EXPECTED_COUNTS, "id_ranges": EXPECTED_RANGES, "digest": digest,
                "generated_at": "2026-09-22T00:00:00+00:00", "duration_ms": 1,
            },
            "load_report": report,
            "resources": {
                "samples": 264, "oom_killed": 0, "restart_count": 0,
                "max_swap_delta_bytes": 0, "load_process_peak_rss_bytes": 100 * 1024 ** 2,
                "peak_container_cpu_percent": 50.0,
                "first_bottleneck": {"component": "none", "reason_code": "none_observed", "fact": "all capacity gates passed"},
            },
            "convergence": {
                "converged": True,
                "outbox_pending": 0, "rabbit_ready": 0, "rabbit_unacked": 0,
                "search_count": 50000, "mysql_posts": 50000, "notifications": 500000, "kafka_lag": 0,
                "logs_count_before": 100, "logs_count_after": 500, "events_count_before": 10, "events_count_after": 40,
                "marshaller_store_before": {"metrics": 100, "logs": 100, "events": 10},
                "marshaller_store_after": {"metrics": 200, "logs": 500, "events": 40},
                "search_seconds": 3, "notification_seconds": 4,
                "metrics_logs_events_seconds": 5, "recovery_seconds": 2,
            },
        })
    document = {
        "schema": "gopulse.phase18.capacity.v1", "execution_status": "complete", "complete": True,
        "candidate": {
            "version": "2.0.1", "revision": "b" * 40,
            "manifest_sha256": "sha256:" + "c" * 64, "bundle_sha256": "sha256:" + "d" * 64,
            "image_digests": {"backend": "sha256:" + "e" * 64},
        },
        "host": {
            "platform": "linux/amd64", "host_os": "Linux",
            "kernel": "6.6.0-microsoft-standard-WSL2", "cpu_count": 8,
            "memory_bytes": 12 * 1024 ** 3, "swap_total_bytes": 8 * 1024 ** 3,
            "disk_available_bytes": 100 * 1024 ** 3,
            "docker_server_os": "linux", "docker_server_arch": "amd64", "docker_server_version": "1.0", "compose_version": "2.0",
            "active_compose_projects": [],
        },
        "recipe": {
            "schema_version": RECIPE_SCHEMA, "seed": 18002005,
            "counts": EXPECTED_COUNTS, "id_ranges": EXPECTED_RANGES,
            "digest": digest, "same_seed_repeat": True, "nonempty_rejection": True,
        },
        "rounds": rounds,
        "repeatability": repeatability(rounds),
        "slo": {**evaluate_slo(rounds), "first_bottleneck": None},
        "cleanup": "passed", "secret_scan": "passed",
    }
    return document


class EvidenceTest(unittest.TestCase):
    def test_valid_capacity_evidence(self):
        document = capacity()
        self.assertIs(document, validate_capacity(document))

    def test_repeatability_is_recomputed(self):
        document = capacity()
        document["repeatability"]["p95_percent"] = 99
        with self.assertRaisesRegex(ValueError, "repeatability"):
            validate_capacity(document)

    def test_secret_value_is_rejected(self):
        document = capacity()
        document["recipe"]["leak"] = "password=abcdefghijk"
        with self.assertRaisesRegex(ValueError, "sensitive"):
            validate_capacity(document)

    def test_round_recipe_mismatch_is_rejected(self):
        document = capacity()
        document["rounds"][1]["recipe_receipt"]["digest"] = "sha256:" + "f" * 64
        with self.assertRaisesRegex(ValueError, "recipe differs"):
            validate_capacity(copy.deepcopy(document))

    def test_deleted_posts_are_compared_to_authoritative_mysql_count(self):
        document = capacity()
        for round_value in document["rounds"]:
            round_value["convergence"]["mysql_posts"] = 45000
            round_value["convergence"]["search_count"] = 45000
        self.assertIs(document, validate_capacity(document))

    def test_notifications_may_exceed_the_initial_recipe(self):
        document = capacity()
        for round_value in document["rounds"]:
            round_value["convergence"]["notifications"] = 510000
        self.assertIs(document, validate_capacity(document))

    def test_missing_observability_progress_is_rejected(self):
        document = capacity()
        document["rounds"][0]["convergence"]["logs_count_after"] = document["rounds"][0]["convergence"]["logs_count_before"]
        with self.assertRaisesRegex(ValueError, "searchable logs"):
            validate_capacity(document)

    def test_swap_above_minimum_is_accepted_and_below_minimum_is_rejected(self):
        document = capacity()
        document["host"]["swap_total_bytes"] = 16 * 1024 ** 3
        self.assertIs(document, validate_capacity(document))
        document["host"]["swap_total_bytes"] = 8 * 1024 ** 3 - 1
        with self.assertRaisesRegex(ValueError, "resource contract"):
            validate_capacity(document)

    def test_competing_compose_project_is_rejected(self):
        document = capacity()
        document["host"]["active_compose_projects"] = ["gopulse-other"]
        with self.assertRaisesRegex(ValueError, "competing Compose"):
            validate_capacity(document)

    def test_bounded_non_convergence_is_recorded_as_failed_slo(self):
        document = capacity()
        convergence = document["rounds"][0]["convergence"]
        convergence.update({
            "converged": False,
            "outbox_pending": 1200, "rabbit_ready": 0, "rabbit_unacked": 0,
            "search_count": 49800, "notifications": 500100, "kafka_lag": 0,
            "search_seconds": 601, "notification_seconds": 601,
            "metrics_logs_events_seconds": 601, "recovery_seconds": 601,
        })
        bottleneck = {
            "component": "backend", "reason_code": "outbox_backlog",
            "fact": "outbox pending reached 1200 during the load window",
        }
        document["rounds"][0]["resources"]["first_bottleneck"] = bottleneck
        document["slo"] = {**evaluate_slo(document["rounds"]), "first_bottleneck": bottleneck}
        self.assertIs(document, validate_capacity(document))
        self.assertFalse(document["slo"]["checks"]["round_1_recovery"])

    def test_non_converged_round_without_incomplete_condition_is_rejected(self):
        document = capacity()
        document["rounds"][0]["convergence"]["converged"] = False
        with self.assertRaisesRegex(ValueError, "no incomplete convergence condition"):
            validate_capacity(copy.deepcopy(document))

    def test_non_converged_round_may_retain_search_backlog_without_data_loss(self):
        document = capacity()
        convergence = document["rounds"][0]["convergence"]
        convergence.update({
            "converged": False, "search_count": 49999, "search_seconds": 601,
            "recovery_seconds": 601,
        })
        document["rounds"][0]["resources"]["first_bottleneck"] = {
            "component": "search-projection", "reason_code": "search_convergence_timeout",
            "fact": "search projection retained one stale document at the recovery deadline",
        }
        document["slo"] = {**evaluate_slo(document["rounds"]), "first_bottleneck": document["rounds"][0]["resources"]["first_bottleneck"]}
        self.assertIs(document, validate_capacity(document))

    def test_dropped_slot_accounting_is_rejected_when_inconsistent(self):
        document = capacity()
        document["rounds"][0]["load_report"]["phases"][1]["dropped_slots"] = 1
        with self.assertRaisesRegex(ValueError, "scheduled, and dropped"):
            validate_capacity(document)


if __name__ == "__main__":
    unittest.main()
