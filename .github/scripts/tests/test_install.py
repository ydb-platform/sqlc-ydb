"""Exercise the piped installer with real archives and an offline download fixture."""
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "installed tools"
        self.mock = self.root / "mock"
        self.mock.mkdir()
        self.env = dict(os.environ, PATH=f"{self.mock}:{os.environ['PATH']}", FIXTURE=str(self.root))
        self.script = (ROOT / "install.sh").read_text()
        self.executable("uname", '#!/bin/bash\nif [[ $1 == -s ]]; then echo "${TEST_OS:-Linux}"; else echo "${TEST_ARCH:-x86_64}"; fi\n')
        self.executable("curl", '''#!/bin/bash
set -eu
out=''
for ((i=1; i<=$#; i++)); do
    if [[ ${!i} == --output ]]; then j=$((i+1)); out=${!j}; fi
done
url=${!#}
[[ ${FAIL_DOWNLOAD:-0} == 0 ]] || exit 22
if [[ $url == */latest ]]; then
    echo 'https://github.com/ydb-platform/sqlc-ydb/releases/tag/v0.1.0'
else
    cp "$FIXTURE/${url##*/}" "$out"
fi
''')

    def executable(self, name, text):
        path = self.mock / name
        path.write_text(text)
        path.chmod(0o755)

    def archive(self, version="0.1.0", os_name="linux", arch="amd64", binary_version=None, binary=None):
        base = f"sqlc-ydb_{version}_{os_name}_{arch}"
        archive = self.root / f"{base}.tar.gz"
        if binary is None:
            binary = f'#!/bin/sh\necho "{binary_version or version}"\n'.encode()
        with tarfile.open(archive, "w:gz") as output:
            member = tarfile.TarInfo(f"{base}/sqlc-ydb")
            member.size = len(binary)
            output.addfile(member, io.BytesIO(binary))
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        (self.root / "SHA256SUMS").write_text(f"{digest}  {archive.name}\n")

    def run_install(self, *args):
        return subprocess.run(["bash", "-s", "--", "--bin-dir", str(self.bin), *args],
                              input=self.script, text=True, capture_output=True, env=self.env)

    def test_platforms_latest_and_repeat(self):
        for system, os_name in [("Linux", "linux"), ("Darwin", "darwin")]:
            for machine, arch in [("x86_64", "amd64"), ("aarch64", "arm64")]:
                with self.subTest(system=system, arch=arch):
                    self.env.update(TEST_OS=system, TEST_ARCH=machine)
                    self.archive(os_name=os_name, arch=arch)
                    result = self.run_install()
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn("PATH", result.stdout)
                    self.assertEqual(subprocess.check_output([self.bin / "sqlc-ydb", "version"], text=True), "0.1.0\n")
                    result = self.run_install()
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn("already installed", result.stdout)

    def test_explicit_rc_and_upgrade(self):
        self.archive()
        self.assertEqual(self.run_install().returncode, 0)
        self.archive("0.2.0-rc1")
        result = self.run_install("--version", "v0.2.0-rc1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Installed sqlc-ydb 0.2.0-rc1", result.stdout)

    def test_failures_preserve_existing_binary(self):
        self.bin.mkdir()
        target = self.bin / "sqlc-ydb"
        target.write_text("old binary")
        for failure in ["download", "checksum", "version"]:
            with self.subTest(failure=failure):
                self.archive(binary_version="9.9.9" if failure == "version" else None)
                self.env["FAIL_DOWNLOAD"] = "1" if failure == "download" else "0"
                if failure == "checksum":
                    (self.root / "SHA256SUMS").write_text("0" * 64 + "  sqlc-ydb_0.1.0_linux_amd64.tar.gz\n")
                result = self.run_install("--version", "v0.1.0")
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(target.read_text(), "old binary")

    def test_invalid_inputs_and_truncated_script(self):
        for args in [("--version", "../../bad"), ("--version",), ("--unknown",)]:
            self.assertNotEqual(self.run_install(*args).returncode, 0)
        self.env["TEST_ARCH"] = "riscv64"
        self.assertIn("supported architectures", self.run_install().stderr)
        self.script = self.script.rsplit('install_sqlc_ydb "$@"', 1)[0]
        self.assertEqual(self.run_install().returncode, 0)
        self.assertFalse(self.bin.exists())

    def test_path_shadowing(self):
        self.archive()
        self.executable("sqlc-ydb", "#!/bin/sh\necho old\n")
        self.env["PATH"] += f":{self.bin}"
        result = self.run_install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("takes precedence", result.stdout)

    def test_real_binary_generates_code(self):
        binary = self.root / "built-sqlc-ydb"
        subprocess.run(["go", "build", "-o", str(binary), "-ldflags",
                        "-X github.com/ydb-platform/sqlc-ydb/internal/cli.Version=0.1.0",
                        "./cmd/sqlc-ydb"], cwd=ROOT, check=True, capture_output=True)
        self.archive(binary=binary.read_bytes())
        result = self.run_install("--version", "v0.1.0")
        self.assertEqual(result.returncode, 0, result.stderr)
        project = self.root / "project"
        project.mkdir()
        (project / "schema.sql").write_text("CREATE TABLE a (id Uint64 NOT NULL, PRIMARY KEY (id));")
        (project / "query.sql").write_text("-- name: GetA :many\nSELECT id FROM a;")
        (project / "sqlc.yaml").write_text("version: '2'\nsql:\n- engine: ydb\n  schema: schema.sql\n  queries: query.sql\n  gen:\n    go:\n      out: db\n")
        subprocess.run([str(self.bin / "sqlc-ydb"), "generate"], cwd=project, check=True, capture_output=True)
        self.assertIn("func (q *Queries) GetA", (project / "db" / "query.sql.go").read_text())
