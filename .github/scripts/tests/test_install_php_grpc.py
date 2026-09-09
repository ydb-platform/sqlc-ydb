"""Exercise installer control flow without Linux, root, network or packaged PHP."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parent.parent / "install-php-grpc"


class InstallPhpGrpcTests(unittest.TestCase):
    def run_installer(self, incompatible=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            hooks = root / "hooks"
            hooks.write_text(r'''
dpkg() { echo amd64; }
curl() { :; }
sha256sum() { cat >/dev/null; }
apt-get() { echo 'php8.2-common is not installable' >&2; return 100; }
dpkg-deb() {
  mkdir -p "$3/usr/lib/php/20220829"
  echo binary > "$3/usr/lib/php/20220829/grpc.so"
}
php() {
  if [[ "$*" == *'ini_get'* ]]; then
    echo "$TEST_ROOT/extensions"
  elif [[ "$1" == -n && "$TEST_INCOMPATIBLE" == 1 ]]; then
    echo 'Unable to load dynamic library' >&2
    return 1
  fi
}
sudo() {
  case "$1" in
    install) command install "$2" "$3" "$4" ;;
    tee) cat > "$TEST_ROOT/grpc.ini" ;;
    phpenmod) test -f "$TEST_ROOT/extensions/grpc.so" ;;
    *) "$@" ;;
  esac
}
''')
            (root / "extensions").mkdir()
            script = root / "installer"
            script.write_text(SCRIPT.read_text().replace(
                ". /etc/os-release", "ID=ubuntu; VERSION_ID=24.04"
            ))
            result = subprocess.run(
                ["bash", str(script)], text=True, capture_output=True,
                env={**os.environ, "BASH_ENV": str(hooks),
                     "TEST_ROOT": str(root), "TEST_INCOMPATIBLE": str(int(incompatible))},
            )
            return result, (root / "extensions/grpc.so").exists()

    def test_installs_with_php_absent_from_apt(self):
        result, installed = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(installed)

    def test_incompatible_binary_is_not_installed(self):
        result, installed = self.run_installer(incompatible=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Unable to load dynamic library", result.stderr)
        self.assertFalse(installed)


if __name__ == "__main__":
    unittest.main()
