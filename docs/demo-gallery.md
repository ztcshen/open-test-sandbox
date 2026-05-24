# Demo Gallery

AgentTestBench needs demos that show the product instead of only describing it.
This gallery gives maintainers, conference talks, README links, and social
posts a neutral way to explain the CLI surface, Evidence model, and Store-first
workflow.

The design follows a pattern used by mature developer-tool projects: keep a
small runnable target, show the exact command sequence, and make the produced
proof visible. Newman makes CLI execution and reports easy to demo; Backstage
uses demo catalog data to make its portal concepts visible; Temporal uses
sample workflows to teach orchestration through runnable examples. AgentTestBench
should combine those ideas around API cases, workflows, Evidence, and SQL
Stores.

Design references:

- [Newman CLI docs](https://learning.postman.com/docs/reference/newman-cli/command-line-integration-with-newman):
  CLI-first API test execution, reporters, and CI integration.
- [Backstage standalone installation](https://backstage.io/docs/getting-started/):
  local demo content that makes catalog concepts visible.
- [Temporal TypeScript samples](https://github.com/temporalio/samples-typescript):
  runnable workflow examples for teaching orchestration.

## Visual Demo Page

Open the static gallery from a running workbench server:

```sh
./bin/agent-testbench.sh serve --store local-personal
# then open:
# http://127.0.0.1:58663/demo-gallery.html
```

The page shows:

- a CLI capability map from Store setup through Evidence review;
- an autoplay CLI automation animation that restores demo services, prioritizes
  risky cases, runs the selected API case, imports Evidence tasks, identifies a
  Root cause, and publishes a quality report;
- a multi-step Evidence timeline;
- three neutral demo service scenarios;
- a short command tour that visitors can map to the CLI help output.

## Start Demo Services

```sh
node tools/examples/demo-service-server.mjs --port 49190
curl -s http://127.0.0.1:49190/scenarios
```

The service catalog lives in
[`../examples/demo-services/catalog.json`](../examples/demo-services/catalog.json).

## CLI Walkthrough

The demo services are intentionally lightweight. They are not a full product
runtime; they are a visible target that explains how AgentTestBench thinks.
The installed wrapper is `agent-testbench`; from this checkout, `./bin/agent-testbench.sh`
uses the same command shape, for example `agent-testbench case run` and
`agent-testbench workflow report`.

The gallery animation compresses the strongest CLI path into one reviewable
loop:

```sh
agent-testbench environment restore retail-demo --store local-personal --workspace .demo/runtime --execute --json
agent-testbench case suite priority --filter retail --changed-service payment --store local-personal --json
agent-testbench case run --case examples/demo-services/retail-fulfillment-mesh/create-order.json --base-url http://127.0.0.1:49190 --store local-personal --evidence-dir .agent-testbench/evidence --json
agent-testbench workflow report --workflow workflow.retail.fulfillment --base-url http://127.0.0.1:49190 --store local-personal --html --json
agent-testbench evidence tasks --run retail-demo-run --failed-only --store local-personal --json
agent-testbench case suite quality-report --filter retail --run retail-demo-run --store local-personal --format html --json
```

That sequence is meant to be shown as an animated runbook: restore the target,
choose the highest-signal tests, run the case, preserve request/response/assertion
Evidence, diagnose the failing edge, and hand reviewers a reproducible quality
report.

```sh
./bin/agent-testbench.sh store config set local-personal \
  --url "postgres://user:pass@host:5432/agent_testbench_demo?sslmode=disable"
./bin/agent-testbench.sh store use local-personal
./bin/agent-testbench.sh store upgrade --store local-personal

./bin/agent-testbench.sh environment register \
  --id retail-demo \
  --display-name "Retail Fulfillment Mesh" \
  --service checkout \
  --health-url http://127.0.0.1:49190/health \
  --json

./bin/agent-testbench.sh case discover --filter retail --json
./bin/agent-testbench.sh case run \
  --case examples/demo-services/retail-fulfillment-mesh/create-order.json \
  --base-url http://127.0.0.1:49190 \
  --json
./bin/agent-testbench.sh workflow report \
  --workflow workflow.retail.fulfillment \
  --base-url http://127.0.0.1:49190 \
  --json
./bin/agent-testbench.sh evidence list --json
```

## Showcase Scenarios

| Scenario | What it demonstrates | Why it helps exposure |
| --- | --- | --- |
| Retail Fulfillment Mesh | Multi-service checkout, payment, risk, warehouse, and carrier proof. | Familiar enough for most backend teams; complex enough to justify workflow Evidence. |
| IoT Telemetry Control | Streaming-style ingest, anomaly scoring, rules, command dispatch, and audit. | Shows AgentTestBench is not limited to CRUD APIs. |
| Content Moderation Pipeline | Policy checks, model scoring, queue escalation, notification, and appeal readiness. | Connects testing with AI-adjacent review workflows without private data. |

## Exposure Plan

1. Put `demo-gallery.html` in screenshots, release notes, and the GitHub profile
   repository.
2. Keep `docs/share-kit.md` focused on copy/paste announcement material.
3. Use the demo service catalog as the public replacement for private business
   examples.
4. Record the CLI automation animation as the primary short video: one loop
   should show `environment restore`, `case suite priority`, `case run`,
   `workflow report`, `evidence tasks`, and `case suite quality-report`.
5. Keep the examples runnable with SQLite compatibility for local exploration,
   but present PostgreSQL/MySQL as the team Store path.
