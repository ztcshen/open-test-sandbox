# API Case Format

API Cases are reviewable JSON files that describe one HTTP interaction, the
assertions to check, and the Evidence files a run should produce. They are
profile-neutral: domain language belongs in profile bundles and example data,
not in the core runner.

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

## Store Indexing

When `otsandbox case run` receives `--store-url`, it records:

- one `runs` row keyed by the run id;
- one `api_case_runs` row keyed by the run id and case id;
- one `evidence_records` row for each Evidence file produced.

The profile id comes from `--profile` and defaults to `default`. Store indexing
does not replace the Evidence bundle; it makes local runs searchable and
connects them to profile or workflow records.

## Examples

See [../examples/api-cases/create-item.json](../examples/api-cases/create-item.json)
for a minimal generic case that can run against a local HTTP endpoint.
