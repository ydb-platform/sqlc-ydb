# Security Policy

## Supported Versions

Security fixes are released for the latest published version of sqlc-ydb. Older releases are not supported; users should upgrade to the [latest release](https://github.com/ydb-platform/sqlc-ydb/releases/latest).

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Older releases | No |

## Reporting a Vulnerability

Do not open a public issue for a suspected vulnerability. Email [security@ydb.tech](mailto:security@ydb.tech) with the subject `[sqlc-ydb security]` and include, where possible:

- The affected sqlc-ydb version or commit
- The affected target language and runtime
- A description of the vulnerability and its potential impact
- Minimal reproduction steps or a proof of concept
- Any known mitigations or suggested fixes
- A safe way to contact you for follow-up

Do not include credentials, production data, or other secrets in the report. The maintainers will investigate the report, coordinate remediation and disclosure with the reporter, and publish an advisory when appropriate.

For non-security defects, use the public [bug report form](https://github.com/ydb-platform/sqlc-ydb/issues/new?template=bug_report.yml).
