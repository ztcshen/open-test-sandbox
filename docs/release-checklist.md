# Release Checklist

Use this checklist before publishing a public tag or sharing the repository
outside a trusted team.

## Required Gate

```sh
# SQL Store examples:
# PostgreSQL:
OTSANDBOX_SMOKE_STORE_DSN="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" npm run release-check
# MySQL:
OTSANDBOX_SMOKE_STORE_DSN="mysql://user:pass@host:3306/otsandbox_smoke?tls=false" npm run release-check
```

## Optional Real-Environment Sign-Off

Generic MySQL release-check wiring is available. A project or organization that
needs stricter real-environment evidence can run the optional MySQL +
SkyWalking gate after supplying real endpoint values:

- `OTSANDBOX_REAL_MYSQL_STORE_DSN` points at a dedicated sandbox/smoke/test/CI
  MySQL Store database, not an application schema.
- `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`,
  `OTS_SMOKE_EXPECTED_STEPS`, and `OTS_SMOKE_TRACE_IDS` are provided for the
  configured workflow.
- `npm run release-check:mysql-real:preflight` passes with the same environment
  and shows the masked MySQL DSN plus the release-check command it would run.
- `npm run release-check:mysql-real` or the manual `mysql-real-signoff` CI job
  passes with the selected MySQL Store and real SkyWalking endpoint.

For public release notes, record only non-secret evidence: the exact command or
CI run URL, Store database name, masked DSN, SkyWalking endpoint host, and
configured workflow report summary.

The public GitHub Actions CI runs this same gate against a temporary MySQL 8.0
service container and the `otsandbox_ci_smoke` Store database. That proves the
generic release gate is executable without relying on a developer laptop or a
private network.

For real SkyWalking validation, add an `http` or `https`
`OTS_TRACE_GRAPHQL_URL` and `OTS_SMOKE_TRACE_IDS` step-to-trace mappings.
Without that URL the smoke uses a deterministic synthetic SkyWalking GraphQL
provider, which verifies Store, Evidence, topology persistence, and UI wiring
but is not proof of a live SkyWalking deployment. A release sign-off that
claims real topology coverage must show the configured SkyWalking endpoint,
trace ids for every configured workflow step, and persisted topology rows with provider,
trace id, status, nodes, and edges. If the endpoint is absent or a trace cannot
be queried, the expected result is unavailable, failed, or skipped topology
collection, not a generated topology.

To make release-check fail unless it is using live topology evidence, set
`OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` together with `OTS_TRACE_GRAPHQL_URL`,
`OTS_SMOKE_EXPECTED_STEPS`, and `OTS_SMOKE_TRACE_IDS`. This mode requires trace
id mappings for every configured workflow step and rejects synthetic or partial
smoke before the expensive gate starts.

For optional real MySQL sign-off, run
`npm run release-check:mysql-real:preflight` first, then
`npm run release-check:mysql-real` with a dedicated `mysql://` Store DSN. Both
entrypoints require the same real SkyWalking mode, an `http` or `https`
GraphQL URL, and complete trace-id mapping for the configured workflow. The
preflight stops before the heavy gate after proving the guarded wrapper would
run release-check with a masked DSN and existing-database mode.
The generic MySQL `npm run release-check` path also refuses MySQL database
names that do not look dedicated to sandbox/smoke/test/CI validation before it
runs Store migrations, tests, CLI smoke, API smoke, or frontend smoke writes.
Generic MySQL release-check sets the Store contract to existing-database mode
unless `OTSANDBOX_MYSQL_TEST_DSN_MODE` is explicitly provided; `create-drop` is
for local admin-only contract tests. The `release-check:mysql-real` wrapper
rejects `create-drop` overrides and always signs off with an existing dedicated
smoke Store database.

CI also exposes a manual `workflow_dispatch` path named
`mysql-real-signoff`. It is intentionally separate from pull requests and only
runs when the operator selects `mysql_real_signoff=true`; it expects repository
secrets for `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`,
`OTS_SMOKE_EXPECTED_STEPS`, and `OTS_SMOKE_TRACE_IDS`. The manual job runs the
same two-stage path as local operators: `release-check:mysql-real:preflight` first, then
`release-check:mysql-real`.

The gate verifies:

- no root `template-packages/` directory exists;
- runtime and dependency output are not tracked;
- source-domain guardrails pass;
- `git diff --check` passes;
- Go tests pass;
- the React workbench builds;
- active SQL Store CLI smoke passes without per-command Store flags;
- SQL Store browser smoke tests pass in a headless context;
- the headless smoke can enter the core workflow from the workbench, click the
  workflow run button, persist the workflow run, open step Evidence, and verify
  stored topology with provider, trace id, status, nodes, and edges. This is a
  deterministic local wiring check unless `OTS_TRACE_GRAPHQL_URL` is configured
  for live SkyWalking validation.

## Completion Audit

Do not mark the SQL Store-first line complete until current evidence proves all
of these items:

- daily CLI/API/workbench paths use the active named PostgreSQL/MySQL Store or
  explicit `--store NAME_OR_DSN`; compatibility SQLite paths remain isolated to
  migration, import, and historical tests;
- active or named SQLite Store configs are rejected by daily commands with
  migration/compatibility guidance;
- deprecated `--store-url` does not appear as a normal daily path and bare local
  paths are not accepted by daily commands;
- local execution paths, including `environment bootstrap`, `sandbox service
  register`, `sandbox interface register`, and `sandbox start`, have named
  SQL Store evidence;
- the core workbench smoke enters from the UI, runs the workflow, shows
  all configured nodes green, and opens Evidence for the steps;
- every interface in the configured workflow run has indexed request/response/assertion
  Evidence in the selected SQL Store;
- live SkyWalking proof was run with `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`,
  `OTS_TRACE_GRAPHQL_URL`, `OTS_SMOKE_EXPECTED_STEPS`, and real
  `OTS_SMOKE_TRACE_IDS`, and the persisted topology rows include provider,
  trace id, status, observed nodes, and confirmed edges;
- `npm run release-check` passed with the selected SQL smoke Store DSN, and the live
  SkyWalking sign-off command above passed when real topology coverage is
  claimed.

## Manual Review

- `README.md` points to the current quick start and public docs.
- `CHANGELOG.md` describes notable changes.
- New CLI, API, Store, report, or template package contracts are documented.
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
- migration notes for template package authors.

## Packaging

The first public release can ship source only. Binary packaging can be added
later with a dedicated release tool once CLI flags and report contracts settle.
