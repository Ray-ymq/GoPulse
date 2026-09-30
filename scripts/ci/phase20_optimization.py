#!/usr/bin/env python3
"""Run and strictly verify the Phase 20-03 single-candidate comparison."""
from __future__ import annotations

import argparse
import contextlib
import hashlib
import importlib.metadata
import io
import json
import re
import shutil
import subprocess
import sys
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CI = ROOT / "scripts" / "ci"
if str(CI) not in sys.path:
    sys.path.insert(0, str(CI))

import phase19_capacity as legacy
import phase20_diagnostic as diagnostic
import phase20_evidence as evidence


OPT_PROFILE_PATH = ROOT / "loadtest/phase20-optimization-profile.json"
OPT_SCHEMA_PATH = ROOT / "loadtest/phase20-optimization-profile.schema.json"
BASE_PROFILE_PATH = ROOT / "loadtest/phase20-capacity-profile.json"
BASE_SCHEMA_PATH = ROOT / "loadtest/phase20-capacity-profile.schema.json"
COMPOSE_PATH = ROOT / "deploy/compose.yaml"
TRACE_PATH = ROOT / "deploy/phase20-trace.yaml"
COLLECTOR_PATH = ROOT / "deploy/otel/phase20-collector.yaml"
RUNTIME_CONTRACT_PATH = ROOT / "deploy/runtime-contracts.json"

SCHEMA = "gopulse.phase20.optimization.v1"
CONTRACT_SCHEMA = "gopulse.phase20.optimization-contract.v1"
MODE = "verify_only"
B0_VERSION = "2.2.2"
B0_REVISION = "4f867ad21783d158619ea88563bb6190364c6398"
DELIVERY_VERSION = "2.2.3"
TRACE_COLLECTOR_REF = "otel/opentelemetry-collector-contrib:0.138.0@sha256:d535a52679b1df0a95b1b6fc4322cb74ecddd61f0b550cb43444d2b22cedec0c"
REVISION_PATTERN = re.compile(r"^[0-9a-f]{40}$")

SELF_IMAGE_NAMES = (
    "backend",
    "business-worker",
    "search-indexer",
    "admin-frontend",
    "frontend",
    "router",
    "marshaller",
    "monitor",
    "redis-exporter",
)
DEPENDENCY_DEFAULT_REFS = {
    "mysql": "mysql:8.4.0",
    "redis": "redis:7.2.5-alpine",
    "rabbitmq": "rabbitmq:3.13.3-management-alpine",
    "elasticsearch": "docker.elastic.co/elasticsearch/elasticsearch:9.5.2",
    "observability-elasticsearch": "docker.elastic.co/elasticsearch/elasticsearch:9.5.2",
    "kafka": "apache/kafka:4.3.1",
    "victoriametrics": "victoriametrics/victoria-metrics:v1.151.0",
}


class Incomplete(ValueError):
    """A contract or evidence condition is not complete."""


def digest(path: Path | str) -> str:
    return evidence.digest(Path(path))


def write_json(path: Path | str, value: object) -> None:
    legacy.atomic_json(Path(path), value)


def read_json(path: Path | str) -> dict:
    return json.loads(Path(path).read_text(encoding="utf-8"))


def relative(path: Path) -> str:
    return str(path.resolve().relative_to(ROOT.resolve()))


def optimization_tool_paths() -> list[Path]:
    return [
        ROOT / "scripts/ci/phase20_optimization.py",
        ROOT / "scripts/ci/test_phase20_optimization.py",
        ROOT / "scripts/verify-phase20-optimization.sh",
        OPT_PROFILE_PATH,
        OPT_SCHEMA_PATH,
        ROOT / "scripts/verify-phase20-evidence.py",
    ]


def optimization_tool_digests() -> dict[str, str]:
    paths = optimization_tool_paths()
    if any(not path.is_file() for path in paths):
        raise Incomplete("optimization tool input is missing")
    return {relative(path): digest(path) for path in paths}


