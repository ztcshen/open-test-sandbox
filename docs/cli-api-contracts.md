# CLI and API Surface

This document summarizes the current Open Test Sandbox command-line and
control-plane HTTP surfaces, then calls out where the two are not yet one-to-one.

Verification baseline: this page was checked against `cmd/otsandbox/main.go`,
`internal/controlplane/server.go`, and `go test ./...`.

## Scope

- CLI means `otsandbox ...`.
- API means routes exposed by `otsandbox serve`.
- Static HTML pages under `control-plane/static/` are UI entrypoints, not API
  contracts, so they are not counted as API parity targets here.
- SQLite Store is the active source of truth for the served control plane.
  Profile packages are import/export and review artifacts, not the daily
  maintenance surface.

## CLI Surface

| Area | CLI commands |
| --- | --- |
| General | `version`, `help` |
| Store | `store status`, `store upgrade` |
| Sandbox runtime | `sandbox start` |
| Profile lifecycle | `profile init`, `profile install`, `profile pack`, `profile list`, `profile inspect`, `profile audit`, `profile audit-plan`, `profile verify`, `profile import` |
| Profile generation/import planning | `profile generation-plan openapi`, `profile import-plan openapi`, `profile import-plan http-capture` |
| Config publication | `config publish` (`config apply` alias) |
| Executor planning | `executor plan` |
| Evidence | `evidence import`, `evidence list`, `evidence tasks` |
| Workflow | `workflow discover`, `workflow plan`, `workflow audit`, `workflow report` |
| Baseline | `baseline get`, `baseline set` |
| Template | `template render` |
| Interface node | `interface-node discover`, `interface-node case audit`, `interface-node case draft`, `interface-node case apply`, `interface-node case report` |
| API case | `case discover`, `case run`, `case incomplete-batches` |
| Case suite | `case suite report`, `case suite coverage`, `case suite stability`, `case suite priority`, `case suite brief`, `case suite quality`, `case suite quality-plan`, `case suite quality-report`, `case suite inspect`, `case suite plan`, `case suite impact`, `case suite impact-report` |
| Server | `serve` |

