# CLI and API Surface

This document summarizes the current Open Test Sandbox command-line and
control-plane HTTP surfaces, then calls out where the two are not yet one-to-one.

Verification baseline: this page was checked against `cmd/otsandbox/main.go`,
`internal/controlplane/server.go`, and `go test ./...`.

## Scope

- CLI means `otsandbox ...`.
- CLI is the primary operator surface for daily local testing workflows.
- API means local control-plane routes exposed by `otsandbox serve` for the
  workbench, automation, and agents; it is not a separate cloud product surface.
- Static HTML pages under `control-plane/static/` are UI entrypoints, not API
  contracts, so they are not counted as API parity targets here.
- PostgreSQL Store is the active source of truth for new daily testing
  workflows. Profile packages are import/export and review artifacts, not the
  daily maintenance surface.
- SQLite is retained only for legacy compatibility and old local runtime import
  paths during the PostgreSQL rollout.
- Daily CLI/API commands read and write the active Store by default, or the
  Store named by `--store NAME_OR_DSN` for a single command. SQLite paths are
  limited to legacy, compatibility, and import-test work.
- Environment Catalog operations are Store-first: register, discover, inspect,
  bootstrap, restore, verify, and publish-verified must share the selected
  Store contract across CLI, API, and UI.
- The sandbox control-plane Store is outside restored Docker target
  environments. Restore may start tested services and their business databases,
  but the PostgreSQL Store used to read the Environment Catalog must already be
  reachable and must remain separate.
- Verified environment discovery is gated by a passed verification workflow plus
  complete Evidence indexes and stored real SkyWalking topology.
- Docker runtime management is local-only for now. The parity target is local
  workflow operation, not remote Docker orchestration.
- API parity is required for daily testing operations: Store configuration
  visibility, local service registration, interface and workflow discovery,
  case registration/execution, run reports, Evidence, post-process status, and
  real topology lookup. Offline package authoring commands may remain CLI-only
  when they are review/migration utilities rather than workbench operations.
- Daily execution and report commands use the selected Store engine end to end.
  When PostgreSQL is selected, commands must not create hidden SQLite runtime
  databases; missing Store configuration fails with clear guidance instead of
  switching engines.

## CLI Surface