def host_identity() -> str:
    try:
        raw = Path("/proc/sys/kernel/random/boot_id").read_bytes()
    except OSError as error:
        raise Incomplete("host boot identity is unavailable") from error
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def load_optimization_profile() -> dict:
    try:
        import jsonschema

        profile = read_json(OPT_PROFILE_PATH)
        jsonschema.validate(profile, read_json(OPT_SCHEMA_PATH))
    except (ImportError, OSError, ValueError, TypeError) as error:
        raise Incomplete("optimization profile/schema is invalid: " + str(error)) from error
    for path_key, digest_key in (
        ("base_profile", "base_profile_sha256"),
        ("base_schema", "base_schema_sha256"),
        ("runtime_contract", "runtime_contract_sha256"),
        ("trace_overlay", "trace_overlay_sha256"),
        ("collector_config", "collector_config_sha256"),
    ):
        path = ROOT / profile[path_key]
        if digest(path) != profile[digest_key]:
            raise Incomplete("frozen optimization input changed: " + profile[path_key])
    base = read_json(BASE_PROFILE_PATH)
    try:
        jsonschema.validate(base, read_json(BASE_SCHEMA_PATH))
    except (ImportError, OSError, ValueError, TypeError) as error:
        raise Incomplete("base capacity profile/schema is invalid: " + str(error)) from error
    if base["repetitions"] != profile["repetitions"] or [stage["name"] for stage in base["stages"]] != profile["stages"]:
        raise Incomplete("optimization profile does not retain the frozen stage schedule")
    if profile["mode"] != MODE or profile["candidate_version"] != B0_VERSION or profile["delivery_version"] != DELIVERY_VERSION:
        raise Incomplete("optimization mode or version contract drifted")
    return profile


def runtime_profile() -> dict:
    profile = read_json(BASE_PROFILE_PATH)
    profile["profile_id"] = "phase20-optimization-runtime"
    profile["target_candidate_version"] = B0_VERSION
    return profile


def runtime_profile_bytes() -> bytes:
    return (json.dumps(runtime_profile(), indent=2, sort_keys=True) + "\n").encode("utf-8")


def runtime_profile_digest() -> str:
    return "sha256:" + hashlib.sha256(runtime_profile_bytes()).hexdigest()


def command(args: list[str], timeout: int = 300, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, text=True, capture_output=True, timeout=timeout, env=env)


def require(result: subprocess.CompletedProcess[str], operation: str) -> str:
    if result.returncode:
        detail = (result.stderr or result.stdout or "").strip()
        raise Incomplete(operation + (": " + detail[-700:] if detail else ""))
    return result.stdout


def image_inspect(ref: str) -> dict:
    result = command(["docker", "image", "inspect", ref], timeout=60)
    raw = require(result, "inspect image " + ref)
    try:
        item = json.loads(raw)[0]
    except (IndexError, json.JSONDecodeError) as error:
        raise Incomplete("image inspection is not JSON: " + ref) from error
    image_id = item.get("Id")
    if not isinstance(image_id, str) or not image_id.startswith("sha256:"):
        raise Incomplete("image identity is not immutable: " + ref)
    return {"ref": ref, "id": image_id, "repo_digests": sorted(item.get("RepoDigests") or [])}


def ensure_image(ref: str) -> dict:
    result = command(["docker", "image", "inspect", ref], timeout=60)
    if result.returncode:
        require(command(["docker", "pull", ref], timeout=1800), "pull dependency image " + ref)
    return image_inspect(ref)


def resolved_dependency_ref(ref: str, inspected: dict) -> str:
    if "@sha256:" in ref:
        return ref
    repository = ref.rsplit(":", 1)[0]
    matches = [value for value in inspected["repo_digests"] if value.startswith(repository + "@sha256:")]
    if len(matches) != 1:
        raise Incomplete("dependency has no unique immutable digest: " + ref)
    return matches[0]


def candidate_manifest_path(contract_path: Path) -> Path:
    return contract_path.parent / "candidate-manifest.json"


