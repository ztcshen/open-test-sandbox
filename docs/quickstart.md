# Quick Start

This guide starts from an empty checkout, configures a SQL Store, and runs a
neutral local smoke flow. It does not require a hosted service or a team-owned
template package.

## Prerequisites

- Go matching `go.mod`
- Node.js 20 or newer
- npm

Install JavaScript dependencies once:

```sh
npm ci
```

## Verify the Checkout

```sh
./bin/otsandbox.sh version
# SQL Store examples:
# PostgreSQL:
OTSANDBOX_DEMO_STORE="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" npm run demo:api-case
OTSANDBOX_SMOKE_STORE_DSN="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" npm run release-check
# MySQL:
OTSANDBOX_DEMO_STORE="mysql://user:pass@host:3306/otsandbox_smoke?tls=false" npm run demo:api-case
OTSANDBOX_SMOKE_STORE_DSN="mysql://user:pass@host:3306/otsandbox_smoke?tls=false" npm run release-check
```

The release check requires a PostgreSQL or MySQL smoke Store DSN. It runs Go
tests, the source-domain guardrail, the React build, active SQL Store CLI
smoke, and a SQL Store headless browser smoke test against a generated generic import
bundle. For final live topology sign-off, add
`OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`,
`OTS_SMOKE_EXPECTED_STEPS`, and `OTS_SMOKE_TRACE_IDS` with trace id mappings
for every configured workflow step so release-check fails instead of using the
synthetic SkyWalking provider or a partial trace-id set.
The demo command starts a temporary local HTTP endpoint, runs the generic
`examples/api-cases/create-item.json` case against the active SQL Store, or
`OTSANDBOX_DEMO_STORE=postgres://...` / `OTSANDBOX_DEMO_STORE=mysql://...`, and
prints the Evidence bundle path.
MySQL demo Stores must use dedicated sandbox/smoke/test/CI-looking database
names and must not point at application schemas.
Demo output is kept under the system temp directory so you can inspect it after
the command exits. Set `OTSANDBOX_CLEAN_DEMO_OUTPUT=1` to remove it
automatically.

## Configure a SQL Store

```sh
./bin/otsandbox.sh store config set local-personal \
  --url "postgres://user:pass@host:5432/otsandbox_local?sslmode=disable"
./bin/otsandbox.sh store use local-personal
./bin/otsandbox.sh store status --store local-personal
./bin/otsandbox.sh store upgrade --store local-personal
./bin/otsandbox.sh store ddl --backend postgres > otsandbox-schema.sql

./bin/otsandbox.sh store config set team-mysql \
  --url "mysql://user:pass@host:3306/otsandbox_local?tls=false"
./bin/otsandbox.sh store use team-mysql
./bin/otsandbox.sh store status --store team-mysql
./bin/otsandbox.sh store upgrade --store team-mysql
./bin/otsandbox.sh store ddl --store team-mysql > otsandbox-mysql-schema.sql
```

Use a private PostgreSQL or MySQL database for unverified local work and a
separate shared database for verified team environments. SQLite is kept only
for legacy compatibility while SQL Store rollout continues.
The Open Test Sandbox Store is the control-plane database and should already
exist outside any Docker environment restored for a tested target. Do not point
the Store DSN at a Docker database that `environment restore` is responsible
for starting; application databases used by the tested services belong to the
target environment, while the sandbox Store remains independent.

For an optional organization-owned MySQL path, validate against a dedicated
sandbox Store database:

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

Run `release-check:mysql-real:preflight` first with the same environment. It
checks the MySQL DSN, dedicated database-name guard, existing-database mode,
real SkyWalking settings, configured workflow trace-id mapping, and credential masking
without running the heavy release gate. The full wrapper rejects non-MySQL DSNs
and database names that do not look dedicated to sandbox/smoke/test/CI
validation. It uses existing-database contract mode, so the operator account
needs normal DDL/DML permissions on that dedicated Store database but does not
need permission to create or drop databases. It also requires the real
SkyWalking release mode and trace ids for every configured workflow step;
`OTS_TRACE_GRAPHQL_URL` must be an `http` or `https` URL. Synthetic topology
smoke is not accepted by this wrapper, and `OTSANDBOX_MYSQL_TEST_DSN_MODE=create-drop`
overrides are rejected.
Direct Go MySQL contract tests also require an explicit
`OTSANDBOX_MYSQL_TEST_DSN_MODE`; use `existing` for shared smoke databases and
reserve `create-drop` for local admin-only tests.
The generic MySQL release-check path, CLI smoke, frontend smoke, and standalone
MySQL API smoke apply the same dedicated database-name guard before running
Store upgrades or smoke writes, so do not point them at an application schema.

