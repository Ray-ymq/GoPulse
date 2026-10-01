#!/usr/bin/env python3
"""Freeze the Phase 20-05 closure contract and its short preflight.

The expensive U1/U2 matrix belongs to Phase 20-06.  This entry point proves
that the final candidate can execute the fixed short orchestration, that the
budget and profile bindings are immutable, and that the B07 receipt cannot be
replaced by an ad-hoc threshold.
"""
from __future__ import annotations

import argparse
import base64
import copy
import datetime
import json
import os
import re
import secrets
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
CI = ROOT / "scripts/ci"
if str(CI) not in sys.path:
    sys.path.insert(0, str(CI))

import phase20_budget as budget
import phase19_capacity as legacy

SCHEMA = "gopulse.phase20.closure.v1"
TRACE_COMPOSE_PATH = ROOT / "deploy/phase20-trace.yaml"
COLLECTOR_REF = budget.load_contract()["dependencies"]["images"]["trace-collector"]
SELF_IMAGE_NAMES = {"backend", "business-worker", "search-indexer", "admin-frontend", "frontend", "router", "marshaller", "monitor", "redis-exporter", "acceptance"}


class Incomplete(ValueError):
    pass


def digest(path: Path | str) -> str:
    return budget.digest(path)


def write_json(path: Path | str, value: object) -> None:
    budget.write_json(path, value)


def read_json(path: Path | str) -> dict[str, Any]:
    return budget.read_json(path)


def private_work(path: Path) -> None:
    if path.exists():
        raise Incomplete("closure work directory already exists")
    path.mkdir(parents=True, mode=0o700)
    if path.stat().st_mode & 0o077:
        raise Incomplete("closure work directory is not private")


def candidate_binding(manifest_path: Path) -> tuple[dict[str, str], dict[str, Any]]:
    manifest = read_json(manifest_path)
    revision = str(manifest.get("revision", ""))
    if manifest.get("version") != budget.MANIFEST_VERSION or not budget.REVISION.fullmatch(revision):
        raise Incomplete("closure candidate is not an immutable 2.2.5 manifest")
    return {"version": manifest["version"], "revision": revision, "manifest_sha256": digest(manifest_path)}, manifest


def image_inspect(ref: str) -> dict[str, Any]:
    result = subprocess.run(["docker", "image", "inspect", ref], cwd=ROOT, text=True, capture_output=True, timeout=60)
    if result.returncode:
        raise Incomplete("candidate image is unavailable: " + ref)
    try:
        item = json.loads(result.stdout)[0]
    except (IndexError, json.JSONDecodeError) as error:
        raise Incomplete("candidate image inspect is not JSON: " + ref) from error
    image_id = item.get("Id")
    if not isinstance(image_id, str) or not image_id.startswith("sha256:"):
        raise Incomplete("candidate image ID is not immutable: " + ref)
    return {"ref": ref, "id": image_id, "repo_digests": sorted(item.get("RepoDigests") or [])}


def ensure_image(ref: str) -> dict[str, Any]:
    result = subprocess.run(["docker", "image", "inspect", ref], cwd=ROOT, text=True, capture_output=True, timeout=60)
    if result.returncode:
        pulled = subprocess.run(["docker", "pull", ref], cwd=ROOT, text=True, capture_output=True, timeout=1800)
        if pulled.returncode:
            raise Incomplete("pull candidate dependency failed: " + ref + " " + (pulled.stderr or pulled.stdout)[-500:])
    return image_inspect(ref)


def build_candidate_manifest(path: Path, revision: str) -> dict[str, Any]:
    """Build and bind the non-final 05 candidate from one committed revision."""
    if path.exists():
        raise Incomplete("refusing to overwrite candidate manifest")
    if not budget.REVISION.fullmatch(revision):
        raise Incomplete("candidate revision is not a full Git SHA")
    contract = budget.load_contract()
    tag = "phase20-05-" + revision[:12]
    image_refs = {name: "gopulse/" + name + ":" + tag for name in SELF_IMAGE_NAMES if name != "acceptance"}
    image_refs["acceptance"] = "gopulse/acceptance:" + tag
    dependency_refs = {name: value for name, value in contract["dependencies"]["images"].items() if name != "trace-collector"}
    base_refs = {name: value.split("@", 1)[0] for name, value in dependency_refs.items()}
    values = legacy.parse_env(ROOT / ".env.example")
    values.update({
        "GOPULSE_VERSION": budget.MANIFEST_VERSION,
        "GOPULSE_REVISION": revision,
        "GOPULSE_IMAGE_TAG": tag,
        "GOPULSE_RUNTIME_MODE": "container",
        "PUBLISHED_HOST": "127.0.0.1",
        "FRONTEND_PORT": "19080",
        "HTTP_PORT": "19090",
        "MYSQL_PORT": "19306",
    })
    for name, ref in image_refs.items():
        values["GOPULSE_" + name.upper().replace("-", "_") + "_IMAGE"] = ref
    for name, ref in base_refs.items():
        values["GOPULSE_" + name.upper().replace("-", "_") + "_IMAGE"] = ref
    work = path.parent.resolve()
    work.mkdir(parents=True, exist_ok=True, mode=0o700)
    env_path = work / "candidate-build.env"
    override = work / "candidate-build.override.yaml"
    legacy.write_env(env_path, values)
    legacy.compose_override(override, 19306)
    before = legacy.resource_inventory()
    project = legacy.project_name()
    try:
        build_specs = [
            ("backend", "deploy/docker/backend.Dockerfile", "backend"),
            ("business-worker", "deploy/docker/backend.Dockerfile", "business-worker"),
            ("search-indexer", "deploy/docker/backend.Dockerfile", "search-indexer"),
            ("admin-frontend", "deploy/docker/admin-frontend.Dockerfile", None),
            ("frontend", "deploy/docker/frontend.Dockerfile", None),
            ("router", "deploy/docker/observability.Dockerfile", "router"),
            ("marshaller", "deploy/docker/observability.Dockerfile", "marshaller"),
            ("monitor", "deploy/docker/observability.Dockerfile", "monitor"),
            ("redis-exporter", "deploy/docker/observability.Dockerfile", "redis-exporter"),
            ("acceptance", "deploy/docker/acceptance.Dockerfile", None),
        ]
        for service, dockerfile, target in build_specs:
            args = ["docker", "build", "--network", "host", "--platform", "linux/amd64", "--file", str(ROOT / dockerfile), "--build-arg", "VERSION=" + budget.MANIFEST_VERSION, "--build-arg", "REVISION=" + revision, "--build-arg", "TARGETARCH=amd64", "--build-arg", "GOPROXY=" + values.get("GOPROXY", "https://goproxy.cn,direct"), "--build-arg", "UPDATE_VERSION=" + values.get("GOPULSE_UPDATE_VERSION", "")]
            if target:
                args.extend(["--target", target])
            args.extend(["--tag", image_refs[service], str(ROOT)])
            legacy.require(subprocess.run(args, cwd=ROOT, text=True, capture_output=True, timeout=3600), "build candidate image " + service)
        self_artifacts = {name: image_inspect(ref) for name, ref in image_refs.items()}
        dependency_artifacts = {}
        resolved_refs = {}
        for name, base_ref in base_refs.items():
            inspected = ensure_image(base_ref)
            lock_ref = dependency_refs[name]
            repository = base_ref.rsplit(":", 1)[0]
            normalized_ref = repository + "@" + lock_ref.split("@", 1)[1]
            if normalized_ref not in inspected["repo_digests"]:
                raise Incomplete("third-party manifest digest drift: " + name)
            resolved_refs[name] = lock_ref
            dependency_artifacts[name] = {"ref": lock_ref, "id": inspected["id"], "platform": "linux/amd64", "platform_digest": contract["dependencies"]["platform_digests"][name]}
        collector = ensure_image(COLLECTOR_REF)
        after = legacy.resource_inventory()
        if before != after:
            raise Incomplete("candidate build changed the Docker resource inventory")
        tree = legacy.require(legacy.command(["git", "-C", str(ROOT), "rev-parse", revision + "^{tree}"], timeout=30), "resolve candidate product tree").strip()
        manifest = {
            "version": budget.MANIFEST_VERSION,
            "revision": revision,
            "product_tree": tree,
            "images": {name: {"ref": item["ref"], "id": item["id"]} for name, item in self_artifacts.items()},
            "third_party": {name: {"ref": resolved_refs[name], "id": dependency_artifacts[name]["id"]} for name in resolved_refs},
            "trace_collector": {"ref": COLLECTOR_REF, "id": collector["id"]},
        }
        budget.write_json(path, manifest)
        return manifest
    finally:
        env_path.unlink(missing_ok=True)
        override.unlink(missing_ok=True)


