# Release Checklist

Use this checklist before publishing a public tag or sharing the repository
outside a trusted team.

## Required Gate

```sh
OTSANDBOX_SMOKE_STORE_DSN="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" npm run release-check
```

For real SkyWalking validation, add `OTS_TRACE_GRAPHQL_URL` and optional
`OTS_SMOKE_TRACE_IDS` step-to-trace mappings. Without that URL the smoke uses a
deterministic synthetic SkyWalking GraphQL provider, which verifies Store,
Evidence, topology persistence, and UI wiring but is not proof of a live
SkyWalking deployment. A release sign-off that claims real topology coverage
must show the configured SkyWalking endpoint, the trace ids used by the 10-step
workflow, and persisted topology rows with provider, trace id, status, nodes,
and edges. If the endpoint is absent or a trace cannot be queried, the expected
result is unavailable, failed, or skipped topology collection, not a generated
topology.

To make release-check fail unless it is using live topology evidence, set
`OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` together with `OTS_TRACE_GRAPHQL_URL` and
`OTS_SMOKE_TRACE_IDS`. This mode rejects synthetic smoke before the expensive
gate starts.

The gate verifies:

- no root `import bundles/` directory exists;
- runtime and dependency output are not tracked;
- source-domain guardrails pass;
- `git diff --check` passes;
- Go tests pass;
- the React workbench builds;
- active PostgreSQL CLI smoke passes without per-command Store flags;
- PostgreSQL-only browser smoke tests pass in a headless context;
- the headless smoke can enter the core workflow from the workbench, click the
  workflow run button, persist the workflow run, open step Evidence, and verify
  stored topology with provider, trace id, status, nodes, and edges. This is a
  deterministic local wiring check unless `OTS_TRACE_GRAPHQL_URL` is configured
  for live SkyWalking validation.

## Completion Audit

Do not mark the PostgreSQL Store-first line complete until current evidence
proves all of these items:

- daily CLI/API/workbench paths use the active named PostgreSQL Store or
  explicit `--store NAME_OR_DSN`; compatibility SQLite paths remain isolated to
  migration, import, and historical tests;
- active or named SQLite Store configs are rejected by daily commands with
  migration/compatibility guidance;
- deprecated `--store-url` does not appear as a normal daily path and bare local
  paths are not accepted by daily commands;
- local execution paths, including `environment bootstrap`, `sandbox service
  register`, `sandbox interface register`, and `sandbox start`, have named
  PostgreSQL evidence;
- the core 10-step workbench smoke enters from the UI, runs the workflow, shows
  all 10 nodes green, and opens Evidence for the steps;
- every interface in the 10-step run has indexed request/response/assertion
  Evidence in the selected PostgreSQL Store;
- live SkyWalking proof was run with `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`,
  `OTS_TRACE_GRAPHQL_URL`, and real `OTS_SMOKE_TRACE_IDS`, and the persisted
  topology rows include provider, trace id, status, observed nodes, and
  confirmed edges;
- `npm run release-check` passed with a PostgreSQL smoke Store DSN, and the live
  SkyWalking sign-off command above passed when real topology coverage is
  claimed.

## Manual Review

- `README.md` points to the current quick start and public docs.
- `CHANGELOG.md` describes notable changes.
- New CLI, API, Store, report, or import bundle contracts are documented.
- Environment Catalog docs describe register, discover, inspect, bootstrap,
  verify, and publish-verified behavior, including the verified discovery gate:
  passed workflow, indexed Evidence, and real SkyWalking topology.
- Generated runtime output remains outside git.
- Public examples use synthetic data only.
- Third-party dependency licenses are reviewed.

## Public Release Notes

For each public release, include:

- what changed;
- any breaking contract changes;
- minimum Go and Node versions;
- known limitations;
- migration notes for import bundle authors.

## Packaging

The first public release can ship source only. Binary packaging can be added
later with a dedicated release tool once CLI flags and report contracts settle.
