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

## Open Task Queue

### Task 1: API Case Format Documentation

Status: open

Goal:
- Document the generic API Case JSON format and Evidence output contract.

Acceptance:
- `docs/api-case-format.md` covers request fields, assertions, dry-run output,
  live run output, and Store indexing.
- The example case links to the document.
- `go test ./...` and the source-domain scan pass.

### Task 2: Control Plane Profile Lists

Status: open

Goal:
- Show loaded services, workflows, interface nodes, and cases in generic pages
  or API endpoints.

Acceptance:
- Empty profile renders without errors.
- A larger profile renders counts and lists from data only.
- Headless smoke verifies the endpoints.

### Task 3: API Case Store Summary

Status: open

Goal:
- Store useful request, response, and assertion summaries for API Case runs.

Acceptance:
- Store records include compact JSON summaries rather than empty objects.
- Evidence files remain the source of detailed runtime records.

### Task 4: Optional Store Backend Boundary

Status: open

Goal:
- Prepare the Store backend interface for an optional team/hosted backend
  without changing the default local path.

Acceptance:
- SQLite remains default.
- Unsupported backend URLs fail with actionable errors.
- Documentation explains the backend boundary.

### Task 5: Evidence Import Report

Status: open

Goal:
- Emit a machine-readable report for runtime Evidence index imports.

Acceptance:
- Import output can be requested as JSON.
- Report includes counts, source path, target Store path, and profile id.
