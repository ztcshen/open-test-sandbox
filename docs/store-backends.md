# Store Backends

Open Test Sandbox treats the Store as a pluggable database backend. Users pick
one backend for a workspace or team, and daily CLI/API/UI commands operate
against the selected Store without changing command shape. The active product
path is SQL Store-first: SQLite, PostgreSQL, and MySQL are supported Store
engines, and users should choose the engine that matches their operational
boundary. SQLite is useful for local and personal Stores; PostgreSQL and MySQL
are better fits for shared, remote, and multi-user Stores. The Store holds environment catalogs, component graph metadata,
service and interface registrations, workflows, maintained API cases, run
records, Evidence indexes, trace topology indexes, baseline gates, timing data,
and post-process task state. Evidence files and local logs may still live on
disk, but their indexes belong in the selected Store.

## Backend Selection

The code path for opening a Store is centralized in `internal/store/open`, and
database-specific SQL differences live behind `internal/store/sqlstore`
dialects. Command handlers and the control-plane should depend on the
`store.Store` contract instead of importing a concrete database package
directly. This keeps PostgreSQL, MySQL, and SQLite specifics from leaking
into daily workflow commands.

Supported backend families:

- SQLite: active product Store engine for local and personal Stores.
- PostgreSQL: active product Store engine for personal and team Stores.
- MySQL: active product Store engine for organizations that require MySQL Stores.

Dialect responsibilities:

- driver name and DSN handoff;
- bind variables, for example `$1` for PostgreSQL and `?` for MySQL/SQLite;
- identifier quoting;
- JSON/time/bool column types;
- upsert syntax and migration DDL differences.

The first shared DDL builder is `sqlstore.CoreSchemaSQL`. It generates the
core workflow tables for runs, API case runs, Evidence indexes, trace topology,
post-process tasks, baseline gates, Environment Catalog rows, and component
graph tables with dialect-specific JSON/time/bool types. New Store tables
should follow this pattern instead of adding SQLite-only SQL to the daily Store
path.

Current SQL Store schema boundaries:

- `runs.environment_id` links workflow, batch, and API case run records back to
  the Environment Catalog entry that produced them when a run is environment
  scoped. Existing upgraded rows may keep an empty value.
- `environments` stores the environment lifecycle, verification workflow,
  restore summary, Evidence completeness, and topology completeness flags.
- `environment_components` stores every Docker-side runtime unit: application,
  middleware, mock, observability, platform, or support process.
- `component_dependencies` stores component-to-component consumption edges.
  Restore projects blocking phases into provider-before-consumer startup order.
- `component_config_assets` stores deterministic startup/config material owned
  by a component, such as generated Compose snippets, cert/key material, DDL,
  seed SQL, Apollo-style key/value assets, or service launch scripts. It must
  not store Docker images, source repositories, large binaries, runtime
  databases, runtime logs, or Evidence payloads. Inline Store material has no
  per-kind size limit; DDL, seed SQL, certificates, keys, and launch scripts all
  use the same 1 MB safety boundary. When an inline asset, environment
  definition/summary, or combined component graph crosses that boundary, the
  Store write is blocked with the exact field, asset, or largest-contributor
  reason.

Dependencies and assets are related but separate. A dependency row records that a
consumer needs a provider capability; the asset rows record the concrete
consumer-owned material needed to use that capability. For example,
`order-api -> mysql` is the edge, while `order-api` owns its MySQL DDL and seed
assets targeted at `mysql`. Likewise, `order-api -> apollo` or
`order-api -> config-service` is the edge, while `order-api` owns its app id,
namespace, and key/value assets targeted at that configuration provider.

## SQL Store First

Use one SQL Store per isolation boundary:

- `local-personal`: a private SQLite, PostgreSQL, or MySQL Store for unverified local work.
- `team-verified`: a shared database for verified environments and reusable
  cases.

Configure named Stores with:

```sh
otsandbox store config set local-personal --url postgres://user:pass@host:5432/otsandbox_local?sslmode=disable
otsandbox store config set team-verified --url postgres://user:pass@host:5432/otsandbox_team?sslmode=disable
otsandbox store config set team-mysql --url mysql://user:pass@host:3306/otsandbox_team?tls=false
otsandbox store config set local-sqlite --url sqlite://$PWD/.runtime/otsandbox-local.sqlite
otsandbox store use local-personal
otsandbox store current
```

