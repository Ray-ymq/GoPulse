#!/usr/bin/env python3
"""根据提交说明同步产品版本，并检查每个新增提交的版本变化。"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

from commits import Policy, commit_ids, event_range, git, read_policy, validate_message


VERSION_FILES = (
    "VERSION", "backend/business/api/openapi.yaml",
    "frontend/web/package.json", "frontend/web/package-lock.json",
)
NUMBER = r"(?:0|[1-9][0-9]*)"
STABLE = re.compile(rf"{NUMBER}\.{NUMBER}\.{NUMBER}")
LEGACY_RC = re.compile(rf"({NUMBER}\.{NUMBER}\.{NUMBER})-rc\.[1-9][0-9]*")


@dataclass(frozen=True)
class VersionPolicy:
    commits: Policy
    required_after: str
    bumps: dict[str, str]


def read_version_policy(repo: Path) -> VersionPolicy:
    commits = read_policy(repo)
    config = json.loads((repo / ".conventional-commits.json").read_text(encoding="utf-8"))["versioning"]
    baseline, bumps = config["required_after"], config["types"]
    if not isinstance(baseline, str) or not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", baseline):
        raise ValueError("versioning.required_after 必须是完整 commit SHA")
    if (
        not isinstance(bumps, dict) or set(bumps) != set(commits.types)
        or any(value not in ("none", "patch", "minor", "major") for value in bumps.values())
    ):
        raise ValueError("versioning.types 必须为每种提交类型指定 none、patch、minor 或 major")
    return VersionPolicy(commits, baseline, bumps)


def stable_base(version: str, *, allow_legacy_rc: bool = False) -> str:
    if STABLE.fullmatch(version):
        return version
    legacy = LEGACY_RC.fullmatch(version) if allow_legacy_rc else None
    if legacy:
        return legacy[1]
    raise ValueError(f"产品版本必须使用 x.y.z，实际为 {version!r}")


def upgrade(message: str, version: str, policy: VersionPolicy, *, allow_legacy_rc: bool = False) -> tuple[str, str]:
    errors = validate_message(message, policy.commits)
    if errors:
        raise ValueError("提交说明无效：" + "；".join(errors))
    title, *body = message.splitlines()
    level = policy.bumps[title.split("(", 1)[0].split(":", 1)[0].rstrip("!")]
    # 忽略注释和代码块中的示例，避免将示例识别为破坏性声明。
    content = re.sub(r"<!--.*?(?:-->|\Z)", "", "\n".join(body), flags=re.DOTALL)
    fence = ""
    for line in content.splitlines():
        delimiter = re.match(r"^\s{0,3}(`{3,}|~{3,})(.*)$", line)
        if delimiter:
            marker, suffix = delimiter.groups()
            if not fence:
                fence = marker
            elif marker[0] == fence[0] and len(marker) >= len(fence) and not suffix.strip():
                fence = ""
            continue
        if not fence and re.match(r"^BREAKING[ -]CHANGE: \S", line):
            level = "major"
    if title.partition(": ")[0].endswith("!"):
        level = "major"
    major, minor, patch = map(int, stable_base(version, allow_legacy_rc=allow_legacy_rc).split("."))
    if level == "major":
        major, minor, patch = major + 1, 0, 0
    elif level == "minor":
        minor, patch = minor + 1, 0
    elif level == "patch":
        patch += 1
    return f"{major}.{minor}.{patch}", level


def read_files(repo: Path, ref: str | None = None) -> dict[str, str]:
    return {
        path: git(repo, "show", f"{ref}:{path}") if ref is not None else (repo / path).read_text(encoding="utf-8")
        for path in VERSION_FILES
    }


def product_versions(files: dict[str, str]) -> list[str]:
    spec = re.findall(r"(?m)^  version: ([^\r\n]+)$", files[VERSION_FILES[1]])
    if len(spec) != 1:
        raise ValueError("OpenAPI 必须包含唯一的 info.version 字段")
    package = json.loads(files[VERSION_FILES[2]])
    lock = json.loads(files[VERSION_FILES[3]])
    return [files["VERSION"].strip(), spec[0], package["version"], lock["version"], lock["packages"][""]["version"]]


def check_files(files: dict[str, str], expected: str | None = None) -> str:
    versions = product_versions(files)
    version = stable_base(versions[0])
    if any(value != version for value in versions) or (expected is not None and version != expected):
        raise ValueError(f"产品版本不一致：{versions}；预期 {expected or version}")
    return version


def replace_versions(files: dict[str, str], expected: str) -> dict[str, str]:
    product_versions(files)
    result = dict(files)
    result["VERSION"] = expected + "\n"
    result[VERSION_FILES[1]] = re.sub(
        r"(?m)^(  version: )[^\r\n]+$", lambda match: match[1] + expected, files[VERSION_FILES[1]],
    )
    for path in VERSION_FILES[2:]:
        document = json.loads(files[path])
        document["version"] = expected
        if path.endswith("package-lock.json"):
            document["packages"][""]["version"] = expected
        result[path] = json.dumps(document, ensure_ascii=False, indent=2) + "\n"
    check_files(result, expected)
    return result


def expected_version(repo: Path, message: str, policy: VersionPolicy, base: str) -> tuple[str, str, str]:
    revision = git(repo, "rev-parse", "--verify", f"{base}^{{commit}}").strip()
    previous = git(repo, "show", f"{revision}:VERSION").strip()
    version, level = upgrade(message, previous, policy, allow_legacy_rc=revision == policy.required_after)
    return previous, version, level


def prepare(repo: Path, message: str, policy: VersionPolicy, base: str, *, staged: bool, write: bool) -> None:
    previous, expected, level = expected_version(repo, message, policy, base)
    print(f"产品版本：{previous} -> {expected}（{level}）")
    if not write:
        return
    files = read_files(repo, "" if staged else None)
    # 只接受基准版本或本次计算结果，重复执行不会再次递增。
    if any(value not in (previous, expected) for value in product_versions(files)):
        raise ValueError("版本字段与基准或目标版本不符；请先核对本次版本修改")
    updated = replace_versions(files, expected)
    changed = [path for path in VERSION_FILES if updated[path] != files[path]]
    for path in changed:
        if staged and (repo / path).read_bytes() != files[path].encode("utf-8"):
            raise ValueError(f"{path} 有未暂存修改；请先暂存或保存该文件，再提交")
    try:
        for path in changed:
            (repo / path).write_text(updated[path], encoding="utf-8")
        if staged and changed:
            git(repo, "add", "--", *changed)
    except (OSError, subprocess.CalledProcessError):
        for path in changed:
            (repo / path).write_text(files[path], encoding="utf-8")
        raise


def check_history(repo: Path, policy: VersionPolicy, base: str | None, head: str) -> None:
    git(repo, "rev-parse", "--verify", f"{policy.required_after}^{{commit}}")
    revisions = commit_ids(repo, policy.commits, base, head)
    restricted = set(git(repo, "rev-list", "--no-merges", head, "--not", policy.required_after).split())
    count = 0
    for revision in revisions:
        if revision not in restricted:
            continue
        message = git(repo, "show", "--no-patch", "--format=%B", revision)
        _, expected, _ = expected_version(repo, message, policy, f"{revision}^")
        try:
            check_files(read_files(repo, revision), expected)
        except ValueError as exc:
            raise ValueError(f"{revision[:12]}: {exc}") from exc
        count += 1
    check_files(read_files(repo, head))
    print(f"已检查 {count} 个新增非 merge 提交的产品版本")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "update", "check"))
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--message-file", type=Path)
    parser.add_argument("--base-ref")
    parser.add_argument("--head-ref", default="HEAD")
    parser.add_argument("--staged", action="store_true")
    parser.add_argument("--current-only", action="store_true")
    args = parser.parse_args()
    repo = args.repo.resolve()
    try:
        policy = read_version_policy(repo)
        if args.staged:
            merge_head = Path(git(repo, "rev-parse", "--git-path", "MERGE_HEAD").strip())
            if not merge_head.is_absolute():
                merge_head = repo / merge_head
            if merge_head.is_file():
                check_files(read_files(repo, ""))
                print("merge 提交仅检查产品版本一致性，不重复递增")
                return 0
        if args.action == "check":
            if args.staged:
                if args.message_file is None:
                    raise ValueError("暂存区版本检查必须指定 --message-file")
                message = git(repo, "stripspace", "--strip-comments", input_text=args.message_file.read_text(encoding="utf-8"))
                _, expected, _ = expected_version(repo, message, policy, args.base_ref or os.environ.get("GOPULSE_VERSION_BASE", "HEAD"))
                try:
                    check_files(read_files(repo, ""), expected)
                except ValueError as exc:
                    raise ValueError(f"{exc}；请使用 make commit MESSAGE_FILE=<提交说明文件> 同步版本后提交") from exc
            elif args.current_only:
                print(f"产品版本一致：{check_files(read_files(repo))}")
            else:
                base, head = args.base_ref, args.head_ref
                if not base and os.environ.get("GITHUB_EVENT_PATH"):
                    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
                    base, head, _ = event_range(repo, os.environ["GITHUB_EVENT_NAME"], event)
                check_history(repo, policy, base, head)
        else:
            if args.message_file is None:
                raise ValueError("plan 和 update 必须指定 --message-file")
            message = git(repo, "stripspace", "--strip-comments", input_text=args.message_file.read_text(encoding="utf-8"))
            prepare(
                repo, message, policy, args.base_ref or os.environ.get("GOPULSE_VERSION_BASE", "HEAD"),
                staged=args.staged, write=args.action == "update",
            )
        return 0
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as exc:
        print(f"版本处理失败：{exc}", file=sys.stderr)
        if isinstance(exc, subprocess.CalledProcessError) and exc.stderr:
            print(exc.stderr, file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
