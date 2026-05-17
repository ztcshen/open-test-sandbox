# Open Test Sandbox

[![CI](https://github.com/ztcshen/open-test-sandbox/actions/workflows/ci.yml/badge.svg)](https://github.com/ztcshen/open-test-sandbox/actions/workflows/ci.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](LICENSE)

**English** | [简体中文](README.zh-CN.md)

Open Test Sandbox is a local-first test sandbox workbench for profile-driven
integration testing. It helps teams and agents discover runnable targets, run
API cases or workflows, record reproducible Evidence, and return compact
HTML/JSON reports without hardcoding one business domain into the core.

Open Test Sandbox 是一个本地优先、配置驱动的测试沙箱工作台。它让团队和测试
agent 能够发现可测目标、执行接口用例或工作流、记录可复现 Evidence，并生成
紧凑的 HTML/JSON 报告，同时保持核心仓库通用、可开源、可跨团队复用。

## Why It Exists

Modern integration testing often has three painful gaps:

- test assets are scattered across code, databases, scripts, and private docs;
- agent-driven regression is hard because target ids and setup context are not
  discoverable;
- failing cases rarely come with enough request, response, log, timing, and
  topology Evidence to review quickly.

Open Test Sandbox turns those pieces into a local control plane. Profiles stay
outside the core repository as reviewable configuration bundles. The core
publishes them into a local Store, runs cases, records Evidence, and exposes
the same facts to the CLI, Control plane APIs, React workbench, and reports.

## What You Get

| Capability | What it means |
| --- | --- |
| Local-first Store | SQLite by default, with schema upgrades, run indexes, case run records, Evidence indexes, timing, logs, topology, and post-process task records. |
| External profiles | Services, workflows, interface nodes, cases, request templates, fixtures, dependencies, and bindings live outside the core repository. |
| Agent-friendly discovery | Agents call `interface-node discover`, `workflow discover`, or `case discover` first, then run reports with exact returned ids. |
| Case maintenance catalog | API cases can carry description, tags, priority, owner, status, runnable file presence, and execution configuration for review, assignment, and suite execution. |
| API case execution | Run a single HTTP case or a maintained case suite, render requests, assert responses, write Evidence, and optionally index results into Store. |
| Interface and workflow reports | Run all cases attached to an interface node or ordered workflow steps, then produce JSON plus temporary HTML reports. |
| Evidence detail APIs | Query request, response, assertions, precondition context, stored topology, persisted logs, status, and elapsed time by run or case run id. |
| Control plane workbench | A React workbench reads the same Store/read-models as CLI and API users. |
| Open-source guardrails | Release checks prevent generated state and source-domain terms from entering the generic core. |

## Who It Helps

- **QA and platform teams** that need a repeatable local workbench for
  integration cases, workflow regressions, and runtime Evidence.
- **Agent builders** that want a clean discover-then-run contract instead of
  prompts full of hidden ids.
- **Backend teams** that need compact failure reports with request, response,
  assertion, timing, log, and topology context.
- **Organizations with many product teams** that want each team to own its
  profile bundle while sharing one generic sandbox core.

## Use Cases

- Generate a regression report for every case attached to one interface node.
- Run a maintained suite selected by tag, owner, priority, status, or node.
- Run a workflow-shaped regression and keep per-step Evidence.
- Let an agent discover available targets before choosing what to test.
- Publish external profile bundles into a local Store for review and replay.
- Inspect a failed case without re-running the whole workflow.
- Keep team-specific test data out of the open-source core.

## Quick Start

Install dependencies and verify the checkout:

```sh
npm ci
./bin/otsandbox.sh version
npm run demo:api-case
npm run release-check
```

The demo command starts a temporary local HTTP endpoint, runs the generic
`examples/api-cases/create-item.json` case, and prints the Evidence bundle
path. The release gate runs whitespace checks, generated-state checks, source
domain guardrails, Go tests, the demo, the React build, and headless browser
smoke tests.

Create a local Store and publish an external profile:

```sh
tmpdir=$(mktemp -d)
store="$tmpdir/store.sqlite"
profile_dir="$tmpdir/sample-profile"

./bin/otsandbox.sh store upgrade --store-url "$store"
./bin/otsandbox.sh profile init --output "$profile_dir" --id sample
./bin/otsandbox.sh profile install --from "$profile_dir"
./bin/otsandbox.sh profile verify --profile sample --store-url "$store"
```

Start the workbench:

```sh
./bin/otsandbox.sh serve \
  --profile sample \
  --store-url "$store" \
  --host 127.0.0.1 \
  --port 18191
```

Then open `http://127.0.0.1:18191/`.

## Agent Workflow

Open Test Sandbox is designed so an agent does not need hidden prompt knowledge
about target ids:

```sh
./bin/otsandbox.sh interface-node discover \
  --profile sample \
  --store-url "$store" \
  --filter "query" \
  --json

./bin/otsandbox.sh case discover \
  --profile sample \
  --store-url "$store" \
  --tag smoke \
  --status active \
  --json

./bin/otsandbox.sh case suite report \
  --profile sample \
  --store-url "$store" \
  --tag smoke \
  --status active \
  --base-url http://127.0.0.1:8080 \
  --output-dir "$tmpdir/reports/smoke-suite" \
  --json

./bin/otsandbox.sh interface-node case report \
  --node NODE_ID \
  --profile sample \
  --store-url "$store" \
  --base-url http://127.0.0.1:8080 \
  --output-dir "$tmpdir/reports" \
  --json
```

The same pattern works for workflows:

```sh
./bin/otsandbox.sh workflow discover --profile sample --store-url "$store" --json
./bin/otsandbox.sh workflow report --workflow WORKFLOW_ID --profile sample --store-url "$store" --json
```

Reports may contain failed cases. That is expected: a successful report means
the sandbox completed its job and preserved the failure details for review.

## Architecture

```text
External profile bundle
  -> audit / verify / publish
  -> SQLite Store and catalog read-models
  -> CLI discovery, Control plane APIs, React workbench
  -> case and workflow execution
  -> Evidence files plus Store indexes
  -> JSON and HTML reports
  -> detail APIs for failed case review
```

Core packages stay generic:

- `cmd/otsandbox/`: CLI entrypoint and command orchestration.
- `internal/store/`: Store contract and runtime records.
- `internal/store/sqlite/`: default local Store backend.
- `internal/profile/`: profile schema and loader.
- `internal/controlplane/`: HTTP APIs, workbench data, reports, and Evidence views.
- `internal/apicase/`: HTTP case runner and Evidence writer.
- `control-plane/frontend/`: React workbench source.
- `control-plane/static/`: built static workbench assets served by `otsandbox serve`.

## Documentation

| Page | What it covers |
| --- | --- |
| [Quick Start](docs/quickstart.md) | First local run, Store setup, profile install, and workbench launch. |
| [Backend Capabilities](docs/backend-capabilities.md) | Store, profile publishing, discovery, execution, reports, Evidence, APIs, and release guardrails. |
| [Share Kit](docs/share-kit.md) | Project tagline, short descriptions, demo script, and announcement snippets for sharing the project. |
| [Roadmap](docs/roadmap.md) | Public development themes and contribution-friendly milestones. |
| [Profile Authoring](docs/profile-authoring.md) | How to keep team-owned test assets outside the core repository. |
| [Profile Format](docs/profile-format.md) | Manifest fields, split assets, audit, install, pack, import, and verify. |
| [API Case Format](docs/api-case-format.md) | Runnable HTTP case JSON and Evidence output contract. |
| [CLI and API Contracts](docs/cli-api-contracts.md) | Agent/CI discovery, reports, asynchronous batches, and failed-case Evidence lookup. |
| [Release Checklist](docs/release-checklist.md) | Local and CI gates before publishing. |
| [Visual Overview](docs/core-capabilities-skills-goals.html) | Bilingual capability map, API surface, data flow, and goals. |

## Project Principles

- Keep the default developer experience local and lightweight.
- Use SQLite as the default Store.
- Keep business or team-specific data in external profile bundles.
- Treat Store rows as indexes and runtime facts, not source assets.
- Treat Evidence, reports, logs, and local databases as generated runtime state.
- Make agent flows discoverable before execution.
- Keep public contracts documented when CLI, API, profile, Store, or report
  shapes change.

## Status

The project is pre-1.0 but already has a complete local loop:

- profile lifecycle: init, install, pack, audit, verify, import, publish;
- Store lifecycle: status, upgrade, runtime indexes, contract tests;
- maintenance: API case metadata and searchable case catalog;
- execution: single API case, maintained case suites, interface-node reports,
  workflow reports;
- Evidence: request, response, assertions, summaries, logs, topology, timing;
- workbench: local React pages backed by Control plane APIs;
- release gate: `npm run release-check`.

Next areas are profile authoring ergonomics, stronger post-process scheduling,
optional team Store backends, and richer public examples.

## Contributing

Run the full local gate before publishing a change:

```sh
npm run release-check
```

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), and
[docs/release-checklist.md](docs/release-checklist.md). Open Test Sandbox is
licensed under the [Apache License 2.0](LICENSE).
