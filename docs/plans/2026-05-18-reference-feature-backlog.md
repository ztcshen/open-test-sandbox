# Reference Feature Backlog

Date: 2026-05-18

This backlog is the evidence gate for optimizing Open Test Sandbox. Every item
below is derived from a downloaded open-source reference repository under:

`/Users/zlh/Documents/Codex/open-test-sandbox-reference-projects/2026-05-18`

Do not promote an idea from this backlog into implementation unless the source
feature, local evidence path, adaptation scope, and verification plan remain
explicit. Open Test Sandbox is currently a local-first control plane with a
PostgreSQL Store-first product path, CLI, HTTP APIs, API-operated catalog,
execution reports, and Evidence lookup APIs (`docs/backend-capabilities.md:3-28`).
SQLite references in older backlog notes describe the historical baseline only;
new daily testing behavior should target the selected PostgreSQL Store.

## Evidence Rules

- Cite source repository paths and line ranges for every proposed feature.
- Prefer README or user-facing docs first; use code paths only to clarify shape.
- Keep the core generic. Source-domain or team-specific assets remain in
  external import bundles.
- Treat the reference repositories as evidence, not dependencies.
- If a feature cannot be traced to reference evidence, do not include it.

## Candidate Slices

### 1. Import Bundle Asset Importers

Source feature:
Microcks turns API and microservice assets such as OpenAPI, AsyncAPI, gRPC
protobuf, GraphQL schema, Postman collections, and SoapUI projects into live
mocks, then reuses the same assets for contract conformance and
non-regression tests.

Evidence:
`microcks/README.md:11-15`

Current fit:
Open Test Sandbox already keeps import bundle bundles outside core, publishes them
into Store/read-models, and exposes catalog APIs (`docs/backend-capabilities.md:17-18`,
`docs/backend-capabilities.md:32-50`, `docs/backend-capabilities.md:56-70`).

Adaptation scope:
Add import planning for external API artifacts into import bundle bundle assets:
services, interface nodes, request templates, API cases, fixtures, and workflow
bindings. Start with OpenAPI only, because both Microcks and Schemathesis use
schema-first API testing as a core entry point.

Verification plan:
Add fixture OpenAPI specs, import-plan tests, import bundle audit tests, and catalog
read-model assertions. No runtime network calls should be required.

Implementation progress:
OpenAPI 3.x JSON import planning now exists in
`internal/domain/profileimport/openapi`. The CLI exposes it as
`otsandbox template-package import-plan openapi` through the Store-first command
alias, with optional `--output-dir` export for reviewable split template package
assets and runnable `api-cases/*.json` case files. The control-plane API also
exposes the same planner as `POST /api/template-packages/import-plan/openapi`
for API-operated review flows. Static HTTP capture import planning follows the
same review-only pattern through `internal/domain/profileimport/httpcapture`,
`otsandbox template-package import-plan http-capture`, and
`POST /api/template-packages/import-plan/http-capture`. OpenAPI negative-case
generation is likewise exposed through `internal/domain/profilegenerate/openapi`,
`otsandbox template-package generation-plan openapi`, and
`POST /api/template-packages/generation-plan/openapi`. This keeps generated
assets draft-only and outside any existing template package until a reviewer
applies them.

### 2. Executor Model for Existing Test Tools

Source feature:
Testkube defines, runs, and analyzes automated tests using existing
tools/scripts, including API, E2E, performance, security, and infrastructure
tests. It can trigger runs manually, on schedules, from CI/CD/GitOps, on
Kubernetes events, through REST API, or MCP. It aggregates results, artifacts,
logs, and resource metrics.

Evidence:
`testkube/README.md:7-10`, `testkube/README.md:27-34`

Current fit:
Open Test Sandbox already has single case execution, workflow reports,
asynchronous batch runs, artifact manifests, failure summaries, and Evidence
detail APIs (`docs/backend-capabilities.md:21-24`, `docs/backend-capabilities.md:96-115`).

Adaptation scope:
Introduce a generic executor descriptor in import bundle config for HTTP case,
Playwright, Postman/Newman, k6, pytest, Karate, and custom command runners.
Keep implementation local-first: executor metadata and run facts live in Store;
tool binaries remain external to core.

Verification plan:
Start with descriptor validation and a no-op command executor test. Then add one
real local executor behind an explicit import bundle setting.

Implementation progress:
Import Bundle bundles now support `executors` manifest entries and split
`executors/*.json` assets. `internal/runner/executor` provides a dry-run plan that
validates supported external tool/script descriptors (`http-case`,
`playwright`, `postman`, `k6`, `pytest`, `karate`, and `custom-command`) and
reports ready versus blocked descriptors without executing external binaries.
The CLI exposes this as `otsandbox executor plan`.

