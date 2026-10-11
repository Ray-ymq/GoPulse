#!/usr/bin/env python3
"""Run native Go or frontend linting and classify baseline findings."""

from __future__ import annotations

import argparse
import json
import os
import platform
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


GO_MODULES = (
    "backend",
    "componentmetrics",
    "router",
    "marshaller",
    "monitor",
    "lifecycle",
    "loadtest",
    "exporters/elasticsearch",
    "exporters/kafka",
    "exporters/mysql",
    "exporters/rabbitmq",
    "exporters/redis",
    "exporters/victoriametrics",
    "devtools",
    "acceptance",
)
NPM_MODULES = ("frontend", "admin-frontend")
DIAGNOSTIC = re.compile(r"^(?P<path>.+?):(?P<line>[0-9]+)(?::[0-9]+)?:\s*(?P<message>.+)$")
HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(?P<start>\d+)(?:,(?P<count>\d+))? @@")


@dataclass(frozen=True)
class Finding:
    path: str
    line: int
    message: str


@dataclass(frozen=True)
class Summary:
    stored: int
    new: int
    total: int
    raw_exit: int
    report: Path


def _git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", *args], cwd=repo, check=True, text=True, encoding="utf-8", capture_output=True
    ).stdout


def common_ancestor(repo: Path, base_ref: str) -> str:
    try:
        return _git(repo, "merge-base", base_ref, "HEAD").strip()
    except subprocess.CalledProcessError as exc:
        raise ValueError(f"invalid BASE_REF {base_ref!r}: {exc.stderr.strip() or exc}") from exc


def _normalise_path(repo: Path, value: str) -> str:
    path = Path(value)
    if path.is_absolute():
        try:
            return path.resolve().relative_to(repo.resolve()).as_posix()
        except ValueError:
            return path.as_posix()
    return value.replace("\\", "/").removeprefix("./")


def changed_lines(repo: Path, base_ref: str, module: str) -> dict[str, set[int] | None]:
    common = common_ancestor(repo, base_ref)
    output = _git(repo, "--no-pager", "diff", "--no-color", "--unified=0", common, "--", module)
    result: dict[str, set[int] | None] = {}
    current: str | None = None
    for line in output.splitlines():
        if line.startswith("+++ b/"):
            current = line[6:]
            result.setdefault(current, set())
            continue
        if current is None:
            continue
        match = HUNK.match(line)
        if match:
            start = int(match.group("start"))
            count = int(match.group("count") or "1")
            result[current] = (result.get(current) or set()) | set(range(start, start + count))
    untracked = _git(repo, "ls-files", "--others", "--exclude-standard", "--", module)
    for path in untracked.splitlines():
        if path:
            result[path] = None
    return result


def _is_new(repo: Path, finding: Finding, changed: dict[str, set[int] | None]) -> bool:
    path = _normalise_path(repo, finding.path)
    if path not in changed:
        return False
    lines = changed.get(path)
    return lines is None or (lines is not None and finding.line in lines)


def _platform_name() -> str:
    operating_system = {"darwin": "darwin", "linux": "linux"}.get(sys.platform.split("-")[0], sys.platform.split("-")[0])
    machine = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine(), platform.machine())
    return f"{operating_system}/{machine}"


def parse_go_findings(repo: Path, output: str) -> list[Finding]:
    findings: list[Finding] = []
    for line in output.splitlines():
        line = re.sub(r"\x1b\[[0-9;]*m", "", line)
        match = DIAGNOSTIC.match(line.strip())
        if match and match.group("path").endswith((".go", ".mod")):
            findings.append(Finding(match.group("path"), int(match.group("line")), match.group("message")))
    return findings


def parse_eslint_findings(output: str) -> list[Finding] | None:
    start = output.find("[")
    if start < 0:
        return None
    try:
        documents = json.loads(output[start:])
    except json.JSONDecodeError:
        return None
    findings: list[Finding] = []
    for document in documents:
        if document.get("fatalErrorCount", 0):
            return None
        path = str(document.get("filePath", ""))
        for message in document.get("messages", []):
            if message.get("fatal") or message.get("severity", 0):
                findings.append(Finding(path, int(message.get("line") or 1), str(message.get("message", ""))))
    return findings


