# Installation

## Linux and macOS

Install the latest stable release without Go or administrator privileges:

```sh
curl -fsSL https://raw.githubusercontent.com/ydb-platform/sqlc-ydb/main/install.sh | bash
```

This permanent URL serves the installer from `main`. The installer downloads published release binaries, not development builds. It selects amd64 or arm64, verifies the release SHA256 checksum and installs to `~/.local/bin`. If needed, it prints a command to update `PATH`; it does not edit shell startup files. Run the same command again to update. Download or verification failures leave the previous binary intact. Windows users should use the ZIP archives below.

To select a version (including a published RC) or an installation directory:

```sh
curl -fsSL https://raw.githubusercontent.com/ydb-platform/sqlc-ydb/main/install.sh | bash -s -- --version v0.1.0 --bin-dir ./bin
```

Replace `v0.1.0` with a published tag. The default excludes prereleases; if no stable release exists, select a published RC explicitly. Draft releases cannot be installed. Use `--help` to see available arguments. To uninstall, remove the installed executable.

For CI, pin both the installer and binary to a release tag containing `install.sh`. Download and execute separately so a failed download fails the step even without shell pipeline error handling:

```sh
set -eu
version=v0.1.0 # Replace with a published tag containing install.sh.
curl -fsSL "https://raw.githubusercontent.com/ydb-platform/sqlc-ydb/$version/install.sh" -o install.sh
bash install.sh --version "$version" --bin-dir ./bin
./bin/sqlc-ydb version --verbose
./bin/sqlc-ydb generate
```

## Build from source

With Go 1.26 or newer, run from a checkout of this repository:

```sh
go build -o bin/sqlc-ydb ./cmd/sqlc-ydb
./bin/sqlc-ydb version
./bin/sqlc-ydb version --verbose
```

The executable includes the YQL parser, analyzer and generators. Generation does not require a running YDB or language SDKs. Applications using the generated code need the dependencies described in the [target reference](targets.md). See the [quick start](../README.md#quick-start) for the first generation.

## Release archives

Check [GitHub Releases](https://github.com/ydb-platform/sqlc-ydb/releases) for published versions and assets. Build from source if a release is not yet available.

The packaging format covers these targets:

| Platform | Architectures | Archive |
| --- | --- | --- |
| Linux | `amd64`, `arm64` | `.tar.gz` |
| macOS (`darwin` in filenames) | `amd64`, `arm64` | `.tar.gz` |
| Windows | `amd64`, `arm64` | `.zip` |

Download the archive for your operating system and architecture together with `SHA256SUMS` from the same release. Archive names follow `sqlc-ydb_VERSION_OS_ARCH.tar.gz` (or `.zip` for Windows), with no leading `v` in `VERSION`. Each archive contains a matching directory with the executable and `LICENSE`; the Windows executable is `sqlc-ydb.exe`.

Compare the archive's SHA256 digest with its entry in `SHA256SUMS` before extracting. For example, replacing `ARCHIVE` with the downloaded filename:

```sh
# Linux
sha256sum ARCHIVE
# macOS
shasum -a 256 ARCHIVE
```

On Windows PowerShell, use `Get-FileHash .\ARCHIVE -Algorithm SHA256`. After extraction, place the executable in a directory on your `PATH` or invoke it by its full path.

## Identify the installed build

`sqlc-ydb version` prints the product version, including an RC suffix if any. `sqlc-ydb version --verbose` also prints the embedded source commit for release builds. Ordinary source builds report `unknown` for that field unless linker flags supply it. Include both values when reporting an issue.

Release candidates are for evaluation before a stable release.
