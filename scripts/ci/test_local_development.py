#!/usr/bin/env python3
"""Behavior tests for the Phase 22 local lifecycle helper."""

from __future__ import annotations

import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import unittest

from local_development import (
    DevelopmentError,
    compose_environment,
    check_ports,
    digest_paths,
    e2e_ports,
    integration_lock,
    port_argument,
    run_integration,
    run_e2e,
    parse_dotenv,
    process_birth_identity,
    process_owned,
    terminate_process,
    source_digest,
    workspace_for,
)


class LocalDevelopmentTests(unittest.TestCase):
    def test_dotenv_merge_keeps_defaults_and_caller_override_without_expansion(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".env.example").write_text("HTTP_PORT=8080\nMYSQL_PASSWORD=example\n", encoding="utf-8")
            caller = root / "local.env"
            caller.write_text("MYSQL_PASSWORD='caller-value'\n", encoding="utf-8")
            merged = compose_environment(root, "dev", str(caller), {"HTTP_PORT": "18080"})
            self.assertEqual(merged["HTTP_PORT"], "18080")
            self.assertEqual(merged["MYSQL_PASSWORD"], "caller-value")
            self.assertEqual(parse_dotenv(caller)["MYSQL_PASSWORD"], "caller-value")

    def test_test_environment_isolation_is_strict(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".env.example").write_text("HTTP_PORT=8080\nMYSQL_PORT=3306\n", encoding="utf-8")
            values = compose_environment(root, "test", environ={"MYSQL_PORT": "3306"})
            self.assertEqual(values["MYSQL_DATABASE"], "gopulse_integration")
            self.assertEqual(values["MYSQL_USER"], "gopulse_integration")
            self.assertEqual(values["REDIS_DB"], "15")
            self.assertEqual(values["MYSQL_PORT"], "23306")
            self.assertEqual(values["HTTP_PORT"], "18080")

    def test_test_environment_rejects_development_dependency_identity(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".env.example").write_text("MYSQL_HOST=127.0.0.1\n", encoding="utf-8")
            caller = root / "dev.env"
            caller.write_text("MYSQL_DATABASE=gopulse\nMYSQL_PORT=3306\nELASTICSEARCH_URL=http://127.0.0.1:9200\n", encoding="utf-8")
            values = compose_environment(root, "test", str(caller), {})
            self.assertEqual(values["MYSQL_DATABASE"], "gopulse_integration")
            self.assertEqual(values["MYSQL_PORT"], "23306")
            self.assertEqual(values["ELASTICSEARCH_URL"], "http://127.0.0.1:29200")
            self.assertEqual(values["OBSERVABILITY_ELASTICSEARCH_URL"], "http://127.0.0.1:29201")

    def test_integration_scope_rejects_unknown_before_docker(self) -> None:
        with self.assertRaises(DevelopmentError):
            run_integration(Path(tempfile.mkdtemp()), "unknown")

    def test_e2e_scope_rejects_unknown_before_docker(self) -> None:
        with self.assertRaises(DevelopmentError):
            run_e2e(Path(tempfile.mkdtemp()), "unknown", {})

    def test_e2e_ports_include_source_and_browser_ports(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".env.example").write_text("HTTP_PORT=8080\n", encoding="utf-8")
            values = compose_environment(root, "test")
            business = e2e_ports(values, False)
            observe = e2e_ports(values, True)
            self.assertIn(18080, business)
            self.assertIn(15173, business)
            self.assertIn(15174, observe)
            self.assertIn(19090, observe)

    def test_e2e_port_argument_rejects_out_of_range_values(self) -> None:
        self.assertEqual(port_argument("15173"), "15173")
        with self.assertRaises(Exception):
            port_argument("65536")

    def test_integration_lock_rejects_second_owner(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            workspace = workspace_for(Path(directory))
            with integration_lock(workspace):
                with self.assertRaises(DevelopmentError):
                    with integration_lock(workspace):
                        pass

    def test_source_digest_includes_local_replace_module(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "backend").mkdir()
            (root / "componentmetrics").mkdir()
            (root / "backend" / "main.go").write_text("backend", encoding="utf-8")
            (root / "componentmetrics" / "runtime.go").write_text("one", encoding="utf-8")
            first = source_digest(root)
            (root / "componentmetrics" / "runtime.go").write_text("two", encoding="utf-8")
            self.assertNotEqual(first, source_digest(root))

    def test_digest_changes_for_path_and_content(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "input.txt"
            path.write_text("one", encoding="utf-8")
            first = digest_paths(root, [path])
            path.write_text("two", encoding="utf-8")
            self.assertNotEqual(first, digest_paths(root, [path]))

    def test_process_birth_identity_prevents_pid_reuse_cleanup(self) -> None:
        child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)"])
        try:
            birth = process_birth_identity(child.pid)
            self.assertIsNotNone(birth)
            record = {"pid": child.pid, "birth": birth}
            self.assertTrue(process_owned(record))
            self.assertFalse(process_owned({"pid": child.pid, "birth": "different"}))
        finally:
            child.send_signal(signal.SIGTERM)
            child.wait(timeout=5)

    def test_process_group_stop_is_bounded_and_owned(self) -> None:
        child = subprocess.Popen(["bash", "-c", "sleep 30 & wait"], start_new_session=True)
        try:
            record = {"pid": child.pid, "birth": process_birth_identity(child.pid)}
            self.assertTrue(process_owned(record))
            terminate_process(record)
            child.wait(timeout=5)
            self.assertFalse(process_owned(record))
        finally:
            if child.poll() is None:
                child.kill()
                child.wait(timeout=5)

    def test_port_conflict_fails_without_touching_existing_socket(self) -> None:
        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        sock.listen()
        try:
            with self.assertRaises(DevelopmentError):
                check_ports([sock.getsockname()[1]])
            self.assertIsNotNone(sock.getsockname())
        finally:
            sock.close()

    def test_compose_environment_rejects_unknown_module(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / ".env.example").write_text("HTTP_PORT=8080\n", encoding="utf-8")
            with self.assertRaises(DevelopmentError):
                compose_environment(root, "missing")

    def test_workspace_identity_is_path_scoped(self) -> None:
        first = workspace_for(Path("/tmp/gopulse-a"))
        second = workspace_for(Path("/tmp/gopulse-b"))
        self.assertNotEqual(first.identity, second.identity)
        self.assertTrue(first.project_dev.endswith("-dev"))

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "VERSION").write_text("2.4.2\n", encoding="utf-8")
            self.assertTrue(workspace_for(root).project_test.endswith("-test-242"))


if __name__ == "__main__":
    unittest.main()