Daily discovery commands do not change when you switch between a local
PostgreSQL Store, a remote team PostgreSQL Store, and a team MySQL Store. Use
`store use NAME` to change the active Store, or `--store NAME_OR_DSN` for a
one-off read:

```sh
./bin/otsandbox.sh case discover --filter "login"
./bin/otsandbox.sh workflow discover --store team-verified --filter "smoke"
./bin/otsandbox.sh interface-node discover --store local-personal --filter "POST /orders"
```

## Register and Verify an Environment

The Environment Catalog lives in the active Store. Register only the minimal
facts needed to reach the service and observability endpoint, then verify before
publishing it to the verified discovery list:

```sh
./bin/otsandbox.sh environment register --store local-personal --id local-sample
./bin/otsandbox.sh environment discover --store local-personal
./bin/otsandbox.sh environment inspect --store local-personal local-sample
./bin/otsandbox.sh environment bootstrap --store local-personal local-sample
./bin/otsandbox.sh environment restore --store local-personal local-sample --workspace "$HOME/open-test-runtime" --json
./bin/otsandbox.sh environment restore --store local-personal local-sample --workspace "$HOME/open-test-runtime" --execute --run-workflow --base-url http://127.0.0.1:8080 --json
./bin/otsandbox.sh environment verify --store local-personal local-sample --run RUN_ID --status passed --evidence-complete --topology-complete
./bin/otsandbox.sh environment publish-verified --store local-personal local-sample
```

An environment can appear in verified discovery only after its verification
workflow passed and the Store contains indexed Evidence plus real SkyWalking
topology for that run. `environment verify` records the run status and
completeness flags; `environment publish-verified` checks the selected Store for
the passed run, indexed Evidence, and complete SkyWalking topology before
promotion. The `--topology-complete` flag is only a recorded completeness
signal; collect real topology separately through a configured SkyWalking
endpoint before publishing a verified environment.

`environment restore` is anchored to the environment's verification workflow.
It prepares the local machine from
the Store-backed environment facts instead of acting as a generic Docker
launcher. The SQL Store stores compact source pointers and restore rules, not
source archives, Docker images, logs, or Evidence payloads. For
SQL Store-backed one-click environments, source pointers must be cloneable
remote Git URLs, including private GitLab and public GitHub repositories. Local
paths are reserved for SQLite compatibility tests and ad-hoc development, not
published one-click environments. Component repositories contain the code
mounted or built by Compose; component-owned Store assets contain only bounded
startup/config material such as generated Compose files, small cert/key
material, DDL, seed SQL, or launch scripts. Large source/runtime artifacts stay
outside the Store. By default restore is a dry run: it resolves component
checkouts under `--workspace`, shows Git clone commands when sources are
recorded, writes Store-generated startup files/assets, and prints preflight
tool checks, Docker Compose pull/build/up commands, and recorded health checks.
Preflight checks `git` when a missing
checkout must be cloned, then checks both `docker` and `docker compose version`
when a compose plan is recorded; it also labels heavy Docker steps so an
operator can review them before destructive local validation. Add
`--execute` to clone missing remote repositories, run Docker Compose, and wait
for recorded health checks. If the environment records `startCommand` without a
compose file, restore reports and can execute that command as the local start
plan. Store-backed compose facts may include a project name, env files,
profiles, a service allow-list, and `skipPull`/`skipBuild` when an environment
should start from existing local images. Add `--pull` with `--execute` to update
existing checkouts using `git pull --ff-only`. Repository
facts may also record `--repo-ref SERVICE=REF`; restore checks out that tag,
commit, or ref after cloning with detached HEAD semantics. Existing checkouts
with a recorded repo URL must be Git work trees, must match the recorded
`origin`, and must have no uncommitted changes before restore will use them;
when `--execute` is set, a clean existing checkout is also detached to the
recorded ref before Docker starts. For existing checkouts, `--repo-ref` takes
precedence over `--pull`; if there is no recorded repo URL, restore can only use
refs that already exist locally and will not fetch or compare `origin`. Add
`--run-workflow` with `--execute` to run the recorded verification workflow
after Docker health checks pass; the run, case runs, Evidence indexes, and
Environment Catalog verification run status are written to the selected Store.
Restore records Evidence completeness from the workflow result but does not
mark SkyWalking topology complete or publish the environment as verified; real
topology collection and `publish-verified` remain separate gates. Use
`--base-url` for the restored target endpoint and `--workflow-output-dir` when
you want a fixed local report directory. When `composeFile` is recorded, the
file must exist under `--workspace` after optional repository preparation;
restore fails before invoking Docker if it is missing.

