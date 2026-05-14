# Continuous Iteration Outline

This outline tracks small verified slices for Open Test Sandbox. Keep tasks
generic, profile-driven, and local-first.

## Completed

- Store interface, SQLite backend, and contract tests.
- Store migrations and CLI status/migrate commands.
- Profile bundle loader, split asset directories, and empty profile.
- Generic Control plane for loaded profiles.
- External profile export fixture and profile import index.
- Runtime Evidence index import from a legacy SQLite source.
- Generic API Case dry-run and HTTP runner.
- API Case Store indexing.
- Local quickstart example.
- API Case format documentation.
- Control Plane profile asset list API.
- API Case Store summaries for requests, assertions, and response Evidence.
- Store backend URL boundary with SQLite as the default.
- Machine-readable Evidence import report.
- Workflow planning command.
- Request template rendering preview.
- Evidence query CLI.

## Open Task Queue

### Task 1: Baseline Gate CLI

Status: open

Goal:
- Let users inspect and update baseline gate state for a profile subject.

Acceptance:
- Commands use the generic Store interface.
- Gate state is profile-id and subject-id based.
- No domain-specific subject names appear in core.

### Task 2: Release Hygiene

Status: open

Goal:
- Prepare the repository for first external review.

Acceptance:
- README current status matches implemented capabilities.
- Runtime files remain ignored.
- A fresh clone can run quickstart commands.