Display commands, including JSON output, mask passwords in PostgreSQL and MySQL
DSNs.
The on-disk Store config keeps the real DSN so CLI commands can still open the
selected database.

To publish a locally verified environment into a team Store, upgrade the target
Store first and then copy the current restore-critical metadata:

```sh
npm run store:publish:mysql -- \
  --from local-personal \
  --to team-mysql \
  --environment local-sample \
  --workflow workflow.local-sample \
  --min-components 1 \
  --min-assets 1 \
  --verify-control-plane-url http://127.0.0.1:58663

otsandbox store status --store team-mysql
otsandbox store status --store team-mysql --json
tools/smoke/mysql-store-preflight.sh --store team-mysql \
  --output-prefix .runtime/team-mysql-preflight
python3 tools/smoke/mysql-handshake-probe.py \
  --url "mysql://user:xxxxx@host:3306/otsandbox_local?tls=false" \
  --json
otsandbox store provision --store team-mysql --json
otsandbox store upgrade --store team-mysql
otsandbox store copy --from local-personal --to team-mysql \
  --require-environment local-sample \
  --require-verification-workflow workflow.local-sample \
  --require-verified-environment \
  --require-min-components 1 \
  --require-min-assets 1 \
  --json
```

`store:publish:mysql` is the shortest repeatable path for MySQL team Store
promotion. It first proves the source Store already contains the verified
environment and component graph, then runs the MySQL handshake preflight,
provisions/upgrades the target Store through the CLI, runs `store copy` with
verified-environment gates, asserts the copy report includes the profile
catalog, profile index, active config version, and read models needed by the
acceptance workflow, reads the environment back from the target Store, asserts
the upgraded schema has no pending migrations, switches the local active Store,
verifies `store current --json` points at the MySQL target, optionally verifies
a running control plane's `/api/store/current` with
`--verify-control-plane-url URL`, and can run `environment restore` when
`--restore --workspace PATH --server-url URL` are provided.

`store copy` copies catalog/read-model data, environments, and component graphs
needed for one-click restore. Use `--require-environment` and
`--require-verification-workflow` plus `--require-verified-environment` in
shared-Store promotion scripts so a missing, wrong-workflow, or unverified
acceptance environment blocks the copy before restore starts. Add minimum
component/dependency/asset thresholds when the environment has an expected
component graph shape. The command intentionally skips historical runs,
Evidence indexes, and topology rows because the acceptance workflow should be
rerun against the shared Store before publishing the environment as verified.

For the colleague/new-machine path, use the shared Store as the source of truth
and do not copy local Store data:

```sh
npm run store:restore:mysql -- \
  --store team-mysql \
  --store-url "mysql://user:pass@host:3306/otsandbox_team?tls=false" \
  --environment local-sample \
  --workspace "$HOME/open-test-runtime" \
  --server-url http://127.0.0.1:58663 \
  --min-components 1 \
  --min-assets 1 \
  --min-acceptance-steps 1
```

`store:restore:mysql` verifies the named Store's MySQL handshake, schema
readiness, active Store selection, running control plane Store selection, copied
verified Environment Catalog entry, component graph thresholds, Docker restore,
and final SkyWalking-backed acceptance report. It is the expected new-machine
proof after an operator has already run `store:publish:mysql`.

Use `store:audit:mysql-goal` for a read-only completion audit:

```sh
npm run store:audit:mysql-goal -- \
  --from local-personal \
  --to team-mysql \
  --environment local-sample \
  --workflow workflow.local-sample \
  --control-plane-url http://127.0.0.1:58663 \
  --min-components 1 \
  --min-assets 1
```

The audit writes JSON and Markdown evidence and exits non-zero until all
read-only gates are true: source Store ready, target MySQL handshake reachable,
target schema current, target environment copied, local active Store selected,
and running control plane selected. Its `nextAction` field points operators to
the publish wrapper while the target Store is not ready, or to the colleague
restore wrapper once the shared Store gates are green. `nextCommand` is a
machine-readable argv array and `nextCommandShell` is the same command rendered
for terminal use.