def image_refs(tag: str) -> dict[str, str]:
    return {name: "gopulse/" + name + ":" + tag for name in SELF_IMAGE_NAMES}


def build_environment(candidate: dict, image_map: dict[str, str], dependency_map: dict[str, str], output: Path) -> dict[str, str]:
    values = legacy.parse_env(ROOT / ".env.example")
    tag = next(iter(image_map.values())).rsplit(":", 1)[1]
    values.update({
        "GOPULSE_VERSION": candidate["version"],
        "GOPULSE_REVISION": candidate["revision"],
        "GOPULSE_IMAGE_TAG": tag,
        "GOPULSE_RUNTIME_MODE": "container",
        "PUBLISHED_HOST": "127.0.0.1",
        "FRONTEND_PORT": "19080",
        "HTTP_PORT": "19090",
        "MYSQL_PORT": "19306",
    })
    for name, ref in {**image_map, **dependency_map}.items():
        values["GOPULSE_" + name.upper().replace("-", "_") + "_IMAGE"] = ref
    legacy.write_env(output, values)
    return values


def prepare_contract(path: Path) -> dict:
    if path.exists():
        raise Incomplete("refusing to overwrite an existing frozen contract")
    load_optimization_profile()
    if (ROOT / "VERSION").read_text(encoding="utf-8").strip() != B0_VERSION:
        raise Incomplete("B0 VERSION is not " + B0_VERSION)
    if not REVISION_PATTERN.fullmatch(B0_REVISION):
        raise Incomplete("B0 revision is invalid")
    tree = require(command(["git", "-C", str(ROOT), "rev-parse", B0_REVISION + "^{tree}"], timeout=30), "resolve B0 product tree").strip()
    tag = "phase20-03-b0-" + B0_REVISION[:12]
    candidate = {"version": B0_VERSION, "revision": B0_REVISION}
    self_refs = image_refs(tag)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    build_env_path = path.parent / "build.env"
    build_environment(candidate, self_refs, DEPENDENCY_DEFAULT_REFS, build_env_path)
    build_override = legacy.compose_override(path.parent / "build.override.yaml", 19300)
    before = legacy.resource_inventory()
    project = legacy.project_name()
    try:
        for service in SELF_IMAGE_NAMES:
            require(legacy.compose(project, build_env_path, [COMPOSE_PATH, build_override], "build", service, timeout=3600), "build B0 image " + service)
        self_artifacts = {name: image_inspect(ref) for name, ref in self_refs.items()}
        dependency_artifacts = {}
        dependency_refs = {}
        for name, ref in DEPENDENCY_DEFAULT_REFS.items():
            inspected = ensure_image(ref)
            immutable_ref = resolved_dependency_ref(ref, inspected)
            dependency_refs[name] = immutable_ref
            dependency_artifacts[name] = {**inspected, "ref": immutable_ref}
        collector_artifact = ensure_image(TRACE_COLLECTOR_REF)
        after = legacy.resource_inventory()
        if before != after:
            raise Incomplete("candidate build changed the Docker resource inventory")
        manifest = {
            "version": B0_VERSION,
            "revision": B0_REVISION,
            "product_tree": tree,
            "images": {name: {"ref": item["ref"], "id": item["id"]} for name, item in self_artifacts.items()},
            "third_party": {name: {"ref": dependency_refs[name], "id": dependency_artifacts[name]["id"]} for name in dependency_refs},
            "trace_collector": {"ref": TRACE_COLLECTOR_REF, "id": collector_artifact["id"]},
        }
        manifest_path = candidate_manifest_path(path)
        write_json(manifest_path, manifest)
        contract = {
            "schema": CONTRACT_SCHEMA,
            "mode": MODE,
            "candidate": {
                "version": B0_VERSION,
                "revision": B0_REVISION,
                "product_tree": tree,
                "manifest_sha256": digest(manifest_path),
            },
            "profile": {
                "path": relative(OPT_PROFILE_PATH),
                "sha256": digest(OPT_PROFILE_PATH),
                "runtime_sha256": runtime_profile_digest(),
                "base_path": relative(BASE_PROFILE_PATH),
                "base_sha256": digest(BASE_PROFILE_PATH),
            },
            "inputs": {
                "compose_sha256": digest(COMPOSE_PATH),
                "runtime_contract_sha256": digest(RUNTIME_CONTRACT_PATH),
                "trace_overlay_sha256": digest(TRACE_PATH),
                "collector_config_sha256": digest(COLLECTOR_PATH),
                "base_schema_sha256": digest(BASE_SCHEMA_PATH),
            },
            "candidate_manifest": {"path": manifest_path.name, "sha256": digest(manifest_path)},
            "artifacts": {
                "images": self_artifacts,
                "third_party": dependency_artifacts,
                "trace_collector": collector_artifact,
            },
            "tools": optimization_tool_digests(),
            "source": {"b0_revision": B0_REVISION, "product_tree": tree},
            "frozen": True,
        }
        write_json(path, contract)
        return contract
    finally:
        build_env_path.unlink(missing_ok=True)
        build_override.unlink(missing_ok=True)