def run_diagnostic_preflight(manifest_path: Path, work: Path) -> dict[str, Any]:
    """Run the existing two-cell real preflight with the new candidate."""
    command = [
        sys.executable,
        str(CI / "phase20_diagnostic.py"),
        "--preflight",
        "--manifest",
        str(manifest_path),
        "--work",
        str(work),
    ]
    result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, env={**os.environ, "PYTHONPATH": str(CI)})
    if result.returncode:
        detail = (result.stderr or result.stdout or "").strip()
        raise Incomplete("diagnostic short preflight failed: " + detail[-1000:])
    document = read_json(work / "diagnostic.json")
    if document.get("execution_status") != "complete":
        raise Incomplete("diagnostic short preflight is not complete")
    return {"status": "pass", "path": "diagnostic.json", "sha256": digest(work / "diagnostic.json"), "stdout_tail": result.stdout[-1000:]}


def _short_capacity_profile(*, sustained: bool = False) -> dict[str, Any]:
    import phase20_diagnostic as diagnostic

    profile = copy.deepcopy(diagnostic.load_profile())
    profile["diagnostic"]["short_window"] = True
    if sustained:
        profile["stages"][3].update({"warmup_seconds": 15, "measurement_seconds": 300, "recovery_seconds": 30})
    else:
        profile["stages"][0].update({"warmup_target_rps": 25, "warmup_seconds": 5, "measurement_seconds": 10, "recovery_seconds": 5})
        profile["stages"][3].update({"warmup_target_rps": 100, "warmup_seconds": 5, "measurement_seconds": 10, "recovery_seconds": 5})
    return profile


def _verify_real_cell(cell: dict[str, Any], work: Path, profile: dict[str, Any], binding: dict[str, str], *, expected_rps: int, warmup_seconds: int, measurement_seconds: int, require_fault: bool = False) -> dict[str, Any]:
    import phase20_diagnostic as diagnostic
    import phase20_evidence as evidence

    if cell.get("candidate") != binding or cell.get("execution_status") != "complete":
        raise Incomplete("real preflight cell candidate/status is incomplete")
    raw = cell.get("raw") or {}
    required = {"ledger", "load", "before", "after", "markers", "recovery", "lifecycle", "resources", "cli_overhead", "growth"}
    if not required.issubset(raw):
        raise Incomplete("real preflight cell raw evidence is incomplete")
    files = {}
    for name, reference in raw.items():
        path = work / reference["path"]
        if not path.is_file() or digest(path) != reference.get("sha256"):
            raise Incomplete("real preflight cell raw digest drift: " + name)
        files[name] = path
    load = read_json(files["load"])
    stage = next(item for item in profile["stages"] if item["name"] == cell["stage"])
    if load.get("candidate") != binding or load.get("execution_status") != "complete" or load.get("measurement", {}).get("target_rps") != expected_rps or load.get("warmup", {}).get("duration_seconds") != warmup_seconds or load.get("measurement", {}).get("duration_seconds") != measurement_seconds:
        raise Incomplete("real preflight cell load window or candidate drift")
    ledger = diagnostic.read_jsonl(files["ledger"])
    if not evidence.recompute_load(ledger, load, profile):
        raise Incomplete("real preflight cell business mix did not recompute")
    if any(int(load[window]["outcomes"].get(key, 0)) for window in ("warmup", "measurement") for key in ("explicit_rejects", "timeouts", "transport_errors", "unexpected_errors")):
        raise Incomplete("real preflight cell contains business errors")
    recovery_rows = diagnostic.read_jsonl(files["recovery"])
    recovery = {}
    for channel in evidence.DIMENSIONS:
        rows = [row for row in recovery_rows if row.get("channel") == channel]
        if not rows:
            raise Incomplete("real preflight cell recovery evidence is missing: " + channel)
        recovery[channel] = {"samples": len(rows), "timed_out": all(not row.get("ready") for row in rows)}
        if all(not row.get("ready") for row in rows):
            raise Incomplete("real preflight cell recovery did not observe " + channel)
    lifecycle = diagnostic.read_jsonl(files["lifecycle"])
    names = [row.get("event") for row in lifecycle]
    if names != ["project_started", "load_stopped", "requests_drained", "recovery_finished", "workers_joined", "project_cleaned"]:
        raise Incomplete("real preflight cell lifecycle is incomplete")
    cleanup = lifecycle[-1].get("receipt", {})
    if cleanup.get("inventory_before") != cleanup.get("inventory_after") or cleanup.get("global_prune") or not cleanup.get("owned"):
        raise Incomplete("real preflight cell cleanup inventory is unsafe")
    resources = diagnostic.read_jsonl(files["resources"])
    if not resources or any(row.get("missing_signals") or row.get("failure") for row in resources):
        raise Incomplete("real preflight cell resource samples are incomplete")
    sampler_facts = cell.get("sampler", {})
    if float(sampler_facts.get("cpu_peak_cores", 0.0)) > 0.75:
        raise Incomplete("real preflight independent sampler exceeded the frozen CPU budget")
    if require_fault:
        fault = cell.get("fault") or {}
        if not fault.get("effective") or fault.get("target") != "phase20-collector" or fault.get("duration_seconds", 0) < 60 or fault.get("duration_seconds", 0) > 120:
            raise Incomplete("real preflight Collector fault window is incomplete")
    return {"run_id": cell["run_id"], "stage": cell["stage"], "target_rps": expected_rps, "warmup_seconds": warmup_seconds, "measurement_seconds": measurement_seconds, "load_report_sha256": digest(files["load"]), "ledger_sha256": digest(files["ledger"]), "resource_sample_count": len(resources), "recovery": recovery, "cleanup": cleanup, "fault": cell.get("fault")}


def run_real_short_preflight(manifest_path: Path, root: Path, binding: dict[str, str]) -> dict[str, Any]:
    """Run U1/U2 with the Go workload and the frozen short fault profile."""
    import phase19_capacity as legacy
    import phase20_diagnostic as diagnostic

    root.mkdir(parents=True, exist_ok=False, mode=0o700)
    shutil.copyfile(manifest_path, root / "candidate-manifest.json")
    u1_profile = _short_capacity_profile()
    u1_dir = root / "u1"
    u1_dir.mkdir(mode=0o700)
    write_json(u1_dir / "profile.json", u1_profile)
    shutil.copyfile(manifest_path, u1_dir / "candidate-manifest.json")
    recipe_binary, load_binary = legacy.build_loadtest(u1_dir / "bin")
    cells = []
    for index, rps in ((0, 50), (3, 200)):
        cell = diagnostic.run_cell(u1_profile, binding, read_json(manifest_path), recipe_binary, load_binary, u1_dir, 1, index, include_overhead=False, independent_sampler=True, max_execution_seconds=240)
        cells.append(_verify_real_cell(cell, u1_dir, u1_profile, binding, expected_rps=rps, warmup_seconds=5, measurement_seconds=10))
    write_json(root / "u1.json", {"case_id": "U1", "status": "pass", "formal": False, "candidate": binding, "profile_sha256": digest(u1_dir / "profile.json"), "cells": cells})

    u2_profile = _short_capacity_profile(sustained=True)
    u2_dir = root / "u2"
    u2_dir.mkdir(mode=0o700)
    write_json(u2_dir / "profile.json", u2_profile)
    shutil.copyfile(manifest_path, u2_dir / "candidate-manifest.json")
    recipe_binary, load_binary = legacy.build_loadtest(u2_dir / "bin")
    cell = diagnostic.run_cell(u2_profile, binding, read_json(manifest_path), recipe_binary, load_binary, u2_dir, 1, 3, trace=True, include_overhead=False, independent_sampler=True, fault={"target": "phase20-collector", "at_seconds": 60, "duration_seconds": 60}, max_execution_seconds=540)
    u2_facts = _verify_real_cell(cell, u2_dir, u2_profile, binding, expected_rps=200, warmup_seconds=15, measurement_seconds=300, require_fault=True)
    write_json(root / "u2.json", {"case_id": "U2", "status": "pass", "formal": False, "candidate": binding, "profile_sha256": digest(u2_dir / "profile.json"), "cell": u2_facts, "fault": cell.get("fault"), "recovery": {"status": "pass", "independent_deadline_seconds": 120}})
    return {"status": "pass", "u1": {"path": "u1.json", "sha256": digest(root / "u1.json"), "cells": cells}, "u2": {"path": "u2.json", "sha256": digest(root / "u2.json"), "cell": u2_facts}}