| Area | CLI commands |
| --- | --- |
| General | `version`, `help` |
| Store | `store config set/list/remove`, `store use`, `store current`, `store status`, `store upgrade`, `store ddl` |
| Environment catalog | `environment register`, `environment discover`, `environment inspect`, `environment bootstrap`, `environment restore`, `environment verify`, `environment publish-verified` |
| Sandbox runtime | `sandbox start`, `sandbox service register`, `sandbox interface register` |
| Template package lifecycle | `template-package ...` / `template-packages ...` aliases for `profile init`, `profile install`, `profile pack`, `profile list`, `profile inspect`, `profile audit`, `profile audit-plan`, `profile verify`, `profile import` |
| Template package generation/import planning | `template-package generation-plan openapi`, `template-package import-plan openapi`, `template-package import-plan http-capture` aliases for the legacy `profile ...` commands |
| Config publication | `config publish` (`config apply` alias) |
| Executor planning | `executor plan` |
| Evidence | `evidence import`, `evidence list`, `evidence tasks`, `replay evidence` |
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
| `GET` | `/api/template-packages/current` | Return current active template package id, display name, and counts. Legacy alias: `/api/profile`. |
| `GET` | `/api/template-packages/assets` | Return current template package services, workflows, interface nodes, and API cases. Legacy alias: `/api/profile/assets`. |
| `POST` | `/api/template-packages/import-plan/openapi` | Produce a review-only OpenAPI import plan for draft services, interface nodes, request templates, API cases, and runnable case files. |
| `POST` | `/api/template-packages/import-plan/http-capture` | Produce a review-only static HTTP capture import plan for draft services, interface nodes, request templates, API cases, and runnable case files. |
| `POST` | `/api/template-packages/generation-plan/openapi` | Produce a review-only OpenAPI generation plan for draft negative API case candidates. |
| `GET` | `/api/template-packages/catalog-index` | Return active Store catalog index and config version. Legacy alias: `/api/profile/catalog-index`. |
| `GET` | `/api/state` | Return dashboard-friendly state from the active profile. |
| `GET` | `/api/store/current` | Return the running control plane's selected Store metadata with the DSN password masked. |
| `POST` | `/api/sandbox/services` | Register or update a Store-backed sandbox service. |
| `POST` | `/api/sandbox/interfaces` | Register or update a Store-backed interface node, request template, API case, and execution config. |
| `POST` | `/api/environments` | Register or update an Environment Catalog entry in the active Store. |
| `GET` | `/api/environments` | Discover Environment Catalog entries from the active Store; verified discovery only returns entries promoted by `publish-verified`. |
| `GET` | `/api/environments/{environmentId}` | Inspect one environment, including runtime facts, workflow coverage metadata, recorded Evidence/topology completeness flags, and verification status. |
| `GET` | `/api/environments/{environmentId}/bootstrap` | Return the local clone/fetch, compose/start, health-check, and verification workflow plan for the environment. |
| `POST` | `/api/environments/{environmentId}/verify` | Persist verification run status and recorded Evidence/topology completeness flags after the configured acceptance workflow has run. |
| `POST` | `/api/environments/{environmentId}/publish-verified` | Promote an environment into verified discovery only after the recorded flags pass and the selected Store contains a passed verification run, indexed Evidence, and a complete SkyWalking topology row. |
| `GET` | `/api/dashboard` | Return dashboard summary, Store-aware when available. |
| `GET` | `/api/catalog` | Return catalog payload, Store-aware when available. |
| `GET` | `/api/interface-nodes` | List interface nodes; accepts `serviceId`, `operation`, and `filter`, matching `interface-node discover`. |
| `GET` | `/api/interface-node` | Return interface-node detail; accepts `id`, plus optional run context. |
| `GET` | `/api/interface-node/coverage` | Return workflow/interface coverage. |
| `GET` | `/api/interface-node/coverage-gaps` | Return coverage gaps. |
| `GET` | `/api/workflows` | List workflows with Store-first `filter` support, matching `workflow discover`. |
| `GET` | `/api/workflow-audit` | Audit one workflow; requires `workflowId`. |
| `GET` | `/api/workflow-plan` | Return workflow-bound steps; requires `workflowId`, accepts `workflow` alias. |
| `GET` | `/api/runs` | List workflow/replay/probe run headers. |
| `POST` | `/api/workflow-runs` | Persist a workflow run snapshot. |
| `GET` | `/api/workflow-runs/{runId}` | Return one workflow run. |
| `GET` | `/api/workflow-runs/step` | Return one run step; requires `runId` and `stepId`. |
| `GET` | `/api/workflow-runs/latest-step` | Return latest matching workflow step; requires `workflowId` and `stepId`. |
| `POST` | `/api/trace-topology/collect` | Query trace provider and persist topology for a run. |
| `GET` | `/api/agent-test` | Return agent workbench payload. |
| `GET` | `/api/executor/plan` | Return the active template package executor dry-run plan. |
| `POST` | `/api/evidence/import` | Import a legacy runtime Evidence SQLite index into the active Store. |
| `GET` | `/api/evidence/list` | List Store Evidence records and API case runs for all runs or `run`/`runId`; `evidenceRecords` use stable lowerCamel attachment fields including `runId`, `caseRunId`, `stepId`, `mediaType`, `sizeBytes`, `sha256`, `category`, `visibility`, and parsed `labels`. |
| `GET`/`POST` | `/api/baseline/gate` | Get or set a Store baseline gate by `profileId` and `subjectId`. |
| `POST` | `/api/template/render` | Render a request template from the active template package. |
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

Template package mutation APIs prefer `templatePackagePath` in request bodies
for import, verify, audit-plan, and install. Legacy callers may still send
`path` while compatibility aliases are retained.

Template package CLI commands prefer `--template-package` where a package
reference is needed for inspect, pack, audit, audit-plan, and verify. Legacy
`--profile` flags remain accepted during migration.

## API/CLI Parity Matrix