def verify_contract(path: Path) -> dict:
    contract = read_json(path)
    if contract.get("schema") != CONTRACT_SCHEMA or contract.get("mode") != MODE or contract.get("frozen") is not True:
        raise Incomplete("optimization contract identity/mode mismatch")
    load_optimization_profile()
    if contract.get("profile", {}).get("sha256") != digest(OPT_PROFILE_PATH) or contract["profile"].get("runtime_sha256") != runtime_profile_digest():
        raise Incomplete("optimization profile binding drift")
    if contract["candidate"].get("version") != B0_VERSION or contract["candidate"].get("revision") != B0_REVISION:
        raise Incomplete("B0 candidate binding drift")
    expected_inputs = {
        "compose_sha256": digest(COMPOSE_PATH),
        "runtime_contract_sha256": digest(RUNTIME_CONTRACT_PATH),
        "trace_overlay_sha256": digest(TRACE_PATH),
        "collector_config_sha256": digest(COLLECTOR_PATH),
        "base_schema_sha256": digest(BASE_SCHEMA_PATH),
    }
    if contract.get("inputs") != expected_inputs:
        raise Incomplete("optimization runtime input digest drift")
    manifest_path = path.parent / contract["candidate_manifest"]["path"]
    if digest(manifest_path) != contract["candidate_manifest"]["sha256"] or digest(manifest_path) != contract["candidate"]["manifest_sha256"]:
        raise Incomplete("candidate manifest digest drift")
    manifest = read_json(manifest_path)
    if manifest.get("version") != B0_VERSION or manifest.get("revision") != B0_REVISION:
        raise Incomplete("candidate manifest B0 identity drift")
    if contract.get("tools") != optimization_tool_digests():
        raise Incomplete("optimization tool input changed after contract freeze")
    if set(manifest.get("images", {})) != set(contract.get("artifacts", {}).get("images", {})):
        raise Incomplete("self-developed artifact set drift")
    if set(manifest.get("third_party", {})) != set(contract.get("artifacts", {}).get("third_party", {})):
        raise Incomplete("dependency artifact set drift")
    verify_artifacts_live(contract, manifest)
    return contract


def verify_artifacts_live(contract: dict, manifest: dict) -> None:
    for group in ("images", "third_party"):
        for name, artifact in contract["artifacts"][group].items():
            ref = artifact["ref"]
            current = image_inspect(ref)
            if current["id"] != artifact["id"]:
                raise Incomplete("candidate image ID changed: " + name)
            entry = manifest[group][name]
            if entry.get("ref") != ref or entry.get("id") != artifact["id"]:
                raise Incomplete("candidate artifact manifest changed: " + name)
    collector = contract["artifacts"]["trace_collector"]
    current = image_inspect(collector["ref"])
    if current["id"] != collector["id"] or manifest["trace_collector"] != {"ref": collector["ref"], "id": collector["id"]}:
        raise Incomplete("trace collector image identity changed")


