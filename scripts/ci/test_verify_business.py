from __future__ import annotations

import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / "scripts" / "verify-business.sh"
OBSERVABILITY_SCRIPT = REPO / "scripts" / "verify-compose-observability.sh"
LIFECYCLE_SCRIPTS = tuple(
    REPO / "scripts" / name
    for name in (
        "dev.sh",
        "down.sh",
        "verify.sh",
        "verify-compose.sh",
        "verify-compose-observability.sh",
        "verify-business.sh",
    )
)


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

    def test_dev_rejects_unsafe_publication_before_docker_access(self) -> None:
        dev = REPO / "scripts" / "dev.sh"
        for key, value in (
            ("PUBLISHED_HOST", "0.0.0.0"),
            ("PUBLISHED_HOST", "localhost"),
            ("PUBLISHED_HOST", "::"),
            ("HTTP_PORT", "0"),
            ("FRONTEND_PORT", "8080:8081"),
        ):
            with self.subTest(key=key, value=value), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                marker_file = root / "docker-called"
                fake_docker = root / "docker"
                fake_docker.write_text(
                    f"#!/usr/bin/env bash\ntouch {marker_file!s}\nexit 99\n",
                    encoding="utf-8",
                )
                fake_docker.chmod(0o755)
                values = {
                    "PUBLISHED_HOST": "127.0.0.1",
                    "HTTP_PORT": "8080",
                    "FRONTEND_PORT": "5173",
                }
                values[key] = value
                env_file = root / "test.env"
                env_file.write_text(
                    "".join(f"{name}={item}\n" for name, item in values.items()),
                    encoding="utf-8",
                )
                environment = os.environ.copy()
                environment["PATH"] = f"{root}:{environment['PATH']}"
                result = subprocess.run(
                    ["bash", str(dev), "--env-file", str(env_file)],
                    cwd=REPO,
                    env=environment,
                    text=True,
                    capture_output=True,
                    check=False,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(marker_file.exists(), result.stderr)

    def test_dev_no_build_rejects_same_version_stale_revision_before_up(self) -> None:
        dev = REPO / "scripts" / "dev.sh"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            startup_marker = root / "startup-called"
            fake_docker = root / "docker"
            fake_docker.write_text(
                """#!/usr/bin/env bash
set -eu
if [[ ${1:-} == info ]]; then exit 0; fi
if [[ ${1:-} == compose && ${2:-} == version ]]; then exit 0; fi
if [[ ${1:-} == image && ${2:-} == inspect ]]; then
  printf '%s\n' 'sha256:stale|1.9.4|0000000000000000000000000000000000000000|https://github.com/Ray-ymq/GoPulse'
  exit 0
fi
if [[ " $* " == *' up '* ]]; then touch "${STARTUP_MARKER}"; fi
exit 99
""",
                encoding="utf-8",
            )
            fake_docker.chmod(0o755)
            env_file = root / "test.env"
            env_file.write_text(
                "PUBLISHED_HOST=127.0.0.1\nHTTP_PORT=8080\nFRONTEND_PORT=5173\n",
                encoding="utf-8",
            )
            environment = os.environ.copy()
            environment["PATH"] = f"{root}:{environment['PATH']}"
            environment["STARTUP_MARKER"] = str(startup_marker)
            result = subprocess.run(
                ["bash", str(dev), "--no-build", "--env-file", str(env_file)],
                cwd=REPO,
                env=environment,
                text=True,
                capture_output=True,
                check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match version", result.stderr)
        self.assertFalse(startup_marker.exists(), "stale images must be rejected before compose up")

    def test_full_runner_rejects_dirty_product_source_before_docker_access(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "backend").mkdir()
            (root / "deploy").mkdir()
            (root / "deploy" / "compose.yaml").write_text("services: {}\n", encoding="utf-8")
            runner = root / "scripts" / "verify-compose-observability.sh"
            runner.write_bytes(OBSERVABILITY_SCRIPT.read_bytes())
            runner.chmod(0o755)
            (root / "VERSION").write_text("1.9.4\n", encoding="utf-8")
            (root / ".dockerignore").write_text("dev\n", encoding="utf-8")
            source = root / "backend" / "source.go"
            source.write_text("package backend\n", encoding="utf-8")
            subprocess.run(["git", "init", "-q"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.name", "GoPulse Test"], cwd=root, check=True)
            subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=root, check=True)
            subprocess.run(["git", "add", "."], cwd=root, check=True)
            subprocess.run(["git", "commit", "-qm", "test baseline"], cwd=root, check=True)
            source.write_text("package backend\n// dirty\n", encoding="utf-8")
            docker_marker = root / "docker-called"
            fake_bin = root / "fake-bin"
            fake_bin.mkdir()
            fake_docker = fake_bin / "docker"
            fake_docker.write_text(
                f"#!/usr/bin/env bash\ntouch {docker_marker!s}\nexit 99\n",
                encoding="utf-8",
            )
            fake_docker.chmod(0o755)
            environment = os.environ.copy()
            environment["PATH"] = f"{fake_bin}:{environment['PATH']}"
            result = subprocess.run(
                ["bash", str(runner)],
                cwd=root,
                env=environment,
                text=True,
                capture_output=True,
                check=False,
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dirty build or runtime source: backend/source.go", result.stderr)
        self.assertFalse(docker_marker.exists(), "dirty source must fail before Docker access")

    def test_compose_environments_and_probe_argv_are_role_minimal(self) -> None:
        environment = os.environ.copy()
        environment.update(
            GOPULSE_VERSION="1.9.4",
            GOPULSE_REVISION="test-revision",
            GOPULSE_IMAGE_TAG="1.9.4-test",
        )
        result = subprocess.run(
            [
                "docker", "compose", "--profile", "operations",
                "--env-file", str(REPO / ".env.example"),
                "--file", str(REPO / "deploy" / "compose.yaml"),
                "config", "--format", "json",
            ],
            cwd=REPO,
            env=environment,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        model = json.loads(result.stdout)
        services = model["services"]

        forbidden = {
            "migrate": {"AUTH_JWT_SECRET", "MONITOR_API_TOKEN", "BACKEND_VICTORIAMETRICS_PASSWORD", "RABBITMQ_URL", "REDIS_PASSWORD", "LOG_MONITOR_INGEST_TOKEN", "ELASTICSEARCH_URL"},
            "admin-role": {"AUTH_JWT_SECRET", "MONITOR_API_TOKEN", "BACKEND_VICTORIAMETRICS_PASSWORD", "RABBITMQ_URL", "REDIS_PASSWORD", "LOG_MONITOR_INGEST_TOKEN", "ELASTICSEARCH_URL"},
            "search-init": {"AUTH_JWT_SECRET", "MONITOR_API_TOKEN", "BACKEND_VICTORIAMETRICS_PASSWORD", "RABBITMQ_URL", "REDIS_PASSWORD", "LOG_MONITOR_INGEST_TOKEN"},
            "business-worker": {"AUTH_JWT_SECRET", "MONITOR_API_TOKEN", "BACKEND_VICTORIAMETRICS_PASSWORD", "REDIS_PASSWORD"},
            "search-indexer": {"AUTH_JWT_SECRET", "MONITOR_API_TOKEN", "BACKEND_VICTORIAMETRICS_PASSWORD", "REDIS_PASSWORD"},
        }
        for service, keys in forbidden.items():
            actual = set(services[service].get("environment", {}))
            self.assertFalse(actual & keys, f"{service} received forbidden environment: {sorted(actual & keys)}")
        self.assertIn("RABBITMQ_URL", services["business-worker"]["environment"])
        self.assertIn("ELASTICSEARCH_URL", services["search-indexer"]["environment"])
        self.assertEqual(
            set(services["migrate"]["environment"]),
            {"GOPULSE_RUNTIME_MODE", "MYSQL_HOST", "MYSQL_PORT", "MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD"},
        )

        sensitive_values = (
            "gopulse-root",
            "gopulse-redis",
            "local-victoriametrics-password32",
        )
        for service in ("mysql", "redis", "victoriametrics"):
            argv = json.dumps(
                {
                    "command": services[service].get("command"),
                    "healthcheck": services[service].get("healthcheck", {}).get("test"),
                }
            )
            for value in sensitive_values:
                self.assertNotIn(value, argv, f"{service} argv leaked a credential")
            self.assertNotIn("Authorization: Basic", argv)
            self.assertNotRegex(argv, r"(?:--password(?:=|\\b)|(?:^|[ \"])\\-a(?:[ \"]|$))")
        self.assertTrue(
            any(
                "file:///run/secrets/victoriametrics_password" in argument
                for argument in services["victoriametrics"]["command"]
            )
        )

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
        self.assertNotIn('127.0.0.1:${ELASTICSEARCH_PORT', compose)
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

    def test_lifecycle_is_container_native_and_label_owned(self) -> None:
        dev = (REPO / "scripts" / "dev.sh").read_text(encoding="utf-8")
        down = (REPO / "scripts" / "down.sh").read_text(encoding="utf-8")
        verify = (REPO / "scripts" / "verify.sh").read_text(encoding="utf-8")
        compose = (REPO / "deploy" / "compose.yaml").read_text(encoding="utf-8")
        self.assertIn("compose build backend business-worker search-indexer frontend acceptance", dev)
        self.assertIn("compose up --detach --wait", dev)
        self.assertNotIn("go run", dev)
        self.assertNotIn("npm run", dev)
        self.assertNotIn("business-worker.json", dev)
        self.assertIn("com.docker.compose.project.working_dir", down)
        self.assertIn("--confirm-project", down)
        self.assertIn("down --remove-orphans", down)
        self.assertIn("acceptance e2e/compose-smoke.spec.ts", verify)
        self.assertIn('image_revision == "$REVISION"', verify)
        self.assertIn('running_image == "$tagged_image"', verify)
        self.assertIn("org.opencontainers.image.source", verify)
        self.assertIn("business-worker:", compose)
        self.assertIn("search-indexer:", compose)
        self.assertIn("internal: true", compose)
        self.assertNotIn("container_name:", compose)

    def test_full_stack_acceptance_is_owned_browser_backed_and_internal(self) -> None:
        source = OBSERVABILITY_SCRIPT.read_text(encoding="utf-8")
        compose = (REPO / "deploy" / "compose.yaml").read_text(encoding="utf-8")
        verify_compose = (REPO / "scripts" / "verify-compose.sh").read_text(encoding="utf-8")

        self.assertIn("^gopulse-accept-[a-f0-9]{12}$", source)
        self.assertIn("com.docker.compose.project.working_dir", source)
        self.assertIn("assert_project_ownership", source)
        self.assertIn("down --volumes --remove-orphans", source)
        self.assertNotIn("docker volume prune", source)
        self.assertNotIn("docker system prune", source)
        self.assertIn("e2e/compose-observability.spec.ts", source)
        for scenario in (
            "business",
            "redis-fallback",
            "worker-seed",
            "indexer-seed",
            "ordinary",
            "admin",
            "vm-down",
            "monitor-down",
            "transport-down",
            "post-restart",
            "manage",
        ):
            self.assertIn(scenario, source)
        self.assertIn('[[ -n $MODE ]] || MODE=--full', verify_compose)
        self.assertIn('exec "$SCRIPT_DIR/verify-compose-observability.sh"', verify_compose)
        self.assertIn("HOST_UTILITIES=(docker git sha256sum", source)
        self.assertIn("PATH=$HOST_BIN", source)
        self.assertIn("host runtime/client unexpectedly available", source)
        self.assertIn("trap early_cleanup EXIT", source)
        self.assertIn('IMAGE_TAG="${VERSION}-accept-${TOKEN}"', source)
        self.assertIn("cleanup_acceptance_images", source)
        self.assertIn("pre-existing image tag mapping changed", source)
        self.assertNotIn("replaced-images", source)

        for service in (
            "elasticsearch",
            "kafka",
            "victoriametrics",
            "router",
            "marshaller",
            "monitor",
            "redis-exporter",
        ):
            match = re.search(
                rf"(?ms)^  {re.escape(service)}:\n.*?(?=^  [A-Za-z0-9_-]+:\n|\Z)",
                compose,
            )
            self.assertIsNotNone(match, f"missing Compose service: {service}")
            self.assertNotIn("\n    ports:", match.group(0), f"{service} must remain internal-only")

    def test_container_fault_injection_validates_owned_targets_first(self) -> None:
        source = (REPO / "scripts" / "verify-compose.sh").read_text(encoding="utf-8")
        for service in ("redis", "business-worker", "search-indexer"):
            self.assertIn(f"owned_service_id {service}", source)
        self.assertLess(source.index("owned_service_id redis"), source.index("compose stop redis"))
        self.assertLess(source.index("owned_service_id business-worker"), source.index("compose pause business-worker"))
        self.assertLess(source.index("owned_service_id search-indexer"), source.index("compose pause search-indexer"))
        self.assertIn("compose down --volumes --remove-orphans", source)
        self.assertNotIn("docker volume prune", source)
        self.assertNotIn("docker system prune", source)



if __name__ == "__main__":
    unittest.main()