## API Surface

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/template-packages/import` | Publish a template package into Store and activate it in the running control plane. Legacy alias: `/api/profile/import`. |
| `POST` | `/api/template-packages/verify` | Audit, publish, and verify Store/read-model effects. Legacy alias: `/api/profile/verify`. |
| `POST` | `/api/template-packages/audit-plan` | Return a deterministic template package repair plan. Legacy alias: `/api/profile/audit-plan`. |
| `POST` | `/api/template-packages/install` | Install an archive or template package path into profile home. Legacy alias: `/api/profile/install`. |
| `GET` | `/api/template-packages/installed` | List installed template packages. Legacy alias: `/api/profile/installed`. |
| `GET` | `/api/profile` | Return current active profile id, display name, and counts. |
| `GET` | `/api/profile/assets` | Return current profile services, workflows, interface nodes, and API cases. |
| `GET` | `/api/template-packages/catalog-index` | Return active Store catalog index and config version. Legacy alias: `/api/profile/catalog-index`. |
| `GET` | `/api/state` | Return dashboard-friendly state from the active profile. |
| `POST` | `/api/sandbox/services` | Register or update a Store-backed sandbox service. |
| `POST` | `/api/sandbox/interfaces` | Register or update a Store-backed interface node, request template, API case, and execution config. |
| `GET` | `/api/dashboard` | Return dashboard summary, Store-aware when available. |
| `GET` | `/api/catalog` | Return catalog payload, Store-aware when available. |
| `GET` | `/api/interface-nodes` | List interface nodes; accepts `serviceId` and `operation`. |
| `GET` | `/api/interface-node` | Return interface-node detail; accepts `id`, plus optional run context. |
| `GET` | `/api/interface-node/coverage` | Return workflow/interface coverage. |
| `GET` | `/api/interface-node/coverage-gaps` | Return coverage gaps. |
| `GET` | `/api/workflow-audit` | Audit one workflow; requires `workflowId`. |
| `GET` | `/api/runs` | List workflow/replay/probe run headers. |
| `POST` | `/api/workflow-runs` | Persist a workflow run snapshot. |
| `GET` | `/api/workflow-runs/{runId}` | Return one workflow run. |
| `GET` | `/api/workflow-runs/step` | Return one run step; requires `runId` and `stepId`. |
| `GET` | `/api/workflow-runs/latest-step` | Return latest matching workflow step; requires `workflowId` and `stepId`. |
| `POST` | `/api/trace-topology/collect` | Query trace provider and persist topology for a run. |
| `GET` | `/api/agent-test` | Return agent workbench payload. |
| `GET` | `/api/case/runs` | List stored API case runs with failure category metadata. |
| `GET` | `/api/case/evidence` | Return case evidence by `caseRunId`, or by `runId` plus optional `caseId`/`stepId`. |
| `GET` | `/api/case-run/evidence` | Return case evidence by `caseRunId`. |
| `GET` | `/api/case/timing` | Return case-run timing summary; accepts `kind` and `maxAgeMinutes`. |
| `GET` | `/api/post-process-tasks` | List post-process tasks; requires `runId`, accepts `stepId`, `caseId`, `kind`, `status`. |
| `GET` | `/api/case/incomplete-batches` | List profile API cases without a passed Store run. |
| `GET` | `/api/case/suite-coverage` | Return suite coverage for selected cases. |
| `GET` | `/api/case/suite-inspection` | Return pre-run suite readiness. |
| `GET` | `/api/case/suite-plan` | Return selected ready cases and a batch-run request. |
| `GET` | `/api/case/suite-stability` | Return recent pass/fail stability. |
| `GET` | `/api/case/suite-priority` | Rank cases using change signals and Store history. |
| `GET` | `/api/case/suite-brief` | Return one-call suite triage. |
| `GET` | `/api/case/suite-quality` | Return suite authoring-readiness quality report. |
| `GET` | `/api/case/suite-quality-plan` | Return suite quality next actions. |
| `GET` | `/api/case/suite-impact` | Map change signals to impacted cases and a batch-run request. |
| `POST` | `/api/case/suite-impact-runs` | Plan impacted cases and start an async batch run. |
| `GET` | `/api/replay/evidence` | Return replay evidence shell by `traceId`. |
| `GET` | `/api/cases/capabilities` | Return runnable case capability payload. |
| `POST` | `/api/cases/run` | Run a case file by `casePath`. |
| `POST` | `/api/cases/batch-runs` | Start an async case batch by `caseIds`, `nodeIds`, `workflowId`, or `suite`. |
| `GET` | `/api/cases/batch-runs/{batchRunId}` | Poll async batch status. |
| `GET` | `/api/cases/batch-runs/{batchRunId}/report.html` | Fetch async batch HTML report. |
| `GET` | `/api/cases/batch-runs/{batchRunId}/report.junit.xml` | Fetch async batch JUnit report. |
| `GET` | `/api/cases/batch-runs/{batchRunId}/artifacts.json` | Fetch async batch artifact manifest. |
| `GET` | `/api/cases/batch-runs/{batchRunId}/failures.json` | Fetch async batch failure summary. |
| `POST` | `/api/test-kit/run` | Run a Store/profile API case by `caseId`. |
| `POST` | `/api/test-kit/run-batch` | Run Store/profile API cases by `caseIds`. |

Common suite selector query parameters are `filter`, `node`/`nodeId`, `tag` or
`tags`, `status`, `owner`, and `priority`. Planning and impact APIs also accept
`action`, `requestId`, `baseUrl`, `evidenceDir`, and `timeoutSeconds`. Impact
and priority APIs accept `signal`, `signals`, `change`, `changes`,
`changedPath`, and `changedPaths`.

## API/CLI Parity Matrix

| Capability | CLI | API | Parity |
| --- | --- | --- | --- |
| Serve control plane | `serve` | Not applicable | CLI-only bootstrap. |
| Version/help | `version`, `help` | None | CLI-only. |
| Store status and schema upgrade | `store status`, `store upgrade` | None | CLI-only. |
| Start registered sandbox service | `sandbox start` | None | CLI-only. |
| Register sandbox service/interface in Store | None | `/api/sandbox/services`, `/api/sandbox/interfaces` | API-only and important. This is currently the clearest Store-first API gap in CLI. |
| Profile install/list | `profile install`, `profile list` | `/api/template-packages/install`, `/api/template-packages/installed` | Mostly paired through Store-first aliases; legacy `/api/profile/*` routes remain. |
| Current profile summary/assets | `profile inspect` | `/api/profile`, `/api/profile/assets` | Partial. CLI inspects a package/reference; API reports the active served profile. |
| Profile import/publish | `profile import`, `config publish` | `/api/template-packages/import` | Mostly paired through Store-first alias. CLI can also audit/require audit ok. |
| Profile verify | `profile verify` | `/api/template-packages/verify` | Paired through Store-first alias. |
| Profile audit repair plan | `profile audit-plan` | `/api/template-packages/audit-plan` | Paired through Store-first alias. |
| Profile init/pack/audit | `profile init`, `profile pack`, `profile audit` | None | CLI-only package authoring. |
| Profile import/generation planning | `profile import-plan ...`, `profile generation-plan openapi` | None | CLI-only. |
| Profile catalog index | None | `/api/template-packages/catalog-index` | API-only Store-first alias; legacy `/api/profile/catalog-index` remains. |
| Catalog/dashboard/state | Roughly `profile inspect`, discovery commands | `/api/state`, `/api/dashboard`, `/api/catalog` | API-first UI payloads; no exact CLI. |
| Interface-node discovery/list | `interface-node discover` | `/api/interface-nodes`, `/api/interface-node` | Partial. CLI has search filter; API has service/operation/detail. |
| Interface-node coverage | None | `/api/interface-node/coverage`, `/api/interface-node/coverage-gaps` | API-only. |
| Interface-node case authoring | `interface-node case audit/draft/apply` | None | CLI-only package authoring. |
| Single interface report | `interface-node case report` | `/api/cases/batch-runs` with `nodeIds` | Partial. CLI is synchronous and writes report files; API is async and process-local. |
| Case discovery/capabilities | `case discover` | `/api/cases/capabilities`, `/api/catalog` | Partial. CLI has richer maintenance filters. |
| Single case run by file | `case run --case PATH` | `/api/cases/run` with `casePath` | Paired. |
| Single case run by catalog id | None | `/api/test-kit/run` with `caseId` | API-only. |
| Case batch run | `case suite report`, `workflow report`, `interface-node case report` | `/api/cases/batch-runs`, `/api/test-kit/run-batch` | Partial. CLI variants are synchronous reports; API variants are async or test-kit oriented. |
| Case run list/evidence | No exact command | `/api/case/runs`, `/api/case/evidence`, `/api/case-run/evidence` | API-only. CLI has `evidence list`, but not case-focused detail payloads. |
| Case timing | None | `/api/case/timing` | API-only. |
| Incomplete case batches | `case incomplete-batches` | `/api/case/incomplete-batches` | Paired. |
| Suite coverage | `case suite coverage` | `/api/case/suite-coverage` | Paired. |
| Suite inspection | `case suite inspect` | `/api/case/suite-inspection` | Paired. |
| Suite plan | `case suite plan` | `/api/case/suite-plan` | Paired. |
| Suite stability | `case suite stability` | `/api/case/suite-stability` | Paired. |
| Suite priority | `case suite priority` | `/api/case/suite-priority` | Paired. |
| Suite brief | `case suite brief` | `/api/case/suite-brief` | Paired. |
| Suite quality | `case suite quality` | `/api/case/suite-quality` | Paired. |
| Suite quality plan | `case suite quality-plan` | `/api/case/suite-quality-plan` | Paired. |
| Suite quality HTML/JSON report | `case suite quality-report` | None | CLI-only artifact generation. |
| Suite impact plan | `case suite impact` | `/api/case/suite-impact` | Paired. |
| Suite impact run/report | `case suite impact-report` | `/api/case/suite-impact-runs` | Partial. CLI is synchronous report generation; API starts async execution. |
| Workflow discovery | `workflow discover` | `/api/catalog`/profile assets only | Partial. No dedicated API filter equivalent. |
| Workflow plan | `workflow plan` | None | CLI-only. |
| Workflow audit | `workflow audit` | `/api/workflow-audit` | Paired. |
| Workflow report/run | `workflow report` | `/api/cases/batch-runs` with `workflowId`, `/api/workflow-runs` | Partial. API has async execution and persisted run snapshots, not the same synchronous report command. |
| Workflow run lookup | None | `/api/runs`, `/api/workflow-runs/*` | API-only. |
| Trace topology collection | No exact command | `/api/trace-topology/collect` | API-only. CLI reports may schedule related work indirectly. |
| Replay evidence shell | None | `/api/replay/evidence` | API-only. |
| Post-process task lookup | `evidence tasks` | `/api/post-process-tasks` | Paired. |
| Evidence import/list | `evidence import`, `evidence list` | No exact API | CLI-only. |
| Executor plan | `executor plan` | None | CLI-only. |
| Baseline get/set | `baseline get`, `baseline set` | None | CLI-only. |
| Template render | `template render` | None | CLI-only. |

## Main Differences

The surfaces are not yet one-to-one. The current design has three distinct
classes of mismatch:

1. Store-first API capabilities without CLI adapters:
   `/api/sandbox/services` and `/api/sandbox/interfaces` let users register
   runtime facts and executable interface cases directly into Store. There is no
   `otsandbox sandbox register ...` or equivalent CLI command today.

2. CLI package-authoring capabilities without API endpoints:
   profile initialization, packing, audit, import-plan generation, OpenAPI
   negative-case generation, interface-node case draft/apply, executor planning,
   baseline updates, and template rendering are available from CLI only.

3. Same domain but different execution model:
   several CLI commands synchronously produce local reports, while the API starts
   async runs and exposes process-local polling/report URLs. This affects
   `case suite report`, `interface-node case report`, `workflow report`, and
   `case suite impact-report`.

There are also naming and selector differences:

- CLI flags use `--profile`, while served APIs operate on the active Store-backed
  profile for the running server.
- CLI uses `--node`; suite APIs accept both `node` and `nodeId`.
- CLI `case run` runs a case file path, while `/api/test-kit/run` runs a catalog
  case id. `/api/cases/run` is the closer API match for `case run`.
- Existing older prose used pre-profile package terminology; current code and
  help text use `profile`. New docs should prefer `profile`.

## Recommended Parity Work

If the product goal is API/CLI one-to-one capability, prioritize parity in this
order:

1. Add CLI wrappers for Store-first registration:
   `otsandbox sandbox service register ...` and
   `otsandbox sandbox interface register ...`, backed by the same Store write
   path as `/api/sandbox/services` and `/api/sandbox/interfaces`.

2. Add read CLI commands for API-only Store views:
   `profile catalog-index`, `case runs`, `case evidence`, `case timing`,
   `workflow runs`, `workflow run`, and `trace topology collect`.

3. Add API endpoints for CLI-only daily testing helpers:
   `executor plan`, `evidence import/list`, `baseline get/set`, and
   `template render`.

4. Decide whether package-authoring commands should be API contracts:
   `profile init/pack/audit/import-plan/generation-plan` and
   `interface-node case draft/apply` may remain CLI-only if they are intended as
   offline authoring tools. If they are part of the workbench product surface,
   expose them under `/api/profile/*` and `/api/interface-node/case/*`.

5. Normalize report execution semantics:
   either add synchronous API report endpoints, or add CLI commands that start
   async batch runs and poll `/api/cases/batch-runs/{batchRunId}`. The current
   split is workable, but it is not one-to-one.

## Notes for Future Changes

- When adding a new CLI command, add the corresponding API row or explicitly
  mark it CLI-only in this document.
- When adding a new API endpoint, add the corresponding CLI row or explicitly
  mark it API-only in this document.
- Keep selector names aligned where possible. If aliases are necessary, document
  both names.
- Prefer Store-first APIs and UI paths for new daily testing behavior; keep
  profile package flows as import/export or compatibility bridges.
