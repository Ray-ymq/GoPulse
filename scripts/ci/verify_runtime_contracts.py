#!/usr/bin/env python3
"""Validate the single machine runtime contract against code and Compose."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
SEMVER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
PROCESS_ROLES = {
    "business-api",
    "business-worker",
    "search-indexer",
    "observability-router",
    "observability-marshaller",
    "observability-monitor",
    "plugin-exporter",
}
ALLOWED_IDENTITY_SOURCES = {"environment", "componentmetrics-fallback"}


def unique(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result


def load(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique)


def schema_check(value: Any, spec: dict[str, Any], path: str = "$") -> None:
    """Check the strict subset of JSON Schema used by the checked-in contract."""

    kinds = spec.get("type", [])
    if isinstance(kinds, str):
        kinds = [kinds]
    types = {
        "object": dict,
        "array": list,
        "string": str,
        "integer": int,
        "boolean": bool,
        "null": type(None),
    }
    if kinds and not any(type(value) is types[kind] for kind in kinds):
        raise ValueError(f"{path}: schema type mismatch")
    if "const" in spec and value != spec["const"]:
        raise ValueError(f"{path}: schema constant mismatch")
    if "enum" in spec and value not in spec["enum"]:
        raise ValueError(f"{path}: schema enum mismatch")
    if type(value) is int:
        if value < spec.get("minimum", value) or value > spec.get("maximum", value):
            raise ValueError(f"{path}: schema range mismatch")
    if isinstance(value, str):
        if "pattern" in spec and not re.fullmatch(spec["pattern"], value):
            raise ValueError(f"{path}: schema pattern mismatch")
        if len(value) < spec.get("minLength", len(value)) or len(value) > spec.get("maxLength", len(value)):
            raise ValueError(f"{path}: schema string length mismatch")
    if isinstance(value, list):
        if len(value) < spec.get("minItems", len(value)) or len(value) > spec.get("maxItems", len(value)):
            raise ValueError(f"{path}: schema item count mismatch")
        if spec.get("uniqueItems") and len({json.dumps(item, sort_keys=True) for item in value}) != len(value):
            raise ValueError(f"{path}: schema duplicate item")
        item_spec = spec.get("items")
        if item_spec:
            for index, item in enumerate(value):
                schema_check(item, item_spec, f"{path}[{index}]")
    if isinstance(value, dict):
        missing = set(spec.get("required", [])) - value.keys()
        if missing:
            raise ValueError(f"{path}: schema required field missing: {sorted(missing)}")
        properties = spec.get("properties", {})
        for key, entry in value.items():
            if key not in properties:
                if spec.get("additionalProperties") is False:
                    raise ValueError(f"{path}: schema unknown field {key}")
                continue
            schema_check(entry, properties[key], f"{path}.{key}")


def compose_document(path: Path) -> dict[str, Any]:
    return json.loads(
        subprocess.check_output(
            ["docker", "compose", "-f", str(path), "config", "--no-interpolate", "--format", "json"],
            text=True,
        )
    )


def sensitive(key: str) -> bool:
    return any(part in key for part in ("PASSWORD", "TOKEN", "SECRET", "DSN")) or key == "RABBITMQ_URL"


def _runtime_definitions(root: Path) -> dict[str, tuple[int, int]]:
    source = (root / "componentmetrics/runtime.go").read_text(encoding="utf-8")
    return {
        name: (int(port), int(budget))
        for name, port, budget in re.findall(r'"([a-z0-9-]+)":\s*\{(\d+),\s*(\d+)\}', source)
    }


def _code_catalog(root: Path) -> tuple[list[str], list[str]]:
    source = (root / "componentmetrics/catalog.go").read_text(encoding="utf-8")
    if "func ProcessIDs() []string" not in source:
        raise ValueError("componentmetrics process catalog entrypoint missing")

    def values(name: str) -> list[str]:
        match = re.search(rf"var\s+{name}\s*=\s*\[\]string\{{([^}}]*)\}}", source)
        if not match:
            raise ValueError(f"componentmetrics catalog missing {name}")
        return re.findall(r'"([a-z0-9-]+)"', match.group(1))

    return values("Components"), values("Plugins")


def _component_map(contract: dict[str, Any]) -> dict[str, dict[str, Any]]:
    components = contract["components"]
    result = {component["id"]: component for component in components}
    if len(result) != len(components):
        raise ValueError("duplicate process id")
    return result


def _check_process_shape(component: dict[str, Any], registered: dict[str, tuple[int, int]]) -> None:
    process_id = component["id"]
    if component["process_id"] != process_id:
        raise ValueError(f"{process_id}: process id drift")
    if component["role"] not in PROCESS_ROLES:
        raise ValueError(f"{process_id}: unknown process role")
    replica = component["replica"]
    if replica["min"] > replica["configured"] or replica["configured"] > replica["max"]:
        raise ValueError(f"{process_id}: replica range is inconsistent")
    if len(replica["instances"]) != replica["configured"]:
        raise ValueError(f"{process_id}: instance count differs from configured replicas")
    if replica["mode"] == "scalable":
        if replica["min"] < 2 or replica["configured"] < 2 or replica["ownership"] == "single-owner":
            raise ValueError(f"{process_id}: scalable process has an invalid replica policy")
    elif replica["mode"] == "singleton":
        if replica["min"] != 1 or replica["max"] != 1 or replica["configured"] != 1 or replica["ownership"] != "single-owner":
            raise ValueError(f"{process_id}: singleton process has an invalid replica policy")
    else:
        raise ValueError(f"{process_id}: unknown replica mode")
    if replica["identity_source"] not in ALLOWED_IDENTITY_SOURCES:
        raise ValueError(f"{process_id}: unknown identity source")
    if replica["identity_source"] == "environment" and replica["identity_env"] != "GOPULSE_INSTANCE_ID":
        raise ValueError(f"{process_id}: environment identity must use GOPULSE_INSTANCE_ID")
    if replica["identity_source"] == "componentmetrics-fallback" and replica["identity_env"] is not None:
        raise ValueError(f"{process_id}: fallback identity may not declare an environment key")
    identity_pattern = re.compile(replica["identity_pattern"])
    if any(not identity_pattern.fullmatch(instance) for instance in replica["instances"]):
        raise ValueError(f"{process_id}: replica identity does not match its contract")

    diagnostic = component["diagnostic"]
    if not diagnostic["independent"] or diagnostic["via_monitor"] or diagnostic["host_port_published"]:
        raise ValueError(f"{process_id}: diagnostic path is not independent")
    if diagnostic["identity_env"] != replica["identity_env"] or diagnostic["identity_source"] != replica["identity_source"]:
        raise ValueError(f"{process_id}: diagnostic identity source drift")
    if diagnostic["paths"] != ["/startup", "/live", "/ready", "/health"]:
        raise ValueError(f"{process_id}: diagnostic probe paths drift")
    probes = component["probes"]
    if set(probes) != {"startup", "live", "ready", "health"} or [probes[key] for key in ("startup", "live", "ready", "health")] != diagnostic["paths"]:
        raise ValueError(f"{process_id}: probe catalog drift")
    listeners = component["listeners"]
    if not listeners or len({listener["name"] for listener in listeners}) != len(listeners):
        raise ValueError(f"{process_id}: listener catalog is not unique")
    if any(listener["scope"] != "private" for listener in listeners):
        raise ValueError(f"{process_id}: public listener is outside the contract")
    probe = next((listener for listener in listeners if listener["name"] == diagnostic["probe_listener"]), None)
    if probe is None:
        raise ValueError(f"{process_id}: diagnostic probe listener is missing")
    if process_id not in registered or (probe["port"], component["shutdown_seconds"]) != registered[process_id]:
        raise ValueError(f"{process_id}: source port or shutdown drift")
    budgets = component["budgets"]
    if budgets["shutdown_seconds"] != component["shutdown_seconds"] or budgets["stop_grace_seconds"] != component["stop_grace_seconds"]:
        raise ValueError(f"{process_id}: shutdown budget drift")
    if component["stop_grace_seconds"] <= component["shutdown_seconds"]:
        raise ValueError(f"{process_id}: stop grace must exceed the application budget")
    for connection in budgets["connections"]:
        if connection["total"] < connection["per_instance"] * replica["configured"]:
            raise ValueError(f"{process_id}: total connection budget is below replica demand")
    for key in ("connections", "queues", "in_flight"):
        names = [item["name"] for item in budgets[key]]
        if len(names) != len(set(names)):
            raise ValueError(f"{process_id}: duplicate {key} budget")


def _check_environment(component: dict[str, Any], env: str, contract_version: str) -> dict[str, dict[str, Any]]:
    fields = {field["key"]: field for field in component["environment"]}
    if len(fields) != len(component["environment"]):
        raise ValueError(f"{component['id']}: duplicate environment key")
    for field in fields.values():
        key = field["key"]
        if sensitive(key) and (not field["sensitive"] or field["default"] is not None):
            raise ValueError(f"{component['id']}: unsafe secret metadata for {key}")
        if len(field["aliases"]) > 1:
            raise ValueError(f"{component['id']}: multiple compatibility aliases for {key}")
        for alias in field["aliases"]:
            if not alias.get("expires") or tuple(map(int, alias["expires"].split("."))) <= tuple(map(int, contract_version.split("."))):
                raise ValueError(f"{component['id']}: alias has no future expiry for {key}")
    if component["replica"]["identity_source"] == "environment" and "GOPULSE_INSTANCE_ID" not in fields:
        raise ValueError(f"{component['id']}: instance identity is not registered")
    if component["replica"]["mode"] == "scalable" and "GOPULSE_REPLICA_COUNT" not in fields:
        raise ValueError(f"{component['id']}: replica count is not registered")
    return fields


def _compose_environment(service: dict[str, Any]) -> dict[str, str]:
    return {key: str(value) for key, value in service.get("environment", {}).items()}


def _check_compose(component: dict[str, Any], compose: dict[str, Any], fields: dict[str, dict[str, Any]]) -> None:
    process_id = component["id"]
    services = component["compose_services"]
    if component["compose_service"] != (services[0] if services else None):
        raise ValueError(f"{process_id}: compose service alias drift")
    if not services:
        return
    for index, service_name in enumerate(services):
        if service_name not in compose["services"]:
            raise ValueError(f"{process_id}: missing Compose service {service_name}")
        service = compose["services"][service_name]
        if service.get("ports"):
            raise ValueError(f"{process_id}: product process publishes a host port")
        if not service.get("networks"):
            raise ValueError(f"{process_id}: Compose service has no private network")
        environment = _compose_environment(service)
        recorded = {key: field["compose_value"] for key, field in fields.items() if field["compose_value"] is not None}
        expected_instance = component["replica"]["instances"][index]
        if component["replica"]["identity_source"] == "environment":
            recorded["GOPULSE_INSTANCE_ID"] = expected_instance
        if recorded != environment:
            raise ValueError(f"{process_id}: unregistered or changed Compose-owned environment key")
        if component["replica"]["identity_source"] == "environment" and environment.get("GOPULSE_INSTANCE_ID") != expected_instance:
            raise ValueError(f"{process_id}: Compose instance identity drift for {service_name}")
        if component["replica"]["mode"] == "scalable" and environment.get("GOPULSE_REPLICA_COUNT") != str(component["replica"]["configured"]):
            raise ValueError(f"{process_id}: Compose replica count drift for {service_name}")
        labels = service.get("labels", {})
        expected_labels = {
            "io.gopulse.runtime.process_id": process_id,
            "io.gopulse.runtime.role": component["role"],
            "io.gopulse.runtime.replica_mode": component["replica"]["mode"],
            "io.gopulse.runtime.ownership": component["replica"]["ownership"],
            "io.gopulse.runtime.diagnostic": "direct-private-probe",
        }
        if any(str(labels.get(key)) != value for key, value in expected_labels.items()):
            raise ValueError(f"{process_id}: Compose runtime labels drift for {service_name}")
        listener = next(listener for listener in component["listeners"] if listener["name"] == component["diagnostic"]["probe_listener"])
        exposed = {str(port).split("/", 1)[0] for port in service.get("expose", [])}
        if str(listener["port"]) not in exposed:
            raise ValueError(f"{process_id}: probe listener is not exposed privately")
        health = service.get("healthcheck", {}).get("test", [])
        if not any(f":{listener['port']}/ready" in str(part) for part in health):
            raise ValueError(f"{process_id}: Docker readiness healthcheck drift")
        if service.get("stop_grace_period") != f"{component['stop_grace_seconds']}s":
            raise ValueError(f"{process_id}: Compose stop grace drift")


def validate(
    contract: dict[str, Any],
    compose: dict[str, Any],
    env: str,
    root: Path = ROOT,
    check_version: bool = True,
    candidate: str | None = None,
) -> dict[str, Any]:
    schema_check(contract, load(root / "deploy/runtime-contracts.schema.json"))
    if contract["authority"]["source"] != "deploy/runtime-contracts.json":
        raise ValueError("runtime contract authority drift")
    if not SEMVER.fullmatch(contract["product_version"]):
        raise ValueError("runtime contract product version is not semver")
    if candidate is not None and candidate != contract["product_version"]:
        raise ValueError("candidate version does not match runtime contract")
    if check_version and contract["product_version"] != (root / "VERSION").read_text(encoding="utf-8").strip():
        raise ValueError("product version mismatch")
    if f'const RuntimeContractVersion = "{contract["contract_version"]}"' not in (root / "componentmetrics/probe.go").read_text(encoding="utf-8"):
        raise ValueError("implementation contract version mismatch")
    env_keys = {line.split("=", 1)[0] for line in env.splitlines() if "=" in line and not line.startswith("#")}
    entries = contract["env_example"]
    if len(entries) != len({entry["key"] for entry in entries}) or {entry["key"] for entry in entries} != env_keys:
        raise ValueError("env example key drift")
    for field in entries:
        if sensitive(field["key"]) and not field["sensitive"]:
            raise ValueError("secret declared public")

    registered = _runtime_definitions(root)
    code_components, code_plugins = _code_catalog(root)
    expected_ids = set(code_components) | {plugin + "-exporter" for plugin in code_plugins}
    components = _component_map(contract)
    if set(components) != expected_ids or set(components) != set(registered) or len(components) != 12:
        raise ValueError("component inventory mismatch")
    if len({component["process_id"] for component in components.values()}) != len(components):
        raise ValueError("duplicate process id")
    ports: set[int] = set()
    compose_services: set[str] = set()
    for component in components.values():
        process_id = component["id"]
        if not (root / component["entrypoint"]).is_file() or not (root / component["module"] / "go.mod").is_file():
            raise ValueError(f"{process_id}: missing process entrypoint or module")
        code = (root / component["entrypoint"]).read_text(encoding="utf-8")
        if process_id.endswith("-exporter"):
            if "componentmetrics.ServeRuntime" not in code:
                raise ValueError(f"{process_id}: exporter runtime adapter missing")
        elif "StartConfiguredWithProbes" not in code:
            raise ValueError(f"{process_id}: private probe adapter missing")
        loader = "\n".join(
            path.read_text(encoding="utf-8")
            for path in (root / component["configuration"]).glob("*.go")
            if not path.name.endswith("_test.go")
        )
        if f'ValidateRuntimeEnvironment("{process_id}")' not in loader and not process_id.endswith("-exporter"):
            raise ValueError(f"{process_id}: typed configuration adapter missing")
        _check_process_shape(component, registered)
        fields = _check_environment(component, env, contract["product_version"])
        for listener in component["listeners"]:
            if listener["port"] in ports:
                raise ValueError(f"{process_id}: duplicate listener port")
            ports.add(listener["port"])
        _check_compose(component, compose, fields)
        compose_services.update(component["compose_services"])

    declared_process_services = {
        name
        for name, service in compose["services"].items()
        if service.get("labels", {}).get("io.gopulse.runtime.process_id")
    }
    if declared_process_services != compose_services:
        raise ValueError("Compose process service inventory mismatch")
    plugin = contract["plugin_manifest"]
    validator = (root / plugin["validator"]).read_text(encoding="utf-8")
    if 'manifest.HealthPath != "/health"' not in validator or 'manifest.MetricsPath != "/metrics"' not in validator:
        raise ValueError("plugin manifest path drift")
    catalog = (root / plugin["catalog"]).read_text(encoding="utf-8")
    if "Port: 9121 + i" not in catalog:
        raise ValueError("plugin listener catalog drift")
    for component in components.values():
        if component["plugin_id"] and '"' + component["id"].removesuffix("-exporter") + '"' not in catalog:
            raise ValueError(f"{component['id']}: plugin omitted")
    return contract


def sha256_file(path: Path) -> str:
    return "sha256:" + hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--contract", type=Path, default=ROOT / "deploy/runtime-contracts.json")
    parser.add_argument("--compose", type=Path, default=ROOT / "deploy/compose.yaml")
    parser.add_argument("--env", type=Path, default=ROOT / ".env.example")
    parser.add_argument("--candidate")
    args = parser.parse_args()
    contract = load(args.contract)
    compose = compose_document(args.compose)
    validate(contract, compose, args.env.read_text(encoding="utf-8"), candidate=args.candidate)
    instances = [
        {
            "process_id": component["process_id"],
            "instances": component["replica"]["instances"],
            "probe": component["diagnostic"]["paths"],
            "independent": component["diagnostic"]["independent"],
            "compose_services": component["compose_services"],
        }
        for component in contract["components"]
    ]
    print(
        json.dumps(
            {
                "schema": "gopulse.runtime-contract-verification.v1",
                "candidate": args.candidate or contract["product_version"],
                "contract_sha256": sha256_file(args.contract),
                "processes": len(contract["components"]),
                "instances": instances,
            },
            sort_keys=True,
        )
    )


if __name__ == "__main__":
    main()
