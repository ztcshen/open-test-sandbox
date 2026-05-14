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
- Baseline gate CLI.
- Release hygiene pass.

## Open Task Queue

### Task 1: First Review Feedback

Goal:
- Collect review feedback from a fresh checkout or external reviewer and turn it
  into the next small implementation queue.

Acceptance:
- Any issue found by review maps to a small, testable slice.
- Core/profile separation remains intact.
- `go test ./...` and the source-domain scan pass after each slice.
