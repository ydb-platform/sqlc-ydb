#!/usr/bin/env python3
"""Prepare release metadata and version files from the Unreleased changelog."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


CHANGELOG = Path("CHANGELOG.md")
VERSION_SOURCE = Path("internal/cli/cli.go")
STABLE_HEADING = re.compile(r"^## v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
VERSION_HEADING = re.compile(r"^## v\S+")
VERSION_DECLARATION = re.compile(r'^var Version = "(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"$', re.MULTILINE)
CHANGE_HEADING = re.compile(
    r"^### (Added|Changed|Deprecated|Removed|Fixed|Security|Compatibility|"
    r"Capabilities|Parity with upstream sqlc|Known limitations|Deliberate exclusions)$",
    re.MULTILINE,
)
BULLET = re.compile(r"^- .+\S", re.MULTILINE)


class ReleaseError(Exception):
    pass


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    commands = parser.add_subparsers(dest="command", required=True)
    prepare = commands.add_parser("prepare")
    prepare.add_argument("--part", required=True, choices=("PATCH", "MINOR", "MAJOR"))
    prepare.add_argument("--rc", required=True, choices=("true", "false"))
    prepare.add_argument("--notes", required=True, type=Path)
    notes = commands.add_parser("notes")
    notes.add_argument("--tag", required=True)
    notes.add_argument("--changelog", required=True, type=Path)
    notes.add_argument("--notes", required=True, type=Path)
    return parser.parse_args()


def version_text(version: tuple[int, int, int]) -> str:
    return ".".join(str(value) for value in version)


def bump(version: tuple[int, int, int], part: str) -> tuple[int, int, int]:
    major, minor, patch = version
    if part == "MAJOR":
        return major + 1, 0, 0
    if part == "MINOR":
        return major, minor + 1, 0
    return major, minor, patch + 1


def section(text: str, heading: str) -> str:
    lines = text.splitlines(keepends=True)
    positions = [index for index, line in enumerate(lines) if line.rstrip("\r\n") == heading]
    if len(positions) != 1:
        raise ReleaseError(f"CHANGELOG must contain exactly one {heading} section")
    start = positions[0] + 1
    end = next(
        (index for index in range(start, len(lines)) if lines[index].startswith("## ")),
        len(lines),
    )
    return "".join(lines[start:end]).strip()


def validate_entries(content: str, label: str) -> None:
    has_heading = False
    has_entry = False
    active_heading = False
    for line in content.splitlines():
        if line.startswith("### "):
            active_heading = CHANGE_HEADING.fullmatch(line) is not None
            if not active_heading:
                raise ReleaseError(f"{label} section has unsupported change heading: {line}")
            has_heading = True
        elif active_heading and BULLET.fullmatch(line):
            has_entry = True
    if not has_heading:
        raise ReleaseError(f"{label} section has no supported change heading")
    if not has_entry:
        raise ReleaseError(f"{label} section has no change entries")


def parse_changelog(text: str) -> tuple[str, list[tuple[tuple[int, int, int], str]], str, str]:
    lines = text.splitlines(keepends=True)
    unreleased = [index for index, line in enumerate(lines) if line.rstrip("\r\n") == "## Unreleased"]
    if len(unreleased) != 1:
        raise ReleaseError("CHANGELOG must contain exactly one ## Unreleased section")
    unreleased_index = unreleased[0]
    for line in lines[:unreleased_index]:
        if line.startswith("## "):
            raise ReleaseError("## Unreleased must be the first level-two CHANGELOG section")

    stable: list[tuple[tuple[int, int, int], str]] = []
    next_section = len(lines)
    for index in range(unreleased_index + 1, len(lines)):
        heading = lines[index].rstrip("\r\n")
        match = STABLE_HEADING.fullmatch(heading)
        if match:
            if next_section == len(lines):
                next_section = index
            stable.append((tuple(int(value) for value in match.groups()), heading))
        elif heading.startswith("## "):
            if VERSION_HEADING.match(heading):
                raise ReleaseError(f"invalid stable CHANGELOG heading: {heading}")
            raise ReleaseError(f"unexpected CHANGELOG section: {heading}")
    versions = [version for version, _ in stable]
    if len(set(versions)) != len(versions):
        raise ReleaseError("CHANGELOG contains duplicate stable version sections")
    if any(older >= newer for newer, older in zip(versions, versions[1:])):
        raise ReleaseError("stable CHANGELOG sections must be in descending version order")

    pending = "".join(lines[unreleased_index + 1 : next_section]).strip()
    prefix = "".join(lines[: unreleased_index + 1]).rstrip() + "\n"
    history = "".join(lines[next_section:]).lstrip("\r\n")
    return pending, stable, prefix, history


def parse_source_version(text: str) -> tuple[tuple[int, int, int], re.Match[str]]:
    matches = list(VERSION_DECLARATION.finditer(text))
    if len(matches) != 1:
        raise ReleaseError('CLI source must contain exactly one var Version = "X.Y.Z" declaration')
    match = matches[0]
    return tuple(int(value) for value in match.groups()), match


def git_tags() -> list[str]:
    result = subprocess.run(
        ["git", "tag", "--list"],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    if result.returncode != 0:
        message = result.stderr.strip() or "git tag --list failed"
        raise ReleaseError(message)
    return result.stdout.splitlines()


def stable_tags(tags: list[str]) -> set[tuple[int, int, int]]:
    result: set[tuple[int, int, int]] = set()
    for tag in tags:
        match = re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag)
        if match:
            result.add(tuple(int(value) for value in match.groups()))
    return result


def target_version(
    part: str,
    source: tuple[int, int, int],
    history: list[tuple[tuple[int, int, int], str]],
) -> tuple[int, int, int]:
    if not history:
        if source != (0, 0, 1):
            raise ReleaseError("CLI Version must remain 0.0.1 before the first stable release")
        return bump((0, 0, 0), part)
    latest = history[0][0]
    if source != latest:
        raise ReleaseError(
            f"CLI Version {version_text(source)} does not match latest stable CHANGELOG version {version_text(latest)}"
        )
    return bump(latest, part)


def validate_latest_tag(
    tags: set[tuple[int, int, int]], history: list[tuple[tuple[int, int, int], str]]
) -> None:
    if not history:
        if tags:
            raise ReleaseError("first-release state cannot contain stable version tags")
        return
    latest = history[0][0]
    if latest not in tags:
        raise ReleaseError(f"latest stable CHANGELOG version v{version_text(latest)} has no Git tag")
    higher = sorted(version for version in tags if version > latest)
    if higher:
        raise ReleaseError("stable Git tag is newer than CHANGELOG history: v" + version_text(higher[-1]))


def next_rc(tags: list[str], version: str) -> int:
    pattern = re.compile(rf"v{re.escape(version)}-rc(0|[1-9][0-9]*)")
    numbers = [int(match.group(1)) for tag in tags if (match := pattern.fullmatch(tag))]
    return max(numbers, default=-1) + 1


def stage_writes(changes: list[tuple[Path, str]]) -> None:
    staged: list[tuple[Path, Path]] = []
    try:
        for path, content in changes:
            path.parent.mkdir(parents=True, exist_ok=True)
            mode = path.stat().st_mode & 0o777 if path.exists() else 0o644
            descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
            temporary = Path(temporary_name)
            with os.fdopen(descriptor, "w", encoding="utf-8", newline="") as stream:
                stream.write(content)
                stream.flush()
                os.fsync(stream.fileno())
            os.chmod(temporary, mode)
            staged.append((temporary, path))
        for temporary, path in staged:
            os.replace(temporary, path)
    finally:
        for temporary, _ in staged:
            try:
                temporary.unlink()
            except FileNotFoundError:
                pass


def write_notes(changelog: Path, tag: str, notes: Path) -> None:
    tag_match = re.fullmatch(
        r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-rc(0|[1-9][0-9]*))?",
        tag,
    )
    if not tag_match:
        raise ReleaseError(f"invalid release tag: {tag}")
    if changelog.resolve() == notes.resolve():
        raise ReleaseError("notes path must differ from CHANGELOG")
    text = changelog.read_text(encoding="utf-8")
    source_heading = "## Unreleased" if tag_match.group(4) is not None else f"## {tag}"
    content = section(text, source_heading)
    validate_entries(content, source_heading.removeprefix("## "))
    stage_writes([(notes, f"## {tag}\n\n{content}\n")])


def prepare_release(args: argparse.Namespace) -> dict[str, object]:
    changelog_path = CHANGELOG.resolve()
    source_path = VERSION_SOURCE.resolve()
    notes_path = args.notes.resolve()
    if notes_path in (changelog_path, source_path):
        raise ReleaseError("notes path must differ from CHANGELOG and CLI source")
    changelog = CHANGELOG.read_text(encoding="utf-8")
    source = VERSION_SOURCE.read_text(encoding="utf-8")
    pending, history, changelog_prefix, old_history = parse_changelog(changelog)
    validate_entries(pending, "Unreleased")
    source_version, source_match = parse_source_version(source)
    tags = git_tags()
    target = target_version(args.part, source_version, history)
    target_text = version_text(target)
    known_stable_tags = stable_tags(tags)
    if target in known_stable_tags:
        raise ReleaseError(f"stable tag v{target_text} already exists")
    validate_latest_tag(known_stable_tags, history)

    release_candidate = args.rc == "true"
    if release_candidate:
        tag = f"v{target_text}-rc{next_rc(tags, target_text)}"
    else:
        tag = f"v{target_text}"
    notes = f"## {tag}\n\n{pending}\n"
    changes = [(notes_path, notes)]
    if not release_candidate:
        new_changelog = f"{changelog_prefix}\n## {tag}\n\n{pending}\n"
        if old_history:
            new_changelog += f"\n{old_history}"
        new_source = source[: source_match.start(1)] + target_text + source[source_match.end(3) :]
        changes.extend(((CHANGELOG, new_changelog), (VERSION_SOURCE, new_source)))
    stage_writes(changes)
    return {"tag": tag, "version": tag.removeprefix("v"), "release_candidate": release_candidate}


def main() -> int:
    args = parse_args()
    try:
        if args.command == "notes":
            write_notes(args.changelog, args.tag, args.notes)
        else:
            result = prepare_release(args)
            print(json.dumps(result, separators=(",", ":")))
        return 0
    except (OSError, ReleaseError) as error:
        print(f"release-version: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