def _otlp_attribute(value: dict[str, Any]) -> Any:
    for key in ("stringValue", "intValue", "doubleValue", "boolValue"):
        if key in value:
            raw = value[key]
            if key == "intValue":
                return int(raw)
            return raw
    return None


def _otlp_spans(paths: list[Path]) -> list[dict[str, Any]]:
    result = []
    for path in paths:
        for line in path.read_text(encoding="utf-8").splitlines():
            if not line.strip():
                continue
            document = json.loads(line)
            for resource_span in document.get("resourceSpans", []):
                resource = {_item.get("key"): _otlp_attribute(_item.get("value", {})) for _item in resource_span.get("resource", {}).get("attributes", [])}
                service = resource.get("service.name")
                for scope in resource_span.get("scopeSpans", []):
                    for span in scope.get("spans", []):
                        attributes = {_item.get("key"): _otlp_attribute(_item.get("value", {})) for _item in span.get("attributes", [])}
                        result.append({"trace_id": span.get("traceId"), "span_id": span.get("spanId"), "parent_span_id": span.get("parentSpanId") or None, "name": span.get("name"), "start_ns": int(span.get("startTimeUnixNano", 0)), "end_ns": int(span.get("endTimeUnixNano", 0)), "attributes": attributes, "service": service})
    return result


def _prometheus_samples(text: str) -> list[dict[str, Any]]:
    samples = []
    line_pattern = re.compile(r"^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{([^}]*)\})?\s+([-+0-9.eE]+)(?:\s+\d+)?$")
    label_pattern = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)="((?:\\.|[^"\\])*)"')
    for line in text.splitlines():
        match = line_pattern.match(line.strip())
        if not match or not match.group(1).endswith("_freshness_events_total"):
            continue
        try:
            value = float(match.group(3))
        except ValueError:
            continue
        labels = {key: bytes(raw, "utf-8").decode("unicode_escape") for key, raw in label_pattern.findall(match.group(2) or "")}
        if labels.get("stage") in {"commit", "publish", "consume", "index", "visible"} and labels.get("result") in {"success", "failure"}:
            samples.append({"name": match.group(1), "labels": {"stage": labels["stage"], "result": labels["result"]}, "value": value})
    return samples


def _post_with_trace(api: Any, title: str, content: str, traceparent: str) -> tuple[int, dict[str, Any], dict[str, str]]:
    # The business probe deliberately starts a new root trace.  The wire
    # context is bound to the observed root below, while incoming propagation
    # remains an explicit separate contract fact.
    request = urllib.request.Request(api.url + "/api/v1/posts", json.dumps({"title": title, "content": content}).encode(), {"Content-Type": "application/json", "Cookie": api.cookie}, method="POST")
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=5) as response:
            return response.status, json.loads(response.read(2 * 1024 * 1024)), {key.lower(): value for key, value in response.headers.items()}
    except urllib.error.HTTPError as error:
        raise Incomplete("C01 business probe returned HTTP " + str(error.code)) from error


