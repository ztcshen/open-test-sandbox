# Store Backends

Open Test Sandbox treats the Store as a pluggable database backend. Users pick
one backend for a workspace or team, and daily CLI/API/UI commands operate
against the selected Store without changing command shape. The active product
path is SQL Store-first: PostgreSQL remains the default upstream path, and
MySQL is supported for teams whose test environments standardize on MySQL. The
Store holds environment catalogs, service and interface
registrations, workflows, maintained API cases, run records, Evidence indexes,
trace topology indexes, baseline gates, timing data, and post-process task
state. Evidence files and local logs may still live on disk, but their indexes
belong in the selected Store.

## Backend Selection

The code path for opening a Store is centralized in `internal/store/open`, and
database-specific SQL differences live behind `internal/store/sqlstore`
dialects. Command handlers and the control-plane should depend on the
`store.Store` contract instead of importing a concrete database package
directly. This keeps PostgreSQL, MySQL, and SQLite compatibility from leaking
into daily workflow commands.

Supported backend families:

- PostgreSQL: default active product path for personal and team Stores.
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
post-process tasks, and baseline gates with dialect-specific JSON/time/bool
types. New Store tables should follow this pattern instead of adding SQLite-only
SQL to the daily Store path.

## SQL Store First

Use one PostgreSQL or MySQL database per isolation boundary:

- `local-personal`: a private database for unverified local work.
- `team-verified`: a shared database for verified environments and reusable
  cases.

Configure named Stores with:

```sh
otsandbox store config set local-personal --url postgres://user:pass@host:5432/otsandbox_local?sslmode=disable
otsandbox store config set team-verified --url postgres://user:pass@host:5432/otsandbox_team?sslmode=disable
otsandbox store config set company-mysql --url mysql://user:pass@host:3306/otsandbox_team?tls=false
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

For a company MySQL validation pass, use a dedicated sandbox Store database and
run the guarded MySQL release wrapper:

```sh
OTSANDBOX_REAL_MYSQL_STORE_DSN="mysql://user:pass@host:3306/otsandbox_smoke?tls=false" \
npm run release-check:mysql-real
```

The wrapper accepts only `mysql://` and refuses database names that do not look
like a sandbox, smoke, test, or CI database. This keeps the sandbox control-plane
Store separate from business schemas used by the services under test.
It also runs the MySQL Store contract in existing-database mode, so the company
account needs normal DDL/DML permissions on that dedicated database but does not
need permission to create or drop databases.

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
deployment. Set `OTS_TRACE_GRAPHQL_URL` and `OTS_SMOKE_TRACE_IDS`
step-to-trace mappings when pointing smoke at an external SkyWalking endpoint.
For final live topology sign-off, set `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` and
provide `OTS_SMOKE_TRACE_IDS` mappings for every workflow step from `step-01`
through `step-10`. When no SkyWalking endpoint is configured, product paths
must report topology as unavailable, failed, or skipped rather than generating
an invented topology.

SQLite smoke or demo execution is available only for explicit compatibility
checks with `OTSANDBOX_ALLOW_SQLITE_COMPAT_SMOKE=1` or
`OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO=1`. Do not combine those compatibility
switches with `OTSANDBOX_DISABLE_SQLITE_STORE=1`.
