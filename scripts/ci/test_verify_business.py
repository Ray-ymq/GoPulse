from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / "scripts" / "verify-business.sh"
LIFECYCLE_SCRIPTS = tuple(REPO / "scripts" / name for name in ("dev.sh", "down.sh", "verify.sh", "verify-business.sh"))


class VerifyBusinessSafetyTests(unittest.TestCase):
    def test_self_test_rejects_unsafe_targets_without_touching_docker(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "docker-called"
            fake_docker = Path(directory) / "docker"
            fake_docker.write_text(f"#!/usr/bin/env bash\ntouch {marker!s}\nexit 99\n", encoding="utf-8")
            fake_docker.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = f"{directory}:{environment['PATH']}"
            result = subprocess.run(
                ["bash", str(SCRIPT), "--self-test"],
                cwd=REPO,
                env=environment,
                text=True,
                capture_output=True,
                check=False,
            )
            docker_was_called = marker.exists()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("6 unsafe targets rejected", result.stdout)
        self.assertFalse(docker_was_called, "safety tests must reject targets before Docker access")

    def test_cleanup_and_destructive_actions_are_scoped(self) -> None:
        source = SCRIPT.read_text(encoding="utf-8")
        self.assertIn("^gopulse-acceptance-[a-f0-9]{12}$", source)
        self.assertIn("^gopulse_acceptance_[a-f0-9]{12}$", source)
        self.assertIn("verify_service_ownership redis 6379", source)
        self.assertIn("verify_service_ownership elasticsearch 9200", source)
        self.assertIn("KAFKA_PORT=$KAFKA_PORT", source)
        self.assertIn("compose up --detach mysql redis rabbitmq elasticsearch", source)
        self.assertIn("Kafka must remain stopped during business isolation acceptance", source)
        self.assertNotIn("compose up --detach\n", source)
        self.assertIn("assert_project_absent", source)
        self.assertLess(source.index("RESOURCES_STARTED=1"), source.index("compose up --detach"))
        self.assertIn("compose down --volumes --remove-orphans", source)
        self.assertIn("trap cleanup EXIT", source)
        self.assertIn("trap 'on_signal 130' INT", source)
        self.assertNotIn("docker volume prune", source)
        self.assertNotIn("docker system prune", source)
        self.assertNotIn("redis-cli FLUSHALL", source)

    def test_search_rebuild_acceptance_is_scoped_and_browser_backed(self) -> None:
        source = SCRIPT.read_text(encoding="utf-8")
        compose = (REPO / "deploy" / "compose.yaml").read_text(encoding="utf-8")
        self.assertIn("--search-rebuild", source)
        self.assertIn("npm run test:e2e -- --grep search-rebuild", source)
        self.assertIn("--search-live", source)
        self.assertIn("npm run test:e2e -- --grep search-live", source)
        full_flow = source[source.index("  run_api_flow\n", source.index("main()")) :]
        self.assertLess(full_flow.index("run_search_rebuild_flow"), full_flow.index("run_search_live_flow"))
        self.assertLess(full_flow.index("run_search_live_flow"), full_flow.index("run_reliability_matrix"))
        self.assertIn("^gopulse-post-search-v1-[a-z0-9-]+$", source)
        self.assertIn('es_request DELETE "/$active_index" 200', source)
        self.assertIn('es_request HEAD "/$unrelated_index" 200', source)
        self.assertIn("docker.elastic.co/elasticsearch/elasticsearch:9.5.2", compose)
        self.assertIn('"127.0.0.1:${ELASTICSEARCH_PORT:?ELASTICSEARCH_PORT is required}:9200"', compose)
        self.assertIn("wait_for_status=yellow", compose)
        self.assertIn("elasticsearch_data:/usr/share/elasticsearch/data", compose)

    def test_logging_live_acceptance_captures_safe_correlated_json(self) -> None:
        source = SCRIPT.read_text(encoding="utf-8")
        self.assertIn("--logging-live", source)
        self.assertIn("X-Request-ID", source)
        self.assertIn("http request completed", source)
        self.assertIn("log_schema_version", source)
        self.assertIn("sensitive sentinel leaked into backend log", source)
        self.assertIn("post detail cache read failed", source)
        self.assertIn("run_logging_live_flow", source)

    def test_lifecycle_scripts_are_executable_lf_and_valid_bash(self) -> None:
        for script in LIFECYCLE_SCRIPTS:
            with self.subTest(script=script.name):
                self.assertTrue(script.stat().st_mode & 0o111, f"{script.name} must remain executable")
                self.assertNotIn(b"\r\n", script.read_bytes())
                subprocess.run(["bash", "-n", str(script)], cwd=REPO, check=True)

    def test_worker_lifecycle_uses_repository_owned_records(self) -> None:
        dev = (REPO / "scripts" / "dev.sh").read_text(encoding="utf-8")
        down = (REPO / "scripts" / "down.sh").read_text(encoding="utf-8")
        verify = (REPO / "scripts" / "verify.sh").read_text(encoding="utf-8")
        for source in (dev, down, verify):
            self.assertIn("business-worker.json", source)
            self.assertIn("gopulse-business-worker", source)
            self.assertIn("search-indexer.json", source)
            self.assertIn("gopulse-search-indexer", source)
        self.assertIn('write_process_record "$WORKER_PID" "$WORKER_RECORD"', dev)
        self.assertIn('write_process_record "$SEARCH_INDEXER_PID" "$SEARCH_INDEXER_RECORD"', dev)
        config_keys = dev[dev.index("ALL_CONFIG_KEYS=(") : dev.index("REQUIRED_KEYS=(")]
        self.assertIn("ELASTICSEARCH_PORT", config_keys)
        self.assertIn('stop_recorded_application "Business Worker" "$WORKER_RECORD"', down)
        self.assertIn('stop_recorded_application "Search Indexer" "$SEARCH_INDEXER_RECORD"', down)
        self.assertIn("DEFAULTS=([ELASTICSEARCH_PORT]=9200 [KAFKA_PORT]=9092)", down)
        self.assertIn('export "$key=${DEFAULTS[$key]}"', down)
        self.assertNotIn("local key=$1 fallback=$2 direct=${!key:-}", verify)
        self.assertIn("direct=${!key:-}", verify)
        self.assertNotIn("marker=${5:-$binary} result", verify)
        self.assertIn("marker=${5:-$binary}", verify)
        self.assertIn("actual_start_ticks != start_ticks", verify)
        self.assertIn("actual_executable != executable", verify)
        self.assertNotIn('rm -f "$WORKER_RECORD"', verify)

    def test_fault_injection_validates_owned_targets_first(self) -> None:
        source = SCRIPT.read_text(encoding="utf-8")
        for service, container_port in (("mysql", "3306"), ("redis", "6379"), ("rabbitmq", "5672"), ("elasticsearch", "9200")):
            self.assertIn(f"verify_service_ownership {service} {container_port}", source)
        self.assertIn('validate_process_ownership "$WORKER_PID"', source)
        self.assertLess(
            source.index('validate_process_ownership "$WORKER_PID"'),
            source.index('kill -STOP -- "-$WORKER_PID"'),
        )


if __name__ == "__main__":
    unittest.main()