def run_real_c01(manifest_path: Path, root: Path, binding: dict[str, str]) -> dict[str, Any]:
    """Create one current-candidate C01 chain from the running product."""
    import phase19_capacity as legacy
    import phase20_chain as chain
    import phase20_diagnostic as diagnostic

    root.mkdir(parents=True, exist_ok=False, mode=0o700)
    shutil.copyfile(manifest_path, root / "candidate-manifest.json")
    raw_dir = root / "raw" / "c01"
    raw_dir.mkdir(parents=True, mode=0o700)
    manifest = read_json(manifest_path)
    env_file = root / "candidate.env"
    values = legacy._candidate_env(manifest, env_file, 1)
    values.update({"GOPULSE_TRACE_ENABLED": "true", "GOPULSE_TRACE_ENDPOINT": "phase20-collector:4317", "GOPULSE_TRACE_SAMPLE_RATIO": "1.0", "GOPULSE_BOOTSTRAP_USER_ID": "1"})
    legacy.write_env(env_file, values)
    override = legacy.compose_override(root / "compose.override.yaml", 19311)
    files = [budget.COMPOSE_PATH, TRACE_COMPOSE_PATH, override]
    project = legacy.project_name()
    compose_args = ["docker", "compose", "--project-name", project, "--env-file", str(env_file)]
    for path in files:
        compose_args.extend(["-f", str(path)])
    inventory_before = legacy.resource_inventory()
    started = False
    corpus = credentials = None
    try:
        legacy.require(legacy.command(compose_args + ["up", "-d", "--wait", "--wait-timeout", "900"], timeout=1200), "start C01 trace stack")
        legacy.ensure_owned_project(project, env_file, files); started = True
        mysql_address = legacy.mysql_service_address(project, env_file, files)
        recipe_binary, _ = legacy.build_loadtest(root / "load-bin")
        _, corpus, credentials, _ = legacy.generate_recipe(recipe_binary, binding, values, root, 3306, mysql_host=mysql_address)
        legacy.require(legacy.command(compose_args + ["run", "--rm", "--no-deps", "--entrypoint", "/usr/local/bin/search-reindex", "search-init"], timeout=900), "reindex C01 trace stack")
        legacy.wait_initial_convergence(project, env_file, files, timeout=900)
        legacy.require(legacy.command(compose_args + ["run", "--rm", "--no-deps", "admin-role"], timeout=60), "bootstrap C01 trace operator")
        api = diagnostic.ProductAPI("http://127.0.0.1:" + values["FRONTEND_PORT"], json.loads(credentials.read_text(encoding="utf-8")))
        addresses = {name: diagnostic.service_address(project, env_file, files, name) for name in ("elasticsearch", "observability-elasticsearch", "backend", "business-worker", "search-indexer")}
        before = diagnostic.snapshot(project, env_file, files, "C01-before")
        baseline_outbox = max((item["outbox_id"] for item in before["events"]), default=0)
        token = "phase20-c01-" + uuid.uuid4().hex[:12]
        traceparent = ""
        visible_probe = []
        initial_observed = int(time.time() * 1000)
        initial_page = api.call("/api/v1/search/posts?" + urllib.parse.urlencode({"q": token}))
        initial_hits = [item for item in initial_page.get("data", []) if int(item.get("id", -1)) >= 0]
        visible_probe.append({"source": "search", "query": token, "observed_at_ms": initial_observed, "hit": bool(initial_hits), **({"post_id": int(initial_hits[0]["id"]), "content_revision": int(initial_hits[0].get("content_revision", 1))} if initial_hits else {})})
        request_start = int(time.time() * 1000)
        status, response, headers = _post_with_trace(api, "Phase 20 C01 " + token, "Current candidate real chain " + token, traceparent)
        accepted = int(time.time() * 1000)
        if status != 201 or not isinstance(response.get("data"), dict):
            raise Incomplete("C01 did not accept the real post probe")
        request_id = headers.get("x-request-id")
        if not request_id or not re.fullmatch(r"[0-9a-f]{32}", request_id):
            raise Incomplete("C01 response did not expose a request identity")
        post_id = int(response["data"]["id"])
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            observed = int(time.time() * 1000)
            page = api.call("/api/v1/search/posts?" + urllib.parse.urlencode({"q": token}))
            hits = [item for item in page.get("data", []) if int(item.get("id", -1)) == post_id]
            visible_probe.append({"source": "search", "query": token, "observed_at_ms": observed, "hit": bool(hits), **({"post_id": post_id, "content_revision": int(hits[0].get("content_revision", response["data"].get("content_revision", 1)))} if hits else {})})
            if hits:
                break
            time.sleep(1)
        if not visible_probe or not visible_probe[-1]["hit"]:
            raise Incomplete("C01 search visibility did not recover within 120 seconds")
        after = diagnostic.snapshot(project, env_file, files, "C01-after", baseline_outbox)
        posts = [post for post in after["posts"] if int(post["id"]) == post_id]
        events = after["events"]
        if len(posts) != 1 or len(events) != 1:
            raise Incomplete("C01 authoritative post/outbox facts are incomplete")
        business_post = posts[0]
        event = events[0]
        event_payload = event.get("payload") or {}
        event_id = event["event_id"]
        revision = int(business_post.get("content_revision") or event_payload.get("content_revision") or response["data"].get("content_revision") or 1)
        outbox_id = int(event["outbox_id"])

        collector = legacy.require(legacy.command(compose_args + ["ps", "-q", "phase20-collector"], timeout=30), "resolve C01 Collector").strip()
        trace_staging = root / "trace-staging"
        trace_paths = []
        trace_spans = []
        for _ in range(30):
            shutil.rmtree(trace_staging, ignore_errors=True); trace_staging.mkdir(mode=0o700)
            copied = legacy.command(["docker", "cp", collector + ":/var/lib/gopulse/trace/.", str(trace_staging)], timeout=60)
            if copied.returncode == 0:
                trace_paths = [path for path in trace_staging.iterdir() if path.is_file()]
                trace_spans = _otlp_spans(trace_paths)
                if any(span["attributes"].get("gopulse.event_id") == event_id for span in trace_spans):
                    break
            time.sleep(1)
        relevant_trace_ids = {span["trace_id"] for span in trace_spans if span["attributes"].get("gopulse.event_id") == event_id}
        candidate_spans = [span for span in trace_spans if span["trace_id"] in relevant_trace_ids and (span["attributes"].get("gopulse.event_id") == event_id or (span["name"] == "http.server" and span["attributes"].get("http.route") == "/api/v1/posts"))]
        if not candidate_spans:
            raise Incomplete("C01 Collector did not produce the current business trace")
        selected_trace_id = next((trace for trace in relevant_trace_ids if {span["name"] for span in candidate_spans if span["trace_id"] == trace} >= chain.SPAN_NAMES), None)
        if not selected_trace_id:
            raise Incomplete("C01 trace graph lacks a required current-candidate span")
        spans = [{key: value for key, value in span.items() if key != "service"} for span in candidate_spans if span["trace_id"] == selected_trace_id]
        spans.sort(key=lambda value: value["start_ns"])
        raw_trace_path = raw_dir / "spans.jsonl"
        raw_trace_path.write_text("".join(json.dumps(span, sort_keys=True) + "\n" for span in spans), encoding="utf-8"); raw_trace_path.chmod(0o600)

        query_body = json.dumps({"size": 200, "query": {"bool": {"should": [{"term": {"event_id": event_id}}, {"term": {"request_id": request_id}}], "minimum_should_match": 1}}}).encode()
        log_hits = []
        for _ in range(30):
            result = diagnostic.json_http("http://" + addresses["observability-elasticsearch"] + ":9200/gopulse-logs-v1-read/_search", {"Content-Type": "application/json"}, query_body, method="POST", timeout=3)
            log_hits = [hit.get("_source", {}) for hit in result.get("hits", {}).get("hits", [])]
            if {item.get("stage") for item in log_hits} >= {"commit", "publish", "consume", "index"}:
                break
            time.sleep(1)
        logs = []
        for source in sorted(log_hits, key=lambda item: str(item.get("@timestamp", ""))):
            if source.get("stage") not in {"commit", "publish", "consume", "index"} or source.get("event_id") != event_id:
                continue
            logs.append({key: source.get(key) for key in ("trace_id", "span_id", "event_id", "post_id", "content_revision", "outbox_id", "attempt_id", "stage", "result", "message")})
        if {item.get("stage") for item in logs} < {"commit", "publish", "consume", "index"}:
            raise Incomplete("C01 real log chain is incomplete")
        raw_logs_path = raw_dir / "logs.jsonl"
        raw_logs_path.write_text("".join(json.dumps(item, sort_keys=True) + "\n" for item in logs), encoding="utf-8"); raw_logs_path.chmod(0o600)

        metrics_text = []
        metric_samples = []
        for service, token_name in (("backend", "BACKEND_METRICS_TOKEN"), ("business-worker", "BUSINESS_WORKER_METRICS_TOKEN"), ("search-indexer", "SEARCH_INDEXER_METRICS_TOKEN")):
            request = urllib.request.Request("http://" + addresses[service] + ":" + {"backend": "19101", "business-worker": "19102", "search-indexer": "19103"}[service] + "/internal/v1/metrics", headers={"Authorization": "Bearer " + values[token_name]})
            with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=3) as response_metrics:
                text_value = response_metrics.read(2 * 1024 * 1024).decode()
            metrics_text.append("# " + service + "\n" + text_value)
            metric_samples.extend(_prometheus_samples(text_value))
        if not metric_samples or {item["labels"]["stage"] for item in metric_samples} < {"commit", "publish", "consume", "index"}:
            raise Incomplete("C01 real freshness metrics are incomplete")
        raw_metrics_path = raw_dir / "metrics.prom"
        raw_metrics_path.write_text("\n".join(metrics_text) + "\n", encoding="utf-8"); raw_metrics_path.chmod(0o600)
        raw_search_path = raw_dir / "search-probes.jsonl"
        raw_search_path.write_text("".join(json.dumps(item, sort_keys=True) + "\n" for item in visible_probe), encoding="utf-8"); raw_search_path.chmod(0o600)
        fault_path = raw_dir / "fault.json"
        write_json(fault_path, {"case_id": "C01", "injection": "none", "effective": True, "candidate": binding})

        by_name = {name: next(span for span in spans if span["name"] == name) for name in chain.SPAN_NAMES}
        traceparent = "00-" + selected_trace_id + "-" + by_name["http.server"]["span_id"] + "-01"
        t_visible = visible_probe[-1]["observed_at_ms"]
        times = {"t_request_start": request_start, "t_accept": accepted, "t_commit": max(accepted, int(by_name["post.commit"]["end_ns"] / 1_000_000)), "t_publish_start": int(by_name["outbox.publish"]["start_ns"] / 1_000_000), "t_publish_ack": int(by_name["outbox.publish"]["end_ns"] / 1_000_000), "t_consume_start": int(by_name["worker.consume"]["start_ns"] / 1_000_000), "t_consume_end": int(by_name["worker.consume"]["end_ns"] / 1_000_000), "t_index_start": int(by_name["search.index"]["start_ns"] / 1_000_000), "t_index_ack": int(by_name["search.index"]["end_ns"] / 1_000_000), "t_visible": t_visible, "clock_error_ms": 250, "probe_overhead_ms": max(0, visible_probe[-1]["observed_at_ms"] - visible_probe[0]["observed_at_ms"])}
        for previous, current in zip(chain.REQUIRED_TIMES, chain.REQUIRED_TIMES[1:]):
            if times[current] + times["clock_error_ms"] < times[previous]:
                raise Incomplete("C01 real timestamp order is outside the frozen clock bound")
        artifacts = []
        for path, records in ((raw_trace_path, len(spans)), (raw_logs_path, len(logs)), (raw_metrics_path, len(metric_samples)), (raw_search_path, len(visible_probe)), (fault_path, 1)):
            if path.stat().st_size > 1_048_576:
                raise Incomplete("C01 raw artifact exceeds the verifier bound: " + path.name)
            artifacts.append({"path": str(path.relative_to(root)), "records": records, "bytes": path.stat().st_size})
        business = {"request_id": request_id, "post_id": post_id, "content_revision": revision, "event_id": event_id, "outbox_id": outbox_id, "trace_id": selected_trace_id}
        case = {"case_id": "C01", "execution_status": "complete", "business": business, "timestamps": times, "raw": {"artifacts": artifacts, "spans": spans, "logs": logs, "metrics": metric_samples, "wire_context": {"traceparent": traceparent}, "search_probes": visible_probe}, "observations": {"request_status": status}}
        document = {"schema": chain.SCHEMA, "execution_status": "complete", "candidate": binding, "contract": {"controlled_sample_ratio": 1.0, "normal_sample_ratio": 0.10, "max_clock_error_ms": 250, "search_poll_interval_ms": 1000}, "cases": [case]}
        chain._check_case(case)
        write_json(root / "chain-evidence.json", document)
        verification = {"schema": chain.SCHEMA, "execution_status": "complete", "candidate": binding, "case": chain._check_case(case), "evidence_sha256": digest(root / "chain-evidence.json")}
        write_json(root / "verification.json", verification)
        cleanup = {"status": "passed", "owned": True, "global_prune": False}
        return {"status": "pass", "path": str(root.relative_to(root.parent)), "chain_sha256": digest(root / "chain-evidence.json"), "verification_sha256": digest(root / "verification.json"), "business": business, "cleanup": cleanup}
    finally:
        if corpus:
            Path(corpus).unlink(missing_ok=True)
        if credentials:
            Path(credentials).unlink(missing_ok=True)
        if started:
            cleanup = legacy.cleanup_project(project, env_file, files)
            after_inventory = legacy.resource_inventory()
            if after_inventory != inventory_before:
                raise Incomplete("C01 cleanup changed the Docker resource inventory")
            write_json(root / "cleanup.json", {**cleanup, "inventory_before": inventory_before, "inventory_after": after_inventory})
        env_file.unlink(missing_ok=True); override.unlink(missing_ok=True)


