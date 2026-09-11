import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parent.parent / "release-version.py"
PENDING = "### Added\n\n- New release behavior."
OLD_SECTION = "## v1.2.3\n\n### Fixed\n\n- Previous fix.\n"


class ReleaseRepo:
    def __init__(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        (self.root / "internal/cli").mkdir(parents=True)
        self.git("init", "-q")
        self.git("config", "user.email", "test@example.com")
        self.git("config", "user.name", "Release Test")
        self.write_state("0.0.1", PENDING)
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")

    def close(self):
        self.temporary.cleanup()

    def git(self, *args):
        return subprocess.run(
            ["git", *args], cwd=self.root, check=True, text=True, capture_output=True
        )

    def tag(self, *tags):
        for tag in tags:
            self.git("tag", tag)

    def clear_tags(self):
        tags = self.git("tag", "--list").stdout.splitlines()
        if tags:
            self.git("tag", "-d", *tags)

    def write_state(self, version, pending, history="", intro="# Changelog\n\nIntro.\n"):
        changelog = f"{intro}\n## Unreleased\n\n{pending}"
        if history:
            changelog += f"\n\n{history.lstrip()}"
        if not changelog.endswith("\n"):
            changelog += "\n"
        (self.root / "CHANGELOG.md").write_text(changelog, encoding="utf-8")
        (self.root / "internal/cli/cli.go").write_text(
            f'package cli\n\nvar Version = "{version}"\nvar Commit = "unknown"\n',
            encoding="utf-8",
        )

    def prepare(self, part="PATCH", rc="false", notes="notes.md"):
        return subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "prepare",
                "--part",
                part,
                "--rc",
                rc,
                "--notes",
                notes,
            ],
            cwd=self.root,
            text=True,
            capture_output=True,
        )

    def notes(self, tag, changelog="CHANGELOG.md", notes="notes.md"):
        return subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "notes",
                "--tag",
                tag,
                "--changelog",
                changelog,
                "--notes",
                notes,
            ],
            cwd=self.root,
            text=True,
            capture_output=True,
        )


