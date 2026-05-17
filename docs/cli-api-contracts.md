# CLI and API Contracts

This page summarizes the public surfaces intended for local agents, CI jobs,
and lightweight automation. Contracts are pre-1.0, but changes should be
documented in `CHANGELOG.md`.

## Discovery

Agents should discover runnable targets before generating reports.

```sh
otsandbox interface-node discover \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --filter "query" \
  --json

otsandbox workflow discover \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --filter "happy path" \
  --json

otsandbox case discover \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --filter "create item" \
  --tag smoke \
  --status active \
  --owner team-a \
  --priority p0 \
  --json
```

The returned ids are the only ids an agent should pass to report commands.
`case discover` searches API case id, display name, description, owner, tags,
and interface node id. Its JSON output includes maintenance metadata plus
whether the case has a runnable file and execution configuration.

## Maintained Case Authoring

Generate a reviewable draft from a discovered interface node:

```sh
otsandbox interface-node case draft \
  --profile /path/to/profile-bundle \
  --node node.alpha \
  --case-id case.generated \
  --title "Generated Case" \
  --tag regression \
  --priority p1 \
  --owner team-a \
  --output .runtime/case-draft.json \
  --json
```

The draft output contains API case metadata, execution config, and a runnable
case file payload. Apply the reviewed bundle to an external profile directory:

```sh
otsandbox interface-node case apply \
  --profile /path/to/profile-bundle \
  --file .runtime/case-draft.json \
  --json
```

`apply` writes profile-owned files only: `catalog.json` receives maintained
case metadata and execution config, while runnable case JSON files are written
under the profile bundle. It does not write Store records; publish or verify
the profile afterward when the caller wants indexed read-models.

## Maintained Case Suite Quality

```sh
otsandbox case suite quality \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --status active \
  --json
```

The quality command audits authoring readiness before runtime execution. It
flags interface nodes with no maintained cases and cases missing description,
tags, priority, owner, runnable source, or execution config. The command is
read-only; it helps agents decide whether to draft/apply more cases or ask a
team owner to fill in metadata before relying on the suite.

The Control plane exposes the same quality contract:

```http
GET /api/case/suite-quality?status=active
```

Use the quality plan when an automation needs next actions instead of raw gaps:

```sh
otsandbox case suite quality-plan \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --status active \
  --json
```

The plan returns stable actions such as `draft-case`,
`complete-case-metadata`, `add-runnable-source`, and `add-execution-config`.
For uncovered interface nodes, the action includes a suggested case id and an
`interface-node case draft` command fragment.

```http
GET /api/case/suite-quality-plan?status=active
```

When the caller needs a shareable artifact instead of a raw plan, generate a
compact JSON and HTML report:

```sh
otsandbox case suite quality-report \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --status active \
  --output-dir .runtime/reports/case-quality \
  --json
```

The command writes `report.json` and `report.html`. The JSON keeps the full
quality plan under `qualityPlan`; the HTML presents a compact action table for
review, assignment, or agent handoff. This report is read-only and does not
execute API requests.

## Maintained Case Suite Report

```sh
otsandbox case suite report \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --tag smoke \
  --owner team-a \
  --status active \
  --base-url http://127.0.0.1:8080 \
  --output-dir .runtime/reports/smoke-suite \
  --json
```

The command turns case maintenance metadata into an executable suite. It
selects active API cases by filter, node id, tag, owner, or priority; executes
the selected cases; and writes `report.json` plus a compact `report.html`.
It also writes `report.junit.xml` for CI systems that collect JUnit artifacts.
The report keeps case title, node id, tags, priority, owner, elapsed time,
status, failed-case Evidence links, and `junitReportUrl`.

## Maintained Case Suite Coverage

```sh
otsandbox case suite coverage \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --tag smoke \
  --owner team-a \
  --status active \
  --json
```

The command uses the same maintenance selector as `case suite report`, but it
does not run any HTTP requests. It reads Store case-run records and returns
the latest status for each selected case, plus passed, failed, and not-run
counts. Failed and latest-run cases include `caseRunId` and `detailUrl` so a
caller can jump directly to Evidence.

The Control plane exposes the same read-only coverage contract:

```http
GET /api/case/suite-coverage?tag=smoke&owner=team-a&status=active
```

## Maintained Case Suite Stability

```sh
otsandbox case suite stability \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --tag smoke \
  --owner team-a \
  --status active \
  --limit 10 \
  --json
```

