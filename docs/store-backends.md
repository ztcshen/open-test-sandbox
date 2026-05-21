# Store Backends

Open Test Sandbox treats the Store as a pluggable database backend. Users pick
one backend for a workspace or team, and daily CLI/API/UI commands operate
against the selected Store without changing command shape. The active product
path is SQL Store-first: PostgreSQL and MySQL are supported product Store
engines, and teams should choose the engine that matches their operational
environment. The Store holds environment catalogs, component graph metadata,
service and interface registrations, workflows, maintained API cases, run
records, Evidence indexes, trace topology indexes, baseline gates, timing data,
and post-process task state. Evidence files and local logs may still live on
disk, but their indexes belong in the selected Store.

## Backend Selection

The code path for opening a Store is centralized in `internal/store/open`, and
database-specific SQL differences live behind `internal/store/sqlstore`
dialects. Command handlers and the control-plane should depend on the
`store.Store` contract instead of importing a concrete database package
directly. This keeps PostgreSQL, MySQL, and SQLite compatibility from leaking
into daily workflow commands.

Supported backend families:

- PostgreSQL: active product Store engine for personal and team Stores.
- MySQL: active product path for organizations that require MySQL Stores.
- SQLite: compatibility backend for migration, old local runs, and tests.

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
- `component_config_assets` stores bounded startup/config material owned by a
  component, such as generated Compose snippets, small cert/key material, DDL,
  seed SQL, Apollo-style key/value assets, or service launch scripts. It must
  not store Docker images, source repositories, large binaries, runtime
  databases, runtime logs, or Evidence payloads.

## SQL Store First

Use one PostgreSQL or MySQL database per isolation boundary:

- `local-personal`: a private database for unverified local work.
- `team-verified`: a shared database for verified environments and reusable
  cases.

Configure named Stores with:

```sh
otsandbox store config set local-personal --url postgres://user:pass@host:5432/otsandbox_local?sslmode=disable
otsandbox store config set team-verified --url postgres://user:pass@host:5432/otsandbox_team?sslmode=disable
otsandbox store config set team-mysql --url mysql://user:pass@host:3306/otsandbox_team?tls=false
otsandbox store use local-personal
otsandbox store current
```

Display commands, including JSON output, mask passwords in PostgreSQL and MySQL
DSNs.
The on-disk Store config keeps the real DSN so CLI commands can still open the
selected database.

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

The command shape is location- and engine-agnostic. A local PostgreSQL
database, a remote team PostgreSQL database, and a remote team MySQL database
use the same daily commands; only the selected Store changes:

```sh
otsandbox store use local-personal
otsandbox case discover --filter refund

otsandbox store use team-verified
otsandbox case discover --filter refund

otsandbox case discover --store team-verified --filter refund
otsandbox workflow discover --store postgres://user:pass@host:5432/team_verified --filter checkout
otsandbox workflow discover --store mysql://user:pass@host:3306/team_verified --filter checkout
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

## SQLite Compatibility

SQLite is no longer the product target for new daily workflows. It remains a
compatibility path for old local runs, legacy Evidence import, and tests that
exercise historical behavior while the SQL Store path is being rolled in.

Do not add new daily testing behavior that only works with SQLite.
Daily execution/report commands must not create a hidden SQLite runtime when
the selected Store is PostgreSQL or MySQL. The inverse also holds for compatibility
runs: a command uses the selected Store engine end to end, and missing Store
configuration fails with guidance instead of switching to another engine.

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
complete through the selected PostgreSQL or MySQL Store.

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

SQLite smoke or demo execution is available only for explicit compatibility
checks with `OTSANDBOX_ALLOW_SQLITE_COMPAT_SMOKE=1` or
`OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO=1`. Do not combine those compatibility
switches with `OTSANDBOX_DISABLE_SQLITE_STORE=1`.