### 3. Test Case Lifecycle and Assignment Fields

Source feature:
Kiwi TCMS is a test management system for manual and automated testing with bug
tracker integration, search pages, access control, automation framework plugins,
visual reports, and a rich API layer.

Evidence:
`kiwi/README.rst:47-50`

Source feature:
TestLink manages test cases, organizes them into test plans, lets team members
execute cases and track results, and answers questions about missing cases,
assigned runs, progress, failures, last-run versions, and release readiness.

Evidence:
`testlink/README.md:41-80`

Current fit:
Open Test Sandbox already indexes case description, tags, priority, owner,
status, runnable source, execution config, readiness issues, latest run state,
stability, impact, quality, and executable plans (`docs/backend-capabilities.md:20`).

Adaptation scope:
Tighten the current case metadata into a documented lifecycle model:
`draft`, `review`, `active`, `quarantined`, `deprecated`. Add assignment and
release/readiness views only where they map to existing import bundle metadata.

Verification plan:
Add casesuite quality tests for lifecycle transitions, filter tests for status
and owner, and docs that distinguish automated execution from manual TCMS
features not yet implemented.

Implementation progress:
`internal/domain/casesuite` now normalizes API case lifecycle status to `draft`,
`review`, `active`, `quarantined`, `deprecated`, or `invalid`. Suite quality
and quality-plan reports flag non-executable lifecycle states and invalid
statuses, while keeping execution readiness limited to `active`. This adapts the
TCMS-style lifecycle and assignment concerns to existing import bundle metadata rather
than adding manual test-plan ownership mechanics.

### 4. Evidence Attachment and Report Categories

Source feature:
Allure 3 has a modular reporting architecture, a plugin system, live reporting,
an agent-friendly command, single-file report output, configurable labels, and
category rules for grouping results.

Evidence:
`allure3/README.md:15-19`, `allure3/README.md:63-78`,
`allure3/README.md:135-183`, `allure3/README.md:187-240`

Source feature:
ReportPortal separates server-side services, attachment storage, client
adapters, framework agents, and logger appenders; client-side integrations
monitor test events, bind logs to test cases, and send them to the server.

Evidence:
`reportportal/README.md:34-55`, `reportportal/README.md:107-115`

Current fit:
Open Test Sandbox already emits JSON, temporary HTML, JUnit XML, artifact
manifests, failure summaries, request/response/assertion details, logs, and
topology (`docs/backend-capabilities.md:22-25`, `docs/backend-capabilities.md:101-115`).

Adaptation scope:
Define a stable local Evidence attachment model with labels, categories,
visibility flags, media type, size, hash, and relation to run/case/step. Add
category rules for failures such as assertion mismatch, transport error,
schema mismatch, timeout, and flaky transition.

Verification plan:
Add Store schema tests, redaction tests, batch report golden tests, and API
payload tests for artifact manifests and failure summaries.

Implementation progress:
Evidence records now carry attachment category, visibility, labels, and
first-class `stepId` relation metadata. `/api/evidence/list` now serializes
records through a stable lowerCamel attachment payload instead of raw Store
structs, including `runId`, `caseRunId`, `stepId`, `mediaType`, `sizeBytes`,
`sha256`, `category`, `visibility`, and parsed `labels`; `/api/case/evidence`
and `/api/case-run/evidence` now use the same attachment metadata for request,
response, and log evidence details. Batch failure summaries expose local
failure categories. Case Evidence details now use shared structured redaction
for sensitive JSON keys, headers, JSON string bodies, and URL query parameters.
Post-process task API and CLI payloads now expose readable Evidence collection
`outcome`, `reason`, and `displayStatus` fields for passed, skipped, failed,
and running tasks. Import Bundle bundles also support ordered
`failureCategories` rules for report-facing failure buckets; rule matching
follows the Allure category constraint that the first matching rule wins. The
implemented matcher surface is intentionally
local and small: status, built-in failure category, and message substring.

### 5. Record/Replay Import Path

Source feature:
Keploy records API calls, database queries, and streaming events, then replays
them as tests. It emphasizes no code changes, complex distributed flow replay,
infra virtualization for databases/queues/external APIs, coverage views, unified
reports, console management, time freezing, and mock registry.

Evidence:
`keploy/README.md:36-39`, `keploy/README.md:49-64`,
`keploy/README.md:70-100`, `keploy/README.md:112-132`

Current fit:
Open Test Sandbox already stores replay/log-style Evidence and exposes replay
lookup APIs, but it does not yet record traffic into maintained import bundle cases
(`docs/backend-capabilities.md:25`).

Adaptation scope:
Add an importer that converts captured request/response pairs into draft API
cases plus fixture candidates. Defer eBPF, database, queue, time-freezing, and
mock registry mechanics until there is a local capture format in import bundles.