def _toolchains(repo: Path) -> dict:
    return json.loads((repo / "devtools/toolchains.json").read_text(encoding="utf-8"))


def lint_tool(repo: Path) -> Path:
    configured = os.environ.get("GOPULSE_LINT_TOOL")
    if configured:
        path = Path(configured)
    else:
        config = _toolchains(repo)["golangci_lint"]
        path = repo / ".run/tools/golangci-lint" / config["version"] / _platform_name().replace("/", "-") / "golangci-lint"
    if not path.is_file() or not os.access(path, os.X_OK):
        raise FileNotFoundError(f"golangci-lint is not prepared at {path}; run make bootstrap TOOLS_ONLY=1")
    return path


def native_command(repo: Path, module: str) -> tuple[list[str], str]:
    if module in GO_MODULES:
        return [str(lint_tool(repo)), "run", "--config", str(repo / ".golangci.yml"), "./..."], "go"
    if module in NPM_MODULES:
        eslint = repo / module / "node_modules/.bin/eslint"
        if not eslint.is_file():
            raise FileNotFoundError(f"frontend dependencies are not prepared for {module}; run make bootstrap MODULE={module}")
        return [str(eslint), ".", "--format", "json"], "eslint"
    raise ValueError(f"unknown lint module {module!r}")


def _write_report(repo: Path, module: str, raw: str, summary: Summary | None = None) -> Path:
    directory = repo / ".run/quality/lint"
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    report = directory / (module.replace("/", "_") + ".txt")
    suffix = ""
    if summary is not None:
        suffix = f"\nsummary: stored={summary.stored} new={summary.new} total={summary.total} raw_exit={summary.raw_exit}\n"
    report.write_text(raw + suffix, encoding="utf-8")
    report.chmod(0o600)
    return report


def run(repo: Path, module: str, base_ref: str | None = None) -> tuple[int, Summary | None]:
    command, kind = native_command(repo, module)
    try:
        process = subprocess.run(
            command,
            cwd=repo / module,
            text=True,
            encoding="utf-8",
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=600,
            check=False,
        )
    except subprocess.TimeoutExpired as exc:
        raw = (exc.stdout or "") + (exc.stderr or "")
        report = _write_report(repo, module, raw + "\nscanner timed out after 600 seconds\n")
        print(f"lint scanner timed out; report={report}", file=sys.stderr)
        return 2, None
    raw = (process.stdout or "") + (process.stderr or "")
    if kind == "go":
        findings = parse_go_findings(repo, raw)
    else:
        findings = parse_eslint_findings(process.stdout or "")
    report = _write_report(repo, module, raw)
    if process.returncode not in {0, 1}:
        print(raw, end="")
        print(f"lint scanner error: native_exit={process.returncode} report={report}", file=sys.stderr)
        return process.returncode, None
    if findings is None:
        print(raw, end="")
        print(f"lint scanner error: native_exit={process.returncode} report={report}", file=sys.stderr)
        return process.returncode or 2, None

    if process.returncode == 1 and not findings:
        print(raw, end="")
        print(f"lint scanner error: native_exit=1 with no parsed findings report={report}", file=sys.stderr)
        return 1, None
    changed = changed_lines(repo, base_ref, module) if base_ref else {}
    new = 0
    for finding in findings:
        if module in GO_MODULES and not Path(finding.path).is_absolute() and not finding.path.startswith(module + "/"):
            finding = Finding(f"{module}/{finding.path}", finding.line, finding.message)
        new += _is_new(repo, finding, changed)
    stored = len(findings) - new
    summary = Summary(stored, new, len(findings), process.returncode, report)
    report = _write_report(repo, module, raw, summary)
    print(raw, end="")
    print(
        f"lint summary: stored={stored} new={new} total={len(findings)} raw_exit={process.returncode} report={report}",
        file=sys.stderr,
    )
    if process.returncode == 0:
        return 0, summary
    if base_ref and new == 0 and process.returncode == 1:
        return 0, summary
    return process.returncode, summary


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--module", required=True)
    parser.add_argument("--base-ref")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        code, _ = run(args.repo.resolve(), args.module, args.base_ref)
    except (FileNotFoundError, ValueError, json.JSONDecodeError, OSError) as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2
    return code


if __name__ == "__main__":
    raise SystemExit(main())
