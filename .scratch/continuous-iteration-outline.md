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
- Frontend interface-node auxiliary slice: reference interface-node history and
  field contract pages are served from generic static assets.
- Frontend agent test slice: reference Agent Test Kit page is served with a
  clean empty-state API contract.
- Frontend case evidence slice: reference API Case Evidence page is served with
  empty timing and incomplete-batch API contracts.
- Frontend evidence viewer slice: reference Evidence Viewer page is served with
  a neutral local-storage key and clean empty state.
- Frontend trace topology slice: reference Trace Topology page is served with a
  clean missing-run recovery state.
- Frontend agent run detail slice: reference Agent Run page is served with a
  clean missing-run recovery state.
- Frontend API Case workbench slice: reference API Case page is served with a
  profile-backed capability API.
- Frontend replay evidence slice: reference Replay Evidence page is served with
  a clean missing-trace recovery state.
- Frontend legacy script asset slice: reference dashboard and workflow catalog
  root scripts are served as neutral static assets.
- Frontend trace evidence React slice: `trace-call.html` and
  `trace-evidence.html` are served through a neutral source-built React bundle.
- Frontend workflow blueprint React slice: `workflow-blueprint-demo.html` and
  `workflow-blueprint-new.html` are served through a neutral source-built
  blueprint bundle.
- API Case run API slice: `/api/cases/run` executes the generic API Case
  runner, writes Evidence, indexes Store records, and returns a viewer URL.
- API Case profile run config slice: profile API Case assets can provide
  `casePath`, `baseUrl`, `evidenceDir`, `timeoutSeconds`, and
  `defaultOverrides` to the workbench.
- API Case override slice: control-plane and CLI runs apply request body
  overrides and record the resolved request in Evidence.
- API Case evidence slice: `/api/case/evidence` exposes raw request and
  response bodies from local Evidence files with Store summary fallback.
- API Case report slice: run API responses include operation, HTTP code, and
  response byte summaries for the frontend result panel.
- Store contract hardening slice: run summaries, API Case summaries, and
  Evidence diagnostic metadata are covered by the SQLite Store contract.
- Workflow audit API slice: `/api/workflow-audit` exposes profile reference
  audit plus optional Store-backed latest run and binding-case state.
- Frontend workflow React slice: `workflow-run.html`, `workflow-detail.html`,
  and `workflow-step.html` are served through source-built React bundles.
- Frontend interface-node React slice: interface-node directory, detail,
  history, and field contract pages are served through source-built React
  bundles.
- Profile import API slice: `/api/profile/import` imports local profile bundles
  into the Store and can include a profile audit report.
- Frontend run evidence React slice: Agent run, replay evidence, and trace
  topology recovery/workbench pages are served through source-built React
  bundles.
- Frontend workbench import React slice: the home workbench is served through a
  source-built React bundle and exposes the local profile import API.

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

### Task 2: Expand Store-Backed Workbench APIs

Goal:
- Replace remaining placeholder or empty-only workbench APIs with Store-backed
  generic contracts where the Store already has enough data.

Candidate APIs:
- `/api/case/incomplete-batches`
- `/api/agent-test`
- missing Store-backed detail fields in Workflow and API Case views

Acceptance:
- Contract tests cover the new response shape.
- A headless page smoke proves the frontend consumes the Store-backed response.
- `go test ./...`, `npm run build:frontend`, and the source-domain scan pass
  after each slice.