Verification plan:
Use static captured HTTP fixtures, generate draft cases, run import bundle audit, and
prove the generated cases can execute against a local httptest server.

Implementation progress:
`internal/import bundleimport/httpcapture` now converts static HTTP capture JSON into
reviewable draft services, interface nodes, request templates, API case
metadata, and runnable `api-cases/*.json` files. The CLI exposes this as
`otsandbox import bundle import-plan http-capture`, sharing the same review-only
`--output-dir` pattern as OpenAPI import planning. The implementation is
intentionally limited to HTTP request/response pairs; eBPF capture,
database/queue virtualization, mock registry, and time freezing remain deferred
until import bundle-owned local capture formats exist.

### 6. Schema-Based Case Generation

Source feature:
Schemathesis tests OpenAPI and GraphQL APIs by generating inputs from schemas,
adapting to responses, and chaining operations into workflows. It targets 500
errors, schema violations, validation bypasses, integration failures, and
stateful bugs. It reports through Allure and JUnit.

Evidence:
`schemathesis/README.md:22-27`, `schemathesis/README.md:45-52`,
`schemathesis/README.md:64-79`, `schemathesis/README.md:103-107`

Source feature:
EvoMaster automatically generates system-level tests for REST, GraphQL, and RPC
APIs; it can generate regression suites, web reports, fault-detecting cases,
auth-aware tests, and database-aware setup in white-box mode.

Evidence:
`evomaster/README.md:23-29`, `evomaster/README.md:93-156`

Current fit:
Open Test Sandbox already has suite impact planning, priority ranking,
coverage views, and executable case plans (`docs/backend-capabilities.md:119-128`).

Adaptation scope:
Create a generation-plan command that produces reviewable candidate cases from
schema assets. It should not directly add generated cases to active suites.
Generated cases start as drafts and are subject to import bundle audit and quality
plan checks.

Verification plan:
Use a tiny OpenAPI fixture. Assert generated draft IDs, negative/edge-case
labels, quality warnings, and non-activation until explicitly applied.

Implementation progress:
`internal/import bundlegenerate/openapi` now produces reviewable draft negative
candidate cases from OpenAPI JSON request schemas. The first implemented
generation rule is intentionally narrow: one missing-required-field case per
required JSON request property, tagged `generated`, `schema`, and `negative`.
The CLI exposes this as `otsandbox import bundle generation-plan openapi`, with the
same review-only `--output-dir` pattern used by import planning. Fuzzing,
stateful operation chaining, live response schema-violation discovery,
white-box analysis, auth-aware generation, and database-aware setup remain
deferred.

### 7. Unified Test DSL and Mock Reuse

Source feature:
Karate combines API testing, mocks, performance testing, and UI automation into
a single framework.

Evidence:
`karate/README.md:1-3`

Current fit:
Open Test Sandbox already has request templates, fixtures, API cases, workflow
bindings, and workbench pages for API-operated execution (`docs/backend-capabilities.md:18-23`).

Adaptation scope:
Do not copy Karate's DSL. Instead, add import bundle compatibility hooks so a import bundle
can reference external DSL files as executable sources while Open Test Sandbox
continues to own discovery, Store indexing, Evidence, and reports.

Verification plan:
Add import bundle schema validation for external test source references and a dry-run
executor descriptor test.

Implementation progress:
Import Bundle API cases now support `sourceKind`, `sourcePath`, and `executorId`
metadata for external executable sources such as Karate feature files. Suite
quality treats an external source as runnable only when it references an active
import bundle executor, and the fields are preserved through import bundle catalog, Store,
and control-plane capability payloads. This intentionally remains a
compatibility hook; Open Test Sandbox does not implement or copy Karate's DSL.

## Priority Order

1. Import Bundle Asset Importers: unlocks schema/import bundle inputs and supports later
   generation.
2. Evidence Attachment and Report Categories: strengthens the thing the project
   already does well.
3. Test Case Lifecycle and Assignment Fields: narrows current case metadata into
   a clearer TCMS-inspired contract.
4. Executor Model: broadens execution while preserving local-first boundaries.
5. Record/Replay Import Path: valuable but needs a local capture format first.
6. Schema-Based Case Generation: powerful, but should stay draft-only at first.
7. Unified Test DSL and Mock Reuse: integration hook only, not a core DSL fork.

## First Implementation Candidate

Implement the Evidence Attachment and Report Categories slice first. It has the
smallest conceptual gap because Open Test Sandbox already writes Evidence,
artifact manifests, failure summaries, and JUnit/HTML reports. Allure and
ReportPortal provide direct source evidence for labels, categories, attachments,
client/agent/logger separation, and report generation.