def validate_dependencies() -> dict[str, str]:
    expected = {"kafka-python": "2.2.15", "python-snappy": "0.7.3", "cramjam": "2.11.0"}
    actual = {name: importlib.metadata.version(name) for name in expected}
    if actual != expected:
        raise Incomplete("acceptance observer dependency drift")
    return actual


def run_optimization_self_tests(work: Path) -> dict:
    loader = unittest.TestLoader()
    suite = loader.loadTestsFromName("test_phase20_optimization")
    output = io.StringIO()
    executed: list[dict] = []

    class Results(unittest.TextTestResult):
        def startTest(self, test):
            self.started = time.monotonic()
            super().startTest(test)

        def addSuccess(self, test):
            case = re.search(r"test_(O[0-9]{2})_", test.id())
            executed.append({
                "test_id": test.id(),
                "case_id": case.group(1) if case else None,
                "result": "pass",
                "elapsed_seconds": time.monotonic() - self.started,
            })
            super().addSuccess(test)

    result = unittest.TextTestRunner(stream=output, resultclass=Results, verbosity=2).run(suite)
    raw = work / "optimization-self-tests.txt"
    raw.write_text(output.getvalue(), encoding="utf-8")
    raw.chmod(0o600)
    if not result.wasSuccessful() or result.skipped:
        raise Incomplete("optimization self-tests failed or skipped")
    required = {"O01", "O02", "O03", "O05"}
    if {row["case_id"] for row in executed} != required:
        raise Incomplete("O01/O02/O03/O05 self-tests are incomplete")
    receipt = {
        "schema": "gopulse.phase20.optimization-self-tests.v1",
        "formal": False,
        "cases": executed,
        "raw_sha256": digest(raw),
        "not_applicable": {"O04": "verify_only does not create B1 or withdraw a product change"},
    }
    receipt_path = work / "optimization-self-tests.json"
    write_json(receipt_path, receipt)
    return {"path": receipt_path.name, "sha256": digest(receipt_path)}


@contextlib.contextmanager
def trace_compose_overlay():
    """Add the frozen Phase 20 trace overlay to every owned diagnostic command."""
    original = legacy.compose
    base = COMPOSE_PATH.resolve()
    trace = TRACE_PATH.resolve()

    def wrapped(project, env_file, compose_file, *args, **kwargs):
        files = list(compose_file) if isinstance(compose_file, (list, tuple)) else [compose_file]
        normalized = [Path(item) for item in files]
        if any(item.resolve() == base for item in normalized) and not any(item.resolve() == trace for item in normalized):
            normalized.insert(1, TRACE_PATH)
        return original(project, env_file, normalized, *args, **kwargs)

    legacy.compose = wrapped
    try:
        yield
    finally:
        legacy.compose = original


def private_work(path: Path) -> None:
    path.mkdir(parents=True, exist_ok=False, mode=0o700)
    if path.stat().st_mode & 0o077:
        raise Incomplete("optimization work directory is not private")


def copy_runtime_inputs(work: Path, contract_path: Path, contract: dict) -> tuple[dict, dict, dict]:
    runtime = runtime_profile()
    profile_path = work / "profile.json"
    profile_path.write_bytes(runtime_profile_bytes())
    profile_path.chmod(0o600)
    if digest(profile_path) != contract["profile"]["runtime_sha256"]:
        raise Incomplete("runtime profile digest is not frozen")
    source_manifest = contract_path.parent / contract["candidate_manifest"]["path"]
    target_manifest = work / "candidate-manifest.json"
    shutil.copyfile(source_manifest, target_manifest)
    target_manifest.chmod(0o600)
    binding, candidate_document = legacy.candidate_binding(target_manifest, runtime)
    if binding != {
        "version": contract["candidate"]["version"],
        "revision": contract["candidate"]["revision"],
        "manifest_sha256": contract["candidate"]["manifest_sha256"],
    }:
        raise Incomplete("work candidate binding differs from frozen contract")
    return runtime, binding, candidate_document