The command uses the same maintenance selector, reads recent Store case-run
history, and reports pass/fail counts, latest status, status transitions,
recent run ids, and detail URLs. A case is marked `unstable` when the selected
history contains both passed and failed runs with at least one status
transition.

The Control plane exposes the same stability contract:

```http
GET /api/case/suite-stability?tag=smoke&owner=team-a&status=active&limit=10
```

## Maintained Case Suite Priority

```sh
otsandbox case suite priority \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --signal "/api/items" \
  --tag smoke \
  --status active \
  --limit 20 \
  --request-id change-005 \
  --base-url http://127.0.0.1:8080 \
  --json
```

The priority command ranks ready maintained cases before execution. It combines
impact matches, latest Store status, recent stability, and case priority
metadata into a score with explanation reasons. The response returns selected,
skipped, and blocked cases plus a `batchRequest` object that can be posted to
`/api/cases/batch-runs`.

The Control plane exposes the same ranking contract:

```http
GET /api/case/suite-priority?signal=/api/items&tag=smoke&status=active&limit=20&requestId=change-005
```

## Maintained Case Suite Brief

```sh
otsandbox case suite brief \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --signal "/api/items" \
  --tag smoke \
  --status active \
  --limit 20 \
  --request-id change-006 \
  --base-url http://127.0.0.1:8080 \
  --json
```

The brief command is the one-call triage surface for agents and CI. It returns
latest coverage, readiness issues, recent stability, ranked recommendations,
blocked cases, and a `batchRequest` payload. Failed, not-run, or unstable cases
remain normal report content; the command only fails on transport or Store
errors.

The Control plane exposes the same brief contract:

```http
GET /api/case/suite-brief?signal=/api/items&tag=smoke&status=active&limit=20&requestId=change-006
```

## Maintained Case Suite Inspection

```sh
otsandbox case suite inspect \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --tag smoke \
  --owner team-a \
  --status active \
  --json
```

The command uses the same maintenance selector as suite reports and coverage,
but returns a pre-run readiness view. Each row includes runnable file presence,
execution configuration presence, latest Store state, blocking issues, and a
suggested action such as `run`, `rerun`, `add-runnable-source`, or `keep`.

The Control plane exposes the same inspection contract:

```http
GET /api/case/suite-inspection?tag=smoke&owner=team-a&status=active
```

## Maintained Case Suite Plan

```sh
otsandbox case suite plan \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --tag smoke \
  --status active \
  --action run \
  --action rerun \
  --request-id change-001 \
  --base-url http://127.0.0.1:8080 \
  --json
```

The plan command turns inspection into a deterministic execution payload. It
returns selected ready `caseIds`, blocked cases, skipped ready cases, and a
`batchRequest` object that can be posted to `/api/cases/batch-runs`.

The Control plane exposes the same planning contract:

```http
GET /api/case/suite-plan?tag=smoke&status=active&action=run&action=rerun&requestId=change-001
```

## Maintained Case Suite Impact

```sh
otsandbox case suite impact \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --signal "/api/items" \
  --change "changed/module/path" \
  --status active \
  --action run \
  --action rerun \
  --request-id change-002 \
  --base-url http://127.0.0.1:8080 \
  --json
```

The impact command maps change signals to interface nodes, workflow bindings,
and maintained API cases, then reuses the suite planning rules to return
selected ready cases, blocked cases, explanation reasons, and a `batchRequest`
that can be posted to `/api/cases/batch-runs`. Signals are generic strings and
can be changed paths, route paths, operation text, workflow names, tags, or
case text.

The Control plane exposes the same impact planning contract:

```http
GET /api/case/suite-impact?signal=/api/items&change=module/path&status=active&action=run&action=rerun&requestId=change-002
```

Use `impact-report` when an automation wants the same selection and execution
in one synchronous CLI call:

```sh
otsandbox case suite impact-report \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --signal "/api/items" \
  --status active \
  --action run \
  --action rerun \
  --request-id change-003 \
  --base-url http://127.0.0.1:8080 \
  --output-dir .runtime/reports/impact-change-003 \
  --json
```

The JSON response contains both the `impact` selection and the executed
`report`. The report still preserves failed cases instead of treating them as
transport failures.

Use the asynchronous Control plane endpoint when a caller should receive
report URLs immediately:

