#!/usr/bin/env python3
"""Check current GoPulse documentation without executing documented commands."""

from __future__ import annotations

import argparse
import html
import json
import re
import shlex
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import unquote, urlsplit


FROZEN_DOCUMENTS = (
    Path("docs/GoPulse 高并发架构设计.md"),
    Path("docs/GoPulse 可观测架构设计.md"),
    Path("dev/design/GoPulse-目标架构落地大纲.md"),
)
MARKDOWN_SUFFIXES = {".md", ".markdown"}
ROOT_ENTRYPOINTS = {
    Path("AGENTS.md"),
    Path("README.md"),
    Path("项目导读.md"),
    Path("使用手册.md"),
}
CURRENT_ROOTS = (
    Path("dev/contracts"),
    Path("dev/design"),
    Path("dev/operations"),
    Path("dev/status"),
    Path("dev/validation"),
    Path("docs"),
)
CURRENT_FILES = {
    Path("ci/AGENTS.md"),
    Path("dev/README.md"),
    Path("dev/phases/README.md"),
    Path("dev/logs/README.md"),
    Path("deploy/release/README.md"),
    Path("admin-frontend/README.md"),
    Path("backend/README.md"),
    Path("monitor/README.md"),
    Path("router/README.md"),
    Path("marshaller/README.md"),
    Path("exporters/README.md"),
}
EXCLUDED_SCAN_PARTS = {"logs", "review", "imple"}


@dataclass(frozen=True)
class Link:
    source: Path
    target: str
    line: int


def _git(repo: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=repo, check=True, text=True, encoding="utf-8", capture_output=True
    )
    return result.stdout.strip()


def _is_current(path: Path) -> bool:
    if any(part in EXCLUDED_SCAN_PARTS for part in path.parts):
        return False
    if path.name == "Plan.md" or path.name.startswith("Phase-"):
        return False
    return True


def current_documents(repo: Path, changed: list[Path] | None = None) -> list[Path]:
    selected: set[Path] = set(ROOT_ENTRYPOINTS | CURRENT_FILES)
    for root in CURRENT_ROOTS:
        directory = repo / root
        if directory.is_dir():
            selected.update(
                path.relative_to(repo)
                for path in directory.rglob("*")
                if path.is_file()
                and path.suffix.lower() in MARKDOWN_SUFFIXES
                and _is_current(path.relative_to(repo))
            )
    for path in changed or []:
        if path.suffix.lower() in MARKDOWN_SUFFIXES and _is_current(path):
            selected.add(path)
    return sorted(path for path in selected if (repo / path).is_file())


def _strip_fenced(text: str) -> str:
    lines: list[str] = []
    fenced = False
    fence = ""
    for line in text.splitlines():
        match = re.match(r"^\s*(`{3,}|~{3,})", line)
        if match:
            marker = match.group(1)[0]
            if not fenced:
                fenced = True
                fence = marker
            elif marker == fence:
                fenced = False
            lines.append("")
            continue
        lines.append("" if fenced else line)
    return "\n".join(lines)


def _links(path: Path, text: str) -> list[Link]:
    visible = _strip_fenced(text)
    links: list[Link] = []
    pattern = re.compile(r"!?\[[^\]]*\]\(\s*(<[^>]*>|[^\s)]+)(?:\s+[^)]*)?\)")
    for line_number, line in enumerate(visible.splitlines(), 1):
        for match in pattern.finditer(line):
            target = match.group(1).strip().strip("<>")
            links.append(Link(path, html.unescape(target), line_number))
        for match in re.finditer(r"<(?:a|img)\b[^>]+(?:href|src)=[\"']([^\"']+)[\"']", line, re.I):
            links.append(Link(path, html.unescape(match.group(1)), line_number))
    return links


def _anchor(text: str) -> str:
    text = re.sub(r"[`*_~]", "", text).strip().lower()
    text = re.sub(r"[^\w\-\s\u0080-\uffff]", "", text, flags=re.UNICODE)
    return re.sub(r"\s+", "-", text)