| Capability | CLI | API | Parity |
| --- | --- | --- | --- |
| Serve control plane | `serve` | Not applicable | CLI-only bootstrap. |
| Version/help | `version`, `help` | None | CLI-only. |
| Store selection visibility | `store current` | `/api/store/current` | Paired as read-only visibility. CLI reports the active named Store; API reports the Store selected when `serve` started. Neither surface exposes raw DSN passwords. |
| Store status, schema upgrade, and DDL | `store status`, `store upgrade`, `store ddl` | None | CLI-only. `store ddl --backend postgres` prints the PostgreSQL Store schema for externally provisioned control-plane databases. |
| Environment catalog lifecycle | `environment register`, `environment discover`, `environment inspect`, `environment bootstrap`, `environment restore`, `environment verify`, `environment publish-verified` | `/api/environments`, `/api/environments/{environmentId}`, `GET /api/environments/{environmentId}/bootstrap`, `/api/environments/{environmentId}/verify`, `/api/environments/{environmentId}/publish-verified` | Mostly paired. CLI and API use the active Store or `--store NAME_OR_DSN`; `restore` is currently CLI-only local machine preparation anchored to the environment verification workflow. It dry-runs by default, can clone remote repos into a workspace with `--execute`, runs the recorded Docker Compose pull/build/up plan, waits for recorded health checks, and can run the recorded verification workflow with `--execute --run-workflow`. `verify` records run status and completeness flags, while `publish-verified` inspects the selected Store for a passed run, indexed Evidence, and complete SkyWalking topology before verified discovery. |
| Start registered sandbox service | `sandbox start` | None | CLI-only local execution. CLI accepts active Store or `--store NAME_OR_DSN`; local and remote PostgreSQL Stores use the same command. |
| Register sandbox service/interface in Store | `sandbox service register`, `sandbox interface register` | `/api/sandbox/services`, `/api/sandbox/interfaces` | Paired. CLI and API share the same Store catalog registration path; CLI accepts active Store or `--store NAME_OR_DSN`. |
| Template package install/list | `template-package install`, `template-package list` (`profile ...` legacy alias) | `/api/template-packages/install`, `/api/template-packages/installed` | Mostly paired through Store-first aliases; legacy `/api/profile/*` routes remain. |
| Current template package summary/assets | `template-package inspect` (`profile inspect` legacy alias) | `/api/template-packages/current`, `/api/template-packages/assets` | Partial. CLI inspects a package/reference; API reports the active served template package. Legacy `/api/profile*` routes remain. |
| Template package import/publish | `template-package import`, `config publish` (`profile import` legacy alias) | `/api/template-packages/import` | Mostly paired through Store-first aliases. CLI accepts active Store or `--store NAME_OR_DSN` and can also audit/require audit ok. |
| Template package verify | `template-package verify` (`profile verify` legacy alias) | `/api/template-packages/verify` | Paired through Store-first aliases. CLI accepts active Store or `--store NAME_OR_DSN`; local and remote PostgreSQL Stores use the same command. |
| Template package audit repair plan | `template-package audit-plan` (`profile audit-plan` legacy alias) | `/api/template-packages/audit-plan` | Paired through Store-first aliases. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Template package init/pack/audit | `template-package init`, `template-package pack`, `template-package audit` (`profile ...` legacy aliases) | None | CLI-only package authoring. |
| Template package import/generation planning | `template-package import-plan openapi`, `template-package import-plan http-capture`, `template-package generation-plan openapi` (`profile ...` legacy aliases) | `/api/template-packages/import-plan/openapi`, `/api/template-packages/import-plan/http-capture`, `/api/template-packages/generation-plan/openapi` | Paired for current OpenAPI import, static HTTP capture import, and OpenAPI generation planners. |
| Template package catalog index | `template-package catalog-index` (`profile catalog-index` legacy alias) | `/api/template-packages/catalog-index` | Paired through Store-first alias; CLI accepts active Store or `--store NAME_OR_DSN`. Legacy `/api/profile/catalog-index` remains. |
| Catalog/dashboard/state | Roughly `profile inspect`, discovery commands | `/api/state`, `/api/dashboard`, `/api/catalog` | API-first UI payloads; no exact CLI. |
| Interface-node discovery/list | `interface-node discover` | `/api/interface-nodes`, `/api/interface-node` | Paired for discovery filters and detail lookup. CLI accepts active Store or `--store NAME_OR_DSN`; local and remote PostgreSQL Stores use the same command. API also keeps `serviceId`/`operation` list filters. |
| Interface-node coverage | `interface-node coverage`, `interface-node coverage-gaps` | `/api/interface-node/coverage`, `/api/interface-node/coverage-gaps` | Paired. CLI and API share the same coverage payloads. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Interface-node case authoring | `interface-node case audit/draft/apply` | None | CLI-only package authoring. |
| Single interface report | `interface-node case report` | `/api/cases/batch-runs` with `nodeIds` | Partial. CLI is synchronous and writes report files; API is async and process-local. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Case discovery/capabilities | `case discover` | `/api/cases/capabilities`, `/api/catalog` | Partial. CLI accepts active Store or `--store NAME_OR_DSN`; local and remote PostgreSQL Stores use the same command. CLI has richer maintenance filters. |
| Single case run by file | `case run --case PATH` | `/api/cases/run` with `casePath` | Paired. CLI writes Store records through the active Store or `--store NAME_OR_DSN`; local and remote PostgreSQL Stores use the same command. |
| Single case run by catalog id | `case run --case-id ID` | `/api/test-kit/run` with `caseId` | Paired. CLI and API execute the Store catalog case through the same test-kit runner, write run/case/Evidence indexes to the active Store, and accept the same command shape for local and remote PostgreSQL Stores. |
| Case batch run | `case suite report`, `workflow report`, `interface-node case report` | `/api/cases/batch-runs`, `/api/test-kit/run-batch` | Partial. CLI variants are synchronous reports; API variants are async or test-kit oriented. |
| Case run list | `case runs` | `/api/case/runs` | Paired. CLI reads Store runs, API case runs, and Evidence counts through the active Store or `--store NAME_OR_DSN`. |
| Case evidence detail | `case evidence` | `/api/case/evidence`, `/api/case-run/evidence` | Paired. CLI reuses the control-plane case Evidence payload and accepts active Store or `--store NAME_OR_DSN`. |
| Case timing | `case timing` | `/api/case/timing` | Paired. CLI reuses the control-plane timing summary payload and accepts active Store or `--store NAME_OR_DSN`. |
| Incomplete case batches | `case incomplete-batches` | `/api/case/incomplete-batches` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite coverage | `case suite coverage` | `/api/case/suite-coverage` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite inspection | `case suite inspect` | `/api/case/suite-inspection` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite plan | `case suite plan` | `/api/case/suite-plan` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite stability | `case suite stability` | `/api/case/suite-stability` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite priority | `case suite priority` | `/api/case/suite-priority` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite brief | `case suite brief` | `/api/case/suite-brief` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite quality | `case suite quality` | `/api/case/suite-quality` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite quality plan | `case suite quality-plan` | `/api/case/suite-quality-plan` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite quality HTML/JSON report | `case suite quality-report` | None | CLI-only artifact generation; CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite impact plan | `case suite impact` | `/api/case/suite-impact` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Suite impact run/report | `case suite impact-report` | `/api/case/suite-impact-runs` | Partial. CLI is synchronous report generation; API starts async execution. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Workflow discovery | `workflow discover` | `/api/workflows` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`; local and remote PostgreSQL Stores use the same command. API and CLI both expose filtered workflow discovery with Store catalog precedence. |
| Workflow plan | `workflow plan` | `/api/workflow-plan` | Paired. CLI and API share the same workflow-bound step payload; CLI accepts active Store or `--store NAME_OR_DSN`. |
| Workflow audit | `workflow audit` | `/api/workflow-audit` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Workflow report/run | `workflow report` | `/api/cases/batch-runs` with `workflowId`, `/api/workflow-runs` | Partial. API has async execution and persisted run snapshots, not the same synchronous report command. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Workflow run lookup | `workflow runs`, `workflow run`, `workflow step`, `workflow latest-step` | `/api/runs`, `/api/workflow-runs/*` | Paired for run list/detail and step-level lookup. CLI accepts active Store or `--store NAME_OR_DSN`; local and remote PostgreSQL Stores use the same command. |
| Trace topology collection | `trace topology collect` | `/api/trace-topology/collect` | Paired. CLI and API share the same SkyWalking GraphQL collection path. CLI writes topology rows through active Store or `--store NAME_OR_DSN`. Real topology proof requires a configured SkyWalking GraphQL endpoint and real trace ids. When the provider is missing or the trace cannot be queried, both surfaces must expose unavailable, failed, or skipped collection status instead of a generated topology. |
| Replay evidence shell | `replay evidence` | `/api/replay/evidence` | Paired. CLI and API share the same replay shell payload. |
| Post-process task lookup | `evidence tasks` | `/api/post-process-tasks` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`. |
| Evidence import | `evidence import` | `/api/evidence/import` | Paired. Imports a legacy runtime SQLite Evidence index into the active or named Store. This is a migration/compatibility path, not a normal daily SQLite execution path. |
| Evidence list | `evidence list` | `/api/evidence/list` | Paired. CLI and API share the same Store Evidence listing helper; CLI accepts active Store or `--store NAME_OR_DSN`. |
| Executor plan | `executor plan` | `/api/executor/plan` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`; API prefers the active Store catalog and uses the served template package only when no Store catalog is available. |
| Baseline get/set | `baseline get`, `baseline set` | `/api/baseline/gate` | Paired. CLI and API read/write the same Store baseline gate through active Store or `--store NAME_OR_DSN`. |
| Template render | `template render` | `/api/template/render` | Paired. CLI accepts active Store or `--store NAME_OR_DSN`; API renders against the active served Store-backed template package. |

## Main Differences

The surfaces are not yet one-to-one. The current design has three distinct
classes of mismatch:

1. Store-first registration is now paired:
   `/api/sandbox/services` and `/api/sandbox/interfaces` let users register
   runtime facts and executable interface cases directly into Store, and the CLI
   exposes the same capability through `sandbox service register` and
   `sandbox interface register`.

2. CLI package-authoring capabilities without API endpoints:
   template package initialization, packing, audit, and interface-node case
   draft/apply are available from CLI only.

3. Same domain but different execution model:
   several CLI commands synchronously produce local reports, while the API starts
   async runs and exposes process-local polling/report URLs. This affects
   `case suite report`, `interface-node case report`, `workflow report`, and
   `case suite impact-report`.

There are also naming and selector differences:

- CLI now has Store-first `template-package`/`template-packages` command aliases.
  Inspect, pack, audit, audit-plan, and verify also accept
  `--template-package`, while older `--profile` flags remain compatibility
  aliases. Served APIs operate on the active Store-backed template package for
  the running server.
- Template package APIs accept Store-first `templatePackagePath`; legacy `path`
  is retained only as an input compatibility alias.
- CLI uses `--node`; suite APIs accept both `node` and `nodeId`.
- CLI `case run` runs a case file path, while `/api/test-kit/run` runs a catalog
  case id. `/api/cases/run` is the closer API match for `case run`.
- Existing older prose used pre-profile package terminology, and current CLI
  compatibility commands still use `profile`. New API/UI/docs should prefer
  Store-first `template package` wording for import/export/review artifacts.

## Recommended Parity Work

The product target is CLI-first with a local control-plane API. Prioritize
parity in this order:

1. Add read CLI commands for API-only Store views:
   currently none in the first Store-view parity set.

2. Add API endpoints for CLI-only daily testing helpers:
   currently none in the first daily helper parity set.

3. Keep offline package-authoring commands explicitly CLI-only unless they
   become part of the daily workbench surface:
   `template-package init/pack/audit` and `interface-node case draft/apply` are
   review/migration utilities, not mandatory runtime operations.

4. Normalize report execution semantics:
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
  legacy `profile` flows as import/export or compatibility bridges.