```http
POST /api/case/suite-impact-runs
Content-Type: application/json

{
  "requestId": "change-004",
  "signals": ["/api/items"],
  "status": "active",
  "actions": ["run", "rerun"],
  "baseUrl": "http://127.0.0.1:8080"
}
```

The response is `202 Accepted` with the `impact` selection, `batchRunId`,
`reportUrl`, and the same batch report fields as `/api/cases/batch-runs`.

## Single Interface Report

```sh
otsandbox interface-node case report \
  --node NODE_ID \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --base-url http://127.0.0.1:8080 \
  --output-dir .runtime/reports \
  --json
```

The command runs every API case attached to the selected interface node. The
report includes per-case status, elapsed time, run id, case run id, Evidence
path, and detail URL. A failing case should still appear in the report.

## Workflow Report

```sh
otsandbox workflow report \
  --workflow WORKFLOW_ID \
  --profile PATH_OR_ID \
  --store-url .runtime/store.sqlite \
  --base-url http://127.0.0.1:8080 \
  --output-dir .runtime/reports \
  --json
```

The command follows workflow bindings in configured order. Each step records
its own case run id and detail URL so a UI or agent can jump directly to the
failed step.

## Asynchronous Batch API

```http
POST /api/cases/batch-runs
Content-Type: application/json
```

Run all cases for selected interface nodes:

```json
{
  "requestId": "change-001",
  "nodeIds": ["node.alpha"],
  "baseUrl": "http://127.0.0.1:8080",
  "evidenceDir": ".runtime/case-batches"
}
```

Run an exact case list, usually produced by `case suite plan`:

```json
{
  "requestId": "planned-001",
  "caseIds": ["case.alpha", "case.beta"],
  "baseUrl": "http://127.0.0.1:8080",
  "evidenceDir": ".runtime/case-batches"
}
```

Run a workflow-shaped regression:

```json
{
  "requestId": "workflow-001",
  "workflowId": "workflow.alpha"
}
```

Run a maintained suite and only rerun cases whose latest Store state is failed
or not-run:

```json
{
  "requestId": "suite-rerun-001",
  "suite": {
    "tags": ["regression"],
    "status": "active",
    "runStates": ["failed", "not-run"]
  },
  "baseUrl": "http://127.0.0.1:8080"
}
```

The `suite` selector accepts `filter`, `nodeId`, `tags`, `status`, `owner`,
`priority`, and `runStates`. Omit `runStates` to run every case matched by the
maintenance selector.

The API returns `202 Accepted` with a batch run id, JSON report URL, HTML
report URL, JUnit report URL, artifact manifest URL, and failure summary URL.
Poll the JSON report URL until the status is terminal, then collect
`/report.junit.xml` when CI needs a test result artifact, `/artifacts.json`
when an agent needs the full archive list, or `/failures.json` when it only
needs failed cases for triage.

```http
GET /api/cases/batch-runs/{batchRunId}/artifacts.json
```

The artifact manifest lists the batch JSON, HTML, JUnit XML, per-case Evidence
paths, and per-case detail API links.

```http
GET /api/cases/batch-runs/{batchRunId}/failures.json
```

The failure summary is a compact JSON document with batch status, failed count,
and one row per failed case. Each row includes case id, display name, status,
elapsed time, case run id, detail URL, Evidence path, and assertion error text
when available.

## Failed Case Evidence

Single-case detail lookup is synchronous:

```http
GET /api/case-run/evidence?caseRunId={caseRunId}
```

The payload contains the case summary, request, response, assertions,
precondition fixture context, stored topology, and persisted runtime log
records when those records exist.

## Post-Process Task Lookup

```sh
otsandbox evidence tasks \
  --store-url .runtime/store.sqlite \
  --run RUN_ID \
  --step STEP_ID \
  --kind trace_topology_collect \
  --json
```

```http
GET /api/post-process-tasks?runId=RUN_ID&stepId=STEP_ID&kind=trace_topology_collect
```

This lookup reads stored post-process task records for one run and can narrow
them by step id, case id, task kind, or status. The response includes compact
counts for passed, failed, running, skipped, and total duration so a UI or agent
can tell whether slow topology, log, or report enrichment happened after the
main request finished.

## Stability Notes

- Runtime ids are unique per run. Target ids come from profile discovery.
- Store rows are indexes and read-models; profile files remain the source of
  truth.
- Reports may contain failed cases. A failed case is not a transport failure if
  the report was produced successfully.
- HTML reports are temporary local artifacts and should not be committed.
