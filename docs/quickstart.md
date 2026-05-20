# Quick Start

This guide starts from an empty checkout and runs a neutral local import bundle. It
does not require a hosted service or a team-owned import bundle bundle.

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
OTSANDBOX_DEMO_STORE="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" npm run demo:api-case
OTSANDBOX_SMOKE_STORE_DSN="postgres://user:pass@host:5432/otsandbox_smoke?sslmode=disable" npm run release-check
```

The release check requires a PostgreSQL smoke Store DSN. It runs Go tests, the
source-domain guardrail, the React build, active PostgreSQL CLI smoke, and a
PostgreSQL-only headless browser smoke test against a generated generic import
bundle. For final live topology sign-off, add
`OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and
`OTS_SMOKE_TRACE_IDS` with trace id mappings for every workflow step from
`step-01` through `step-10` so release-check fails instead of using the
synthetic SkyWalking provider or a partial trace-id set.
The demo command starts a temporary local HTTP endpoint, runs the generic
`examples/api-cases/create-item.json` case against the active PostgreSQL Store
or `OTSANDBOX_DEMO_STORE=postgres://...`, and prints the Evidence bundle path.
Demo output is kept under the system temp directory so you can inspect it after
the command exits. Set `OTSANDBOX_CLEAN_DEMO_OUTPUT=1` to remove it
automatically.

## Configure a PostgreSQL Store

```sh
./bin/otsandbox.sh store config set local-personal \
  --url "postgres://user:pass@host:5432/otsandbox_local?sslmode=disable"
./bin/otsandbox.sh store use local-personal
./bin/otsandbox.sh store status --store local-personal
./bin/otsandbox.sh store upgrade --store local-personal
./bin/otsandbox.sh store ddl --backend postgres > otsandbox-schema.sql
```

Use a private PostgreSQL database for unverified local work and a separate
shared database for verified team environments. SQLite is kept only for legacy
compatibility while PostgreSQL rollout continues.
The Open Test Sandbox Store is the control-plane database and should already
exist outside any Docker environment restored for a tested target. Do not point
the Store DSN at a Docker database that `environment restore` is responsible
for starting; business databases used by the tested services belong to the
target environment, while the sandbox Store remains independent.

Daily discovery commands do not change when you switch between a local
PostgreSQL Store and a remote team PostgreSQL Store. Use `store use NAME` to
change the active Store, or `--store NAME_OR_DSN` for a one-off read:

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

`environment restore` is anchored to the environment's verification workflow,
for example the team core 10-step workflow. It prepares the local machine from
the Store-backed environment facts instead of acting as a generic Docker
launcher. By default it is a dry run: it resolves optional repository checkouts
under `--workspace`, shows Git clone commands when repos are recorded, and
prints preflight tool checks, Docker Compose pull/build/up commands, and
recorded health checks. Preflight checks `git` when a missing checkout must be
cloned and `docker` when a compose plan is recorded; it also labels heavy Docker
steps so an operator can review them before destructive local validation. Add
`--execute` to clone missing remote repositories, run Docker Compose, and wait
for recorded health checks. Add `--pull` with `--execute` to update existing
checkouts using `git pull --ff-only`. Add `--run-workflow` with `--execute` to
run the recorded verification workflow after Docker health checks pass; the run,
case runs, Evidence indexes, and Environment Catalog verification run status are
written to the selected Store. Restore records Evidence completeness from the
workflow result but does not mark SkyWalking topology complete or publish the
environment as verified; real topology collection and `publish-verified` remain
separate gates. Use `--base-url` for the restored target endpoint and
`--workflow-output-dir` when you want a fixed local report directory. When
`composeFile` is recorded, the file must exist under `--workspace` after
optional repository preparation; restore fails before invoking Docker if it is
missing.

The control-plane API exposes the same recovery shape through
`GET /api/environments/{environmentId}/bootstrap`: repository steps, Docker
commands, health checks, and the verification workflow are returned as a plan
for UI review. The API does not execute local Docker; execution stays in the
CLI restore path.

## Create and Install a Import Bundle

```sh
import bundle_dir="$(mktemp -d)/import bundle"
./bin/otsandbox.sh import bundle init \
  --output "$import bundle_dir" \
  --id sample \
  --display-name "Sample Import Bundle"

./bin/otsandbox.sh import bundle install --from "$import bundle_dir"
./bin/otsandbox.sh import bundle verify --import bundle sample --store local-personal
```

The core repository intentionally ships without bundled import bundles. A import bundle is
the source-owned configuration bundle for services, workflows, interface nodes,
API cases, templates, fixtures, and bindings.

## Start the Workbench

```sh
./bin/otsandbox.sh serve \
  --import bundle sample \
  --store local-personal \
  --host 127.0.0.1 \
  --port 18191
```

Open `http://127.0.0.1:18191/`.

The PostgreSQL Store is the target for daily testing workflows. The same CLI
commands work for a local PostgreSQL database or a remote team PostgreSQL
database; switch the selected Store with `store use NAME` or override one
command with `--store NAME_OR_DSN`.

## Next Steps

- Read [import bundle-authoring.md](import bundle-authoring.md) to build a real bundle.
- Read [cli-api-contracts.md](cli-api-contracts.md) before wiring an agent or
  CI job to the sandbox.
- Read [api-case-format.md](api-case-format.md) for runnable case files and
  Evidence output.