def diagnostic_document(formal: bool, binding: dict, profile_path: Path, contract_path: Path, host_id: str) -> dict:
    tools = {relative(path): digest(path) for path in evidence.tool_inputs()}
    return {
        "schema": "gopulse.phase20.diagnostic.v1",
        "formal": formal,
        "candidate": binding,
        "profile_sha256": digest(profile_path),
        "tools": tools,
        "execution_status": "incomplete",
        "cells": [],
        "stop": None,
        "host_identity": host_id,
        "tool_dependencies": None,
        "contract_sha256": digest(contract_path),
    }


def optimization_document(formal: bool, binding: dict, contract_path: Path, profile_path: Path, host_id: str) -> dict:
    return {
        "schema": SCHEMA,
        "formal": formal,
        "mode": MODE,
        "candidate": binding,
        "contract_sha256": digest(contract_path),
        "profile_sha256": digest(OPT_PROFILE_PATH),
        "runtime_profile_sha256": digest(profile_path),
        "tools": optimization_tool_digests(),
        "host_identity": host_id,
        "self_tests": None,
        "b1": None,
        "improvement": None,
        "optimization_status": "incomplete",
        "execution_status": "incomplete",
        "capability_status": None,
        "cells": [],
        "stop": None,
    }


def select_preflight(contract_path: Path, binding: dict, profile_digest: str, host_id: str, tools: dict) -> dict:
    matches = []
    for path in sorted(contract_path.parent.glob("preflight*/diagnostic.json")):
        try:
            document = read_json(path)
            optimization = read_json(path.parent / "optimization.json")
            if (
                document.get("formal") is False
                and document.get("execution_status") == "complete"
                and document.get("candidate") == binding
                and document.get("profile_sha256") == profile_digest
                and document.get("host_identity") == host_id
                and document.get("tools") == tools
                and optimization.get("contract_sha256") == digest(contract_path)
                and optimization.get("optimization_status") == "preflight_passed"
            ):
                verify_optimization_directory(path.parent, contract_path, expected_formal=False, final=False)
                matches.append(path)
        except (OSError, KeyError, TypeError, ValueError):
            continue
    if not matches:
        raise Incomplete("same frozen candidate has no complete verified optimization preflight")
    selected = matches[-1]
    return {"path": str(selected), "sha256": digest(selected)}


