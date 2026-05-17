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

## Stability Notes

- Runtime ids are unique per run. Target ids come from profile discovery.
- Store rows are indexes and read-models; profile files remain the source of
  truth.
- Reports may contain failed cases. A failed case is not a transport failure if
  the report was produced successfully.
- HTML reports are temporary local artifacts and should not be committed.