class ReleaseVersionTest(unittest.TestCase):
    def setUp(self):
        self.repo = ReleaseRepo()

    def tearDown(self):
        self.repo.close()

    def assert_success(self, result):
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")

    def assert_failure_without_writes(self, result, changelog, source):
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertEqual((self.repo.root / "CHANGELOG.md").read_text(), changelog)
        self.assertEqual((self.repo.root / "internal/cli/cli.go").read_text(), source)
        self.assertFalse((self.repo.root / "notes.md").exists())

    def test_repository_changelog_can_be_used_as_release_notes(self):
        changelog = SCRIPT.parents[2] / "CHANGELOG.md"
        sections = re.split(r"^## ", changelog.read_text(encoding="utf-8"), flags=re.MULTILINE)[1:]
        checked = 0
        for block in sections:
            heading, _, content = block.partition("\n")
            content = content.strip()
            # Stable preparation leaves Unreleased empty before running tests.
            if heading == "Unreleased" and not content:
                continue
            checked += 1
            with self.subTest(heading=heading):
                tag = "v0.0.1-rc0" if heading == "Unreleased" else heading
                result = self.repo.notes(tag, changelog=str(changelog))
                self.assert_success(result)
                self.assertEqual(
                    (self.repo.root / "notes.md").read_text(),
                    f"## {tag}\n\n{content}\n",
                )
                # Exercise preparation as well as extraction, without changing
                # the checkout's changelog, version, or tags.
                self.repo.write_state("0.0.1", content)
                self.assert_success(self.repo.prepare(rc="true"))
                self.assert_success(self.repo.prepare(rc="false"))
        self.assertGreater(checked, 0, "CHANGELOG contains no release notes")

    def test_initial_stable_release_is_001_patch(self):
        result = self.repo.prepare()
        self.assert_success(result)
        self.assertEqual(
            json.loads(result.stdout),
            {"tag": "v0.0.1", "version": "0.0.1", "release_candidate": False},
        )
        changelog = (self.repo.root / "CHANGELOG.md").read_text()
        self.assertIn("## Unreleased\n\n## v0.0.1\n\n" + PENDING, changelog)
        self.assertEqual(changelog.count("- New release behavior."), 1)
        self.assertEqual((self.repo.root / "notes.md").read_text(), "## v0.0.1\n\n" + PENDING + "\n")
        self.assertIn('var Version = "0.0.1"', (self.repo.root / "internal/cli/cli.go").read_text())

    def test_first_release_rejects_other_parts_and_versions(self):
        for part, version in (("MINOR", "0.0.1"), ("MAJOR", "0.0.1"), ("PATCH", "0.0.2")):
            with self.subTest(part=part, version=version):
                self.repo.clear_tags()
                self.repo.write_state(version, PENDING)
                before_changelog = (self.repo.root / "CHANGELOG.md").read_text()
                before_source = (self.repo.root / "internal/cli/cli.go").read_text()
                result = self.repo.prepare(part=part)
                self.assertIn("first release", result.stderr)
                self.assert_failure_without_writes(result, before_changelog, before_source)

    def test_patch_minor_and_major_bumps_reset_lower_parts(self):
        expected = {"PATCH": "1.2.4", "MINOR": "1.3.0", "MAJOR": "2.0.0"}
        for part, target in expected.items():
            with self.subTest(part=part):
                self.repo.clear_tags()
                self.repo.write_state("1.2.3", PENDING, OLD_SECTION)
                self.repo.tag("v1.2.3")
                result = self.repo.prepare(part=part)
                self.assert_success(result)
                self.assertEqual(json.loads(result.stdout)["version"], target)
                self.assertIn(f'var Version = "{target}"', (self.repo.root / "internal/cli/cli.go").read_text())

    def test_rc_uses_highest_numeric_suffix_and_leaves_inputs_untouched(self):
        self.repo.write_state("1.2.3", PENDING, OLD_SECTION)
        self.repo.tag("v1.2.3", "v1.2.4-rc0", "v1.2.4-rc2", "v1.2.4-rc10", "v1.2.4-rc03")
        changelog = (self.repo.root / "CHANGELOG.md").read_bytes()
        source = (self.repo.root / "internal/cli/cli.go").read_bytes()
        result = self.repo.prepare(rc="true")
        self.assert_success(result)
        self.assertEqual(
            json.loads(result.stdout),
            {"tag": "v1.2.4-rc11", "version": "1.2.4-rc11", "release_candidate": True},
        )
        self.assertEqual((self.repo.root / "CHANGELOG.md").read_bytes(), changelog)
        self.assertEqual((self.repo.root / "internal/cli/cli.go").read_bytes(), source)
        self.assertEqual((self.repo.root / "notes.md").read_text(), "## v1.2.4-rc11\n\n" + PENDING + "\n")

    def test_stable_moves_pending_once_and_preserves_history(self):
        self.repo.write_state("1.2.3", PENDING, OLD_SECTION)
        self.repo.tag("v1.2.3")
        result = self.repo.prepare(part="MINOR")
        self.assert_success(result)
        changelog = (self.repo.root / "CHANGELOG.md").read_text()
        self.assertEqual(changelog.count(PENDING), 1)
        self.assertIn("## Unreleased\n\n## v1.3.0\n\n" + PENDING, changelog)
        self.assertTrue(changelog.endswith(OLD_SECTION))
        notes_result = self.repo.notes("v1.3.0", notes="stable-notes.md")
        self.assert_success(notes_result)
        self.assertEqual(notes_result.stdout, "")
        self.assertEqual((self.repo.root / "stable-notes.md").read_text(), "## v1.3.0\n\n" + PENDING + "\n")

    def test_notes_command_extracts_unreleased_for_rc_without_git_or_source(self):
        (self.repo.root / ".git").rename(self.repo.root / "git-away")
        (self.repo.root / "internal/cli/cli.go").unlink()
        result = self.repo.notes("v0.0.1-rc0")
        self.assert_success(result)
        self.assertEqual(result.stdout, "")
        self.assertEqual((self.repo.root / "notes.md").read_text(), "## v0.0.1-rc0\n\n" + PENDING + "\n")
        (self.repo.root / "CHANGELOG.md").write_text(
            "# Changelog\n\n## Unreleased\n\n" + OLD_SECTION,
            encoding="utf-8",
        )
        stable = self.repo.notes("v1.2.3", notes="stable-notes.md")
        self.assert_success(stable)
        self.assertEqual(stable.stdout, "")
        self.assertEqual(
            (self.repo.root / "stable-notes.md").read_text(),
            "## v1.2.3\n\n### Fixed\n\n- Previous fix.\n",
        )

    def test_empty_duplicate_and_malformed_changelog_are_rejected(self):
        cases = {
            "empty": "### Added",
            "no heading": "- Entry without a category.",
            "duplicate unreleased": PENDING + "\n\n## Unreleased\n\n" + PENDING,
            "invalid version": PENDING + "\n\n## v01.2.3\n\n### Fixed\n\n- Old.",
            "ascending history": PENDING + "\n\n## v1.2.3\n\n- A.\n\n## v1.3.0\n\n- B.",
            "unexpected section": PENDING + "\n\n## Draft\n\n- Hidden.",
            "unsupported later category": PENDING + "\n\n### Mystery\n\n- Hidden.",
        }
        for name, pending in cases.items():
            with self.subTest(name=name):
                self.repo.clear_tags()
                self.repo.write_state("0.0.1", pending)
                before_changelog = (self.repo.root / "CHANGELOG.md").read_text()
                before_source = (self.repo.root / "internal/cli/cli.go").read_text()
                result = self.repo.prepare()
                self.assert_failure_without_writes(result, before_changelog, before_source)

        self.repo.write_state(
            "0.0.1",
            PENDING,
            intro="# Changelog\n\n## v0.0.0\n\n### Added\n\n- Misordered history.\n",
        )
        before_changelog = (self.repo.root / "CHANGELOG.md").read_text()
        before_source = (self.repo.root / "internal/cli/cli.go").read_text()
        result = self.repo.prepare()
        self.assertIn("must be the first level-two", result.stderr)
        self.assert_failure_without_writes(result, before_changelog, before_source)

    def test_source_and_latest_tag_mismatches_are_rejected(self):
        cases = (("1.2.2", ("v1.2.3",), "does not match"), ("1.2.3", (), "has no Git tag"))
        for source_version, tags, message in cases:
            with self.subTest(source_version=source_version, tags=tags):
                self.repo.clear_tags()
                self.repo.write_state(source_version, PENDING, OLD_SECTION)
                self.repo.tag(*tags)
                before_changelog = (self.repo.root / "CHANGELOG.md").read_text()
                before_source = (self.repo.root / "internal/cli/cli.go").read_text()
                result = self.repo.prepare()
                self.assertIn(message, result.stderr)
                self.assert_failure_without_writes(result, before_changelog, before_source)

    def test_existing_target_stable_tag_is_rejected(self):
        self.repo.write_state("1.2.3", PENDING, OLD_SECTION)
        self.repo.tag("v1.2.3", "v1.2.4")
        before_changelog = (self.repo.root / "CHANGELOG.md").read_text()
        before_source = (self.repo.root / "internal/cli/cli.go").read_text()
        result = self.repo.prepare()
        self.assertIn("stable tag v1.2.4 already exists", result.stderr)
        self.assert_failure_without_writes(result, before_changelog, before_source)


if __name__ == "__main__":
    unittest.main()
