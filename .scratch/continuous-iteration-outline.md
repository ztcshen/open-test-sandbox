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

## Open Task Queue

### Task 1: Workflow Plan Command

Status: open

Goal:
- Load a profile workflow and print its bound steps without executing runtime
  actions.

Acceptance:
- A generic profile fixture can plan a workflow by id.
- Missing workflow ids fail with actionable errors.
- Output includes step id, node id, case id, and required flag.
- `go test ./...` and the source-domain scan pass.

### Task 2: Request Template Rendering

Status: open

Goal:
- Render a request template with fixture data into a concrete API Case request
  preview.

Acceptance:
- Template and fixture files remain profile-owned assets.
- Core rendering stays generic JSON/path/method oriented.
- Render output can be inspected without sending HTTP.

### Task 3: Evidence Query CLI

Status: open

Goal:
- List runs, case runs, and Evidence records from the local Store.

Acceptance:
- Query commands read from SQLite by default.
- Text output is useful for humans and can be requested as JSON.
- Missing records return not-found errors without panics.

### Task 4: Baseline Gate CLI

Status: open

Goal:
- Let users inspect and update baseline gate state for a profile subject.

Acceptance:
- Commands use the generic Store interface.
- Gate state is profile-id and subject-id based.
- No domain-specific subject names appear in core.

### Task 5: Release Hygiene

Status: open

Goal:
- Prepare the repository for first external review.

Acceptance:
- README current status matches implemented capabilities.
- Runtime files remain ignored.
- A fresh clone can run quickstart commands.