def _anchors(path: Path) -> set[str]:
    result: set[str] = set()
    counts: dict[str, int] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        match = re.match(r"^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$", line)
        if match:
            base = _anchor(match.group(1))
            index = counts.get(base, 0)
            counts[base] = index + 1
            result.add(base if index == 0 else f"{base}-{index}")
        for explicit in re.finditer(r"(?:id|name)=[\"']([^\"']+)[\"']", line, re.I):
            result.add(unquote(explicit.group(1)).lower())
    return result


def _resolve_target(repo: Path, source: Path, target: str) -> tuple[Path | None, str | None]:
    if not target or target.startswith("#"):
        return repo / source, unquote(target[1:]).lower() if target.startswith("#") else None
    if target.startswith("/"):
        # Absolute filesystem examples (usually copied from a developer's
        # checkout) are not repository-relative Markdown links.
        return None, None
    parsed = urlsplit(target)
    if parsed.scheme or parsed.netloc:
        return None, None
    raw_path = unquote(parsed.path)
    candidate = (repo / source).resolve() if not raw_path else (repo / source.parent / raw_path).resolve()
    try:
        candidate.relative_to(repo.resolve())
    except ValueError:
        return None, "path escapes repository"
    if candidate.is_dir() and parsed.fragment:
        candidate = candidate / "README.md"
    return candidate, unquote(parsed.fragment).lower() or None


def check_links(repo: Path, documents: list[Path]) -> list[str]:
    errors: list[str] = []
    anchor_cache: dict[Path, set[str]] = {}
    for relative in documents:
        path = repo / relative
        try:
            text = path.read_text(encoding="utf-8")
        except OSError as exc:
            errors.append(f"{relative}: cannot read document: {exc}")
            continue
        for link in _links(relative, text):
            target, fragment = _resolve_target(repo, link.source, link.target)
            if target is None:
                if fragment == "path escapes repository":
                    errors.append(f"{relative}:{link.line}: link escapes repository: {link.target}")
                continue
            try:
                target_relative = target.relative_to(repo.resolve())
            except ValueError:
                errors.append(f"{relative}:{link.line}: link escapes repository: {link.target}")
                continue
            if not target.is_file() and not target.is_dir():
                errors.append(f"{relative}:{link.line}: missing local link target: {link.target}")
                continue
            if fragment:
                if target.is_dir():
                    target = target / "README.md"
                    if not target.is_file():
                        errors.append(f"{relative}:{link.line}: missing anchor target document: {link.target}")
                        continue
                if target_relative not in anchor_cache:
                    anchor_cache[target_relative] = _anchors(target)
                if fragment not in anchor_cache[target_relative]:
                    errors.append(f"{relative}:{link.line}: missing anchor {fragment!r} in {target_relative}")
    return errors


def _make_targets(repo: Path) -> set[str]:
    makefile = repo / "Makefile"
    if not makefile.is_file():
        return set()
    content = makefile.read_text(encoding="utf-8")
    targets = set(re.findall(r"^([A-Za-z0-9_.-]+):", content, re.M))
    for declaration in re.findall(r"^\.PHONY:\s*(.+)$", content, re.M):
        targets.update(declaration.split())
    return targets


def _make_modules(repo: Path) -> set[str]:
    content = (repo / "Makefile").read_text(encoding="utf-8")
    modules: set[str] = set()
    for variable in ("GO_MODULES", "NPM_MODULES"):
        match = re.search(rf"^{variable}\s*:?=\s*(.*?)(?=^\S|\Z)", content, re.M | re.S)
        if match:
            modules.update(match.group(1).replace("\\", " ").split())
    return modules


def _package_scripts(path: Path) -> set[str]:
    try:
        return set(json.loads(path.read_text(encoding="utf-8")).get("scripts", {}))
    except (OSError, ValueError):
        return set()


