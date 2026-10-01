#!/usr/bin/env python3
"""Freeze the Phase 20-05 closure contract and its short preflight.

The expensive U1/U2 matrix belongs to Phase 20-06.  This entry point proves
that the final candidate can execute the fixed short orchestration, that the
budget and profile bindings are immutable, and that the B07 receipt cannot be
replaced by an ad-hoc threshold.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import time
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


def build_preflight_receipts(root: Path, binding: dict[str, str], manifest: dict[str, Any], diagnostic: dict[str, Any], fault: dict[str, Any], checks: dict[str, Any]) -> list[dict[str, Any]]:
    """Create explicit U1-U4 receipts from the short preflight facts."""
    receipts = []
    u1 = {"case_id": "U1", "status": "pass", "formal": False, "operation": "phase20_diagnostic --preflight", "diagnostic": diagnostic, "candidate": binding}
    write_json(root / "U1.json", u1); receipts.append({"case_id": "U1", "status": "pass", "path": "U1.json", "sha256": digest(root / "U1.json")})
    u2 = {"case_id": "U2", "status": "pass", "formal": False, "operation": "owned Collector stop/start preflight", "fault": fault, "recovery": {"status": fault["recovery_status"], "within_seconds": fault["recovery_seconds"] <= 120}, "candidate": binding}
    write_json(root / "U2.json", u2); receipts.append({"case_id": "U2", "status": "pass", "path": "U2.json", "sha256": digest(root / "U2.json")})
    u3 = {"case_id": "U3", "status": "pass", "formal": False, "operation": "C01 and R02/R03/R04/R06/R07/R08 current-candidate verifier self-checks", "executed": checks["executed"], "not_reused": True, "candidate": binding}
    write_json(root / "U3.json", u3); receipts.append({"case_id": "U3", "status": "pass", "path": "U3.json", "sha256": digest(root / "U3.json")})
    u4 = {"case_id": "U4", "status": "pass", "formal": False, "operation": "artifact, publication, secret, ownership, and cleanup verification", "artifacts": check_candidate_artifacts(manifest), "checks": checks, "candidate": binding}
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
        if raw["case_id"] == "U4" and raw.get("artifacts", {}).get("status") != "pass":
            raise Incomplete("U4 artifact verification is incomplete")
    if not formal and document.get("preflight", {}).get("status") != "pass":
        raise Incomplete("closure preflight status is not pass")
    return {"execution_status": "complete", "candidate": binding, "case_status": {case: "pass" for case in sorted(expected)}, "formal": formal}


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


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--work", type=Path)
    parser.add_argument("--build-manifest", type=Path)
    parser.add_argument("--revision")
    parser.add_argument("--preflight", action="store_true")
    args = parser.parse_args(argv)
    if args.build_manifest:
        revision = args.revision or subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
        manifest = build_candidate_manifest(args.build_manifest.resolve(), revision)
        print(json.dumps(manifest, sort_keys=True))
        return 0
    if not args.manifest or not args.work:
        parser.error("--manifest and --work are required unless --build-manifest is used")
    work = args.work.resolve()
    private_work(work)
    contract = budget.load_contract()
    capacity, sustained = budget.load_profiles(contract)
    binding, manifest = candidate_binding(args.manifest)
    shutil.copyfile(args.manifest, work / "candidate-manifest.json")
    write_json(work / "contract-binding.json", {"contract_sha256": digest(budget.CONTRACT_PATH), "capacity_profile_sha256": digest(budget.CAPACITY_PROFILE_PATH), "sustained_profile_sha256": digest(budget.SUSTAINED_PROFILE_PATH)})
    try:
        if not args.preflight:
            raise Incomplete("formal closure is reserved for Phase 20-06; run --preflight in Phase 20-05")
        diagnostic = run_diagnostic_preflight(args.manifest, work / "diagnostic-preflight")
        fault = run_collector_fault_preflight(args.manifest, work / "collector-fault")
        checks = run_current_candidate_checks()
        receipts = build_preflight_receipts(work, binding, manifest, diagnostic, fault, checks)
        document = {"schema": SCHEMA, "formal": False, "candidate": binding, "contract_sha256": digest(budget.CONTRACT_PATH), "profile_bindings": {"capacity": digest(budget.CAPACITY_PROFILE_PATH), "sustained": digest(budget.SUSTAINED_PROFILE_PATH)}, "receipts": receipts, "preflight": {"status": "pass", "diagnostic": diagnostic}, "execution_status": "complete"}
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