Treat a TCP or SOCKS `connect` result as only a network hint. Shared MySQL Store
promotion requires a real MySQL initial handshake before provisioning. If
`tools/smoke/mysql-handshake-probe.py` reports that the connection closes before
reading the 4-byte MySQL packet header, fix the VPN route or proxy rule first;
do not run `store provision`, `store upgrade`, or `store copy` through that
path. For scripted shared-Store promotion, capture route and handshake
diagnostics before provisioning so failures can be triaged without touching the
remote database.

Keep the sandbox control-plane Store outside restored Docker environments. The
Store may describe and verify a target environment, but it must not be hosted
by that same target environment; otherwise restore would depend on the database
it is trying to discover. Target application databases used by tested services
are separate Docker-managed runtime dependencies.
Docker cleanup for colleague-machine simulation is scoped to the recorded
target Compose project only; it must not clean, recreate, or host the sandbox
SQL Store.
Restore attempt summaries are Store-first diagnostics kept in Environment
Catalog `summary.lastRestore` and `summary.restoreAttempts`; they are not
portable template package data and not a replacement for full Evidence or
workflow reports.

Commands may also use `--store NAME_OR_DSN` for a one-off override. Daily
CLI/API commands read and write the active Store unless that explicit override
is present. Legacy `--store-url` remains accepted during migration and import
tests.

The command shape is location- and engine-agnostic. A local SQLite Store, a
local PostgreSQL database, a remote team PostgreSQL database, and a remote team MySQL database
use the same daily commands; only the selected Store changes:

```sh
otsandbox store use local-personal
otsandbox case discover --filter refund

otsandbox store use team-verified
otsandbox case discover --filter refund

otsandbox case discover --store team-verified --filter refund
otsandbox workflow discover --store postgres://user:pass@host:5432/team_verified --filter checkout
otsandbox workflow discover --store mysql://user:pass@host:3306/team_verified --filter checkout
otsandbox workflow discover --store sqlite://$PWD/.runtime/otsandbox-local.sqlite --filter checkout
```

## Environment Catalog

Environment Catalog entries are active Store records, not daily-maintained file
packages. The supported lifecycle is:

- `register`: record the minimal runtime facts needed to reach a service,
  workflow target, or observability endpoint.
- `discover`: list environments from the active Store or `--store NAME_OR_DSN`.
- `inspect`: show connection facts, workflow coverage metadata, recorded
  Evidence/topology completeness flags, and verification status for one
  environment.
- `bootstrap`: prepare local runtime facts and Store rows needed before the
  first run.
- `verify`: record the selected verification run status and completeness flags.
  Evidence and topology are expected to have been produced and indexed by the
  run or collection paths before publication.
- `publish-verified`: promote only after the recorded flags pass and the
  selected Store contains a passed verification run, indexed Evidence, and a
  complete SkyWalking topology row.

The verified discovery list must not include environments that only have a
successful registration. Verification requires a passed workflow run, indexed
Evidence, and stored real SkyWalking topology with provider, trace id, status,
nodes, and edges. This verified-environment gate is stricter than deterministic
local smoke: local synthetic provider rows do not replace live endpoint
validation for team-ready environments.

## SQLite Store

SQLite is a supported SQL Store engine for local and personal workflows. It
uses the same `--store NAME_OR_DSN`, active Store config, schema upgrade,
Environment Catalog, case execution, workflow execution, Evidence index, and
report paths as PostgreSQL and MySQL.

Do not add daily testing behavior that only works with SQLite. Daily
execution/report commands must use the selected Store engine end to end and
must not create a hidden SQLite runtime when another Store is selected. Missing
Store configuration fails with guidance instead of switching engines. The
deprecated `--store-url` flag remains reserved for migration and compatibility
commands; daily commands should use `--store NAME_OR_DSN` or a named Store.

## SQL Store Validation

Release and environment verification can hard-disable SQLite Store usage:

```sh
OTSANDBOX_DISABLE_SQLITE_STORE=1 \
OTSANDBOX_SMOKE_STORE_DSN="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" \
npm run smoke:frontend

OTSANDBOX_DISABLE_SQLITE_STORE=1 \
OTSANDBOX_SMOKE_STORE_DSN="mysql://user:pass@host:3306/otsandbox_smoke?tls=false" \
npm run smoke:frontend
```

