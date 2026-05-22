# Frontend Reference Backlog

Date: 2026-05-18

This backlog is the evidence gate for frontend optimization in Open Test
Sandbox. It complements the backend reference backlog and keeps the same rule:
do not promote a frontend idea unless the source project evidence and adaptation
scope are explicit.

Current frontend shape:
AgentTestBench serves small React entrypoints from
`control-plane/frontend/src` through static HTML shells under
`control-plane/static`. The main frontend surfaces are dashboard, workflows,
interface nodes, API cases, case runs, evidence viewer, replay evidence, trace
topology, and workflow details.

## Evidence Rules

- Prefer downloaded reference repositories under
  `/Users/zlh/Documents/Codex/agent-testbench-reference-projects/2026-05-18`.
- Use official project docs or source repository pages only when the local
  shallow clone does not contain the frontend surface.
- Keep adaptations local-first and generic. Do not introduce product-specific
  SaaS assumptions or hosted-only concepts.
- Treat reference projects as evidence, not dependencies.
- Preserve a first-class discovery path for the configured target Workflow and
  the interface/API cases mapped to that Workflow. The concrete target step
  count, interface count, and label belong in import bundle/config data; templates,
  default assets, and source code must stay generic and must not hardcode a
  domain-specific target count or keyword.

## Candidate Slices

### 1. Run Analysis Center

Source feature:
ReportPortal separates UI, API, analyzer, attachment storage, framework agents,
and loggers; its docs and project overview emphasize dashboarding, real-time
reporting, launch statistics, failure categorization, logs, and filtering.

Evidence:
`reportportal/README.md:34-55`, `reportportal/README.md:107-115`,
ReportPortal docs "Filtering launches", ReportPortal GitHub organization
overview.

Source feature:
Sorry Cypress exposes an OSS dashboard for browsing test results, screenshots,
videos, failure stack traces, flaky test detection, run progress, spec/test
status, and durations.

Evidence:
`https://sorry-cypress.dev/`, `https://github.com/sorry-cypress/sorry-cypress`

Current fit:
AgentTestBench already has `case-runs.html`, `/api/case/runs`,
`/api/case/timing`, `/api/case/incomplete-batches`, failure categories, and
Evidence links.

Adaptation scope:
Turn `caseRuns.jsx` into a run analysis center with status facets, failure
category groups, duration outliers, run freshness, and direct Evidence links.
Do not add cloud recording, screenshots, video playback, or hosted integrations
until local Evidence assets exist.

Verification plan:
Add pure model tests for grouping and filtering, build the frontend bundle, and
run a headless smoke against `case-runs.html`.

Implementation progress:
`case-runs.html` supports exact `case` URL focus so links from Workflow case
sequence can open the run report already narrowed to one mapped case. This
keeps the TestLink-style plan-to-execution path and Sorry Cypress-style result
browsing connected without adding hosted run concepts.
The focused page now renders a `Case execution summary` panel with run counts,
pass/fail totals, latest result, latest Evidence handoff, and longest duration
for the selected case.

### 2. Evidence Timeline Viewer

Source feature:
Playwright Trace Viewer presents a timeline and filters actions, console logs,
and network logs by the selected time span. It also supports source views and
visual comparison for screenshots.

Evidence:
`https://github.com/microsoft/playwright/blob/main/docs/src/trace-viewer.md`

Source feature:
Allure web commons includes attachment fetching helpers and report data access
modules for attachments.

Evidence:
`allure3/packages/web-commons/src/attachments.ts`,
`allure3/packages/web-commons/src/data.ts`

Current fit:
AgentTestBench already exposes `evidence-viewer.html` and Evidence records
for request, response, assertions, summaries, runtime logs, and topology.

Adaptation scope:
Organize Evidence into a timeline with tabs for request/response/assertions/logs
and topology. Keep it file-based and local-first; do not add browser trace
replay until trace artifacts exist.

Verification plan:
Add rendering smoke tests for evidence tabs and ensure raw Evidence links remain
reachable.

Implementation progress:
`control-plane/frontend/src/evidenceTimelineModel.mjs` now groups local Evidence
payloads into request, response, assertions, fixture, topology, and logs
sections. `evidence-viewer.html` renders those sections as a selectable,
filterable timeline and keeps the selected section's raw payload visible. The
headless smoke injects a local Evidence bundle through the existing
localStorage path and verifies the timeline filter and log detail view.
The same model now extracts an `Evidence Artifacts` list from explicit artifact
records and summary paths, following Allure's attachment/data access model and
Monocart's relative artifact links. The viewer only opens web-safe URLs and
keeps local runtime paths as visible text so the local-first report does not
create broken browser links.
`evidence-viewer.html` now builds a local `Reproduction Command` panel from the
captured request evidence, showing a redacted curl command alongside the HTTP
status and failure reason. This follows Schemathesis' Allure reporting feature
for per-operation results, failure steps, and curl reproduction commands, and
uses Playwright Trace Viewer's network-detail pattern as the local evidence
source.

