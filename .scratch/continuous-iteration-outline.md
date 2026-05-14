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
- Review feedback: source-transfer notes removed from repo, and source-domain
  checks live under neutral guardrails.
- Frontend navigation slice: reference environment node and service inventory
  pages are served from generic static assets.
- Frontend run history slice: reference Workflow run page is served and handles
  an empty local run index.
- Frontend node detail slice: reference environment node detail page is served
  with a profile-backed interface-node list API.
- Frontend workflow detail slice: reference Workflow detail page is served and
  renders a clean empty-profile state.
- Frontend step detail slice: reference Workflow step page and local rendering
  helpers are served with a clean empty-profile state.
- Frontend workbench slice: reference index page is served at `/` with minimal
  empty-state APIs for local workbench panels.
- Frontend interface-node directory slice: reference interface-node list page is
  served from the profile-backed interface-node API.
- Frontend interface-node detail slice: reference interface-node detail page is
  served with a profile-backed detail API.

## Open Task Queue

### Task 1: Expand Profile-Driven Frontend Pages

Goal:
- Continue adapting frontend pages from the reference control plane while
  keeping domain text in profile/config bundles only.

Acceptance:
- Copied frontend source is scrubbed of source-domain terms before entering
  core/default assets.
- The React build and at least one headless page smoke pass.
- Core/profile separation remains intact.
- `go test ./...` and the source-domain scan pass after each slice.
