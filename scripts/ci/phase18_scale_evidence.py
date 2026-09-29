#!/usr/bin/env python3
"""Strictly validate Phase 18-05 closure evidence without selecting a run."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

from phase18_scale_closure import ALLOWED_FILES, EXPECTED_REPETITIONS, TARGET_VERSION, UNIT_NAMES, final_result

SEMVER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")
STATUSES = {"target_met", "boundary_found", "execution_failed"}


def _load(path: Path) -> Any:
    def unique(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise ValueError(f"duplicate JSON key in {path}")
            result[key] = value
        return result

    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique)


def _require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def _relative_file(root: Path, relative: str) -> Path:
    path = root / relative
    _require(path.is_file(), f"evidence file is missing: {relative}")
    return path


def _output_file(root: Path, relative: str) -> Path:
    direct = root / relative
    if direct.is_file():
        return direct
    matches = [path for path in root.rglob(relative) if path.is_file()]
    _require(len(matches) == 1, f"command output is missing or ambiguous: {relative}")
    return matches[0]


def _check_command(root: Path, item: dict[str, Any], location: str) -> None:
    _require(isinstance(item.get("exit_code"), int), f"{location}: command exit code is missing")
    for key in ("stdout", "stderr"):
        value = item.get(key)
        _require(isinstance(value, str) and value, f"{location}: {key} output is missing")
        _output_file(root, value)
    if item["exit_code"] != 0:
        failure = item.get("failure")
        _require(isinstance(failure, dict), f"{location}: failed command has no failure record")
        _require(failure.get("stage") == item.get("name"), f"{location}: failure stage is not bound to the command")
        _require(failure.get("exit_code") == item.get("exit_code"), f"{location}: failure exit code drift")
        _require(failure.get("stdout") == item.get("stdout") and failure.get("stderr") == item.get("stderr"), f"{location}: failure output drift")


def _walk_commands(root: Path, value: Any, location: str) -> list[dict[str, Any]]:
    commands: list[dict[str, Any]] = []
    if isinstance(value, dict):
        if "exit_code" in value:
            _check_command(root, value, location)
            commands.append(value)
        for key, entry in value.items():
            if key not in {"command", "failure"}:
                commands.extend(_walk_commands(root, entry, f"{location}.{key}"))
    elif isinstance(value, list):
        for index, entry in enumerate(value):
            commands.extend(_walk_commands(root, entry, f"{location}[{index}]"))
    return commands


def _validate_binding(root: Path, binding_document: dict[str, Any], expected_candidate: dict[str, Any] | None) -> dict[str, Any]:
    _require(binding_document.get("schema") == "gopulse.phase18.scale-closure-binding.v1", "invalid closure binding schema")
    candidate = binding_document.get("candidate")
    _require(isinstance(candidate, dict), "closure candidate is missing")
    _require(candidate.get("version") == TARGET_VERSION and SEMVER.fullmatch(candidate.get("version", "")), "closure candidate version drift")
    _require(re.fullmatch(r"develop/[0-9]+\.[0-9]+\.[0-9]+", candidate.get("branch", "")) is not None, "closure candidate branch is invalid")
    _require(re.fullmatch(r"[0-9a-f]{40}", candidate.get("revision", "")) is not None, "closure candidate revision is not immutable")
    _require(candidate.get("condition", {}).get("runner") == "scripts/verify-phase18-scale-closure.sh --repetitions 2", "closure runner condition drift")
    _require(candidate.get("condition", {}).get("matrix_order"), "closure matrix order is missing")
    _require(len(candidate["condition"]["matrix_order"]) == 13, "closure matrix order is incomplete")
    _require(candidate.get("condition", {}).get("diagnostics") == "direct-private-probe", "closure diagnostics are not direct")
    ledger = candidate.get("file_ledger")
    _require(isinstance(ledger, list), "candidate file ledger is missing")
    _require([entry.get("path") for entry in ledger] == list(ALLOWED_FILES), "candidate file ledger order drift")
    _require(all(entry.get("status") in {"changed", "not_needed", "deviation"} for entry in ledger), "candidate file ledger status is invalid")
    changed = candidate.get("candidate_files")
    _require(isinstance(changed, list) and all(path in ALLOWED_FILES for path in changed), "candidate changed file set is outside the plan")
    for entry in ledger:
        _require((entry["status"] == "changed") == (entry["path"] in changed), f"candidate file ledger mismatch: {entry['path']}")
    if expected_candidate is not None:
        _require(candidate == expected_candidate, "closure evidence belongs to another candidate")
    return candidate


def _validate_run(root: Path, run_number: int, binding_document: dict[str, Any], candidate: dict[str, Any]) -> dict[str, Any]:
    run_root = root / f"run-{run_number}"
    _require(run_root.is_dir(), f"run-{run_number} evidence is missing")
    run_binding = _load(_relative_file(run_root, "binding.json"))
    _require(run_binding == binding_document, f"run-{run_number} candidate binding drift")
    summary = _load(_relative_file(run_root, "summary.json"))
    _require(summary.get("schema") == "gopulse.phase18.scale-closure-run.v1", f"run-{run_number} summary schema is invalid")
    _require(summary.get("run") == run_number, f"run-{run_number} number drift")
    _require(summary.get("candidate") == candidate, f"run-{run_number} candidate drift")
    units = summary.get("units")
    _require(isinstance(units, dict) and set(units) == set(UNIT_NAMES), f"run-{run_number} unit inventory is incomplete")
    _require(summary.get("status") in STATUSES, f"run-{run_number} status is invalid")
    _require(summary.get("passed") == {name: units[name].get("status") == "target_met" for name in UNIT_NAMES}, f"run-{run_number} pass counts are not derived")
    all_commands = _walk_commands(run_root, units, f"run-{run_number}.units")
    for name in UNIT_NAMES:
        unit = units[name]
        _require(unit.get("status") in STATUSES, f"run-{run_number}.{name} status is invalid")
        if unit.get("status") != "target_met":
            _require(unit.get("runner_error") or unit.get("failure_stage") or any(command["exit_code"] != 0 for command in _walk_commands(run_root, unit, f"run-{run_number}.{name}")), f"run-{run_number}.{name} failure has no evidence")
    u3 = units["U3"]
    if "matrix.json" in {path.name for path in (run_root / "U3").glob("matrix.json")}:
        matrix = _load(run_root / "U3" / "matrix.json")
        _require(matrix.get("run") == run_number, f"run-{run_number}: matrix run number drift")
        _require(matrix.get("status") == u3.get("status"), f"run-{run_number}: matrix status drift")
        _require(matrix.get("cleanup", {}).get("exit_code") is not None, f"run-{run_number}: cleanup result is missing")
    _require(all(command.get("stdout") and command.get("stderr") for command in all_commands), f"run-{run_number}: command output binding is incomplete")
    return summary


def validate_evidence(root: Path, expected_candidate: dict[str, Any] | None = None) -> dict[str, Any]:
    root = root.resolve()
    binding_document = _load(_relative_file(root, "binding.json"))
    candidate = _validate_binding(root, binding_document, expected_candidate)
    run_directories = sorted(path.name for path in root.iterdir() if path.is_dir() and path.name.startswith("run-"))
    _require(run_directories == ["run-1", "run-2"], "closure evidence must contain exactly run-1 and run-2")
    runs = [_validate_run(root, number, binding_document, candidate) for number in (1, 2)]
    summary = _load(_relative_file(root, "summary.json"))
    _require(summary.get("schema") == "gopulse.phase18.scale-closure-summary.v1", "closure summary schema is invalid")
    _require(summary.get("candidate") == candidate, "closure summary candidate drift")
    _require(summary.get("runs") == EXPECTED_REPETITIONS, "closure summary run count is invalid")
    _require(summary.get("run_statuses") == [run["status"] for run in runs], "closure summary statuses are not raw")
    pass_counts = {name: sum(run["passed"][name] for run in runs) for name in UNIT_NAMES}
    _require(summary.get("unit_pass_counts") == pass_counts, "closure summary unit counts are not derived")
    _require(summary.get("deterministic_counts") == {name: f"{pass_counts[name]}/2" for name in UNIT_NAMES}, "closure deterministic counts drift")
    raw_numeric = summary.get("raw_numeric", {})
    averages = summary.get("numeric_averages", {})
    _require(isinstance(raw_numeric, dict) and isinstance(averages, dict), "closure numeric summary is missing")
    for key, values in raw_numeric.items():
        _require(isinstance(values, list) and len(values) == 2, f"closure numeric raw values do not contain two runs: {key}")
        _require(key in averages and averages[key] == round((float(values[0]) + float(values[1])) / 2, 3), f"closure numeric average is incorrect: {key}")
    _require(summary.get("run_summaries") == ["run-1/summary.json", "run-2/summary.json"], "closure run summary references drift")
    _require(summary.get("result") == final_result(run["status"] for run in runs), "closure result classification is incorrect")
    cleanup = summary.get("cleanup")
    _require(isinstance(cleanup, list) and len(cleanup) == 2, "closure cleanup summary is incomplete")
    for item in cleanup:
        _require(item.get("run") in (1, 2) and isinstance(item.get("exit_code"), int), "closure cleanup result is not bound")
    for entry in candidate["file_ledger"]:
        if entry["status"] == "changed":
            _require(entry["path"] in candidate["candidate_files"], f"changed file is absent from candidate set: {entry['path']}")
    return summary


def validate_summary(root: Path, expected_candidate: dict[str, Any] | None = None) -> dict[str, Any]:
    return validate_evidence(root, expected_candidate)


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--closure", type=Path, required=True)
    args = parser.parse_args()
    try:
        summary = validate_evidence(args.closure)
    except Exception as error:
        print(f"Phase 18-05 closure evidence rejected ({type(error).__name__}): {error}")
        raise SystemExit(1)
    print(f"PASS: Phase 18-05 closure evidence, two runs, averages, failures and file ledger ({summary['result']})")