def execute(work_path: Path, contract_path: Path, formal: bool) -> int:
    contract = verify_contract(contract_path)
    work = work_path.resolve()
    private_work(work)
    shutil.copyfile(contract_path, work / "contract.json")
    (work / "contract.json").chmod(0o600)
    host_id = host_identity()
    binding = None
    document = None
    optimization = None
    try:
        runtime, binding, candidate_document = copy_runtime_inputs(work, contract_path, contract)
        profile_path = work / "profile.json"
        document = diagnostic_document(formal, binding, profile_path, contract_path, host_id)
        optimization = optimization_document(formal, binding, contract_path, profile_path, host_id)
        write_json(work / "diagnostic.json", document)
        write_json(work / "optimization.json", optimization)
        document["tool_dependencies"] = validate_dependencies()
        optimization["self_tests"] = run_optimization_self_tests(work)
        document["self_tests"] = diagnostic.run_self_tests(work)
        write_json(work / "diagnostic.json", document)
        write_json(work / "optimization.json", optimization)
        if formal:
            optimization["preflight"] = select_preflight(contract_path, binding, digest(profile_path), host_id, document["tools"])
            document["preflight"] = optimization["preflight"]
            write_json(work / "diagnostic.json", document)
        host = legacy.host_inventory()
        legacy.validate_host(host, runtime)
        write_json(work / "host.json", host)
        recipe_binary, load_binary = legacy.build_loadtest(work)
        first = legacy.inspect_recipe(recipe_binary, binding, work / "recipe-inspect-1.json")
        second = legacy.inspect_recipe(recipe_binary, binding, work / "recipe-inspect-2.json")
        if any(first[key] != second[key] for key in ("digest", "counts", "id_ranges")):
            raise Incomplete("deterministic recipe inspection drift")
        schedule = [(repeat, index) for repeat in range(1, 4) for index in range(4)] if formal else [(1, 0), (1, 1)]
        with trace_compose_overlay():
            for repeat, index in schedule:
                print(json.dumps({"event": "cell_started", "repeat": repeat, "stage": runtime["stages"][index]["name"]}), flush=True)
                cell = diagnostic.run_cell(runtime, binding, candidate_document, recipe_binary, load_binary, work, repeat, index)
                document["cells"].append(cell)
                optimization["cells"].append({"repeat": repeat, "stage": cell["stage"], "run_id": cell["run_id"], "candidate": cell["candidate"]})
                write_json(work / "diagnostic.json", document)
                write_json(work / "optimization.json", optimization)
        document["execution_status"] = "complete"
        optimization["execution_status"] = "complete"
        write_json(work / "diagnostic.json", document)
        write_json(work / "optimization.json", optimization)
        result = verify_optimization_directory(work, contract_path, expected_formal=formal, final=False)
        if formal:
            if result["capability_status"] != "target_met" or any(not cell["passed"] for cell in result["cells"]):
                raise Incomplete("B0 correctness or non-degradation gate failed")
            optimization["optimization_status"] = "not_needed"
            optimization["capability_status"] = result["capability_status"]
            optimization["aggregates"] = result["aggregates"]
            optimization["improvement_status"] = "not_applicable"
            write_json(work / "optimization.json", optimization)
            final = verify_optimization_directory(work, contract_path, expected_formal=True, final=True)
            write_json(work / "verification.json", final)
        else:
            optimization["optimization_status"] = "preflight_passed"
            optimization["capability_status"] = result["capability_status"]
            write_json(work / "optimization.json", optimization)
            write_json(work / "verification.json", result)
        print(json.dumps({"execution_status": "complete", "optimization_status": optimization["optimization_status"], "capability_status": result["capability_status"]}, sort_keys=True), flush=True)
        return 0
    except Exception as error:
        if document is not None:
            document["execution_status"] = "incomplete"
            document["stop"] = {"classification": "acceptance_failure", "reason": type(error).__name__ + ": " + str(error)}
            write_json(work / "diagnostic.json", document)
        if optimization is not None:
            optimization["execution_status"] = "incomplete"
            optimization["stop"] = {"classification": "acceptance_failure", "reason": type(error).__name__ + ": " + str(error)}
            write_json(work / "optimization.json", optimization)
        print(json.dumps({"execution_status": "incomplete", "error": type(error).__name__ + ": " + str(error)}), file=sys.stderr, flush=True)
        return 1


