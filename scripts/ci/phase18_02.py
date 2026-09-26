#!/usr/bin/env python3
"""Run the bounded Phase 18-02 Outbox capacity window."""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import re
import secrets
import subprocess
import sys
import time
from pathlib import Path

from phase18_capacity import (
    ROOT,
    baseline_environment,
    build_loadtest,
    candidate_environment,
    compose,
    compose_override,
    generate_recipe,
    inspect_recipe,
    parse_env,
    prepare_workspace,
    resource_inventory,
    require,
    run_preflight,
    write_env,
    wait_convergence,
)
from phase18_diagnostic import DiagnosticSampler
from phase18_evidence import atomic
from phase18_sampler import load_samples, sha256_file, summarize_samples
from release_artifacts import platform_ref, verify_bundle

EXPECTED_VERSION = "2.0.2"
SEED = 18002005
VALID_PROJECT = re.compile(r"^gopulse-p18-02-[0-9a-f]{12}$")
WINDOW = {
    "warmup_seconds": 60,
    "steady_seconds": 300,
    "burst_seconds": 60,
    "recovery_seconds": 180,
    "steady_target_rps": 150,
    "burst_target_rps": 300,
    "virtual_users": 1024,
}


def sha256_text(value: str) -> str:
    return "sha256:" + hashlib.sha256(value.encode()).hexdigest()


def candidate_binding(manifest_path: Path) -> tuple[dict, dict]:
    manifest = verify_bundle(manifest_path)
    if manifest["version"] != EXPECTED_VERSION:
        raise ValueError("Phase 18-02 requires a 2.0.2 candidate manifest")
    image_digests = {
        name: platform_ref(image, "linux/amd64").split("@", 1)[1]
        for name, image in manifest["images"].items()
    }
    binding = {
        "version": manifest["version"],
        "revision": manifest["revision"],
        "manifest_sha256": sha256_file(manifest_path),
        "bundle_sha256": manifest["bundle_sha256"],
        "image_digests": image_digests,
    }
    return manifest, binding