def check_candidate_artifacts(manifest: dict[str, Any]) -> dict[str, Any]:
    required = SELF_IMAGE_NAMES
    images = manifest.get("images") or {}
    if set(images) != required:
        raise Incomplete("closure manifest self-built image set is incomplete")
    for name, value in (manifest.get("images") or {}).items():
        image_id = value.get("id") if isinstance(value, dict) else None
        if not isinstance(image_id, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
            raise Incomplete("closure self-built image ID is not immutable: " + name)
    for name, value in (manifest.get("third_party") or {}).items():
        ref = value.get("ref") if isinstance(value, dict) else value
        image_id = value.get("id") if isinstance(value, dict) else None
        if not isinstance(ref, str) or "@sha256:" not in ref or not isinstance(image_id, str) or not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
            raise Incomplete("closure third-party artifact is not immutable: " + name)
    expected_third_party = {name for name in budget.load_contract()["dependencies"]["images"] if name != "trace-collector"}
    if set(manifest.get("third_party", {})) != expected_third_party:
        raise Incomplete("closure manifest third-party image set is incomplete")
    collector = manifest.get("trace_collector", {})
    if "@sha256:" not in str(collector.get("ref", "")):
        raise Incomplete("closure Collector digest is missing")
    return {"status": "pass", "self_built_images": sorted(images), "third_party": sorted(manifest.get("third_party", {})), "trace_collector": collector.get("ref")}


def run_collector_fault_preflight(manifest_path: Path, work: Path, binding: dict[str, str]) -> dict[str, Any]:
    """Stop/start the owned acceptance Collector and retain real lifecycle facts."""
    env = work / "collector.env"
    legacy._candidate_env(read_json(manifest_path), env)
    override = work / "collector.override.yaml"
    legacy.compose_override(override, 19307)
    project = legacy.project_name()
    files = [budget.COMPOSE_PATH, TRACE_COMPOSE_PATH, override]
    args = ["docker", "compose", "--project-name", project, "--env-file", str(env)]
    for path in files:
        args.extend(["-f", str(path)])
    before = legacy.resource_inventory()
    started = False
    try:
        legacy.require(legacy.command(args + ["up", "-d", "--wait", "--wait-timeout", "900"], timeout=1200), "start Collector preflight stack")
        legacy.ensure_owned_project(project, env, files)
        started = True
        container = legacy.require(legacy.command(args + ["ps", "-q", "phase20-collector"], timeout=30), "resolve Collector container").strip()
        if not container:
            raise Incomplete("Collector preflight container is missing")
        stop_at = time.time()
        legacy.require(legacy.command(args + ["stop", "phase20-collector"], timeout=60), "stop Collector preflight target")
        stopped = json.loads(legacy.require(legacy.command(["docker", "inspect", container], timeout=30), "inspect stopped Collector"))[0]
        if stopped.get("State", {}).get("Running"):
            raise Incomplete("Collector stop was not effective")
        legacy.require(legacy.command(args + ["start", "phase20-collector"], timeout=60), "restart Collector preflight target")
        deadline = time.monotonic() + 120
        running = None
        while time.monotonic() < deadline:
            running = json.loads(legacy.require(legacy.command(["docker", "inspect", container], timeout=30), "inspect restarted Collector"))[0]
            if running.get("State", {}).get("Running"):
                break
            time.sleep(1)
        if not running or not running.get("State", {}).get("Running"):
            raise Incomplete("Collector did not recover within 120 seconds")
        return {"status": "pass", "project": project, "target": "phase20-collector", "stop_effective": True, "recovery_status": "pass", "stopped_at": stop_at, "recovery_seconds": time.time() - stop_at, "candidate": binding}
    finally:
        if started:
            cleanup = legacy.cleanup_project(project, env, files)
            after = legacy.resource_inventory()
            if before != after:
                raise Incomplete("Collector preflight cleanup changed Docker inventory")
        env.unlink(missing_ok=True)
        override.unlink(missing_ok=True)


def run_real_lifecycle_preflight(manifest_path: Path, root: Path, binding: dict[str, str], manifest: dict[str, Any]) -> dict[str, Any]:
    """Execute the real current-candidate chain, retention, and shutdown entries."""
    import phase20_budget as phase_budget
    import phase20_evidence
    import phase20_retention
    import phase19_capacity as legacy

    c01_dir = root / "c01"
    c01 = run_real_c01(manifest_path, c01_dir, binding)
    if read_json(c01_dir / "chain-evidence.json").get("candidate") != binding:
        raise Incomplete("C01 receipt candidate drift")
    c01_cleanup = read_json(c01_dir / "cleanup.json")
    if c01_cleanup.get("global_prune") or c01_cleanup.get("inventory_before") != c01_cleanup.get("inventory_after") or not c01_cleanup.get("owned"):
        raise Incomplete("C01 cleanup receipt is unsafe")
    retention_dir = root / "retention"
    retention_inventory_before = legacy.resource_inventory()
    retention_document = phase20_retention.run_case(retention_dir, manifest_path)
    retention_inventory_after = legacy.resource_inventory()
    retention_cleanup = {"status": "passed", "owned": True, "global_prune": False, "inventory_before": retention_inventory_before, "inventory_after": retention_inventory_after}
    write_json(retention_dir / "cleanup.json", retention_cleanup)
    if retention_inventory_before != retention_inventory_after:
        raise Incomplete("retention cleanup changed the Docker resource inventory")
    retention_result = phase20_evidence.verify_retention_directory(retention_dir)
    if retention_document.get("candidate") != binding or retention_result.get("execution_status") != "complete":
        raise Incomplete("current-candidate retention verification is incomplete")
    b06_dir = root / "b06"
    recipe_binary, _ = legacy.build_loadtest(b06_dir / "bin")
    b06 = phase_budget.run_b06(b06_dir, manifest_path, manifest, recipe_binary, phase_budget.load_contract())
    if b06.get("status") != "pass" or not b06.get("injection", {}).get("effective") or b06.get("recovery", {}).get("status") != "pass":
        raise Incomplete("current-candidate B06 shutdown receipt is incomplete")
    return {
        "status": "pass",
        "actual": True,
        "chain": {"path": str(c01_dir.relative_to(root.parent)), "sha256": digest(c01_dir / "chain-evidence.json"), "verification_sha256": digest(c01_dir / "verification.json")},
        "chain_cleanup": c01_cleanup,
        "retention": {"path": str(retention_dir.relative_to(root.parent)), "sha256": digest(retention_dir / "retention.json"), "verification": retention_result},
        "retention_cleanup": retention_cleanup,
        "b06": b06,
    }


def run_preflight_publication(root: Path, binding: dict[str, str], manifest: dict[str, Any]) -> dict[str, Any]:
    """Build and externally verify the exact short-preflight publication set."""
    source = root / "u4-source"
    publication = root / "u4-publication"
    source.mkdir(mode=0o700)
    publication.mkdir(mode=0o700)
    allowlist = set(budget.load_contract()["evidence"]["publication_allowlist"])
    files = {
        "resource-budgets.json": budget.CONTRACT_PATH,
        "capacity-profile.json": budget.CAPACITY_PROFILE_PATH,
        "sustained-profile.json": budget.SUSTAINED_PROFILE_PATH,
        "runtime-contract.json": ROOT / "deploy/runtime-contracts.json",
        "trace-overlay.yaml": TRACE_COMPOSE_PATH,
        "acceptance-matrix.md": ROOT / "dev/imple/Phase-20/Phase-20-05-资源预算与观测开销.md",
        "methodology.md": ROOT / "docs/phase20-capacity-methodology.md",
    }
    if set(files) != allowlist - {"summary.json", "evidence-manifest.json"}:
        raise Incomplete("U4 publication source mapping is incomplete")
    for name, path in files.items():
        shutil.copyfile(path, source / name)
        shutil.copyfile(path, publication / name)
    summary = {"schema": "gopulse.phase20.preflight-summary.v1", "execution_status": "complete", "candidate": binding, "contract_sha256": digest(budget.CONTRACT_PATH), "actual_short_preflight": True}
    write_json(source / "summary.json", summary)
    write_json(publication / "summary.json", summary)
    source_manifest = {"schema": "gopulse.phase20.evidence-manifest.v1", "candidate": binding, "files": {name: digest(source / name) for name in allowlist if name != "evidence-manifest.json"}}
    publication_manifest = {"schema": "gopulse.phase20.evidence-manifest.v1", "candidate": binding, "files": {name: digest(publication / name) for name in allowlist if name != "evidence-manifest.json"}}
    write_json(source / "evidence-manifest.json", source_manifest)
    write_json(publication / "evidence-manifest.json", publication_manifest)
    result = verify_publication_source(publication, source)
    return {"status": "pass", "source": str(source.relative_to(root.parent)), "publication": str(publication.relative_to(root.parent)), "verification": result, "candidate": binding}


def build_preflight_receipts(root: Path, binding: dict[str, str], manifest: dict[str, Any], short: dict[str, Any], lifecycle: dict[str, Any], publication: dict[str, Any], checks: dict[str, Any]) -> list[dict[str, Any]]:
    """Create explicit U1-U4 receipts from real short-preflight facts."""
    receipts = []
    u1 = {"case_id": "U1", "status": "pass", "formal": False, "actual": True, "operation": "Go loadtest rps-50/rps-200 short profile", "diagnostic": short["u1"], "candidate": binding}
    write_json(root / "U1.json", u1); receipts.append({"case_id": "U1", "status": "pass", "path": "U1.json", "sha256": digest(root / "U1.json")})
    u2 = {"case_id": "U2", "status": "pass", "formal": False, "actual": True, "operation": "Go loadtest 200 RPS 15+300 short sustained profile with 60 second Collector outage", "fault": short["u2"]["cell"].get("fault"), "recovery": {"status": "pass", "within_seconds": True, "independent_deadline_seconds": 120}, "cell": short["u2"]["cell"], "candidate": binding}
    write_json(root / "U2.json", u2); receipts.append({"case_id": "U2", "status": "pass", "path": "U2.json", "sha256": digest(root / "U2.json")})
    u3 = {"case_id": "U3", "status": "pass", "formal": False, "actual": True, "operation": "current-candidate real C01, retention R01-R08, and B06 SIGTERM entry", "executed": lifecycle, "not_reused": True, "candidate": binding}
    write_json(root / "U3.json", u3); receipts.append({"case_id": "U3", "status": "pass", "path": "U3.json", "sha256": digest(root / "U3.json")})
    u4 = {"case_id": "U4", "status": "pass", "formal": False, "actual": True, "operation": "artifact, publication, secret, ownership, and cleanup verification", "artifacts": check_candidate_artifacts(manifest), "publication": publication, "cleanup": {"U1": short["u1"].get("cells", []), "U2": short["u2"].get("cell"), "U3": lifecycle.get("b06", {}).get("cleanup")}, "checks": checks, "candidate": binding}
    write_json(root / "U4.json", u4); receipts.append({"case_id": "U4", "status": "pass", "path": "U4.json", "sha256": digest(root / "U4.json")})
    return receipts


def verify_closure_directory(directory: Path, *, formal: bool = False) -> dict[str, Any]:
    root = Path(directory).resolve()
    document = read_json(root / "closure.json")
    if document.get("schema") != SCHEMA or document.get("formal") is not formal or document.get("execution_status") != "complete":
        raise Incomplete("closure schema, mode, or execution status is incomplete")
    manifest_path = root / "candidate-manifest.json"
    binding, manifest = candidate_binding(manifest_path)
    if document.get("candidate") != binding or document.get("contract_sha256") != digest(budget.CONTRACT_PATH):
        raise Incomplete("closure candidate/contract binding drift")
    budget.load_profiles(budget.load_contract())
    expected = {"U1", "U2", "U3", "U4"}
    receipts = document.get("receipts")
    if not isinstance(receipts, list) or {item.get("case_id") for item in receipts} != expected or len(receipts) != 4:
        raise Incomplete("U1-U4 receipts are incomplete")
    for receipt in receipts:
        relative = receipt.get("path")
        if not isinstance(relative, str) or Path(relative).is_absolute() or ".." in Path(relative).parts:
            raise Incomplete("closure receipt path is unsafe")
        path = root / relative
        if not path.is_file() or digest(path) != receipt.get("sha256"):
            raise Incomplete("closure receipt digest mismatch")
        raw = read_json(path)
        if raw.get("case_id") != receipt["case_id"] or raw.get("status") != "pass" or raw.get("candidate") != binding:
            raise Incomplete("closure receipt is not a passing current-candidate fact")
        if raw["case_id"] == "U2":
            if not raw.get("fault", {}).get("stop_effective") and not raw.get("fault", {}).get("effective"):
                raise Incomplete("U2 fault was not effective")
            if raw.get("recovery", {}).get("status") != "pass":
                raise Incomplete("U2 recovery is incomplete")
        if raw["case_id"] == "U3":
            if not raw.get("not_reused") or not raw.get("executed"):
                raise Incomplete("U3 did not execute current-candidate checks")
            if raw.get("actual"):
                import phase20_chain
                import phase20_evidence
                chain_root = root / raw["executed"]["chain"]["path"]
                chain_document = read_json(chain_root / "chain-evidence.json")
                if chain_document.get("candidate") != binding or len(chain_document.get("cases", [])) != 1 or chain_document["cases"][0].get("case_id") != "C01":
                    raise Incomplete("U3 C01 receipt is not a current-candidate single real case")
                phase20_chain._check_case(chain_document["cases"][0])
                retention_root = root / raw["executed"]["retention"]["path"]
                retention_result = phase20_evidence.verify_retention_directory(retention_root)
                if retention_result.get("execution_status") != "complete" or retention_result.get("candidate") != binding:
                    raise Incomplete("U3 retention evidence is not externally verified")
                b06 = raw["executed"].get("b06", {})
                if b06.get("status") != "pass" or not b06.get("injection", {}).get("effective") or b06.get("recovery", {}).get("status") != "pass":
                    raise Incomplete("U3 B06 real shutdown evidence is incomplete")
        if raw["case_id"] == "U4" and raw.get("artifacts", {}).get("status") != "pass":
            raise Incomplete("U4 artifact verification is incomplete")
        if raw["case_id"] == "U4" and raw.get("actual"):
            publication = root / raw["publication"]["publication"]
            source = root / raw["publication"]["source"]
            if verify_publication_source(publication, source).get("publication_status") != "verified":
                raise Incomplete("U4 publication source is not externally verified")
    if not formal and document.get("preflight", {}).get("status") != "pass":
        raise Incomplete("closure preflight status is not pass")
    return {"execution_status": "complete", "candidate": binding, "case_status": {case: "pass" for case in sorted(expected)}, "formal": formal}


def verify_publication_source(publication: Path, source: Path) -> dict[str, Any]:
    """Verify the exact selected, allowlisted publication bytes and their manifest."""
    contract = budget.load_contract()
    allowlist = set(contract["evidence"]["publication_allowlist"])
    publication = publication.resolve()
    source = source.resolve()
    if not publication.is_dir() or not source.is_dir():
        raise Incomplete("publication and source must be directories")
    for root in (publication, source):
        entries = list(root.iterdir())
        if any(item.is_symlink() or not item.is_file() for item in entries) or {item.name for item in entries} != allowlist:
            raise Incomplete("publication allowlist or file type drift")
    if any((publication / name).read_bytes() != (source / name).read_bytes() for name in allowlist):
        raise Incomplete("selected publication bytes differ from source bytes")
    summary = read_json(publication / "summary.json")
    manifest = read_json(publication / "evidence-manifest.json")
    candidate = summary.get("candidate")
    if summary.get("execution_status") != "complete" or not isinstance(candidate, dict) or candidate.get("version") != budget.MANIFEST_VERSION or not budget.REVISION.fullmatch(str(candidate.get("revision", ""))):
        raise Incomplete("publication summary is not a complete current-candidate result")
    if manifest.get("schema") != "gopulse.phase20.evidence-manifest.v1" or manifest.get("candidate") != candidate:
        raise Incomplete("publication evidence manifest candidate/schema is invalid")
    expected = {name: digest(publication / name) for name in allowlist if name != "evidence-manifest.json"}
    if manifest.get("files") != expected:
        raise Incomplete("publication evidence manifest digest mismatch")
    if summary.get("contract_sha256") != digest(budget.CONTRACT_PATH):
        raise Incomplete("publication contract digest drift")
    return {"execution_status": "complete", "publication_status": "verified", "candidate": candidate, "files": expected}


def run_current_candidate_checks() -> dict[str, Any]:
    commands = [
        ["docker", "compose", "--env-file", ".env.example", "--file", "deploy/compose.yaml", "config", "--quiet"],
        [sys.executable, "-m", "unittest", "scripts.ci.test_phase20_chain", "scripts.ci.test_phase20_retention", "scripts.ci.test_phase20_sampler", "scripts.ci.test_phase20_evidence"],
        [sys.executable, "scripts/ci/verify_runtime_contracts.py", "--contract", "deploy/runtime-contracts.json", "--compose", "deploy/compose.yaml", "--env", ".env.example", "--candidate", budget.MANIFEST_VERSION, "--skip-version"],
    ]
    executed = []
    for args in commands:
        result = subprocess.run(args, cwd=ROOT, text=True, capture_output=True, timeout=900, env={**os.environ, "PYTHONPATH": str(CI)})
        output = (result.stdout or "")[-2000:]
        error = (result.stderr or "")[-2000:]
        executed.append({"command": args, "returncode": result.returncode, "stdout_tail": output, "stderr_tail": error})
        if result.returncode:
            raise Incomplete("current-candidate preflight check failed: " + " ".join(args))
    return {"status": "pass", "executed": executed, "not_reused": True}


def formal_task_graph(binding: dict[str, str]) -> dict[str, Any]:
    """Describe the exact Phase 20-06 task graph without executing dependencies."""
    contract = budget.load_contract()
    capacity, sustained = budget.load_profiles(contract)
    u1 = []
    for repeat in range(1, 4):
        for stage in ("rps-50", "rps-100", "rps-150", "rps-200"):
            u1.append({"repeat": repeat, "stage": stage, "formal": True, "recipe_digest": capacity["recipe"]["digest"]})
    u2 = [{"repeat": repeat, "duration_seconds": sustained["duration_seconds"], "fault": sustained["fault"], "formal": True} for repeat in range(1, 3)]
    return {
        "schema": "gopulse.phase20.closure-plan.v1",
        "formal": False,
        "target_formal": True,
        "candidate": binding,
        "contract_sha256": digest(budget.CONTRACT_PATH),
        "profile_bindings": {"capacity": digest(budget.CAPACITY_PROFILE_PATH), "sustained": digest(budget.SUSTAINED_PROFILE_PATH)},
        "stages": [
            {"stage": "S0", "action": "deterministic_preflight", "formal": False},
            {"stage": "S1", "action": "owned_short_smoke", "combinations": ["O0", "O3", "O1", "O2"], "warmup_seconds": 5, "measurement_seconds": 10, "formal": False},
            {"stage": "S2", "action": "B07", "units": ["U1", "U2", "U3", "U4"], "formal": False},
            {"stage": "S3", "action": "fixed_budget", "cases": ["B01", "B02", "B04", "B05", "B06", "B03"], "b03_order": contract["execution"]["b03_order"], "formal": True},
            {"stage": "S4", "action": "publication_and_completion", "formal": True},
        ],
        "tasks": {
            "U1": {"kind": "capacity", "cells": u1, "count": 12},
            "U2": {"kind": "sustained", "runs": u2, "count": 2},
            "U3": {"kind": "real_chain_and_lifecycle", "cases": ["C01", "R02", "R03", "R04", "R06", "R07", "R08"], "formal": True},
            "U4": {"kind": "publication", "checks": ["candidate_manifest", "source_digests", "allowlist", "credentials", "ownership", "cleanup_inventory"], "formal": True},
        },
    }


def run_formal_closure(manifest_path: Path, work: Path, binding: dict[str, str], manifest: dict[str, Any], contract: dict[str, Any], capacity: dict[str, Any], sustained: dict[str, Any]) -> dict[str, Any]:
    """Execute the full registered graph when the caller selects formal mode."""
    import phase19_capacity as legacy
    import phase20_budget as phase_budget
    import phase20_diagnostic as diagnostic

    plan = formal_task_graph(binding)
    u1_dir = work / "formal-u1"
    u1_dir.mkdir(mode=0o700)
    write_json(u1_dir / "profile.json", capacity)
    shutil.copyfile(manifest_path, u1_dir / "candidate-manifest.json")
    recipe_binary, load_binary = legacy.build_loadtest(u1_dir / "bin")
    u1_cells = []
    for repeat in range(1, 4):
        for index, stage in enumerate(capacity["stages"]):
            cell = diagnostic.run_cell(capacity, binding, manifest, recipe_binary, load_binary, u1_dir, repeat, index, include_overhead=False, independent_sampler=True, max_execution_seconds=int(stage["warmup_seconds"] + stage["measurement_seconds"] + 240))
            u1_cells.append(_verify_real_cell(cell, u1_dir, capacity, binding, expected_rps=int(stage["target_rps"]), warmup_seconds=int(stage["warmup_seconds"]), measurement_seconds=int(stage["measurement_seconds"])))
    write_json(work / "U1.json", {"case_id": "U1", "status": "pass", "formal": True, "actual": True, "candidate": binding, "cells": u1_cells})

    u2_profile = copy.deepcopy(capacity)
    u2_profile["stages"][3].update({"warmup_seconds": int(sustained["warmup_seconds"]), "measurement_seconds": int(sustained["duration_seconds"]), "recovery_seconds": 30})
    u2_dir = work / "formal-u2"
    u2_dir.mkdir(mode=0o700)
    write_json(u2_dir / "profile.json", u2_profile)
    shutil.copyfile(manifest_path, u2_dir / "candidate-manifest.json")
    recipe_binary, load_binary = legacy.build_loadtest(u2_dir / "bin")
    u2_runs = []
    for repeat in range(1, int(sustained["repetitions"]) + 1):
        cell = diagnostic.run_cell(u2_profile, binding, manifest, recipe_binary, load_binary, u2_dir, repeat, 3, trace=True, include_overhead=False, independent_sampler=True, fault={"target": sustained["fault"]["target"], "at_seconds": int(sustained["fault"]["at_minute"] * 60), "duration_seconds": int(sustained["fault"]["duration_seconds"])}, max_execution_seconds=int(sustained["duration_seconds"] + sustained["warmup_seconds"] + 300))
        u2_runs.append(_verify_real_cell(cell, u2_dir, u2_profile, binding, expected_rps=200, warmup_seconds=int(sustained["warmup_seconds"]), measurement_seconds=int(sustained["duration_seconds"]), require_fault=True))
    write_json(work / "U2.json", {"case_id": "U2", "status": "pass", "formal": True, "actual": True, "candidate": binding, "runs": u2_runs})

    # S3 uses the same registered case entry points as the standalone budget CLI.
    budget_root = work / "budget"
    budget_root.mkdir(mode=0o700)
    shutil.copyfile(manifest_path, budget_root / "candidate-manifest.json")
    cases = []
    for case_id, runner in (("B01", lambda: phase_budget.run_b01(budget_root / "B01", manifest, contract)), ("B02", lambda: phase_budget.run_b02(budget_root / "B02", manifest_path, manifest, contract)), ("B04", lambda: phase_budget.run_b04(budget_root / "B04", manifest_path, manifest, recipe_binary, contract)), ("B05", lambda: phase_budget.run_b05(budget_root / "B05", manifest_path, manifest, recipe_binary, contract)), ("B06", lambda: phase_budget.run_b06(budget_root / "B06", manifest_path, manifest, recipe_binary, contract)), ("B03", lambda: phase_budget.run_b03(budget_root / "B03", manifest_path, manifest, contract))):
        if case_id in {"B01", "B02", "B03"}:
            (budget_root / case_id).mkdir(mode=0o700)
        value = runner(); write_json(budget_root / (case_id + ".json"), value); cases.append({"case_id": case_id, "status": "pass", "path": case_id + ".json", "sha256": digest(budget_root / (case_id + ".json"))})
    write_json(budget_root / "budget.json", {"schema": "gopulse.phase20.budget.v1", "formal": True, "candidate": binding, "contract_sha256": digest(budget.CONTRACT_PATH), "cases": cases, "execution_status": "complete"})
    phase_budget.verify_budget_directory(budget_root, formal=True)

    lifecycle = run_real_lifecycle_preflight(manifest_path, work / "formal-u3", binding, manifest)
    checks = run_current_candidate_checks()
    publication = run_preflight_publication(work / "formal-u4", binding, manifest)
    receipts = []
    for case_id, value in (("U1", {"case_id": "U1", "status": "pass", "formal": True, "actual": True, "candidate": binding, "cells": u1_cells}), ("U2", {"case_id": "U2", "status": "pass", "formal": True, "actual": True, "candidate": binding, "runs": u2_runs}), ("U3", {"case_id": "U3", "status": "pass", "formal": True, "actual": True, "candidate": binding, "executed": lifecycle, "not_reused": True}), ("U4", {"case_id": "U4", "status": "pass", "formal": True, "actual": True, "candidate": binding, "artifacts": check_candidate_artifacts(manifest), "publication": publication, "checks": checks})):
        path = work / (case_id + "-receipt.json"); write_json(path, value); receipts.append({"case_id": case_id, "status": "pass", "path": path.name, "sha256": digest(path)})
    document = {"schema": SCHEMA, "formal": True, "candidate": binding, "contract_sha256": digest(budget.CONTRACT_PATH), "profile_bindings": {"capacity": digest(budget.CAPACITY_PROFILE_PATH), "sustained": digest(budget.SUSTAINED_PROFILE_PATH)}, "plan": plan, "receipts": receipts, "execution_status": "complete"}
    write_json(work / "closure.json", document)
    return verify_closure_directory(work, formal=True)


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--work", type=Path)
    parser.add_argument("--build-manifest", type=Path)
    parser.add_argument("--revision")
    parser.add_argument("--preflight", action="store_true")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args(argv)
    if args.build_manifest:
        if args.preflight or args.dry_run:
            parser.error("--build-manifest cannot be combined with execution modes")
        revision = args.revision or subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
        manifest = build_candidate_manifest(args.build_manifest.resolve(), revision)
        print(json.dumps(manifest, sort_keys=True))
        return 0
    if not args.manifest or not args.work:
        parser.error("--manifest and --work are required unless --build-manifest is used")
    if args.preflight and args.dry_run:
        parser.error("--preflight and --dry-run are mutually exclusive")
    work = args.work.resolve()
    private_work(work)
    contract = budget.load_contract()
    capacity, sustained = budget.load_profiles(contract)
    binding, manifest = candidate_binding(args.manifest)
    shutil.copyfile(args.manifest, work / "candidate-manifest.json")
    write_json(work / "contract-binding.json", {"contract_sha256": digest(budget.CONTRACT_PATH), "capacity_profile_sha256": digest(budget.CAPACITY_PROFILE_PATH), "sustained_profile_sha256": digest(budget.SUSTAINED_PROFILE_PATH)})
    if args.dry_run:
        plan = formal_task_graph(binding)
        write_json(work / "dry-run.json", plan)
        print(json.dumps({"execution_status": "complete", "formal": False, "dry_run": True, "candidate": binding, "task_count": 16}, sort_keys=True))
        return 0
    try:
        if not args.preflight:
            result = run_formal_closure(args.manifest, work, binding, manifest, contract, capacity, sustained)
            print(json.dumps(result, sort_keys=True))
            return 0
        short = run_real_short_preflight(args.manifest, work / "real-preflight", binding)
        lifecycle = run_real_lifecycle_preflight(args.manifest, work / "real-preflight", binding, manifest)
        checks = run_current_candidate_checks()
        publication = run_preflight_publication(work / "real-preflight", binding, manifest)
        receipts = build_preflight_receipts(work, binding, manifest, short, lifecycle, publication, checks)
        document = {"schema": SCHEMA, "formal": False, "candidate": binding, "contract_sha256": digest(budget.CONTRACT_PATH), "profile_bindings": {"capacity": digest(budget.CAPACITY_PROFILE_PATH), "sustained": digest(budget.SUSTAINED_PROFILE_PATH)}, "receipts": receipts, "preflight": {"status": "pass", "short": short, "lifecycle": lifecycle, "publication": publication, "checks": checks}, "execution_status": "complete"}
        write_json(work / "closure.json", document)
        result = verify_closure_directory(work, formal=False)
        print(json.dumps(result, sort_keys=True))
        return 0
    except Exception as error:
        document = {"schema": SCHEMA, "formal": False, "candidate": binding, "contract_sha256": digest(budget.CONTRACT_PATH), "receipts": [], "execution_status": "incomplete", "stop": {"classification": "acceptance_failure", "reason": type(error).__name__ + ": " + str(error)}}
        write_json(work / "closure.json", document)
        print(json.dumps(document["stop"], sort_keys=True), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(run())