def verify_optimization_directory(directory: Path, contract_path: Path, expected_formal: bool | None = None, final: bool = True) -> dict:
    root = Path(directory).resolve()
    contract = verify_contract(contract_path.resolve())
    optimization = read_json(root / "optimization.json")
    document = read_json(root / "diagnostic.json")
    if optimization.get("schema") != SCHEMA or optimization.get("mode") != MODE:
        raise Incomplete("optimization evidence schema/mode mismatch")
    if expected_formal is not None and optimization.get("formal") is not expected_formal:
        raise Incomplete("optimization execution mode mismatch")
    if optimization.get("contract_sha256") != digest(contract_path):
        raise Incomplete("optimization contract binding mismatch")
    if optimization.get("profile_sha256") != digest(OPT_PROFILE_PATH):
        raise Incomplete("optimization profile changed after execution")
    if optimization.get("tools") != optimization_tool_digests():
        raise Incomplete("optimization tool inputs changed after execution")
    if document.get("contract_sha256") != digest(contract_path):
        raise Incomplete("diagnostic contract binding mismatch")
    result = evidence.verify_directory(root, formal=bool(optimization["formal"]))
    frozen_binding = {
        "version": contract["candidate"]["version"],
        "revision": contract["candidate"]["revision"],
        "manifest_sha256": contract["candidate"]["manifest_sha256"],
    }
    if optimization.get("candidate") != frozen_binding or document.get("candidate") != frozen_binding:
        raise Incomplete("optimization candidate binding drift")
    if optimization.get("b1") is not None:
        raise Incomplete("verify_only evidence contains a B1 candidate")
    if optimization.get("improvement") is not None or optimization.get("improvement_rate") is not None:
        raise Incomplete("verify_only evidence contains an improvement result")
    tests = optimization.get("self_tests")
    if not tests or digest(root / tests["path"]) != tests["sha256"]:
        raise Incomplete("optimization self-test receipt binding missing")
    self_test_receipt = read_json(root / tests["path"])
    required_cases = {"O01", "O02", "O03", "O05"}
    if {row["case_id"] for row in self_test_receipt.get("cases", [])} != required_cases or any(row["result"] != "pass" for row in self_test_receipt["cases"]):
        raise Incomplete("optimization O01/O02/O03/O05 self-test receipt is incomplete")
    if digest(root / "optimization-self-tests.txt") != self_test_receipt["raw_sha256"]:
        raise Incomplete("optimization self-test raw output changed")
    cells = result["cells"]
    if optimization["formal"]:
        expected = {(repeat, stage["name"]) for repeat in range(1, 4) for stage in runtime_profile()["stages"]}
        actual = {(cell["repeat"], cell["stage"]) for cell in cells}
        if actual != expected or len(cells) != 12:
            raise Incomplete("O01 formal B0 repetition/stage set is incomplete or mixed")
        if final:
            if optimization.get("execution_status") != "complete" or optimization.get("optimization_status") != "not_needed":
                raise Incomplete("final verify_only status is not not_needed")
            if optimization.get("improvement_status") != "not_applicable":
                raise Incomplete("verify_only improvement status is not not_applicable")
            if result["execution_status"] != "complete" or result["capability_status"] != "target_met":
                raise Incomplete("O05 correctness/non-degradation gate failed")
            if any(not cell["passed"] for cell in cells):
                raise Incomplete("O05 retained a failed cell")
    else:
        expected = {(1, "rps-50"), (1, "rps-100")}
        if {(cell["repeat"], cell["stage"]) for cell in cells} != expected or len(cells) != 2:
            raise Incomplete("O01 preflight cell set is incomplete")
        if final and optimization.get("optimization_status") != "preflight_passed":
            raise Incomplete("preflight status is not complete")
    return {
        "schema": SCHEMA,
        "execution_status": result["execution_status"],
        "capability_status": result["capability_status"],
        "optimization_status": optimization.get("optimization_status"),
        "mode": MODE,
        "candidate": frozen_binding,
        "cells": cells,
        "measurements": result["measurements"],
        "aggregates": result["aggregates"],
        "improvement": "not_applicable",
        "b1": "not_applicable",
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--init-contract", action="store_true")
    modes.add_argument("--preflight", action="store_true")
    modes.add_argument("--formal", action="store_true")
    parser.add_argument("--contract", required=True, type=Path)
    parser.add_argument("--work", required=False, type=Path)
    args = parser.parse_args(argv)
    try:
        if not (args.init_contract or args.preflight or args.formal):
            if args.work is None:
                parser.error("execution requires --preflight, --formal, or --work")
            # The plan's formal invocation intentionally omits a mode flag.
            args.formal = True
        if args.init_contract:
            contract = prepare_contract(args.contract.resolve())
            print(json.dumps({"contract": str(args.contract.resolve()), "candidate": contract["candidate"], "mode": MODE}, sort_keys=True))
            return 0
        if args.work is None:
            parser.error("execution requires --work")
        return execute(args.work.resolve(), args.contract.resolve(), formal=args.formal)
    except Exception as error:
        print(json.dumps({"execution_status": "incomplete", "error": type(error).__name__ + ": " + str(error)}), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
