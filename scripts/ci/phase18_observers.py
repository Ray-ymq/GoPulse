"""Structured parsers for Phase 18 acceptance observations."""
from __future__ import annotations

import json
import re


def parse_mysql_json_object(output: str, expected_keys: set[str], label: str) -> dict:
    """Parse one MySQL JSON_OBJECT result without collapsing NULL and empty text."""
    rows = [line for line in output.splitlines() if line.strip()]
    if len(rows) != 1:
        raise RuntimeError(label + " observation must contain exactly one JSON row")
    try:
        value = json.loads(rows[0])
    except json.JSONDecodeError as error:
        raise RuntimeError(label + " observation is not valid JSON") from error
    if not isinstance(value, dict) or set(value) != expected_keys:
        raise RuntimeError(label + " observation has an unexpected JSON shape")
    return value


KAFKA_REQUIRED_COLUMNS = {
    "GROUP", "TOPIC", "PARTITION", "CURRENT-OFFSET", "LOG-END-OFFSET", "LAG",
}


def parse_kafka_consumer_group(
    stdout: str,
    *,
    expected_group: str | None = None,
    expected_topic: str | None = None,
    stderr: str = "",
    returncode: int = 0,
) -> dict:
    """Parse the controlled kafka-consumer-groups --describe table by header."""
    text = stdout.strip()
    combined = (text + "\n" + stderr).strip()
    if returncode:
        if re.search(r"Consumer group .+ does not exist", combined, re.IGNORECASE):
            return {
                "status": "missing", "reason_code": "group_not_found",
                "group": expected_group, "topic": expected_topic,
                "members": [], "partitions": {}, "lag": None,
            }
        raise RuntimeError("Kafka consumer group command failed")

    lines = [line.strip() for line in text.splitlines() if line.strip()]
    message = " ".join(lines)
    if not lines and not stderr.strip():
        raise RuntimeError("Kafka consumer group output is empty")

    header_index = None
    header = None
    for index, line in enumerate(lines):
        columns = line.split()
        normalized = {item.upper() for item in columns}
        if KAFKA_REQUIRED_COLUMNS.issubset(normalized) and (
            "CONSUMER-ID" in normalized or "MEMBER-ID" in normalized
        ):
            header_index = index
            header = [item.upper() for item in columns]
            break

    if header is None:
        if re.search(r"Consumer group .+ has no active members", message, re.IGNORECASE):
            return {
                "status": "empty", "reason_code": "no_active_members",
                "group": expected_group, "topic": expected_topic,
                "members": [], "partitions": {}, "lag": 0,
            }
        raise RuntimeError("Kafka consumer group table header is missing or unsupported")

    indices = {name: index for index, name in enumerate(header)}
    member_column = "CONSUMER-ID" if "CONSUMER-ID" in indices else "MEMBER-ID"
    selected_topic = expected_topic
    rows = []
    group_names = set()
    table_rows = 0
    rebalance_notice = bool(re.search(r"rebalance|rebalancing", message, re.IGNORECASE))
    for line in lines[header_index + 1:]:
        if line.lower().startswith("consumer group "):
            continue
        fields = line.split()
        if len(fields) != len(header):
            raise RuntimeError("Kafka consumer group row width differs from its header")
        row = dict(zip(header, fields))
        group_name = row["GROUP"]
        topic_name = row["TOPIC"]
        group_names.add(group_name)
        table_rows += 1
        if expected_group and group_name != expected_group:
            raise RuntimeError("Kafka consumer group row belongs to another group")
        if selected_topic is None:
            selected_topic = topic_name
        if topic_name != selected_topic:
            continue
        member = row[member_column]
        member = None if member == "-" else member
        raw_partition = row["PARTITION"]
        partition = None if raw_partition == "-" else raw_partition
        if partition is not None and not partition.isdigit():
            raise RuntimeError("Kafka consumer group partition is not numeric")
        offsets = {}
        for column, output_key in (
            ("CURRENT-OFFSET", "committed"),
            ("LOG-END-OFFSET", "log_end"),
            ("LAG", "lag"),
        ):
            raw_value = row[column]
            if raw_value == "-":
                offsets[output_key] = None
            elif raw_value.isdigit():
                offsets[output_key] = int(raw_value)
            else:
                raise RuntimeError("Kafka consumer group offset is not numeric")
        if partition is None:
            if member is not None:
                rows.append({"partition": None, "member": member, **offsets})
            continue
        rows.append({"partition": int(partition), "member": member, **offsets})

    partitions = {}
    for row in rows:
        partition = row["partition"]
        if partition is None:
            continue
        if partition in partitions:
            raise RuntimeError("Kafka consumer group output repeats a partition")
        partitions[partition] = {
            "member": row["member"],
            "committed": row["committed"],
            "log_end": row["log_end"],
            "lag": row["lag"],
        }
    members = sorted({row["member"] for row in rows if row["member"]})
    if expected_topic and table_rows and not rows:
        raise RuntimeError("Kafka consumer group output does not include the requested topic")
    unassigned = [row for row in rows if row["partition"] is None]
    known_lags = [row["lag"] for row in rows if row["lag"] is not None]
    if rebalance_notice:
        status = "rebalancing"
    elif unassigned:
        status = "assignment_pending"
    elif members:
        status = "active"
    else:
        status = "empty"
    return {
        "status": status,
        "reason_code": None if status == "active" else status,
        "group": expected_group or (next(iter(group_names)) if len(group_names) == 1 else None),
        "topic": selected_topic,
        "members": members,
        "partitions": partitions,
        "unassigned_members": sorted({row["member"] for row in unassigned if row["member"]}),
        "lag": sum(known_lags) if known_lags and len(known_lags) == len(rows) else None,
        "rows": len(rows),
    }