### 3. Failure Triage View

Source feature:
Allure categories group test results into named buckets. A result belongs to
only one category, and the first matching rule wins.

Evidence:
`allure3/README.md:187-245`,
`allure3/packages/plugin-awesome/src/categories.ts`

Source feature:
ReportPortal highlights failure categorization and dashboarding for triage.

Evidence:
ReportPortal GitHub organization overview and `reportportal/README.md:34-55`.

Current fit:
AgentTestBench batch failure summaries already expose `failureCategory`, and
import bundle bundles can define ordered `failureCategories` rules.

Adaptation scope:
Add failure-category groups to `caseRuns.jsx` and report/detail pages. Make the
group cards navigable filters over existing case run data.

Verification plan:
Add model tests for category counts and click-to-filter behavior; verify no
failed case is hidden by default.

Implementation progress:
`/api/case/runs` now exposes import bundle-owned ordered `failureCategories` rules
and report-facing failure categories for failed case runs. `case-runs.html`
uses those rules to build an Allure-style first-match triage navigator: each
failed run belongs to one bucket, buckets keep the rule/default explanation,
and each bucket links to a sample Evidence bundle for inspection. This keeps
the ReportPortal-style failure analysis surface local-first and avoids adding
hosted analyzer services.

### 4. High-Density Report Grid

Source feature:
Monocart Reporter displays Playwright reports in a Tree Grid and includes a
Timeline Workers Graph.

Evidence:
`monocart-reporter/README.md:14-20`,
`monocart-reporter/README.md:150-205`,
`monocart-reporter/lib/default/columns.js`,
`monocart-reporter/packages/app/src/modules/grid-rows.js`

Current fit:
AgentTestBench has large case run and batch report lists but currently uses
simple card grids.

Adaptation scope:
Introduce dense report rows for case runs and suite reports, with stable
columns for status, case, operation, duration, failure category, and Evidence.
Avoid virtualized grids until volume proves it necessary.

Verification plan:
Build responsive row layout and screenshot it at desktop and mobile widths.

Implementation direction:
Use a report-workbench structure instead of simple cards: stable sortable
columns, compact rows, status/failure grouping, and direct Evidence actions.
Keep columns hardcoded to current local case-run data, mirroring Monocart's
default/custom column idea without adding a plugin system.

Source feature:
Sorry Cypress separates runs feed, run summary, run details, spec counts,
duration chips, flaky visibility, and result browsing under its dashboard run
modules.

Evidence:
`sorry-cypress/README.md:38-40`,
`sorry-cypress/packages/dashboard/src/run/runsView.tsx`,
`sorry-cypress/packages/dashboard/src/run/runSummary/runSummary.tsx`,
`sorry-cypress/packages/dashboard/src/run/runDetails/runDetailsView.tsx`

Current fit:
AgentTestBench already has case run summaries, latest run metadata, timing
evidence, and direct Evidence detail URLs.

Adaptation scope:
Use a dashboard/report-workbench composition with separate summary, grouping,
controls, and dense result rows. Do not add screenshots, videos, cloud storage,
or Cypress-specific spec mechanics until AgentTestBench produces those local
artifacts.

### 5. Case Management Search

Source feature:
Kiwi TCMS is an open-source test management system with search pages, visual
reports, automation plugins, bug tracker integration, and rich APIs.

Evidence:
`kiwi/README.rst:47-50`, `kiwi/tcms/static/js/index.js`

Source feature:
TestLink organizes cases into plans, supports executing cases, tracks results,
and answers release readiness and missing-case questions.

Evidence:
`testlink/README.md:41-80`

Current fit:
AgentTestBench already has case lifecycle, owner, priority, quality-plan,
coverage, and suite inspection APIs.

Adaptation scope:
Improve `apiCases.jsx` and future case-maintenance pages with saved-style
filters for lifecycle, owner, priority, tags, readiness, and latest run state.
Do not add manual test-plan assignment mechanics unless import bundle metadata grows
to support it.

Verification plan:
Add model tests for filters and preserve direct run/evidence actions.

