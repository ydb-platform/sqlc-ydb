# Contributing to sqlc-ydb

Thank you for helping improve sqlc-ydb. Contributions can include bug reports, feature proposals, documentation, tests, and code.

All participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md). Report suspected security vulnerabilities through the private process in [SECURITY.md](SECURITY.md), not through a public issue.

## Before You Start

Search the [existing issues](https://github.com/ydb-platform/sqlc-ydb/issues) before opening a new one. Use the repository's bug report or feature request form so maintainers have the context needed to evaluate the change.

For non-trivial features or changes to generated APIs, open an issue before implementation. Describe the use case, the relevant YQL and target language or runtime, the desired generated API, and any compatibility impact. Early discussion helps keep the implementation consistent across the analyzer and all affected generators.

## Development Setup

Exact toolchain and test prerequisites are maintained in [the development guide](.agents/development.md). A normal generator build requires Go. Install the compiler and SDK dependencies documented there for every generated target you change because the standard Go suite does not run every target-specific check.

Clone your fork, create a focused branch from the current `main`, and download the Go dependencies:

```sh
go mod download
```

Run the baseline test suite before making code changes:

```sh
make test
```

## Making Changes

- Keep each pull request focused on one change.
- Add a regression test for a bug before fixing it, and test both successful behavior and actionable diagnostics where relevant.
- Keep YQL semantic analysis before code generation. Do not make generators guess types or silently ignore unsupported syntax or options.
- Do not hand-edit generated examples or golden fixtures. Change the source, regenerate with `make generate`, and review every generated diff.
- Update public documentation and the `Unreleased` section of [CHANGELOG.md](CHANGELOG.md) when behavior visible to users changes.
- Preserve third-party license notices and update [source provenance](docs/provenance.md) when adapting external source or tests.

The maintained architecture, code map, and implementation boundaries are in [AGENTS.md](AGENTS.md) and the linked `.agents/` documents.

## Testing

Run focused package tests while developing. Before submitting a code, configuration, or generated-output change, run the integrated checks:

```sh
make check
```

`make check` verifies release tooling, regenerates examples, runs linters and Go tests, checks generated output drift, builds the Go examples, and validates Python syntax. Some runtime-specific and live YDB checks are opt-in; run the checks named in [the development guide](.agents/development.md) for every affected target.

For documentation-only changes, stage each changed file explicitly with `git add -- <path>`, review the staged diff, and at minimum run:

```sh
git diff --cached --check
```

## Pull Requests

In the pull request description:

- Explain the problem and the chosen solution.
- Link the related issue, or explain why no issue is needed.
- List the exact validation commands and their results.
- Call out user-visible compatibility, generated API, or migration effects.
- Include regenerated output and documentation when they are part of the change.

Maintainers may ask for changes to keep behavior consistent across supported targets. Review feedback is part of the contribution process; please keep follow-up commits focused and leave resolved discussion visible for future readers.
