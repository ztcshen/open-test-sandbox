# AgentTestBench

[![CI](https://github.com/ztcshen/agent-testbench/actions/workflows/ci.yml/badge.svg)](https://github.com/ztcshen/agent-testbench/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

**English** | [简体中文](README.zh-CN.md)

AgentTestBench is an agent-native test environment for API workflows,
auditable Evidence, and quality gates. It helps test engineers and automation
agents discover runnable targets, run API cases and workflows, record
reproducible Evidence, and inspect compact HTML/JSON reports without
hardcoding one business domain into the open-source core.

## Product Direction

The current product direction is agent-native and Store-first:

- Test engineers and agents use the workbench through APIs and UI. They do not
  maintain a separate sandbox project or repeatedly edit external configuration.
- SQL Store is the active Store for current sandbox state, runtime facts,
  workflow catalog, execution state, Evidence indexes, and verification
  results. SQLite, PostgreSQL, and MySQL are supported SQL Store engines. Local
  and remote SQL Stores use the same daily commands; users switch the active
  database through named Store config.
- SQLite is useful for local and personal Stores; PostgreSQL and MySQL are
  stronger fits for shared, remote, and multi-user Stores. Old local data
  import remains a compatibility path.
- Optional template packages may exist only for import, export, sharing, review,
  or migration. They are not the daily testing surface.
- New functionality should expose Store-first APIs and UI flows before adding
  any file-package compatibility bridge.

## Why It Exists

Modern integration testing often has three painful gaps:

- test assets are scattered across code, databases, scripts, and private docs;
- automation agents cannot reliably discover target ids and runtime context;
- failing cases rarely come with enough request, response, log, timing, and
  topology Evidence to review quickly.

AgentTestBench turns those pieces into one local control plane. The Store is
the live source of truth; CLI, Control plane APIs, the React workbench, reports,
and validation tools all read the same facts. Agents get a discover-then-run
contract instead of guessing target ids from prompts or private notes.

## Current Shape

- **Store engines**: SQLite, PostgreSQL, and MySQL are product SQL Store
  engines with different operating boundaries.
- **Daily workflow**: configure or switch a named Store once, then use the same
  CLI/API/UI commands for local and remote SQL Stores.
- **Environment Catalog**: register environments, inspect bootstrap plans,
  restore target Docker stacks from remote component repositories plus compact
  Store-backed startup/config assets, record acceptance workflow results, and
  publish only verified environments.
- **Acceptance proof**: verified environments require a passed workflow run,
  indexed Evidence, and real SkyWalking topology stored in the selected Store.
- **Release gates**: generic SQLite/PostgreSQL/MySQL `release-check` is wired; optional
  organization-owned real-environment sign-off can add the stricter two-stage
  real SkyWalking gate with externally supplied secrets and trace ids.

## What You Get

| Capability | What it means |
| --- | --- |
| SQL Store-first | Named SQLite, PostgreSQL, or MySQL Stores with schema upgrades, run indexes, case run records, Evidence indexes, timing, logs, topology, and post-process task records. |
| API-operated catalog | Services, workflows, interface nodes, cases, request templates, fixtures, dependencies, and bindings are exposed through AgentTestBench APIs and UI discovery. |
| Agent-friendly discovery | Agents call discovery APIs first, then run reports with exact returned ids instead of hidden prompt knowledge. |
| API case execution | Run one HTTP case, a maintained case suite, or only the failed/not-run part of a suite; render requests, assert responses, write Evidence, and index results into Store. |
| Workflow execution | Run ordered workflow steps and keep per-step Evidence, timing, status, logs, and topology. |
| Environment restore | Store-backed Environment Catalog entries can plan or execute remote repository preparation, compact startup-file generation, Docker Compose pull/build/up, health checks, and the bound verification workflow. |
| Evidence detail APIs | Query request, response, assertions, precondition context, stored topology, persisted logs, artifact manifests, failure summaries, status, and elapsed time by run or case run id. |
| Real topology gate | Synthetic SkyWalking smoke is useful for wiring, but verified-environment publication and optional real-environment sign-off require a live SkyWalking endpoint and trace ids for every configured workflow step. |
| Control plane workbench | A React workbench reads the same Store/read-models as CLI and API users. |
| Open-source guardrails | Release checks prevent generated state and source-domain terms from entering the generic core. |

## Who It Helps

- **Test engineers** who need to consume a test environment through stable APIs and UI,
  not maintain another local project.
- **QA and platform teams** that need a repeatable local workbench for
  integration cases, workflow regressions, and runtime Evidence.
- **Agent builders** that want a clean discover-then-run contract.
- **Backend teams** that need compact failure reports with request, response,
  assertion, timing, log, and topology context.

## Quick Start

Install dependencies and verify the checkout:

```sh
npm ci
./bin/agent-testbench.sh version
# SQL Store examples:
# PostgreSQL:
AGENT_TESTBENCH_DEMO_STORE='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run release-check
# MySQL:
AGENT_TESTBENCH_DEMO_STORE='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run release-check
# SQLite:
AGENT_TESTBENCH_DEMO_STORE="sqlite://$PWD/.runtime/agent-testbench-smoke.sqlite" npm run demo:api-case
AGENT_TESTBENCH_SMOKE_STORE_DSN="sqlite://$PWD/.runtime/agent-testbench-smoke.sqlite" npm run release-check
```

The primary CLI is `agent-testbench`; public configuration and smoke-test
environment variables use the `AGENT_TESTBENCH_*` namespace.

The demo command starts a temporary local HTTP endpoint, runs the generic
`examples/api-cases/create-item.json` case against the active SQL Store or
`AGENT_TESTBENCH_DEMO_STORE=postgres://...` /
`AGENT_TESTBENCH_DEMO_STORE=mysql://...` /
`AGENT_TESTBENCH_DEMO_STORE=sqlite://...`, and prints the Evidence bundle path. The
demo and release gate require dedicated MySQL Store database names that look
like sandbox/smoke/test/CI targets; do not point them at an application schema. The
release gate requires a SQLite, PostgreSQL, or MySQL smoke Store DSN.
It runs whitespace checks, generated-state checks, source-domain guardrails, Go
tests, the demo, the React build, active SQL Store CLI smoke tests, and
headless browser smoke tests.

By default, smoke tests use a deterministic synthetic SkyWalking GraphQL
provider so local wiring checks are repeatable. This is not release evidence
for a real SkyWalking deployment. To validate the real topology path, set
`AGENT_TESTBENCH_TRACE_GRAPHQL_URL`, `AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS`, and
`AGENT_TESTBENCH_SMOKE_TRACE_IDS` so the configured workflow smoke uses real trace ids. For
final sign-off that must fail instead of using synthetic topology evidence,
also set `AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1`; in that mode
`AGENT_TESTBENCH_SMOKE_TRACE_IDS` must map every configured workflow step.
When no SkyWalking endpoint is configured, topology collection must
report unavailable, failed, or skipped status instead of inventing a topology.

## Architecture

```text
AgentTestBench APIs and UI
  -> active SQL Store (SQLite, PostgreSQL, or MySQL)
  -> catalog read-models
  -> Environment Catalog and component graph
  -> remote component repositories plus target Docker runtime
  -> CLI discovery, Control plane APIs, React workbench
  -> case and workflow execution
  -> Evidence files plus Store indexes
  -> JSON and HTML reports
  -> detail APIs for failed case review
```

Core packages stay generic:

- `cmd/agent-testbench/`: CLI entrypoint and command orchestration.
- `internal/server/controlplane/`: HTTP APIs, workbench data, reports, and Evidence views.
- `internal/runner/`: runnable automation helpers such as API cases, request templates, JUnit output, executor planning, and Evidence import.
- `internal/domain/`: generic profile, case-suite, redaction, and audit domain logic.
- `internal/store/`: Store contract and runtime records.
- `internal/store/postgres/`: PostgreSQL product Store backend.
- `internal/store/mysql/`: MySQL product Store backend.
- `internal/store/sqlite/`: SQLite product Store backend for local and personal Stores.
- `control-plane/frontend/`: React workbench source.
- `control-plane/static/`: built static workbench assets served by `agent-testbench serve`.

## Documentation

| Page | What it covers |
| --- | --- |
| [Quick Start](docs/quickstart.md) | First local run, Store setup, and workbench launch direction. |
| [Backend Capabilities](docs/backend-capabilities.md) | Store, Environment Catalog, clean-machine restore, discovery, execution, reports, Evidence, APIs, and release guardrails. |
| [Share Kit](docs/share-kit.md) | Project tagline, short descriptions, demo script, and announcement snippets for sharing the project. |
| [Roadmap](docs/roadmap.md) | Public development themes and contribution-friendly milestones. |
| [API Case Format](docs/api-case-format.md) | Runnable HTTP case JSON and Evidence output contract. |
| [Store Backends](docs/store-backends.md) | SQLite/PostgreSQL/MySQL Store setup, MySQL safety guards, and Store boundaries. |
| [CLI and API Contracts](docs/cli-api-contracts.md) | Agent/CI discovery, Environment Catalog lifecycle, reports, asynchronous batches, topology collection, and failed-case Evidence lookup. |
| [Release Checklist](docs/release-checklist.md) | Local gates, CI gates, real SkyWalking requirements, and optional real-environment sign-off. |
| [Visual Overview](docs/core-capabilities-skills-goals.html) | Bilingual capability map, API surface, data flow, and goals. |

## Project Principles

- Keep the default developer experience local and lightweight while using a
  named SQL Store for daily product flows.
- Keep local and remote SQL Store usage command-compatible: switch the active
  Store instead of changing daily commands.
- Test engineers should call sandbox APIs or use the UI, not maintain separate
  configuration projects.
- Treat Evidence, reports, logs, and local databases as generated runtime state.
- Make agent flows discoverable before execution.
- Keep public contracts documented when CLI, API, Store, or report shapes change.

## Status

The project is pre-1.0. Some internal package names and compatibility commands
still reflect the earlier file-package design; those are migration debt, not the
target product model.

Current working areas:

- Store lifecycle: named SQLite/PostgreSQL/MySQL config, active Store switching,
  backend-specific DDL, schema status/upgrade, and contract tests.
- Catalog maintenance: API case metadata, searchable case catalog, request
  templates, fixtures, dependencies, workflow bindings, and suite coverage.
- Execution: single API case, maintained case suites, async batch surfaces,
  interface-node reports, workflow reports, and persisted workflow run lookup.
- Evidence: request, response, assertions, summaries, logs, topology, timing,
  artifact manifests, failure summaries, and redaction for sensitive fields.
- Environment Catalog: Store-backed environment register/discover/inspect,
  bootstrap plan, restore diagnostics, component graph readiness, remote component
  repository preparation, Docker Compose/start orchestration, health gates,
  acceptance workflow recording, and verified publishing gates.
- Workbench: local React pages backed by Control plane APIs for catalog,
  workflow, environment, run, Evidence, and topology review.
- Release gate: `AGENT_TESTBENCH_SMOKE_STORE_DSN=postgres://... npm run release-check`
  or `AGENT_TESTBENCH_SMOKE_STORE_DSN=mysql://... npm run release-check`; for an
  optional organization-owned MySQL Store sign-off, run
  `npm run release-check:mysql-real:preflight` first, then
  `npm run release-check:mysql-real` with
  `AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1`, `AGENT_TESTBENCH_TRACE_GRAPHQL_URL`,
  `AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS`, and `AGENT_TESTBENCH_SMOKE_TRACE_IDS` for every configured
  workflow step.

Remaining optional real-environment proof is operational rather than
architectural: operators must supply a real MySQL Store DSN, real SkyWalking
GraphQL endpoint, configured workflow step count, and complete trace-id mapping
for that workflow before running the strict gate.

## Contributing

Run the full local gate before publishing a change:

```sh
AGENT_TESTBENCH_SMOKE_STORE_DSN='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run release-check
# or
AGENT_TESTBENCH_SMOKE_STORE_DSN='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run release-check
```

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), and
[docs/release-checklist.md](docs/release-checklist.md). AgentTestBench is
licensed under the [Apache License 2.0](LICENSE).