Implementation progress:
`control-plane/frontend/src/apiCasesModel.mjs` now builds TCMS-style case
management rows, facets, readiness groups, and latest-run filters from the
existing case capability payload. `api-cases.html` renders a case management
search workbench with summary metrics, lifecycle/owner/priority/tag/latest
facets, readiness groups, a dense management table, and the existing run
action/detail panels. The design follows Kiwi's searchable test management
surfaces and TestLink's plan/progress/result management questions without
adding manual assignment or non-local hosted concepts.
`api-cases.html?workflow=ID` also derives a Workflow case set from
`/api/catalog`, filters the management table to the cases mapped by that
Workflow, and shows step/interface/case counts before the search controls. This
keeps the TestLink-style plan-to-case relationship navigable without adding a
separate test-plan ownership model.
The same page now renders a Workflow case sequence that preserves step order,
interface id, mapped case, readiness, latest result, and Evidence action. The
interface id is also a direct handoff to the Interface Node detail page. This
handoff keeps the selected Workflow and case context, and Interface Node pages
render a Workflow case-set back-link when opened with that context. This
adapts TestLink's execution/result tracking questions and Kiwi's case/run search
surfaces to AgentTestBench's local catalog data.
Interface Node directory links now preserve the same context when opened from a
Workflow case sequence with only a service id available. The filtered directory
keeps the Workflow case-set back-link and passes the selected Workflow/case into
each node detail card, so the handoff remains auditable even before a concrete
node id is known.
`api-cases.html` now also renders a Coverage matrix from
`/api/case/suite-coverage`, grouping cases by interface node and surfacing
passed, failed, and not-run gaps with direct Runs and Evidence handoffs. This
adapts TestLink's missing/failing case questions, Kiwi's search/reporting
surface, and Schemathesis/Microcks-style API coverage/contract visibility
without adding schema fuzzing or hosted contract-test services to the frontend.

### 6. Configured Target Workflow Checklist

Source feature:
TestLink organizes test cases into test plans, lets team members execute cases,
tracks test results, and answers questions about missing cases and failing
results.

Evidence:
`testlink/README.md:41-80`

Source feature:
Kiwi exposes separate search page handlers for test cases, test plans, and test
runs, plus visual reports.

Evidence:
`kiwi/README.rst:47-50`,
`kiwi/tcms/static/js/index.js:7-16`,
`kiwi/tcms/static/js/index.js:40-49`

Source feature:
Monocart Reporter emphasizes a Tree Grid, grouping, searchable fields, and
stable report columns.

Evidence:
`monocart-reporter/README.md:15-20`,
`monocart-reporter/README.md:158-168`

Current fit:
AgentTestBench already exposes workflow steps, interface identifiers, case
identifiers, and API-operated `presentation.workflowFinder` configuration
through `/api/catalog`.

Adaptation scope:
Render the configured target workflow as an auditable checklist with stable
columns for step, interface, case, and state. Keep the target counts and label
API-operated; do not introduce a fixed target count in source or default
templates.

Implementation progress:
`control-plane/frontend/src/workflowDiscoveryModel.mjs` now builds
`targetChecklist` records for each configured target match. `workflows.html`
renders those rows next to the existing target interface strip, marking missing
interface or case links without hiding the Workflow. The smoke import bundle supplies
the target through `templateConfigs`, and `/api/catalog.presentation.workflowFinder`
is checked before browser verification.
The checklist rows now include direct Interface detail and Runs handoffs for
each mapped case, preserving the Workflow -> Interface -> Case -> Run path in
one auditable view.
Case handoffs from the checklist keep the `workflow` query parameter alongside
the selected `case`, so Case Management opens with the matching Workflow case
set and sequence still visible.
Runs handoffs from the Workflow case sequence now keep the same Workflow
context, and the Run Analysis Center renders a back-link to the matching
Workflow case set.
Evidence links generated from the Run Analysis Center also preserve that
Workflow context, and `evidence-viewer.html` renders a Workflow case-set
handoff when opened with a `workflow` query parameter. Evidence Viewer also
keeps the API Case Evidence back-link scoped to the same case and Workflow.
The home workbench now promotes the configured Workflow target as a first-row
capability card when `presentation.workflowFinder` is present, using the same
config-driven counts and linking back to `workflows.html`.

## First Frontend Implementation Candidate

Implement the Run Analysis Center slice first by extracting a pure model for
`caseRuns.jsx` grouping and filtering. It is the smallest frontend step that
uses existing APIs and directly reflects Allure, ReportPortal, Sorry Cypress,
and Monocart report-analysis patterns.
