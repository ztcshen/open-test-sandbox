# API Case Format

API Cases are reviewable JSON files that describe one HTTP interaction, the
assertions to check, and the Evidence files a run should produce. They are
template package-neutral: domain language belongs in external template packages and
example data, not in the core runner.

## Case File

```json
{
  "id": "case.create-item",
  "title": "Create Item",
  "request": {
    "method": "POST",
    "path": "/v1/items",
    "headers": {
      "Content-Type": "application/json"
    },
    "body": {
      "id": "item-001",
      "name": "Example Item"
    }
  },
  "assertions": {
    "expectedStatusCodes": [200, 201],
    "responseContains": ["created"]
  }
}
```

### Top-Level Fields

- `id`: stable case identifier used in Evidence and Store records.
- `title`: human-readable case title.
- `request`: HTTP request definition.
- `assertions`: response checks for live runs.

### Request Fields

- `method`: HTTP method. The runner sends it in uppercase.
- `path`: request path or relative URL resolved against `--base-url`.
- `headers`: optional string map of HTTP headers.
- `body`: optional JSON object. When present, the runner sends it as JSON and
  defaults `Content-Type` to `application/json` if the case does not set it.

### Assertion Fields

- `expectedStatusCodes`: optional list of acceptable response status codes.
- `responseContains`: optional list of strings that must appear in the response
  body.

If an assertion list is empty, that assertion type is skipped. A live run fails
when any configured assertion fails.

## Evidence Contract

The runner writes Evidence under:

```text
<evidence-dir>/<run-id>/
```

Every run writes the request and runtime response Evidence:

- `case.json`: normalized copy of the input case.
- `request.json`: rendered request definition.
- `response.json`: status code, response headers, and response body.
- `assertions.json`: assertion status and any failure messages.
- `summary.json`: run summary with run id, case id, status, Evidence path, and
  creation time.

Evidence files are the detailed runtime record. Store rows are indexes and
summaries that point back to these files.

Store Evidence records also carry attachment metadata inspired by the reference
feature backlog's Allure and ReportPortal evidence model:

- `category`: local grouping such as `http-exchange`, `assertion-result`,
  `run-summary`, or `runtime-attachment`.
- `visibility`: whether the attachment is intended for normal report surfaces.
  API case runner records use `public`.
- `labels`: JSON labels such as case id, run id, and Evidence kind for
  agent-friendly filtering and future report categories.
- `stepId`: optional first-class workflow step relation for Store-backed
  workflow and topology evidence.

Case Evidence detail APIs redact common sensitive JSON keys, sensitive headers,
and URL query parameters before returning request, response, log, and summary
payloads for normal report surfaces.

## Store Indexing

`agent-testbench case run` can execute a case file with `--case PATH` or a Store
catalog case with `--case-id ID`. The `--case-id` form requires the active
Store or `--store NAME_OR_DSN`, reads the case execution adapter from that
Store catalog, and uses the same local test-kit runner as `/api/test-kit/run`.

When `agent-testbench case run` receives an active Store or `--store NAME_OR_DSN`, it
records:

- one `runs` row keyed by the run id;
- one `api_case_runs` row keyed by the run id and case id;
- one `evidence_records` row for each Evidence file produced.

The profile id comes from `--profile` and defaults to `default`. When the run is
started from an environment-scoped workflow or batch request, `runs.environment_id`
records the Environment Catalog id so reports, Evidence, and topology can be
joined back to the verified environment. Store indexing does not replace the
Evidence bundle; it makes local runs searchable and connects them to the active
SQL Store, template package, workflow records, or Environment Catalog records.

The deprecated compatibility flag `--store-url` is reserved for migration and
legacy compatibility paths. Daily SQL Store-first usage should use an active
named Store or `--store NAME_OR_DSN`, including explicit `sqlite://`,
`postgres://`, and `mysql://` Store references.

## Async Batch Runs

The control plane can start a local asynchronous API case batch for agent or CI
callers that already know which interface nodes are affected by a change:

```http
POST /api/cases/batch-runs
Content-Type: application/json
```

```json
{
  "requestId": "change-001",
  "nodeIds": ["node.alpha", "node.beta"],
  "baseUrl": "http://127.0.0.1:8080",
  "evidenceDir": ".runtime/case-batches",
  "overrides": {
    "id": "item-override"
  }
}
```

Use `nodeIds` to run all template package API cases attached to one or more interface
nodes. To run a workflow-shaped regression, send `workflowId` instead:

```json
{
  "requestId": "workflow-001",
  "workflowId": "workflow.ten"
}
```

The response is `202 Accepted` and contains a `batchRunId`, JSON `reportUrl`,
and temporary HTML `htmlReportUrl`. The batch runner selects every matching
template package API case, returns immediately, and executes the selected cases in the
background. Workflow selection follows `workflowBindings` sorted by
`sortOrder` and `stepId`. Each finished case is still recorded as a normal API
case run with Evidence and Store rows.

Poll the report URL until `status` becomes `passed` or `failed`:

```http
GET /api/cases/batch-runs/{batchRunId}
```

The JSON report includes aggregate counts, optional `workflowId`,
`htmlReportPath`, `htmlReportUrl`, and per-case `stepId`, `runId`,
`caseRunId`, `status`, `viewerUrl`, `detailUrl`, `evidencePath`, and
`elapsedMs`. The HTML report is rendered from the built-in report template
under the Evidence directory and is refreshed as each case completes, so a
caller can return either a machine-readable result or a human-readable
temporary report.

When a case fails, use the per-case `detailUrl` or query the synchronous detail
API directly:

```http
GET /api/case-run/evidence?caseRunId={caseRunId}
```

The detail payload reuses the same Evidence shape as the browser evidence
viewer. It includes the selected case summary, request, response, assertions,
precondition fixture context, stored trace topology, and persisted runtime log
records when they exist. A single-case detail lookup is synchronous because it
only reads Store rows and local Evidence files.

## Examples

See [../examples/api-cases/create-item.json](../examples/api-cases/create-item.json)
for a minimal generic case that can run against a local HTTP endpoint.
