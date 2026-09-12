# Release operations

Required reviews, documentation and consumer pilots are tracked in [the release plan](release-plan.md). Releases are started manually from **Actions → publish → Run workflow**. A tag push does not start a release.

## Changelog and version selection

Accumulate consumer-visible changes under `## Unreleased` in [CHANGELOG.md](../CHANGELOG.md). Describe the first release's capabilities; reserve `Fixed` for corrections to behavior in published versions. Do not add the next version heading or change the CLI version by hand.

The first-release headings are `Capabilities`, `Parity with upstream sqlc`, `Known limitations` and `Deliberate exclusions`. Later releases can use `Added`, `Changed`, `Deprecated`, `Removed`, `Fixed`, `Security` and `Compatibility`. `make test-release` checks the repository's actual changelog as well as synthetic versioning cases, including extraction after a stable release clears Unreleased.

The form has three inputs:

| Input | Effect |
| --- | --- |
| Version part | `PATCH` increments the patch number; `MINOR` increments the minor number and resets patch; `MAJOR` increments major and resets both. Choose based on the accumulated changes and compatibility impact. |
| Release candidate | Enabled by default. Creates the next `-rcN` tag and a **draft prerelease** with all binaries. Leaves the source version and Unreleased entries unchanged. |
| Dry run | Builds and checks everything without pushing commits, tags or GitHub Releases. Disabled by default. Enable it to rehearse publication. |

Before the first stable release, the base is **0.0.0**: `PATCH` selects **0.0.1**, `MINOR` selects **0.1.0**, and `MAJOR` selects **1.0.0**. The development CLI version remains 0.0.1 until stable preparation updates it; RC binaries receive their version at build time.

Release candidates use the highest existing suffix for the selected version plus one, starting at `rc0`. For example, `v0.0.1-rc3` does not prevent a first minor candidate `v0.1.0-rc0`. After the first stable release, the selected part is incremented from the last stable version; RC tags do not advance that base.

The maintainer chooses the version part. The workflow computes the number, but does not infer compatibility impact from prose. Empty Unreleased sections, inconsistent source/history versions and existing target tags fail preparation.

## Publication sequence

1. Complete the release gates, including the applicable SDK and sequential YDB acceptance checks.
2. Select the intended branch in the form and enable **Dry run**. A rehearsal can use a development branch; publication requires the default branch.
3. The workflow extracts release notes. For a stable release it updates the CLI version, moves pending notes under `## vVERSION`, leaves an empty Unreleased section, and creates a local release commit. RCs use the selected source commit.
4. It runs `make check`, builds the six archives sequentially, verifies their contents, Go build metadata and checksums on Linux, and executes the packaged Linux/amd64 binary. A Git bundle carries the exact prepared commit to the publication job, including the stable version and changelog changes.
5. Inspect the rehearsal, then start **publish** from the default branch with **Dry run** disabled. Releasing a stable version also requires clearing **Release candidate**. This run repeats the checks before publication.
6. After all checks pass, the workflow verifies that the branch has not moved, then pushes the prepared commit and tag together. It uploads the archives and checksums to a draft. Stable releases become public after upload; RC releases remain drafts, as in the SDK workflow.

Publication uses the repository's `GITHUB_TOKEN` with `contents: write`. Branch rules must permit its release commit; the workflow does not bypass protection. A branch update during verification requires a new run. Tag pushes made by this workflow do not need to trigger another workflow: packaging and verification already ran in the same invocation.

Existing tags and releases are not overwritten. If an upload fails after the push, the tag and possibly a draft remain. Inspect the failed run and recover using its verified artifacts; do not rerun version selection expecting it to repair the same release automatically.

## Artifacts

The release matrix is exactly:

| Platform | Architectures | Archive |
| --- | --- | --- |
| Linux | amd64, arm64 | `.tar.gz` |
| macOS (`darwin` in artifact names) | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

Each archive contains `sqlc-ydb` (or `sqlc-ydb.exe`) and `LICENSE` in a directory named `sqlc-ydb_VERSION_OS_ARCH`. `SHA256SUMS` covers all six archives. Builds use `CGO_ENABLED=0`; applications using generated code still need their SDKs.

`sqlc-ydb version` prints the version, including any RC suffix. `sqlc-ydb version --verbose` also prints the embedded commit. Release builds carry the exact prepared source commit. Ordinary source builds report `unknown` for the commit unless linker flags supply it.

## Local checks

Version/changelog tests use Python's standard library and are part of `make check`; they can also run separately with `make test-release`. Packaging requires Bash, Python 3.9+, Go, `tar`, `zip`/`unzip`, and `sha256sum` or `shasum`. From a clean repository checkout, preview the next RC locally:

```sh
release_work=$(mktemp -d)
python3 .github/scripts/release-version.py prepare --part PATCH --rc true \
  --notes "$release_work/notes.md" >"$release_work/plan.json"
release_tag=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["tag"])' "$release_work/plan.json")
release_commit=$(git rev-parse HEAD)
mkdir -p "$release_work/dist"
printf '%s\n' "$release_commit" >"$release_work/dist/COMMIT"
while read -r release_os release_arch; do
  .github/scripts/release build "$release_tag" "$release_commit" "$release_os" "$release_arch" "$release_work/dist"
done <.github/scripts/release-targets
.github/scripts/release verify "$release_tag" "$release_work/dist" .github/scripts/release-targets
```

This creates no Git commit, tag or release and leaves Unreleased intact. Using `--rc false` instead **edits CHANGELOG.md and the source version locally**; use a disposable checkout for stable-release rehearsals. The workflow commits those edits before building, so artifact metadata points to that prepared commit.

The verifier executes only the host's archive: Linux/amd64 in the workflow. Other targets are cross-compiled and checked for archive contents, Go command, GOOS, GOARCH, disabled CGO and source revision; they are not executed in CI. For a single-host probe, use a one-line targets file and a separate output directory; publication always uses the complete [target list](../.github/scripts/release-targets).

## Reference

The form, pending changelog entries, stable version commit and draft RC behavior follow the YDB Go SDK's [publish workflow](https://github.com/ydb-platform/ydb-go-sdk/blob/e2334b78e3adee03b9054a79108bae8e6516e9b8/.github/workflows/publish.yml), inspected at commit `e2334b78e3adee03b9054a79108bae8e6516e9b8`. sqlc-ydb also supports the major version choice, explicit Unreleased sections, a dry run, and checked binaries for all six targets before pushing release refs.

The consumer installer is `/install.sh`, served at the permanent raw GitHub `main/install.sh` URL. Keep this path stable. It resolves stable release tags and accepts explicit RC tags; it never installs a development build. Tagged source URLs pin the installer for CI. Installer regressions run in `make test-release`.
