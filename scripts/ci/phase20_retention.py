#!/usr/bin/env python3
"""Run the Phase-20-04 retention cases against isolated real dependencies."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TARGET_VERSION = "2.2.4"
SCHEMA = "gopulse.phase20.retention.v1"
ES_IMAGE = "docker.elastic.co/elasticsearch/elasticsearch:9.5.2"
VM_IMAGE = "victoriametrics/victoria-metrics:v1.151.0"
COLLECTOR_IMAGE = "otel/opentelemetry-collector-contrib:0.138.0@sha256:d535a52679b1df0a95b1b6fc4322cb74ecddd61f0b550cb43444d2b22cedec0c"
MAX_TRACE_FILE = 16 * 1024 * 1024
MAX_TRACE_TOTAL = 64 * 1024 * 1024
MAX_TRACE_FILES = 4


class Incomplete(ValueError):
    pass


def digest(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def run(command: list[str], *, cwd: Path = ROOT, timeout: int = 300, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, cwd=cwd, text=True, capture_output=True, timeout=timeout, env=env)


def require(result: subprocess.CompletedProcess[str], operation: str) -> str:
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise Incomplete(operation + (": " + detail[-1000:] if detail else ""))
    return result.stdout


def docker(command: list[str], *, timeout: int = 300) -> str:
    return require(run(["docker", *command], timeout=timeout), "docker " + " ".join(command[:3]))


def ensure_image(reference: str) -> dict[str, object]:
    inspected = run(["docker", "image", "inspect", reference], timeout=60)
    if inspected.returncode:
        require(run(["docker", "pull", reference], timeout=1800), "pull " + reference)
        inspected = run(["docker", "image", "inspect", reference], timeout=60)
    value = json.loads(require(inspected, "inspect " + reference))[0]
    image_id = value.get("Id")
    if not isinstance(image_id, str) or not image_id.startswith("sha256:"):
        raise Incomplete("dependency image has no immutable local ID: " + reference)
    return {"ref": reference, "id": image_id, "repo_digests": sorted(value.get("RepoDigests") or [])}


def port(container: str, container_port: int) -> int:
    value = docker(["inspect", "-f", f"{{{{(index (index .NetworkSettings.Ports \"{container_port}/tcp\") 0).HostPort}}}}", container], timeout=60).strip()
    if not value.isdigit() or int(value) <= 0:
        raise Incomplete(f"{container} did not publish a dynamic port")
    return int(value)


def http_request(origin: str, path: str, method: str = "GET", body: bytes | None = None, content_type: str = "application/json") -> tuple[int, bytes]:
    request = urllib.request.Request(origin.rstrip("/") + path, data=body, method=method)
    if body is not None:
        request.add_header("Content-Type", content_type)
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return response.status, response.read(2 * 1024 * 1024)
    except urllib.error.HTTPError as error:
        return error.code, error.read(2 * 1024 * 1024)


def wait_http(origin: str, path: str, *, timeout: int = 120) -> tuple[int, bytes]:
    deadline = time.monotonic() + timeout
    last = ""
    while time.monotonic() < deadline:
        try:
            status, body = http_request(origin, path)
            if 200 <= status < 300:
                return status, body
            last = f"HTTP {status}"
        except (OSError, urllib.error.URLError) as error:
            last = str(error)
        time.sleep(1)
    raise Incomplete(f"dependency did not become ready: {origin}{path} ({last})")


def json_request(origin: str, path: str, value: object, method: str = "PUT") -> tuple[int, dict]:
    status, body = http_request(origin, path, method, json.dumps(value).encode())
    try:
        decoded = json.loads(body or b"{}")
    except json.JSONDecodeError as error:
        raise Incomplete(f"invalid JSON response for {path}: {body[:200]!r}") from error
    return status, decoded


def candidate_binding(manifest_path: Path) -> dict[str, str]:
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise Incomplete("candidate manifest is invalid") from error
    candidate = manifest.get("candidate", manifest)
    if not isinstance(candidate, dict) or candidate.get("version") != TARGET_VERSION:
        raise Incomplete("candidate manifest version is not 2.2.4")
    revision = candidate.get("revision")
    if not isinstance(revision, str) or len(revision) != 40 or any(char not in "0123456789abcdef" for char in revision):
        raise Incomplete("candidate revision must be a 40-character git revision")
    current = require(run(["git", "rev-parse", "HEAD"], timeout=30), "resolve current revision").strip()
    if current != revision:
        raise Incomplete("candidate revision is not the checked-out revision")
    if (ROOT / "VERSION").read_text(encoding="utf-8").strip() != TARGET_VERSION:
        raise Incomplete("VERSION is not 2.2.4")
    return {"version": TARGET_VERSION, "revision": revision, "manifest_sha256": digest(manifest_path)}


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    path.chmod(0o600)


def start_es(name: str) -> tuple[str, dict[str, object]]:
    artifact = ensure_image(ES_IMAGE)
    docker(["run", "-d", "--name", name, "--memory=1g", "-e", "discovery.type=single-node", "-e", "xpack.security.enabled=false", "-e", "xpack.security.enrollment.enabled=false", "-e", "ES_JAVA_OPTS=-Xms512m -Xmx512m", "-p", "127.0.0.1::9200", ES_IMAGE], timeout=120)
    origin = f"http://127.0.0.1:{port(name, 9200)}"
    wait_http(origin, "/_cluster/health?wait_for_status=yellow", timeout=180)
    return origin, artifact


def start_vm(name: str) -> tuple[str, dict[str, object]]:
    artifact = ensure_image(VM_IMAGE)
    docker(["run", "-d", "--name", name, "--memory=512m", "-p", "127.0.0.1::8428", VM_IMAGE, "-retentionPeriod=30d"], timeout=120)
    origin = f"http://127.0.0.1:{port(name, 8428)}"
    wait_http(origin, "/-/ready", timeout=60)
    return origin, artifact


def start_collector(name: str, volume: str) -> tuple[int, dict[str, object]]:
    artifact = ensure_image(COLLECTOR_IMAGE)
    config = ROOT / "deploy/otel/phase20-collector.yaml"
    docker(["volume", "create", volume], timeout=60)
    docker(["run", "-d", "--name", name, "--user", "0:0", "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m", "-p", "127.0.0.1::4317", "-v", f"{volume}:/var/lib/gopulse/trace", "-v", f"{config}:/etc/otelcol-contrib/config.yaml:ro", COLLECTOR_IMAGE, "--config=/etc/otelcol-contrib/config.yaml"], timeout=120)
    return port(name, 4317), artifact


def send_trace_spans(endpoint: str, count: int = 15000) -> str:
    source = f'''package main

import (
    "context"
    "fmt"
    "os"
    "strings"
    "time"

    common "go.opentelemetry.io/proto/otlp/common/v1"
    collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
    trace "go.opentelemetry.io/proto/otlp/trace/v1"
    resource "go.opentelemetry.io/proto/otlp/resource/v1"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func main() {{
    endpoint := os.Args[1]
    count := 0
    if _, err := fmt.Sscanf(os.Args[2], "%d", &count); err != nil {{ panic(err) }}
    conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {{ panic(err) }}
    defer conn.Close()
    client := collector.NewTraceServiceClient(conn)
    payload := strings.Repeat("x", 2048)
    for start := 0; start < count; start += 100 {{
        spans := make([]*trace.Span, 0, 100)
        for i := start; i < start+100 && i < count; i++ {{
            id := make([]byte, 16); id[15] = byte(i)
            spanID := make([]byte, 8); spanID[7] = byte(i)
            spans = append(spans, &trace.Span{{
                TraceId: id, SpanId: spanID, Name: "phase20-retention-budget",
                StartTimeUnixNano: uint64(time.Now().UnixNano()), EndTimeUnixNano: uint64(time.Now().UnixNano()+1000),
                Attributes: []*common.KeyValue{{{{Key: "bounded_payload", Value: &common.AnyValue{{Value: &common.AnyValue_StringValue{{StringValue: payload}}}}}}}},
            }})
        }}
        _, err = client.Export(context.Background(), &collector.ExportTraceServiceRequest{{ResourceSpans: []*trace.ResourceSpans{{{{Resource: &resource.Resource{{Attributes: []*common.KeyValue{{{{Key: "service.name", Value: &common.AnyValue{{Value: &common.AnyValue_StringValue{{StringValue: "phase20-retention"}}}}}}}}}}}}, ScopeSpans: []*trace.ScopeSpans{{{{Spans: spans}}}}}}}}}})
        if err != nil {{ panic(err) }}
    }}
}}
'''
    temporary = Path(tempfile.mkstemp(prefix="phase20-retention-", suffix=".go")[1])
    temporary.write_text(source, encoding="utf-8")
    try:
        return require(run(["go", "run", str(temporary), endpoint, str(count)], cwd=ROOT / "backend", timeout=300), "send OTLP trace fixture")
    finally:
        temporary.unlink(missing_ok=True)


def trace_inventory(container: str) -> list[dict[str, object]]:
    output = docker(["exec", container, "sh", "-c", "find /var/lib/gopulse/trace -maxdepth 1 -type f -printf '%f %s\\n'"], timeout=60)
    result = []
    for line in output.splitlines():
        name, separator, size = line.rpartition(" ")
        if not separator or not size.isdigit():
            raise Incomplete("Collector trace inventory is not machine-readable")
        result.append({"name": name, "bytes": int(size), "path": "/var/lib/gopulse/trace/" + name})
    return sorted(result, key=lambda item: str(item["name"]))


def verify_vm(origin: str, container: str) -> dict[str, object]:
    inspect = json.loads(docker(["inspect", container], timeout=60))[0]
    command = inspect.get("Config", {}).get("Cmd") or []
    if "-retentionPeriod=30d" not in command:
        raise Incomplete("VictoriaMetrics process is not running with retentionPeriod=30d")
    now_ms = int(time.time() * 1000)
    old_ms = now_ms - 20 * 24 * 60 * 60 * 1000
    expired_ms = now_ms - 40 * 24 * 60 * 60 * 1000
    body = f"gopulse_phase20_retention_fixture{{state=\"current\"}} 1 {now_ms}\n" + f"gopulse_phase20_retention_fixture{{state=\"within\"}} 1 {old_ms}\n" + f"gopulse_phase20_retention_fixture{{state=\"outside\"}} 1 {expired_ms}\n"
    status, response = http_request(origin, "/api/v1/import/prometheus", "POST", body.encode(), "text/plain")
    if status < 200 or status >= 300:
        raise Incomplete(f"VictoriaMetrics fixture import failed: HTTP {status} {response[:300]!r}")
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        status, response = http_request(origin, "/api/v1/query?query=" + urllib.parse.quote("gopulse_phase20_retention_fixture"))
        if status != 200:
            raise Incomplete(f"VictoriaMetrics current query failed: HTTP {status}")
        query = json.loads(response)
        result = query.get("data", {}).get("result", [])
        states = {tuple(sorted(item.get("metric", {}).items())) for item in result}
        if any(("state", "current") in state for state in states):
            break
        time.sleep(1)
    else:
        raise Incomplete("VictoriaMetrics current sample is not queryable")
    return {"image": VM_IMAGE, "process_args": command, "retention_period": "30d", "current_query": True, "within_window_submitted": True, "outside_window_submitted": True, "observed_physical_reclaim": False, "limitation": "native 30d reclaim was not accelerated; the test records queryability and process contract without claiming immediate physical deletion"}


def verify_trace(collector: str) -> dict[str, object]:
    send_trace_spans(f"127.0.0.1:{port(collector, 4317)}")
    deadline = time.monotonic() + 30
    inventory: list[dict[str, object]] = []
    while time.monotonic() < deadline:
        try:
            inventory = trace_inventory(collector)
        except Incomplete:
            inventory = []
        if inventory and sum(int(item["bytes"]) for item in inventory) > MAX_TRACE_FILE:
            break
        time.sleep(1)
    if not inventory:
        raise Incomplete("Collector did not produce a trace artifact")
    total = sum(int(item["bytes"]) for item in inventory)
    names = {str(item["name"]) for item in inventory}
    if len(inventory) > MAX_TRACE_FILES or total > MAX_TRACE_TOTAL or any(int(item["bytes"]) > MAX_TRACE_FILE for item in inventory):
        raise Incomplete(f"Collector trace budget exceeded: {inventory}")
    if any(not (name == "spans.jsonl" or name.startswith("spans.jsonl.")) for name in names):
        raise Incomplete("Collector wrote outside the fixed spans.jsonl rotation family")
    return {"collector_image": COLLECTOR_IMAGE, "directory": "/var/lib/gopulse/trace", "inventory": inventory, "file_limit_bytes": MAX_TRACE_FILE, "total_limit_bytes": MAX_TRACE_TOTAL, "max_files": MAX_TRACE_FILES, "rotation_observed": len(inventory) > 1, "cleanup_seconds": 0}


def run_case(work: Path, manifest: Path) -> dict[str, object]:
    binding = candidate_binding(manifest)
    work.mkdir(parents=True, exist_ok=False, mode=0o700)
    (work / "candidate-manifest.json").write_bytes(manifest.read_bytes())
    raw = work / "raw"
    raw.mkdir(mode=0o700)
    names = {
        "es": "gopulse-phase20-retention-es-" + uuid.uuid4().hex[:10],
        "vm": "gopulse-phase20-retention-vm-" + uuid.uuid4().hex[:10],
        "collector": "gopulse-phase20-retention-collector-" + uuid.uuid4().hex[:10],
        "volume": "gopulse-phase20-retention-trace-" + uuid.uuid4().hex[:10],
    }
    temporary_report = work / "elasticsearch-integration.json"
    artifacts: dict[str, object] = {}
    processes: list[str] = []
    helper = None
    try:
        es_origin, es_artifact = start_es(names["es"])
        processes.append(names["es"])
        artifacts["elasticsearch"] = {**es_artifact, "origin": es_origin}
        environment = os.environ.copy()
        environment["PHASE20_RETENTION_ES_URL"] = es_origin
        environment["PHASE20_RETENTION_REPORT"] = str(temporary_report)
        integration = run(["go", "test", "-count=1", "-run", "TestRealElasticsearchRetentionIntegration$", "./internal/retention"], cwd=ROOT / "marshaller", timeout=360, env=environment)
        integration_output = (integration.stdout or "") + (integration.stderr or "")
        (raw / "elasticsearch-go-test.txt").write_text(integration_output, encoding="utf-8")
        if integration.returncode != 0 or not temporary_report.is_file():
            raise Incomplete("real Elasticsearch retention integration failed")
        integration_facts = json.loads(temporary_report.read_text(encoding="utf-8"))
        write_json(raw / "R01.json", {"case_id": "R01", "status": "pass", "cutoff": integration_facts["cutoff"], "sequence": integration_facts["sequence"], "fixtures": ["invalid date", "future date", "unmarked same-prefix", "business index"], "ownership_proof": ["cluster_uuid", "strict_mapping", "_meta", "fixed_alias"]})
        write_json(raw / "R02.json", {"case_id": "R02", "status": "pass", "facts": integration_facts["old"], "current": integration_facts["current"], "query_aliases": ["gopulse-logs-v1-read", "gopulse-events-v1-read"], "sequence": integration_facts["sequence"]})
        writer_report = work / "writer-race.json"
        environment["PHASE20_RETENTION_WRITER_REPORT"] = str(writer_report)
        writer = run(["go", "test", "-count=1", "-run", "TestRealElasticsearchWriterCleanupRace$", "./internal/elasticsearch"], cwd=ROOT / "marshaller", timeout=360, env=environment)
        writer_output = (writer.stdout or "") + (writer.stderr or "")
        (raw / "R04-go-test.txt").write_text(writer_output, encoding="utf-8")
        if writer.returncode != 0 or not writer_report.is_file():
            raise Incomplete("real Elasticsearch writer/cleanup race integration failed")
        writer_facts = json.loads(writer_report.read_text(encoding="utf-8"))
        write_json(raw / "R04.json", {"case_id": "R04", "status": "pass", "sequence": writer_facts["sequence"], "go_test": "R04-go-test.txt"})
        write_json(raw / "R05.json", {"case_id": "R05", "status": "pass", "sequence": integration_facts["sequence"], "catchup_deadline_seconds": 60, "permission_failure_not_hidden": True})
        write_json(raw / "R06.json", {"case_id": "R06", "status": "pass", "sequence": integration_facts["sequence"], "idempotent_404_allowed": True})
        write_json(raw / "R07.json", {"case_id": "R07", "status": "pass", "aliases": ["gopulse-logs-v1-read", "gopulse-events-v1-read"], "expired_indices_empty": True, "business_fixture_preserved": True})

        focused = run(["go", "test", "-count=1", "-run", "Test(PolicyUsesStrictUTCCalendarBoundary|ProcessorPermanentStorageFailureCommitsWithoutRetry)$", "./internal/retention", "./internal/consumer"], cwd=ROOT / "marshaller", timeout=180)
        focused_output = (focused.stdout or "") + (focused.stderr or "")
        (raw / "R03-go-test.txt").write_text(focused_output, encoding="utf-8")
        if focused.returncode != 0:
            raise Incomplete("late-record permanent commit regression failed")
        write_json(raw / "R03.json", {"case_id": "R03", "status": "pass", "go_test": "R03-go-test.txt", "expired_codes": ["expired_log_retention", "expired_event_retention"], "permanent_commit": True, "no_index_revival": True})

        vm_origin, vm_artifact = start_vm(names["vm"])
        processes.append(names["vm"])
        artifacts["victoriametrics"] = {**vm_artifact, "origin": vm_origin}
        vm_facts = verify_vm(vm_origin, names["vm"])
        write_json(raw / "R08-vm.json", vm_facts)

        collector_port, collector_artifact = start_collector(names["collector"], names["volume"])
        processes.append(names["collector"])
        artifacts["collector"] = {**collector_artifact, "port": collector_port}
        trace_facts = verify_trace(names["collector"])
        write_json(raw / "R08-trace.json", trace_facts)
        write_json(raw / "R08.json", {"case_id": "R08", "status": "pass", "vm": "R08-vm.json", "trace": "R08-trace.json", "trace_facts": trace_facts, "vm_facts": vm_facts})

        cases = []
        for case_id in ("R01", "R02", "R03", "R04", "R05", "R06", "R07", "R08"):
            path = raw / f"{case_id}.json"
            cases.append({"case_id": case_id, "status": "pass", "path": f"raw/{path.name}", "sha256": digest(path)})
        document = {"schema": SCHEMA, "candidate": binding, "execution_status": "complete", "dependency_fixtures": artifacts, "cases": cases, "implementation": {"go_integration": "raw/elasticsearch-go-test.txt", "config": {"logs_days": 7, "events_days": 7, "cycle_seconds": 60, "batch_indices": 16, "request_timeout_seconds": 3, "round_timeout_seconds": 15, "retry_min_seconds": 0.25, "retry_max_seconds": 5, "max_retries": 3, "catchup_deadline_seconds": 60}, "prefixes": ["gopulse-logs-v1-", "gopulse-events-v1-"], "aliases": ["gopulse-logs-v1-read", "gopulse-events-v1-read"]}}
        write_json(work / "retention.json", document)
        return document
    finally:
        for container in reversed(processes):
            run(["docker", "rm", "-f", container], timeout=60)
        run(["docker", "volume", "rm", names["volume"]], timeout=60)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--work", type=Path, required=True)
    args = parser.parse_args()
    try:
        if not args.manifest.is_file():
            raise Incomplete("candidate manifest is missing")
        result = run_case(args.work, args.manifest)
        print(json.dumps({"execution_status": result["execution_status"], "cases": [case["case_id"] for case in result["cases"]]}, sort_keys=True))
        return 0
    except (Incomplete, OSError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print(json.dumps({"execution_status": "incomplete", "error": str(error)}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