def utc_timestamp(value: float) -> str:
    return dt.datetime.fromtimestamp(value, dt.timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def parse_timestamp(value: str) -> float:
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()


def project_mysql_query(env_file: Path, compose_file: Path, project: str, query: str) -> str:
    result = compose(
        env_file,
        compose_file,
        project,
        "exec",
        "-T",
        "mysql",
        "sh",
        "-c",
        'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u"$MYSQL_USER" -N -B "$MYSQL_DATABASE" -e "$1"',
        "sh",
        query,
        timeout=30,
    )
    return require(result, "query Outbox state")


def parse_outbox_state(lines: list[str]) -> dict:
    result = {
        "rows": {},
        "total": 0,
        "unique_event_ids": 0,
        "max_attempt_count": 0,
        "total_observed": False,
    }
    for line in lines:
        fields = line.split()
        if not fields:
            continue
        if fields[0] == "status" and len(fields) == 5:
            _, status, count, unique, attempts = fields
            count = int(count)
            if status not in {"pending", "leased", "published"}:
                result["invalid_status_rows"] = result.get("invalid_status_rows", 0) + count
                continue
            result["rows"][status] = {
                "count": count,
                "unique_event_ids": int(unique),
                "max_attempt_count": int(attempts),
            }
            result["max_attempt_count"] = max(result["max_attempt_count"], int(attempts))
        elif fields[0] == "total" and len(fields) == 4:
            _, total, unique, attempts = fields
            result["total"] = int(total)
            result["unique_event_ids"] = int(unique)
            result["max_attempt_count"] = max(result["max_attempt_count"], int(attempts))
            result["total_observed"] = True
    result["invalid_status_rows"] = result.get("invalid_status_rows", 0)
    result["duplicate_event_ids"] = result["total"] - result["unique_event_ids"]
    result["state_check_passed"] = (
        result["total_observed"]
        and result["invalid_status_rows"] == 0
        and result["duplicate_event_ids"] == 0
    )
    return result


def new_outbox_state(env_file: Path, compose_file: Path, project: str, started_at: float) -> dict:
    cutoff = dt.datetime.fromtimestamp(started_at, dt.timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f")
    query = (
        "SELECT CONCAT('status\\t', status, '\\t', COUNT(*), '\\t', COUNT(DISTINCT event_id), '\\t', "
        "COALESCE(MAX(attempt_count), 0)) "
        "FROM business_outbox WHERE created_at >= '" + cutoff + "' "
        "GROUP BY status ORDER BY status;"
        "SELECT CONCAT('total\\t', COUNT(*), '\\t', COUNT(DISTINCT event_id), '\\t', "
        "COALESCE(MAX(attempt_count), 0)) "
        "FROM business_outbox WHERE created_at >= '" + cutoff + "';"
    )
    return parse_outbox_state(project_mysql_query(env_file, compose_file, project, query).splitlines())


def sample_outbox(record: dict) -> dict | None:
    status = ((record.get("mysql") or {}).get("status") or {})
    if not status:
        return None
    pending = sum(int((status.get(name) or {}).get("count", 0)) for name in ("pending", "leased"))
    backend = ((record.get("links") or {}).get("backend") or {})
    oldest = backend.get("gopulse_backend_outbox_oldest_age_seconds")
    if oldest is None:
        return None
    return {"observed_at": float(record["observed_at"]), "pending": pending, "oldest_age_seconds": float(oldest)}


def phase_boundaries(report: dict) -> dict:
    started = parse_timestamp(report["started_at"])
    phases = {item["name"]: item for item in report["phases"]}
    warmup = float(phases["warmup"]["duration_seconds"])
    steady = float(phases["steady"]["duration_seconds"])
    burst = float(phases["burst"]["duration_seconds"])
    steady_start = started + warmup
    burst_start = steady_start + steady
    return {
        "load_started_at": started,
        "steady_start": steady_start,
        "steady_end": burst_start,
        "burst_start": burst_start,
        "burst_end": burst_start + burst,
        "durations": {"warmup": warmup, "steady": steady, "burst": burst},
    }


def phase_counts(report: dict, name: str) -> dict:
    for phase in report.get("phases", []):
        if phase.get("name") == name:
            return phase.get("counts") or {}
    return {}


def accepted_event_requests(report: dict) -> int:
    event_templates = {
        "POST /api/v1/posts",
        "POST /api/v1/posts/:postId/comments",
        "PATCH /api/v1/posts/:postId",
        "DELETE /api/v1/posts/:postId",
        "PUT /api/v1/posts/:postId/like",
        "PUT /api/v1/users/:userId/follow",
    }
    return sum(
        int((route.get("counts") or {}).get("succeeded", 0))
        for template, route in (report.get("routes") or {}).items()
        if template in event_templates
    )


def evaluate_window(
    report: dict,
    records: list[dict],
    resources: dict,
    event_state: dict,
    claim_batch: int,
    recovery_started_at: float,
) -> dict:
    boundaries = phase_boundaries(report)
    samples = [sample_outbox(record) for record in records]
    samples = [sample for sample in samples if sample is not None]
    steady_samples = [
        sample for sample in samples
        if boundaries["steady_start"] <= sample["observed_at"] <= boundaries["steady_end"]
    ]
    last_two = [
        sample for sample in steady_samples
        if sample["observed_at"] >= boundaries["steady_end"] - 120
    ]
    burst_start = next(
        (sample for sample in samples if sample["observed_at"] >= boundaries["burst_start"]), None
    )
    recovery_samples = [sample for sample in samples if sample["observed_at"] >= recovery_started_at]
    steady_start = steady_samples[0] if steady_samples else None
    steady_end = steady_samples[-1] if steady_samples else None
    recovery_end = recovery_samples[-1] if recovery_samples else None
    steady_counts = phase_counts(report, "steady")
    burst_counts = phase_counts(report, "burst")
    steady_failures = int(steady_counts.get("timeouts", 0)) + int(steady_counts.get("errors", 0))
    steady_requests = max(int(steady_counts.get("requests", 0)), 1)
    burst_requests = max(int(burst_counts.get("requests", 0)), 1)
    burst_rejects = int(burst_counts.get("explicit_rejects", 0))

    checks = {
        "steady_samples_available": bool(steady_start and steady_end and last_two),
        "steady_oldest_age_bound": bool(last_two) and max(
            sample["oldest_age_seconds"] for sample in last_two
        ) <= 30,
        "steady_pending_bound": bool(steady_start and steady_end)
        and steady_end["pending"] <= steady_start["pending"] + claim_batch,
        "recovery_sample_available": bool(burst_start and recovery_end),
        "recovery_pending_bound": bool(burst_start and recovery_end)
        and recovery_end["pending"] <= burst_start["pending"] + claim_batch,
        "steady_error_rate_bound": steady_failures / steady_requests <= 0.01,
        "burst_explicit_reject_bound": burst_rejects / burst_requests <= 0.05,
        "load_schedule_bound": all(
            int(phase.get("dropped_slots", 0)) <= int(phase.get("scheduled_slots", 0) * 0.001)
            for phase in report.get("phases", [])
        ),
        "no_oom": int(resources.get("oom_killed", 0)) == 0,
        "swap_bound": int(resources.get("max_swap_delta_bytes", 0)) <= 256 * 1024 * 1024,
        "event_state_check": bool(event_state.get("state_check_passed")),
    }
    return {
        "passed": all(checks.values()),
        "checks": checks,
        "boundaries": boundaries,
        "steady": {
            "requests": int(steady_counts.get("requests", 0)),
            "unexpected_failures": steady_failures,
            "unexpected_failure_rate": steady_failures / steady_requests,
            "samples": len(steady_samples),
            "last_two_samples": len(last_two),
            "start": steady_start,
            "end": steady_end,
            "max_oldest_age_seconds": max((sample["oldest_age_seconds"] for sample in last_two), default=None),
        },
        "burst": {
            "requests": int(burst_counts.get("requests", 0)),
            "explicit_rejects": burst_rejects,
            "explicit_reject_rate": burst_rejects / burst_requests,
            "start": burst_start,
        },
        "recovery": {"started_at": recovery_started_at, "end": recovery_end},
        "event_state": event_state,
        "accepted_event_requests": accepted_event_requests(report),
        "claim_batch": claim_batch,
    }


def redacted_recipe(receipt: dict) -> dict:
    return {
        key: receipt[key]
        for key in ("schema_version", "seed", "counts", "id_ranges", "digest")
        if key in receipt
    }


def build_summary(
    binding: dict,
    receipt: dict | None,
    report: dict | None,
    diagnostic: Path,
    raw_samples: Path,
    resources: dict | None,
    evaluation: dict | None,
    cleanup: dict,
    failure: BaseException | None = None,
) -> dict:
    summary = {
        "schema": "gopulse.phase18.outbox-capacity.v1",
        "execution_status": "failed" if failure or not (evaluation and evaluation["passed"]) else "passed",
        "complete": not failure and bool(evaluation and evaluation["passed"]),
        "candidate": binding,
        "window": dict(WINDOW),
        "recipe": redacted_recipe(receipt) if receipt else None,
        "load": {
            "report_sha256": sha256_file(report_path) if (report_path := diagnostic.parent / "load-report.json").is_file() else None,
            "diagnostic_sha256": sha256_file(diagnostic) if diagnostic.is_file() else None,
            "phases": [
                {
                    "name": phase.get("name"),
                    "requests": (phase.get("counts") or {}).get("requests", 0),
                    "succeeded": (phase.get("counts") or {}).get("succeeded", 0),
                    "explicit_rejects": (phase.get("counts") or {}).get("explicit_rejects", 0),
                    "timeouts": (phase.get("counts") or {}).get("timeouts", 0),
                    "errors": (phase.get("counts") or {}).get("errors", 0),
                    "dropped_slots": phase.get("dropped_slots", 0),
                    "scheduled_slots": phase.get("scheduled_slots", 0),
                }
                for phase in (report or {}).get("phases", [])
            ],
        },
        "resources": resources,
        "evaluation": evaluation,
        "samples": {
            "raw_sha256": sha256_file(raw_samples) if raw_samples.is_file() else None,
            "raw_records": len(load_samples(raw_samples)) if raw_samples.is_file() else 0,
        },
        "cleanup": cleanup,
    }
    if failure:
        summary["failure"] = {"type": type(failure).__name__}
    return summary


def run_window(manifest_path: Path, work: Path) -> dict:
    preflight = run_preflight(work)
    manifest, binding = candidate_binding(manifest_path)
    preflight["candidate"] = binding
    atomic(work / "evidence" / "preflight.json", preflight)
    if preflight["problems"]:
        raise RuntimeError("reference-host preflight failed: " + "; ".join(preflight["problems"]))

    summary_path = work / "evidence" / "phase18-02-summary.json"
    if summary_path.exists():
        raise ValueError("Phase 18-02 summary already exists; refusing to rerun this candidate")
    manifest["compose"]["path"] = str(manifest_path.parent / manifest["compose"]["path"])
    lock = prepare_workspace(work, binding)
    recipe_binary, load_binary = build_loadtest(work)
    recipe_inspect = inspect_recipe(recipe_binary, binding, work / "recipe-inspect-1.json")
    recipe_inspect_repeat = inspect_recipe(recipe_binary, binding, work / "recipe-inspect-2.json")
    if any(
        recipe_inspect.get(key) != recipe_inspect_repeat.get(key)
        for key in ("digest", "counts", "id_ranges")
    ):
        raise RuntimeError("two recipe inspections were not deterministic")
    output = work / "window"
    output.mkdir(parents=True, exist_ok=True, mode=0o700)
    environment = candidate_environment(manifest, output / "candidate.env", 1)
    recipe_environment = dict(environment)
    recipe_environment.update(
        {
            "OUTBOX_POLL_INTERVAL": "10ms",
            "OUTBOX_CLAIM_BATCH": "100",
            "OUTBOX_LEASE_DURATION": "10m",
            "BUSINESS_WORKER_PREFETCH": "100",
            "SEARCH_INDEXER_PREFETCH": "100",
        }
    )
    recipe_env_file = output / "recipe.env"
    write_env(recipe_env_file, recipe_environment)
    baseline_env_file = output / "baseline.env"
    baseline_environment(environment, baseline_env_file)
    override = output / "compose.override.yaml"
    compose_override(override, int(environment["MYSQL_PORT"]))
    project = "gopulse-p18-02-" + secrets.token_hex(6)
    if not VALID_PROJECT.fullmatch(project):
        raise RuntimeError("generated unsafe Phase 18-02 project name")
    project_hash = sha256_text(project)
    before = resource_inventory()
    compose_path = Path(manifest["compose"]["path"])
    recipe_dir = output / "recipe"
    recipe_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    sampler = None
    load_process = None
    report = None
    receipt = None
    resources = None
    evaluation = None
    failure = None
    cleanup = {"attempted": False, "status": "not_started", "project": project, "project_sha256": project_hash}
    raw_samples = output / "resources.raw.jsonl"
    diagnostic = output / "load-diagnostic.json"
    try:
        require(
            compose(
                recipe_env_file,
                compose_path,
                project,
                "-f",
                str(override),
                "up",
                "-d",
                "--wait",
                "--wait-timeout",
                "900",
                timeout=1000,
            ),
            "start isolated Phase 18-02 project",
        )
        receipt = generate_recipe(recipe_binary, binding, recipe_environment, recipe_dir, int(environment["MYSQL_PORT"]))
        require(
            compose(
                recipe_env_file,
                compose_path,
                project,
                "run",
                "--rm",
                "--no-deps",
                "--entrypoint",
                "/usr/local/bin/search-reindex",
                "search-init",
                timeout=1800,
            ),
            "run formal search reindex",
        )
        wait_convergence(recipe_env_file, compose_path, project, timeout=600)
        require(
            compose(
                baseline_env_file,
                compose_path,
                project,
                "up",
                "-d",
                "--force-recreate",
                "--wait",
                "--wait-timeout",
                "900",
                "backend",
                "business-worker",
                "search-indexer",
                timeout=1000,
            ),
            "restart candidate with baseline Outbox configuration",
        )
        claim_batch = int(parse_env(baseline_env_file).get("OUTBOX_CLAIM_BATCH", "10"))
        sampler = DiagnosticSampler(project, compose_path, baseline_env_file, interval=5, raw_path=raw_samples)
        sampler.start()
        load_started_at = time.time()
        load_process = subprocess.Popen(
            [
                str(load_binary),
                "--base-url",
                "http://127.0.0.1:" + environment["FRONTEND_PORT"],
                "--corpus",
                str(recipe_dir / "corpus.json"),
                "--credentials",
                str(recipe_dir / "credentials.json"),
                "--report",
                str(output / "load-report.json"),
                "--diagnostic-report",
                str(diagnostic),
                "--vus",
                str(WINDOW["virtual_users"]),
                "--warmup",
                str(WINDOW["warmup_seconds"]) + "s",
                "--steady",
                str(WINDOW["steady_seconds"]) + "s",
                "--burst",
                str(WINDOW["burst_seconds"]) + "s",
                "--steady-rps",
                str(WINDOW["steady_target_rps"]),
                "--burst-rps",
                str(WINDOW["burst_target_rps"]),
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        sampler.set_load_pid(load_process.pid)
        try:
            stdout, stderr = load_process.communicate(timeout=600)
        except subprocess.TimeoutExpired:
            load_process.kill()
            stdout, stderr = load_process.communicate()
            raise RuntimeError("short capacity load exceeded its fixed 10-minute window")
        load_returncode = load_process.returncode
        load_process = None
        if load_returncode:
            raise RuntimeError("short capacity load failed: " + (stderr or stdout)[-300:])
        if not (output / "load-report.json").is_file():
            raise RuntimeError("short capacity load did not produce a report")
        report = json.loads((output / "load-report.json").read_text())
        recovery_started_at = time.time()
        recovery_deadline = time.monotonic() + WINDOW["recovery_seconds"]
        while time.monotonic() < recovery_deadline:
            recent = [sample_outbox(record) for record in sampler.records]
            recent = [sample for sample in recent if sample is not None and sample["observed_at"] >= recovery_started_at]
            if recent and recent[-1]["pending"] <= claim_batch:
                time.sleep(5)
                recent = [sample_outbox(record) for record in sampler.records]
                recent = [sample for sample in recent if sample is not None and sample["observed_at"] >= recovery_started_at]
                if recent and recent[-1]["pending"] <= claim_batch:
                    break
            time.sleep(5)
        sampler.stop()
        sampler = None
        records, resources = summarize_samples(raw_samples, output / "resources.json")
        event_state = new_outbox_state(baseline_env_file, compose_path, project, load_started_at)
        evaluation = evaluate_window(report, records, resources, event_state, claim_batch, recovery_started_at)
        if not evaluation["passed"]:
            raise RuntimeError("Phase 18-02 short capacity gates failed")
    except BaseException as error:
        failure = error
    finally:
        if load_process is not None and load_process.poll() is None:
            load_process.kill()
            load_process.communicate()
        if sampler is not None:
            try:
                sampler.stop()
            except BaseException as error:
                if failure is None:
                    failure = error
        cleanup["attempted"] = True
        cleanup_result = compose(
            recipe_env_file,
            compose_path,
            project,
            "-f",
            str(override),
            "down",
            "--volumes",
            "--remove-orphans",
            "--timeout",
            "30",
            timeout=300,
        )
        if cleanup_result.returncode:
            cleanup["status"] = "failed"
            cleanup["residual_project"] = project
            if failure is None:
                failure = RuntimeError("owned Phase 18-02 project cleanup failed")
        else:
            try:
                after = resource_inventory()
                if after != before:
                    raise RuntimeError("owned project cleanup changed unrelated Docker resources")
                cleanup["status"] = "passed"
            except BaseException as error:
                cleanup["status"] = "failed"
                cleanup["residual_project"] = project
                if failure is None:
                    failure = error

        if report is None and (output / "load-report.json").is_file():
            try:
                report = json.loads((output / "load-report.json").read_text())
            except json.JSONDecodeError:
                pass
        if resources is None and raw_samples.is_file():
            try:
                _, resources = summarize_samples(raw_samples, output / "resources.json")
            except BaseException:
                pass
        summary = build_summary(binding, receipt, report, diagnostic, raw_samples, resources, evaluation, cleanup, failure)
        summary["recipe_inspect_digest"] = recipe_inspect.get("digest")
        atomic(summary_path, summary)

    if failure is not None:
        raise failure
    return summary


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--preflight-only", action="store_true")
    args = parser.parse_args()
    work = args.work.resolve()
    try:
        if args.preflight_only:
            document = run_preflight(work)
            manifest, binding = candidate_binding(args.manifest.resolve())
            document["candidate"] = binding
            atomic(work / "evidence" / "preflight.json", document)
            print(json.dumps({"candidate": binding, "host": document["host"], "problems": document["problems"]}, sort_keys=True))
            return 1 if document["problems"] else 0
        summary = run_window(args.manifest.resolve(), work)
        print(json.dumps({"status": summary["execution_status"], "cleanup": summary["cleanup"]["status"], "summary": str(work / "evidence" / "phase18-02-summary.json")}, sort_keys=True))
        return 0
    except BaseException as error:
        work.mkdir(parents=True, exist_ok=True, mode=0o700)
        error_path = work / "run-error.txt"
        error_path.write_text(type(error).__name__ + ": " + str(error) + "\n")
        error_path.chmod(0o600)
        print("Phase 18-02 execution failed (details are in the private work directory)", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
