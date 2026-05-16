# Continuous Iteration Outline

This outline tracks small verified slices for Open Test Sandbox. Keep tasks
generic, profile-driven, and local-first.

## Completed

- Store interface, SQLite backend, and contract tests.
- Store schema upgrades and CLI status/upgrade commands.
- Profile bundle loader, split asset directories, and empty profile.
- Generic Control plane for loaded profiles.
- External profile export fixture and profile import index.
- Runtime Evidence index import from a legacy SQLite source.
- Generic API Case preview and HTTP runner.
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
- Frontend Evidence Viewer React slice: case Evidence viewing and topology
  rendering are served through a source-built React bundle.
- Active profile import slice: importing a local profile now switches the
  running control plane to the imported bundle for profile, catalog, and
  interface-node APIs.
- Agent test workbench Store slice: `/api/agent-test` now exposes generic
  Store run history, status/failure summaries, and active profile metadata for
  the React workbench.
- Interface node run history slice: `/api/interface-node` now hydrates case
  `latestRun`, node history, and evidence run indexes from Store API case run
  records.
- Workflow catalog run state slice: `/api/catalog` now hydrates workflow
  `runCount` and `latestRun` from Store runs, and the React workflow catalog
  and detail pages render that state.
- API Case capability run state slice: `/api/cases/capabilities` now hydrates
  case `runCount` and `latestRun` from Store API case records, and the React
  API Case page renders the selected case run state.
- Workflow step run evidence slice: `workflow-step.html` now reads the latest
  Store-backed step run and renders status plus request/response summaries.
- Workflow step context/service slice: `workflow-step.html` now renders
  reference-style context and service evidence panels from catalog and runtime
  snapshot data.
- Workflow run step evidence slice: `workflow-run.html` now renders stored
  step summaries with catalog service links, step detail links, topology
  filters, and body-health diagnostics.
- Workflow detail interface coverage slice: `workflow-detail.html` now consumes
  interface-node coverage APIs and renders mapped/unmapped step rows plus a
  coverage-gap JSON entrypoint.
- Workflow detail runner slice: `workflow-detail.html` now runs Workflow steps
  through the generic Test Kit preview path, saves a Store-backed Workflow run,
  and links to the saved run evidence page.
- API Case post-run refresh slice: `api-cases.html` now refreshes Store-backed
  case run capability state after one-click runs while preserving the selected
  case.
- React-only static surface slice: legacy top-level `dashboard.js` and
  `workflows.js` are no longer served; dashboard/catalog pages use only the
  source-built React bundles.
- Store schema terminology slice: SQLite Store version management now uses
  neutral schema upgrade terms across package names, CLI, docs, and tests.
- Template config schema slice: SQLite Store schema version 2 now includes the
  generic template, node, workflow, interface-node, case, and fixture tables
  from the reference control-plane database model.
- Profile catalog indexing slice: profile import now projects services,
  workflows, interface nodes, API cases, request templates, bindings,
  fixtures, dependencies, and template/config rows into the generic
  template-config Store tables.
- CLI profile catalog indexing slice: CLI and API profile imports now share the
  same profile-to-Store catalog projection path.
- External profile init slice: `profile init` creates a neutral external bundle
  skeleton and refuses output under the core repository profile directory.
- Audit-gated publish slice: CLI and Control plane imports can now require a
  clean pre-publish profile audit before writing Store/read-model state.
- Profile verification slice: `profile verify` now audits an external bundle,
  publishes it through the strict Store path, and checks profile index,
  catalog index, active config, and base read-model persistence.
- Control plane profile verify slice: `POST /api/profile/verify` and the
  workbench Profile panel now expose the same audit-gated publish and read-model
  persistence checks.
- External profile placement slice: `profile install` and `profile list` define
  the standard local profile home outside core (`$HOME/.otsandbox/profiles` by
  default, overrideable with `OTSANDBOX_PROFILE_HOME` or `--profile-home`).
  Profile-facing publish, audit, verify, and inspect commands can resolve either
  a filesystem path or an installed profile id.
- Control plane profile placement slice: profile home install/list/resolve logic
  now lives in `internal/profilehome`, `serve --profile` can resolve installed
  profile ids, and the workbench/API can install, list, import, and verify
  external bundles through the same profile-home rules.
- Runtime acceptance gate slice: `profile verify --require-case-runs`,
  `POST /api/profile/verify` with `requireCaseRuns`, and the workbench
  `要求用例已通过` option now require every declared API Case to have a latest
  passed Store run before acceptance succeeds.
- Workflow runtime acceptance gate slice: `profile verify --require-workflow-runs`,
  `POST /api/profile/verify` with `requireWorkflowRuns`, and the workbench
  `要求工作流已通过` option now apply the same latest-passed Store run gate to
  every declared Workflow.
- Profile verification diagnostics slice: CLI JSON output, `POST
  /api/profile/verify`, and the workbench now keep structured `summary` and
  per-check details for failed acceptance runs, so missing runtime gates are
  visible without rerunning or inspecting logs.
- Profile install hygiene slice: `profile init` writes ignore rules for local
  runtime files, and `profile install` skips generated runtime state, local
  databases, logs, and VCS directories when copying bundles into profile home.
- Profile home tolerance slice: `profile list`, `GET /api/profile/installed`,
  and the workbench selector now report malformed installed bundles as invalid
  items instead of failing the whole profile-home listing.
- Profile packaging slice: `profile pack` creates a clean `.tar.gz`
  distributable from either a filesystem path or installed profile id, using
  the same runtime/VCS filtering rules as profile installation.
- Profile archive install slice: `profile install --from bundle.tar.gz` can
  install packed profile archives into profile home and rejects unsafe archive
  entries that would escape the extracted profile root.
- Control plane profile archive publish slice: `POST /api/profile/import` and
  `POST /api/profile/verify` can accept packed profile archives, install them
  into profile home, then publish or verify the installed bundle in the
  Store/read-model path.
- CLI profile archive acceptance slice: `profile audit`, `profile import`,
  `profile verify`, and `config publish` can accept packed profile archives
  directly, install them into profile home, and use the stable installed path
  for audit and Store/read-model publication.

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
- missing Store-backed detail fields in Workflow and API Case views

Acceptance:
- Contract tests cover the new response shape.
- A headless page smoke proves the frontend consumes the Store-backed response.
- `go test ./...`, `npm run build:frontend`, and the source-domain scan pass
  after each slice.
