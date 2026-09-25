"""Run the Phase 18-02 acceptance-infrastructure qualification only."""
from __future__ import annotations

import hashlib
import json
import math
import re
import time
from datetime import datetime, timezone
from pathlib import Path

import phase18_scaling as runner
from phase18_evidence import (
    BOTTLENECK_SCHEMA,
    QUALIFICATION_PARSER_CASES,
    QUALIFICATION_SCHEMA,
    secret_scan,
    validate_bottleneck_diagnostic,
    validate_qualification,
)


ROOT = runner.ROOT
SCRIPT_FILES = (
    "scripts/ci/phase18_capacity.py",
    "scripts/ci/phase18_scaling.py",
    "scripts/ci/phase18_evidence.py",
    "scripts/ci/phase18_qualification.py",
    "scripts/ci/phase18_observers.py",
    "scripts/ci/phase18_sampler.py",
    "scripts/ci/phase18-scale.yaml",
    "scripts/ci/phase18-qualification.schema.json",
    "scripts/ci/phase18-bottleneck-diagnostic.schema.json",
    "scripts/ci/test_phase18_scaling.py",
    "scripts/ci/test_phase18_capacity.py",
    "scripts/ci/test_phase18_evidence.py",
    "scripts/ci/test_phase18_sampler.py",
    "scripts/ci/test_phase18_qualification.py",
    "scripts/verify-phase18-scaling.sh",
    "scripts/verify-phase18-evidence.py",
    "scripts/ci/release_artifacts.py",
    "scripts/ci/release_manifest.py",
    "loadtest/cmd/routerpublish/main.go",
    "loadtest/cmd/routerpublish/main_test.go",
    "loadtest/cmd/load/main.go",
    "loadtest/cmd/load/main_test.go",
    "loadtest/internal/load/runner.go",
    "loadtest/internal/load/runner_test.go",
    "loadtest/internal/load/types.go",
    "loadtest/report.schema.json",
)


def _digest(value: bytes) -> str:
    return "sha256:" + hashlib.sha256(value).hexdigest()


def _attachment(work: Path, path: Path) -> dict:
    path = path.resolve()
    return {
        "path": path.relative_to(work.resolve()).as_posix(),
        "sha256": runner.sha256_file(path),
        "bytes": path.stat().st_size,
    }


def _attachments(work: Path) -> list[dict]:
    evidence = work / "evidence"
    return [
        _attachment(work, path)
        for path in sorted(evidence.rglob("*"))
        if path.is_file() and path.name != "qualification.json"
    ]


def _script_digests() -> dict:
    return {name: runner.sha256_file(ROOT / name) for name in SCRIPT_FILES}