The restore report also includes `readiness`, the final pre-Docker review gate
for a colleague-machine simulation. It checks that the sandbox SQL Store
is outside the target Docker environment, the restore is anchored to a
verification workflow, all recorded component repositories can be cloned or
validated before Docker, a Compose/start plan exists, recorded Compose services
cover the application services and middleware images, at least one health probe is
recorded, cleanup commands are reviewable when requested, and the operator pause
is preserved before container/image deletion or long downloads. If a workflow
needs several application services, those services should appear as repository
items or existing checkout items and must pass before Docker pull/build/up can
start. Middleware such as config services or databases normally appears through the recorded
Compose service plan and image pull/build plan, then is checked through the
same health probe gate.

Every restore attempt writes a compact diagnostic back to the selected Store's
Environment Catalog entry. `summary.lastRestore` is the quick pointer, and
`summary.restoreAttempts` keeps the most recent 20 attempts. The summary
includes restore id, phase, preflight status, repository actions, readiness
status, Docker action/cleanup status, health check counts, workflow action, and
next actions. It is intentionally not full Evidence: full command output,
workflow reports, and runtime logs stay in the existing local report/Evidence
paths, and the summary must not contain credentials, raw DSNs, or full logs.
This keeps dry-runs, blocked cleanup attempts, readiness failures, and
successful executions visible through `environment inspect` and the
control-plane API.

Health checks are Store-backed probes, not only HTTP pings. Use `--health-url`
for GET 2xx checks, `--health-tcp HOST:PORT` for port readiness,
`--health-command CMD` for workspace-local command probes, and
`--health-compose-service SERVICE` to inspect a Docker Compose service after
startup. Restore does not run probes during dry-run. During `--execute`, all
registered component repositories must clone, fetch, and ref-prepare first; only
then does restore run Compose pull/build/up, wait for health probes, and run the
verification workflow. A failed repository precheck stops before Docker startup,
and a failed probe records `phase=health-check` in `summary.lastRestore`.

For a colleague-machine simulation, add `--clean-docker-state` during dry-run
review to include a Compose-scoped cleanup plan before startup. Add
`--clean-docker-images` only when local images should also be removed with the
Compose project. The cleanup plan first records review commands
`docker compose ps`, `docker compose images`, and `docker compose config`, then
plans `docker compose down --remove-orphans`; it never uses global Docker prune
commands and never adds volume deletion flags. These review commands are a
state snapshot for human inspection, not a backup of volumes, databases, or
runtime data. During `--execute`, requested cleanup is blocked unless
`--allow-destructive-docker-cleanup` is also present. This cleanup applies only
to the recorded target Compose project; the sandbox SQL control-plane Store
must stay outside that Docker environment.

When you want to evaluate a new colleague machine without touching the current
machine's running target containers, use `--assume-clean-docker` on a dry-run.
That mode assumes the target Docker containers do not exist on the colleague
machine, skips local fixed-name container conflict blocking, and still checks
the SQL Store boundary, remote component repositories, Store-generated startup
files, component startup batches, Docker Compose commands, and health gates. It
cannot be combined with `--execute`, container adoption, or cleanup flags.

The control-plane API exposes the same recovery shape through
`GET /api/environments/{environmentId}/bootstrap`: repository steps, Docker
commands or start-command steps, health checks, and the verification workflow
are returned as a plan for UI review. The API does not execute local Docker; execution stays in the
CLI restore path.

## Optional: Create a Template Package Artifact

```sh
template_dir="$(mktemp -d)/template-package"
./bin/otsandbox.sh template-package init \
  --output "$template_dir" \
  --id sample \
  --display-name "Sample Template Package"
./bin/otsandbox.sh template-package install --from "$template_dir" --force
./bin/otsandbox.sh template-package verify --template-package "$template_dir" --store local-personal --force
```

The core repository intentionally ships without bundled team template packages.
Template packages are optional import/export/review/migration artifacts for
services, workflows, interface nodes, API cases, templates, fixtures, and
bindings. They are not the normal daily maintenance surface; daily testing uses
the active SQL Store, Environment Catalog, CLI/API discovery, and the workbench.

## Start the Workbench

```sh
./bin/otsandbox.sh serve \
  --store local-personal \
  --host 127.0.0.1 \
  --port 18191
```

Open `http://127.0.0.1:18191/`.

SQL Store is the target for daily testing workflows. The same CLI commands work
for a local PostgreSQL/MySQL database or a remote team PostgreSQL/MySQL
database; switch the selected Store with `store use NAME` or override one
command with `--store NAME_OR_DSN`.

## Next Steps

- Read [profile-authoring.md](profile-authoring.md) to build an optional
  template package artifact for import, export, review, or migration.
- Read [cli-api-contracts.md](cli-api-contracts.md) before wiring an agent or
  CI job to the sandbox.
- Read [api-case-format.md](api-case-format.md) for runnable case files and
  Evidence output.
