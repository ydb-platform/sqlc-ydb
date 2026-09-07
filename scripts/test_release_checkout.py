from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class ReleaseCheckoutTest(unittest.TestCase):
    def test_windows_checkout_preserves_release_input_bytes(self):
        paths = [
            "scripts/release",
            "scripts/release-targets",
            "examples/authors/queries.sql",
            "examples/authors/go/native/queries.sql.go",
        ]
        expected = {name: (ROOT / name).read_bytes() for name in paths}
        with tempfile.TemporaryDirectory() as temporary:
            checkout = Path(temporary)

            def git(*args):
                subprocess.run(
                    ["git", "-c", "core.autocrlf=true", *args],
                    cwd=checkout, check=True, capture_output=True,
                )

            git("init", "--quiet")
            attributes = ROOT / ".gitattributes"
            if attributes.exists():
                shutil.copyfile(attributes, checkout / ".gitattributes")
                git("add", ".gitattributes")
            for name, content in expected.items():
                path = checkout / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(content)
            git("add", "--", *paths)
            for name in paths:
                (checkout / name).unlink()
            git("checkout-index", "--all", "--force")
            for name, content in expected.items():
                with self.subTest(path=name):
                    self.assertEqual((checkout / name).read_bytes(), content)


if __name__ == "__main__":
    unittest.main()