def check_commands(repo: Path, documents: list[Path]) -> list[str]:
    targets = _make_targets(repo)
    modules = _make_modules(repo)
    errors: list[str] = []
    make_pattern = re.compile(r"\bmake(?:\s+[^\n`|;&]+)?")
    npm_pattern = re.compile(r"\bnpm\s+run\s+([A-Za-z0-9:_-]+)")
    for relative in documents:
        path = repo / relative
        if not path.is_file() or not _is_current(relative):
            continue
        raw_lines = path.read_text(encoding="utf-8").splitlines()
        fenced = False
        fence = ""
        for line_number, line in enumerate(raw_lines, 1):
            fence_match = re.match(r"^\s*(`{3,}|~{3,})", line)
            if fence_match:
                marker = fence_match.group(1)[0]
                if not fenced:
                    fenced, fence = True, marker
                elif marker == fence:
                    fenced = False
                continue
            inline = re.findall(r"`([^`]+)`", line)
            eligible = fenced or line.lstrip().startswith(("make ", "$ make ")) or any(
                re.search(r"\bmake\s+", part) for part in inline
            )
            if not eligible:
                continue
            for match in make_pattern.finditer(line):
                try:
                    args = shlex.split(match.group(0))
                except ValueError:
                    continue
                args = [arg.rstrip(",.:;)") for arg in args[1:]]
                while args and ("=" in args[0] or args[0].startswith("-")):
                    if args[0] in {"-C", "--directory"} and len(args) > 1:
                        args = args[2:]
                    else:
                        args = args[1:]
                if not args:
                    continue
                target = args[0]
                if target not in targets and "*" not in target and not (target.startswith("<") and target.endswith(">")):
                    errors.append(f"{relative}:{line_number}: unknown Make target {target!r}")
                for arg in args[1:]:
                    if arg.startswith("MODULE="):
                        module = arg.split("=", 1)[1]
                        if module and "<" not in module and module not in modules:
                            errors.append(f"{relative}:{line_number}: unknown MODULE {module!r}")
            for match in npm_pattern.finditer(line):
                script = match.group(1)
                if "admin-frontend" in line or relative.parts[:1] == ("admin-frontend",):
                    candidates = [repo / "admin-frontend/package.json"]
                elif "frontend" in line or relative.parts[:1] == ("frontend",):
                    candidates = [repo / "frontend/package.json"]
                else:
                    candidates = [repo / "frontend/package.json", repo / "admin-frontend/package.json"]
                if not any(script in _package_scripts(candidate) for candidate in candidates):
                    errors.append(f"{relative}:{line_number}: unknown npm script {script!r}")
    return errors


def check_frozen(repo: Path, base_ref: str | None) -> list[str]:
    if not base_ref:
        return []
    try:
        common = _git(repo, "merge-base", base_ref, "HEAD")
    except subprocess.CalledProcessError as exc:
        return [f"invalid BASE_REF {base_ref!r}: {exc.stderr.strip() or exc}"]
    errors: list[str] = []
    for relative in FROZEN_DOCUMENTS:
        path = repo / relative
        if not path.is_file():
            errors.append(f"frozen document is missing: {relative}")
            continue
        result = subprocess.run(["git", "diff", "--quiet", common, "--", str(relative)], cwd=repo, check=False)
        if result.returncode != 0:
            errors.append(f"frozen document changed relative to {common[:12]}: {relative}")
    return errors


def changed_paths(repo: Path, base_ref: str | None) -> list[Path]:
    if not base_ref:
        return []
    try:
        common = _git(repo, "merge-base", base_ref, "HEAD")
        raw = _git(repo, "-c", "core.quotepath=false", "diff", "--name-only", "-z", common, "HEAD")
    except subprocess.CalledProcessError:
        return []
    return [Path(item) for item in raw.split("\0") if item]


def check(repo: Path, base_ref: str | None = None) -> list[str]:
    documents = current_documents(repo, changed_paths(repo, base_ref))
    errors = check_links(repo, documents)
    errors.extend(check_commands(repo, documents))
    errors.extend(check_frozen(repo, base_ref))
    return errors


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path.cwd())
    parser.add_argument("--base-ref")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    errors = check(args.repo.resolve(), args.base_ref)
    if errors:
        for error in errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1
    print("Documentation checks passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
