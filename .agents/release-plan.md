# Release readiness

The first stable version is selected in the release workflow. Its scope is recorded in [CHANGELOG.md](../CHANGELOG.md), with the detailed contract in [compatibility](../docs/compatibility.md). Publication steps and recovery rules are in [release operations](releasing.md).

## Before publication

- Run `make check` on the intended release commit. Require all three main CI jobs to pass: standalone checks, YDB acceptance and C++ acceptance, plus the separate lint workflow. SDK compilation and live database tests cover different failure modes; neither replaces the other. See [development](development.md) for local commands.
- Keep generated examples synchronized with their generators. The nine language review PRs (#2–#10) were merged by 2026-09-11. Later binding changes still need compilation against the pinned SDK and the affected live tests.
- Rehearse the publish workflow with **Dry run** enabled on the intended commit. The workflow cross-compiles six archives, checks their contents and metadata, and executes Linux/amd64. It does not execute the other five targets.
- Review the release notes and installation instructions against those artifacts. Record evidence with the exact commit and Actions run; an older green run does not validate a newer commit.

Preparing code and release notes does not publish a release. A maintainer starts publication after these checks pass.

## Follow-up validation

The ydb.tech guide should cover installation, schema/query inputs, generation, parameter types, result cardinality and caller-owned transactions. Use the [executable examples](../examples/README.md) and [target contracts](../docs/targets.md) as source material; run the guide against the selected release artifacts.

Production-corpus validation awaits approved query inputs and schema/type context. Keep raw queries and detailed reports outside Git. Report parsing, analysis, generation and SDK compilation separately: parsing success alone does not establish correct generated code. Do not execute production queries; reduce confirmed failures to shareable regression cases.

Before 1.0.0, validate the generated helpers in two or three consumer projects. Repository examples and SDK reviews provide coverage, but do not substitute for consumer feedback. The user selects the projects and decides readiness for 1.0.0.