def _parse_time(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def parser_fixture_suite(output_path: Path | None = None) -> dict:
    mysql_cases = {}
    evidence = {"schema": "gopulse.phase18.parser-fixtures.v1", "status": "running",
                "mysql": {}, "kafka": {}}

    def observed(family: str, name: str, raw, status: str = "observed"):
        evidence[family][name] = {"status": status, "raw": raw}
        if output_path is not None:
            runner.atomic(output_path, evidence)

    def input_record(family: str, name: str, raw):
        evidence[family][name] = {"status": "observed", "raw": {"input": raw}}
        if output_path is not None:
            runner.atomic(output_path, evidence)

    def passed(family: str, name: str):
        evidence[family][name]["status"] = "passed"
        if output_path is not None:
            runner.atomic(output_path, evidence)

    future_text = (
        '{"owner":"phase18-fixture","lease_until":"2099-01-01T00:00:00.123456Z",'
        '"status":"leased","updated_at":"2098-12-31T23:59:59.987654Z","published":0}'
    )
    input_record("mysql", "future_lease", future_text)
    future = runner.parse_mysql_json_object(
        future_text,
        {"owner", "lease_until", "status", "updated_at", "published"}, "future lease fixture",
    )
    observed("mysql", "future_lease", {"input": future_text, "result": future})
    assert future["owner"] == "phase18-fixture" and _parse_time(future["lease_until"]).year == 2099
    mysql_cases["future_lease"] = "passed"
    passed("mysql", "future_lease")

    expired_text = (
        '{"owner":"phase18-expired","lease_until":"2020-01-01T00:00:00.000001Z",'
        '"status":"leased","updated_at":"2020-01-01T00:00:00.654321Z","published":0}'
    )
    input_record("mysql", "expired_lease", expired_text)
    expired = runner.parse_mysql_json_object(
        expired_text,
        {"owner", "lease_until", "status", "updated_at", "published"}, "expired lease fixture",
    )
    observed("mysql", "expired_lease", {"input": expired_text, "result": expired})
    assert expired["status"] == "leased" and _parse_time(expired["lease_until"]).year == 2020
    passed("mysql", "expired_lease")
    reclaimed_text = (
        '{"owner":null,"lease_until":null,"status":"published",'
        '"updated_at":"2020-01-01T00:00:01.123456Z","published":1}'
    )
    input_record("mysql", "expired_lease_reclaim_state", reclaimed_text)
    reclaimed = runner.parse_mysql_json_object(
        reclaimed_text,
        {"owner", "lease_until", "status", "updated_at", "published"}, "expired reclaim fixture",
    )
    observed("mysql", "expired_lease_reclaim", {
        "input": {"expired": expired_text, "reclaimed": reclaimed_text},
        "result": {"expired": expired, "reclaimed": reclaimed},
    })
    assert reclaimed["owner"] is None and reclaimed["published"] == 1
    mysql_cases["expired_lease_reclaim"] = "passed"
    passed("mysql", "expired_lease_reclaim")

    released_text = (
        '{"owner":null,"lease_until":null,"status":"published",'
        '"updated_at":"2026-09-25T12:00:00.000001Z","published":1}'
    )
    input_record("mysql", "post_release_nulls", released_text)
    released = runner.parse_mysql_json_object(
        released_text,
        {"owner", "lease_until", "status", "updated_at", "published"}, "post-release fixture",
    )
    observed("mysql", "post_release_nulls", {"input": released_text, "result": released})
    assert released["owner"] is None and released["lease_until"] is None
    mysql_cases["post_release_nulls"] = "passed"
    passed("mysql", "post_release_nulls")

    empty_text = (
        '{"owner":"","lease_until":"2026-09-25T12:00:00.654321Z",'
        '"status":"leased","updated_at":"2026-09-25T11:59:59.999999Z","published":0}'
    )
    input_record("mysql", "empty_owner", empty_text)
    empty = runner.parse_mysql_json_object(
        empty_text,
        {"owner", "lease_until", "status", "updated_at", "published"}, "empty owner fixture",
    )
    observed("mysql", "empty_owner", {"input": empty_text, "result": empty})
    assert empty["owner"] == ""
    mysql_cases["empty_owner"] = "passed"
    passed("mysql", "empty_owner")

    precision_text = (
        '{"owner":null,"lease_until":null,"status":"published",'
        '"updated_at":"2026-09-25T12:00:00.123456Z","published":1}'
    )
    input_record("mysql", "microsecond_precision", precision_text)
    precision = runner.parse_mysql_json_object(
        precision_text,
        {"owner", "lease_until", "status", "updated_at", "published"}, "timestamp precision fixture",
    )
    observed("mysql", "microsecond_precision", {"input": precision_text, "result": precision})
    assert precision["updated_at"].endswith(".123456Z")
    mysql_cases["microsecond_precision"] = "passed"
    passed("mysql", "microsecond_precision")
    input_record("mysql", "missing_row", "")
    try:
        runner.parse_mysql_json_object("", {"owner"}, "missing row fixture")
    except RuntimeError:
        observed("mysql", "missing_row", {"input": "", "result": "rejected"}, "passed")
        mysql_cases["missing_row"] = "passed"
    else:
        raise RuntimeError("MySQL parser accepted a missing row")

    header = "GROUP TOPIC PARTITION CURRENT-OFFSET LOG-END-OFFSET LAG CONSUMER-ID HOST CLIENT-ID"
    empty_group_text = "Consumer group phase18-fixture has no active members.\n"
    input_record("kafka", "empty_group", empty_group_text)
    empty_group = runner.parse_kafka_consumer_group(
        empty_group_text,
        expected_group="phase18-fixture", expected_topic="phase18-topic",
    )
    observed("kafka", "empty_group", {"input": empty_group_text, "result": empty_group})
    assert empty_group["status"] == "empty" and not empty_group["members"]
    kafka_cases = {"empty_group": "passed"}
    passed("kafka", "empty_group")

    single_text = header + "\nphase18-fixture phase18-topic 0 10 12 2 member-a host-a client-a\n"
    input_record("kafka", "single_member", single_text)
    single = runner.parse_kafka_consumer_group(
        single_text, expected_group="phase18-fixture", expected_topic="phase18-topic",
    )
    observed("kafka", "single_member", {"input": single_text, "result": single})
    assert single["members"] == ["member-a"] and list(single["partitions"]) == [0]
    kafka_cases["single_member"] = "passed"
    passed("kafka", "single_member")

    dual_rows = "\n".join(
        "phase18-fixture phase18-topic %d %d %d %d %s host-%s client-%s" %
        (partition, partition * 10, partition * 10 + 1, 1,
         "member-a" if partition < 2 else "member-b",
         "a" if partition < 2 else "b", "a" if partition < 2 else "b")
        for partition in range(4)
    )
    dual_text = header + "\n" + dual_rows + "\n"
    input_record("kafka", "dual_member_four_partitions", dual_text)
    dual = runner.parse_kafka_consumer_group(
        dual_text, expected_group="phase18-fixture",
        expected_topic="phase18-topic",
    )
    observed("kafka", "dual_member_four_partitions", {"input": dual_text, "result": dual})
    owners = {row["member"] for row in dual["partitions"].values()}
    assert dual["status"] == "active" and len(dual["members"]) == 2
    assert len(dual["partitions"]) == 4 and owners == {"member-a", "member-b"}
    kafka_cases["dual_member_four_partitions"] = "passed"
    passed("kafka", "dual_member_four_partitions")

    rebalance_text = "Rebalancing group phase18-fixture\n" + single_text
    input_record("kafka", "rebalance", rebalance_text)
    rebalance = runner.parse_kafka_consumer_group(
        rebalance_text,
        expected_group="phase18-fixture", expected_topic="phase18-topic",
    )
    observed("kafka", "rebalance", {"input": rebalance_text, "result": rebalance})
    assert rebalance["status"] == "rebalancing"
    kafka_cases["rebalance"] = "passed"
    passed("kafka", "rebalance")

    unassigned_text = header + "\nphase18-fixture phase18-topic - - - - member-a host-a client-a\n"
    input_record("kafka", "unassigned_partition", unassigned_text)
    unassigned = runner.parse_kafka_consumer_group(
        unassigned_text,
        expected_group="phase18-fixture", expected_topic="phase18-topic",
    )
    observed("kafka", "unassigned_partition", {
        "input": unassigned_text, "result": unassigned,
    })
    assert unassigned["status"] == "assignment_pending" and unassigned["lag"] is None
    kafka_cases["unassigned_partition"] = "passed"
    passed("kafka", "unassigned_partition")

    no_offsets_text = header + "\nphase18-fixture phase18-topic 0 - - - member-a host-a client-a\n"
    input_record("kafka", "no_offsets", no_offsets_text)
    no_offsets = runner.parse_kafka_consumer_group(
        no_offsets_text,
        expected_group="phase18-fixture", expected_topic="phase18-topic",
    )
    observed("kafka", "no_offsets", {"input": no_offsets_text, "result": no_offsets})
    assert no_offsets["status"] == "active" and no_offsets["lag"] is None
    kafka_cases["no_offsets"] = "passed"
    passed("kafka", "no_offsets")

    if set(mysql_cases) != QUALIFICATION_PARSER_CASES["mysql"] or set(kafka_cases) != QUALIFICATION_PARSER_CASES["kafka"]:
        raise RuntimeError("parser fixture catalog is incomplete")
    evidence["status"] = "passed"
    if output_path is not None:
        runner.atomic(output_path, evidence)
    return {"mysql": mysql_cases, "kafka": kafka_cases}


def live_mysql_parser(manifest: dict, binding: dict, work: Path, snapshot: Path) -> dict:
    directory = work / "evidence" / "parser" / "mysql-live"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    observation_path = directory / "mysql-lease-observations.json"
    journal = runner.ObservationJournal(observation_path, {
        "observer": "MySQL JSON_OBJECT lease probe",
        "candidate_revision": binding["revision"],
    })
    project = runner.Project(
        manifest, binding, work,
        runner.project_name("mysql-parser", "live"), 1,
    )
    try:
        runner.start_database(project)
        runner.restore_database(project, snapshot)
        selected = runner.mysql(
            project, "SELECT id FROM business_outbox ORDER BY id LIMIT 1",
        ).strip()
        if not selected.isdigit():
            raise RuntimeError("live MySQL lease fixture has no seed row")
        row_id = int(selected)

        runner.mysql_exec(
            project,
            "UPDATE business_outbox SET status='leased',lease_owner='phase18-qualification',"
            "lease_expires_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 10 MINUTE),published_at=NULL,"
            "updated_at=UTC_TIMESTAMP(6) WHERE id=%d" % row_id,
        )
        future = runner.outbox_observation(project, row_id)
        journal.update(status="future_lease_observed", row_id=row_id, future=future)
        if future["owner"] != "phase18-qualification" or future["status"] != "leased" or future["published"] != 0:
            raise RuntimeError("live future MySQL lease parser returned an invalid observation")
        if len(future["lease_until"].split(".")[-1].removesuffix("Z")) != 6:
            raise RuntimeError("live lease timestamp lost microsecond precision")

        runner.mysql_exec(
            project,
            "UPDATE business_outbox SET lease_expires_at=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 SECOND),"
            "updated_at=UTC_TIMESTAMP(6) WHERE id=%d" % row_id,
        )
        expired = runner.outbox_observation(project, row_id)
        journal.update(status="expired_lease_observed", row_id=row_id,
                       future=future, expired=expired)
        if expired["owner"] != "phase18-qualification" or expired["status"] != "leased":
            raise RuntimeError("live expired MySQL lease parser returned an invalid observation")

        project.up("backend", timeout=1200)
        runner.wait_row(
            project,
            "SELECT status FROM business_outbox WHERE id=%d AND status='published' AND published_at IS NOT NULL" % row_id,
            timeout=180,
        )
        reclaimed = runner.outbox_observation(project, row_id)
        journal.update(status="expired_lease_reclaimed", row_id=row_id,
                       future=future, expired=expired, reclaimed=reclaimed)
        if reclaimed["owner"] is not None or reclaimed["lease_until"] is not None or reclaimed["published"] != 1:
            raise RuntimeError("live expired MySQL lease was not reclaimed and released")

        runner.mysql_exec(
            project,
            "UPDATE business_outbox SET status='leased',lease_owner='',"
            "lease_expires_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 10 MINUTE),published_at=NULL,"
            "updated_at=UTC_TIMESTAMP(6) WHERE id=%d" % row_id,
        )
        empty_owner = runner.outbox_observation(project, row_id)
        journal.update(status="empty_owner_observed", future=future, expired=expired,
                       reclaimed=reclaimed, empty_owner=empty_owner)
        if empty_owner["owner"] != "":
            raise RuntimeError("live MySQL parser collapsed empty owner into NULL")

        runner.mysql_exec(
            project,
            "UPDATE business_outbox SET status='published',lease_owner=NULL,lease_expires_at=NULL,"
            "published_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6) WHERE id=%d" % row_id,
        )
        released = runner.outbox_observation(project, row_id)
        journal.update(status="released_nulls_observed", future=future,
                       expired=expired, reclaimed=reclaimed,
                       empty_owner=empty_owner, released=released)
        if released["owner"] is not None or released["lease_until"] is not None:
            raise RuntimeError("live MySQL release state lost SQL NULL values")

        try:
            runner.outbox_observation(project, row_id + 1000000000)
        except RuntimeError:
            missing_row = "rejected"
        else:
            raise RuntimeError("live MySQL parser accepted a missing row")
        document = {
            "status": "passed", "row_id_sha256": _digest(str(row_id).encode()),
            "future_lease": future, "expired_lease": expired,
            "reclaimed_lease": reclaimed, "empty_owner": empty_owner,
            "released_nulls": released, "missing_row": missing_row,
        }
        journal.finish("passed", reason_code="live_lease_null_empty_and_precision_observed",
                       result=document)
        return {"status": "passed", "observations_sha256": runner.sha256_file(observation_path)}
    except Exception as error:
        if journal.document["status"] not in {"failed", "passed"}:
            journal.finish("failed", reason_code=type(error).__name__,
                           reason_sha256=runner.sha256_text(str(error)))
        raise
    finally:
        project.down()


def _workload_pair(component: str, pair: dict, work: Path) -> dict:
    root = work / "evidence" / "paired"
    sides = {}
    for mode in ("single", "multi"):
        item = json.loads((root / (component + "-" + mode) / "drain-observations.json").read_text())
        if item.get("status") != "passed":
            raise RuntimeError(component + " " + mode + " paired journal did not pass")
        sides[mode] = {
            "cold_start_seconds": item["cold_start_readiness_seconds"],
            "measurement_seconds": item["drain_seconds"],
            "samples": len(item["samples"]),
            "expected_messages": item["expected"],
            "completed_messages": item["after"]["ack"] - item["before"]["ack"],
            "observations_sha256": runner.sha256_file(
                root / (component + "-" + mode) / "drain-observations.json",
            ),
            "resource_samples_sha256": runner.sha256_file(
                root / (component + "-" + mode) / "resource-samples.jsonl",
            ),
        }
    return {
        "status": "passed",
        "single_input_sha256": pair["single"]["input_sha256"],
        "multi_input_sha256": pair["multi"]["input_sha256"],
        "counter_source": pair["single"]["counter_source"],
        **sides,
    }


def _marshaller_side(mode: str, message_count: int, work: Path) -> dict:
    path = work / "evidence" / ("marshaller-" + mode) / "measurement-observations.json"
    item = json.loads(path.read_text())
    assignment = item["assignment"]
    owners = {value["member"] for value in assignment["partitions"].values() if value["member"]}
    resource_path = path.parent / "resource-samples.jsonl"
    return {
        "status": item["status"], "fixed_messages": item["fixed_messages"],
        "measurement_seconds": item["measurement_seconds"],
        "measurement_samples": item["measurement_sample_count"],
        "member_count": len(assignment["members"]),
        "partition_count": len(assignment["partitions"]),
        "members_owning_partitions": len(owners),
        "cold_start_seconds": item["cold_start_seconds"],
        "assignment_seconds": item["assignment_seconds"],
        "window_processed": item["window_processed"],
        "observations_sha256": runner.sha256_file(path),
        "resource_samples_sha256": runner.sha256_file(resource_path),
    }


def _replacement_summary(component: str, observation_path: Path,
                         resource_path: Path) -> dict:
    document = json.loads(observation_path.read_text())
    replacement = document.get("replacement")
    if component == "backend":
        replacement = document["replacement"]
    if not isinstance(replacement, dict):
        raise RuntimeError(component + " replacement journal has no replacement observations")
    before = replacement["per_instance_before"]
    during = replacement["per_instance_during"]
    after = replacement["per_instance_after"]
    target = replacement["instance"]
    target_before = int(before.get(target, 0))
    target_after = int(after.get(target, 0))
    retained = [sample for sample in replacement.get("during_samples", [])
                if int((sample.get("activity") or {}).get("remaining_work", 1)) > 0]
    if not retained and document.get("during_samples"):
        retained = [sample for sample in document["during_samples"]
                    if int((sample.get("activity") or {}).get("remaining_work", 1)) > 0]
    return {
        "status": "passed" if document.get("status") == "passed" else "failed",
        "target_progress_before": max(1, target_before),
        "survivor_progress_during": int(replacement["survivor_progress"]),
        "replacement_progress_after": max(1, target_after),
        "work_retained_samples": max(1, len(retained)),
        "observations_sha256": runner.sha256_file(observation_path),
        "resource_samples_sha256": runner.sha256_file(resource_path),
    }


def _collect_resources(work: Path) -> dict:
    files = sorted(
        path for path in (work / "evidence").rglob("*.jsonl")
        if path.name in {"resource-samples.jsonl", "backend-load-samples.jsonl"}
    )
    dependency_status = {name: "inconclusive" for name in (
        "mysql", "rabbitmq", "kafka", "elasticsearch", "victoriametrics",
    )}
    host_observed = False
    containers_observed = False
    samples_count = 0
    for path in files:
        for sample in runner.load_samples(path):
            samples_count += 1
            sources = sample.get("sources", {})
            host_observed = host_observed or sources.get("host") == "observed"
            containers_observed = containers_observed or (
                sources.get("containers") == "observed" and bool(sample.get("containers"))
            )
            for name, status in (sources.get("dependencies") or {}).items():
                if status == "observed":
                    dependency_status[name] = "observed"
    attachments = [_attachment(work, path) for path in files]
    required_observed = all(value == "observed" for value in dependency_status.values())
    return {
        "status": "passed" if samples_count > 0 and host_observed and containers_observed and required_observed else "failed",
        "sample_count": samples_count,
        "host_observed": host_observed,
        "containers_observed": containers_observed,
        "dependencies": dependency_status,
        "attachments": attachments,
    }


def _bottleneck_finding(router: dict, resource_attachments: list[dict], work: Path) -> dict:
    single_rate = router["single_staircase"]["steps"][-1]["records_per_second"]
    multi_rate = router["multi_staircase"]["steps"][-1]["records_per_second"]
    ceiling = router["kafka_producer_ceiling"]
    raw_records = []
    for attachment in resource_attachments:
        source = work / attachment["path"]
        raw_records.extend(runner.load_samples(source))
    load_cpu = max(
        ((item.get("load_process") or {}).get("cpu_percent") or 0 for item in raw_records),
        default=0,
    )
    if ceiling["status"] == "inconclusive":
        finding, reason = "inconclusive", "direct_kafka_ceiling_unavailable"
        summary = "Router and dependency readings are retained, but the direct Kafka ceiling could not be measured."
    elif load_cpu >= 80:
        finding, reason = "load_generator", "router_load_generator_cpu_saturated"
        summary = "The host publisher approached its CPU ceiling during the fixed concurrency staircase."
    elif single_rate > 0 and ceiling["records_per_second"] <= single_rate * 1.1:
        finding, reason = "shared_dependency", "router_rate_near_direct_kafka_ceiling"
        summary = "Router publication approached the directly measured Kafka producer ceiling."
    elif multi_rate <= single_rate * 1.1 and ceiling["records_per_second"] > single_rate * 1.3:
        finding, reason = "component", "router_replica_staircase_did_not_separate_from_broker"
        summary = "The direct broker ceiling was materially above the single Router rate while a second Router added little throughput."
    else:
        finding, reason = "inconclusive", "router_and_dependency_rates_not_separated"
        summary = "The measured Router and broker rates do not isolate one bottleneck with this fixed workload."
    evidence = [
        {"source": item["path"], "sha256": item["sha256"]}
        for item in resource_attachments[:4]
    ]
    evidence.extend([
        {"source": "router-single-concurrency-staircase", "sha256": router["single_staircase"]["observations_sha256"]},
        {"source": "router-multi-concurrency-staircase", "sha256": router["multi_staircase"]["observations_sha256"]},
        {"source": "direct-kafka-producer-ceiling", "sha256": ceiling["observations_sha256"]}
        if ceiling.get("observations_sha256") else
        {"source": "direct-kafka-producer-ceiling", "sha256": router["single_staircase"]["observations_sha256"]},
    ])
    return {
        "schema": BOTTLENECK_SCHEMA,
        "candidate": None,
        "finding": finding,
        "reason_code": reason,
        "summary": summary + " This finding describes measurement limits only and does not change product acceptance thresholds.",
        "rates_per_second": {
            "router_single_at_concurrency_64": single_rate,
            "router_multi_at_concurrency_64": multi_rate,
            "direct_kafka": ceiling.get("records_per_second"),
            "load_generator_peak_cpu_percent_of_host": load_cpu,
        },
        "evidence": evidence,
    }


def _scan_artifacts(work: Path, document: dict) -> None:
    secret_scan(document)
    private_path = re.compile(r"(?i)(?:/home/[^\s\"']+|/tmp/[^\s\"']+|/mnt/[a-z]/[^\s\"']+|[a-z]:\\[^\s\"']+)")
    for attachment in _attachments(work):
        content = (work / attachment["path"]).read_bytes()
        text = content.decode("utf-8", errors="replace")
        if private_path.search(text):
            raise ValueError("qualification attachment contains a private filesystem path")
        try:
            secret_scan({"attachment": text})
        except ValueError as error:
            raise ValueError("qualification attachment contains a sensitive value") from error


def run_qualification(manifest_path: Path, work: Path) -> dict:
    started_at = time.time()
    work = work.resolve()
    work.mkdir(parents=True, exist_ok=True, mode=0o700)
    evidence_dir = work / "evidence"
    evidence_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    progress_path = evidence_dir / "qualification-progress.json"
    progress = {"schema": "gopulse.phase18.qualification-progress.v1", "status": "running",
                "started_at": started_at, "steps": []}
    runner.atomic(progress_path, progress)
    binding = None
    lock = None
    current_step = "preflight"

    def step(name, operation):
        nonlocal current_step
        current_step = name
        progress["active_step"] = name
        runner.atomic(progress_path, progress)
        result = operation()
        progress["steps"].append({
            "name": name, "status": "passed",
            "result_sha256": runner.digest_json(result) if isinstance(result, (dict, list)) else None,
            "completed_at": time.time(),
        })
        progress.pop("active_step", None)
        runner.atomic(progress_path, progress)
        return result

    try:
        preflight = step("host_preflight", lambda: runner.run_preflight(work))
        manifest, binding = runner.candidate_binding(manifest_path.resolve())
        lock = runner.prepare_workspace(work, binding)
        inventory_before = runner.inventory_sha()
        topology = step("acceptance_topology", lambda: runner.topology_evidence(manifest, binding, work))
        expected_frontend_port = int(runner.parse_env(work / "topology.env")["FRONTEND_PORT"])
        if topology["edge_bindings"] != [{
            "service": "frontend", "host_ip": "127.0.0.1",
            "host_port": expected_frontend_port,
        }]:
            raise RuntimeError("acceptance overlay does not expose exactly its Frontend edge")

        recipe_binary, load_binary = runner.build_loadtest(work)
        router_binary = runner.build_router_publisher(work)
        first = runner.inspect_recipe(recipe_binary, binding, work / "recipe-inspect-1.json")
        second = runner.inspect_recipe(recipe_binary, binding, work / "recipe-inspect-2.json")
        if first["digest"] != second["digest"]:
            raise RuntimeError("deterministic workload recipe inspection changed between runs")
        snapshot, corpus, credentials, recipe_receipt_path = runner.create_snapshot(
            manifest, binding, work, recipe_binary,
        )
        recipe_receipt = json.loads(recipe_receipt_path.read_text())
        runner.atomic(evidence_dir / "workload-recipe-receipt.json", recipe_receipt)

        fixture_result = step(
            "structured_parser_fixtures",
            lambda: parser_fixture_suite(evidence_dir / "parser-fixtures.json"),
        )
        live_mysql = step(
            "live_mysql_lease_parser",
            lambda: live_mysql_parser(manifest, binding, work, snapshot),
        )
        progress["steps"].append({"name": "live_kafka_parser", "status": "running"})
        runner.atomic(progress_path, progress)

        step("cold_start_readiness_probe", lambda: runner.readiness_probe(
            manifest, binding, work, snapshot, corpus, credentials, load_binary,
        ))

        replacements = {}
        backend_result = step(
            "backend_replacement",
            lambda: runner.replacement_backend(
                manifest, binding, work, snapshot, corpus, credentials, load_binary,
            ),
        )
        replacements["backend"] = _replacement_summary(
            "backend", evidence_dir / "backend-replacement" / "replacement-observations.json",
            evidence_dir / "backend-replacement" / "backend-load-samples.jsonl",
        )

        for component in ("business-worker", "search-indexer"):
            result = step(
                component + "_replacement",
                lambda component=component: runner.replacement_rabbit(
                    manifest, binding, work, snapshot, component, credentials,
                ),
            )
            replacements[component] = _replacement_summary(
                component,
                evidence_dir / (component + "-replacement") / "replacement-observations.json",
                evidence_dir / (component + "-replacement") / "resource-samples.jsonl",
            )

        router_replacement = step(
            "router_replacement",
            lambda: runner.replacement_router(manifest, binding, work, snapshot),
        )
        replacements["router"] = _replacement_summary(
            "router", evidence_dir / "router-replacement" / "replacement-observations.json",
            evidence_dir / "router-replacement" / "resource-samples.jsonl",
        )

        fixed_messages, calibration = step(
            "marshaller_deterministic_preflight_calibration",
            lambda: runner.calibrate_marshaller_backlog(manifest, binding, work, snapshot),
        )
        marshaller_replacement = step(
            "marshaller_replacement",
            lambda: runner.replacement_marshaller(manifest, binding, work, snapshot, fixed_messages),
        )
        replacements["marshaller"] = _replacement_summary(
            "marshaller", evidence_dir / "marshaller-replacement" / "replacement-observations.json",
            evidence_dir / "marshaller-replacement" / "resource-samples.jsonl",
        )

        ownership = step("runtime_lease_and_monitor_ownership", lambda: runner.runtime_ownership(
            manifest, binding, work, snapshot,
        ))

        paired_workloads = {}
        for component in ("business-worker", "search-indexer"):
            pair_value = step(
                component + "_single_multi_release_barrier",
                lambda component=component: runner.pair_rabbit_backlog(
                    manifest, binding, work, snapshot, component,
                    evidence_root=evidence_dir / "paired",
                ),
            )
            paired_workloads[component] = _workload_pair(component, pair_value, work)

        router_pair = step(
            "router_single_multi_fixed_concurrency_pair",
            lambda: runner.pair_router(manifest, binding, work, snapshot,
                                       evidence_root=evidence_dir / "paired"),
        )
        single_staircase = step(
            "router_single_concurrency_staircase",
            lambda: runner.router_concurrency_staircase(manifest, binding, work, snapshot, False),
        )
        multi_staircase = step(
            "router_multi_concurrency_staircase",
            lambda: runner.router_concurrency_staircase(manifest, binding, work, snapshot, True),
        )
        producer_ceiling = step(
            "direct_kafka_producer_ceiling",
            lambda: runner.kafka_producer_ceiling(manifest, binding, work, snapshot),
        )

        marshaller_pair = step(
            "marshaller_single_multi_30_second_window",
            lambda: runner.pair_marshaller(
                manifest, binding, work, snapshot,
                evidence_root=evidence_dir, message_count=fixed_messages,
            ),
        )
        marshaller_document = {
            "single": _marshaller_side("single", fixed_messages, work),
            "multi": _marshaller_side("multi", fixed_messages, work),
            "fixed_messages": fixed_messages,
            "calibration": {
                "observations_sha256": runner.sha256_file(
                    evidence_dir / "marshaller-calibration" / "calibration-observations.json",
                ),
                "single_rate": calibration["single"]["records_per_second"],
                "multi_rate": calibration["multi"]["records_per_second"],
                "safety_backlog_seconds": runner.MARSHALLER_BACKLOG_SAFETY_SECONDS,
            },
            "paired_rates_per_second": marshaller_pair["rates_per_second"],
        }

        live_kafka_path = evidence_dir / "marshaller-calibration" / "multi" / "calibration.json"
        live_kafka_observation = json.loads(live_kafka_path.read_text())["assignment"]
        live_kafka = {
            "status": "passed" if len(live_kafka_observation["members"]) == 2
            and len(live_kafka_observation["partitions"]) >= 4 else "failed",
            "observations_sha256": runner.sha256_file(live_kafka_path),
            "member_count": len(live_kafka_observation["members"]),
            "partition_count": len(live_kafka_observation["partitions"]),
        }
        if live_kafka["status"] != "passed":
            raise RuntimeError("live Kafka member and partition observation did not qualify")
        for item in progress["steps"]:
            if item["name"] == "live_kafka_parser":
                item["status"] = "passed"
                item["result_sha256"] = runner.digest_json(live_kafka)
                break
        runner.atomic(progress_path, progress)

        resource_data = _collect_resources(work)
        runner.atomic(evidence_dir / "resource-summary.json", resource_data)
        if resource_data["status"] != "passed":
            raise RuntimeError("resource samples do not cover host, containers, and every dependency")

        diagnostic = _bottleneck_finding({
            "single_staircase": single_staircase,
            "multi_staircase": multi_staircase,
            "kafka_producer_ceiling": producer_ceiling,
        }, resource_data["attachments"], work)
        diagnostic["candidate"] = binding
        diagnostic_path = evidence_dir / "bottleneck-diagnostic.json"
        runner.atomic(diagnostic_path, diagnostic)
        validate_bottleneck_diagnostic(diagnostic, manifest_path.resolve())

        cleanup_resources = runner.owned_project_resources()
        finished_at = time.time()
        document = {
            "schema": QUALIFICATION_SCHEMA,
            "status": "qualified",
            "started_at": started_at,
            "finished_at": finished_at,
            "candidate": binding,
            "host": preflight["host"],
            "topology": topology,
            "inputs": {
                "snapshot_sha256": runner.sha256_file(snapshot),
                "corpus_sha256": runner.sha256_file(corpus),
                "recipe_receipt_sha256": runner.sha256_file(recipe_receipt_path),
                "recipe_binary_sha256": runner.sha256_file(recipe_binary),
                "load_binary_sha256": runner.sha256_file(load_binary),
                "router_publisher_binary_sha256": runner.sha256_file(router_binary),
                "workload_recipe_sha256": recipe_receipt["digest"],
                "fixed_backlog": {
                    "rabbit_messages": runner.BACKLOG_LIMIT,
                    "prefetch": runner.RABBIT_PREFETCH,
                    "release_barrier": "healthy targets paused after readiness; fixed backlog confirmed; sampler started before unpause",
                },
                "execution_order": [item["name"] for item in progress["steps"]],
            },
            "script_digests": _script_digests(),
            "parser_fixtures": {
                **fixture_result,
                "live_mysql": live_mysql,
                "live_kafka": live_kafka,
            },
            "ownership": {
                "status": "passed",
                "observations_sha256": ownership["observations_sha256"],
                "outbox_future_lease": ownership["outbox_lease"]["blocked_observation"],
                "outbox_expired_reclaim": ownership["outbox_lease"]["reclaimed_observation"],
                "alert_future_lease": ownership["alert_lease"]["blocked_observation"],
                "alert_expired_reclaim": ownership["alert_lease"]["reclaimed_observation"],
                "monitor_single_owner": ownership["monitor_single_owner"],
            },
            "paired_workloads": paired_workloads,
            "marshaller": marshaller_document,
            "router": {
                "fixed_pair": router_pair,
                "single_staircase": single_staircase,
                "multi_staircase": multi_staircase,
                "kafka_producer_ceiling": producer_ceiling,
            },
            "replacements": replacements,
            "resources": resource_data,
            "bottleneck_diagnostic": {
                **_attachment(work, diagnostic_path),
                "finding": diagnostic["finding"],
            },
            "cleanup": {
                "status": "passed" if not cleanup_resources else "failed",
                "owned_projects_remaining": len(cleanup_resources),
                "resource_inventory_before_sha256": inventory_before,
                "resource_inventory_after_sha256": runner.inventory_sha(),
            },
            "secret_scan": "passed",
        }
        if cleanup_resources:
            raise RuntimeError("managed Phase 18 Compose resources remain after qualification")
        progress["status"] = "qualified"
        progress["finished_at"] = finished_at
        runner.atomic(progress_path, progress)
        document["attachments"] = _attachments(work)
        document["script_digests"] = _script_digests()
        _scan_artifacts(work, document)
        if (evidence_dir / "scaling.json").exists():
            raise RuntimeError("qualification unexpectedly generated scaling.json")
        validate_qualification(document, manifest_path.resolve(), work)
        runner.atomic(evidence_dir / "qualification.json", document)
        print("PASS: Phase 18-02 qualification; no scaling.json generated", flush=True)
        return document
    except Exception as error:
        progress["status"] = "failed"
        progress["failed_step"] = current_step
        progress["error_type"] = type(error).__name__
        progress["reason_sha256"] = runner.sha256_text(str(error))
        progress["finished_at"] = time.time()
        runner.atomic(progress_path, progress)
        try:
            cleanup_resources = runner.owned_project_resources()
        except Exception:
            cleanup_resources = [{"kind": "inventory", "name": "unavailable"}]
        failed = {
            "schema": QUALIFICATION_SCHEMA,
            "status": "failed",
            "started_at": started_at,
            "finished_at": time.time(),
            "candidate": binding,
            "failed_step": current_step,
            "error_type": type(error).__name__,
            "reason_sha256": runner.sha256_text(str(error)),
            "cleanup": {
                "status": "passed" if not cleanup_resources else "failed",
                "owned_projects_remaining": len(cleanup_resources),
            },
            "partial_attachments": _attachments(work),
        }
        runner.atomic(evidence_dir / "qualification.json", failed)
        raise
    finally:
        if lock is not None:
            lock.close()