For an optional organization-owned MySQL validation pass, use a dedicated
sandbox Store database and run the guarded MySQL release wrapper:

```sh
OTSANDBOX_REQUIRE_REAL_SKYWALKING=1 \
OTS_TRACE_GRAPHQL_URL="http://skywalking.example/graphql" \
OTS_SMOKE_EXPECTED_STEPS=2 \
OTS_SMOKE_TRACE_IDS='{"step-01":"trace-01","step-02":"trace-02"}' \
OTSANDBOX_REAL_MYSQL_STORE_DSN="mysql://user:pass@host:3306/otsandbox_smoke?tls=false" \
npm run release-check:mysql-real:preflight

OTSANDBOX_REQUIRE_REAL_SKYWALKING=1 \
OTS_TRACE_GRAPHQL_URL="http://skywalking.example/graphql" \
OTS_SMOKE_EXPECTED_STEPS=2 \
OTS_SMOKE_TRACE_IDS='{"step-01":"trace-01","step-02":"trace-02"}' \
OTSANDBOX_REAL_MYSQL_STORE_DSN="mysql://user:pass@host:3306/otsandbox_smoke?tls=false" \
npm run release-check:mysql-real
```

The preflight should run first with the same environment; it validates the
guarded wrapper inputs and masks credentials without running the full release
gate. This external sign-off is intentionally separate from local documentation
and schema maintenance work. The wrapper accepts only `mysql://` and refuses
database names that do not look like a sandbox, smoke, test, or CI database.
This keeps the sandbox control-plane Store separate from application schemas
used by the services under test.
The generic MySQL release-check, CLI smoke, frontend smoke, and standalone
MySQL API smoke use the same database-name guard before Store upgrades or smoke
writes begin.
It also runs the MySQL Store contract in existing-database mode, so the
operator account needs normal DDL/DML permissions on that dedicated database
but does not need permission to create or drop databases. This wrapper is signoff
oriented: it also requires `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`,
an `http` or `https` `OTS_TRACE_GRAPHQL_URL`, `OTS_SMOKE_EXPECTED_STEPS`, and
`OTS_SMOKE_TRACE_IDS` for all configured workflow steps. It rejects
`OTSANDBOX_MYSQL_TEST_DSN_MODE=create-drop` overrides.
Direct Go contract tests require an explicit `OTSANDBOX_MYSQL_TEST_DSN_MODE`.
Use `existing` for shared smoke databases. Use `create-drop` only for local
admin-only contract tests where the account is allowed to create and drop
temporary databases.

When this flag is set, any accidental SQLite Store open fails immediately.
This is the repeatable equivalent of taking the local SQLite path offline before
running the core workflow. When `OTSANDBOX_SMOKE_STORE_DSN` is present, the smoke
harness configures a temporary named Store, selects it as active, upgrades the
schema, and serves the workbench through that named Store. The smoke must still
complete through the selected SQLite, PostgreSQL, or MySQL Store.

Smoke topology collection uses a deterministic synthetic SkyWalking GraphQL
provider unless `OTS_TRACE_GRAPHQL_URL` is set. That provider is only a local
wiring check: it proves SQL Store writes, Evidence lookup, and topology
rendering semantics, but it is not release evidence for a real SkyWalking
deployment. Set `OTS_TRACE_GRAPHQL_URL`, `OTS_SMOKE_EXPECTED_STEPS`, and
`OTS_SMOKE_TRACE_IDS` step-to-trace mappings when pointing smoke at an external
SkyWalking endpoint. For final live topology sign-off, set
`OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` and provide `OTS_SMOKE_TRACE_IDS`
mappings for every configured workflow step. When no SkyWalking endpoint is
configured, product paths must report topology as unavailable, failed, or
skipped rather than generating an invented topology.

SQLite smoke and demo execution can use an explicit `sqlite://` or `file:` DSN.
The `OTSANDBOX_ALLOW_SQLITE_COMPAT_SMOKE=1` and
`OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO=1` switches remain as convenience shortcuts
for temporary local SQLite Stores. Do not combine SQLite smoke or demo inputs
with `OTSANDBOX_DISABLE_SQLITE_STORE=1`.
