# Feature Research Radar

AgentTestBench CLI changes should start from a feature question, then compare
against mature open-source references before implementation.

The crawler and generated inventory live outside this repository in
`$RADAR_HOME`. AgentTestBench only consumes the
generated `data/feature-index.json`; it does not crawl GitHub or bundle the
radar data into the core project.

Set `RADAR_HOME` to the local checkout of the external radar project:

```sh
export RADAR_HOME=/path/to/github-feature-radar
```

## Refresh The Radar

```sh
cd $RADAR_HOME
npm test
npm run refresh -- --seed-only
npm run status -- --max-age-hours 72 --min-references 3
npm run audit
npm run index
```

For broader GitHub search, set `GITHUB_TOKEN`:

```sh
GITHUB_TOKEN=ghp_xxx npm run refresh -- --limit 20
```

The radar policy is:

- stars >= 3000;
- pushed within the last 3 months;
- non-archived repositories;
- non-fork repositories.

## Query From AgentTestBench

List the available feature index before choosing the next CLI slice:

```sh
./bin/agent-testbench.sh research search \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --query "quality gate" \
  --limit 5 \
  --json

./bin/agent-testbench.sh research brief \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --query "quality gate" \
  --min-references 3 \
  --format markdown

./bin/agent-testbench.sh research features \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --filter "gate" \
  --json
```

`research search` is the feature-search front door. It uses the generated
token index to rank candidate features for a query, reports matched tokens,
reference counts, top recent 3K+ star references, and a copyable
`research plan` command for each candidate. Use it when the feature wording is
still fuzzy and several maintained feature records may apply.

`research brief` is the one-shot pre-design runbook. It starts from a fuzzy
query, selects the highest-ranked feature candidate, runs the same freshness,
audit, reference, and optional command-path gates used by `research gate`, then
returns the selected references plus copyable `search`, `matrix`, `gate`, and
`plan` commands. Use it before changing a CLI capability so the implementation
starts from the maintained feature radar instead of ad hoc repository lookup.

`research features` returns the feature id, title, intent, aliases, reference
count, and ranked reference projects. Use it as the local feature-search entry
point; the crawler stays in the external radar project.

```sh
./bin/agent-testbench.sh research feature \
  --feature "case run" \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --require-min-matches 3 \
  --json
```

You can also set the index path once:

```sh
export AGENT_TESTBENCH_FEATURE_RADAR_INDEX=$RADAR_HOME/data/feature-index.json
./bin/agent-testbench.sh research features --filter "workflow"
./bin/agent-testbench.sh research feature --feature "workflow report"
```

The command returns the matched feature, policy metadata, and ranked reference
projects with stars, last push date, feature score, and evidence reasons.
`--require-min-matches N` turns the query into a design gate: the command exits
non-zero when the radar does not have enough qualifying references for the
feature.

Before picking the next CLI slice, check the whole feature index:

```sh
./bin/agent-testbench.sh research status \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --max-age-hours 72 \
  --json

./bin/agent-testbench.sh research audit \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --min-references 3 \
  --json

./bin/agent-testbench.sh research coverage \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --min-references 3 \
  --limit 3 \
  --json

./bin/agent-testbench.sh research matrix \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --filter "workflow" \
  --limit 3 \
  --json

./bin/agent-testbench.sh research refresh-plan \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --min-references 3 \
  --max-age-hours 72 \
  --limit 5 \
  --json

./bin/agent-testbench.sh research gate \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --feature "workflow report" \
  --require-min-matches 3 \
  --require-command "workflow report" \
  --max-age-hours 72 \
  --json
```

`research status` checks the generated/source timestamp before AgentTestBench
trusts the local radar index. It reports feature, ranked-reference, project
index, and cached refresh counts from the radar `refreshSummary`, fails when
the index is stale, and prints the refresh/status/audit/coverage/index commands
needed to make the external radar usable again.

`research audit` checks the local radar index for policy violations before the
CLI trusts it: references must have a GitHub name and URL, meet the configured
star floor, satisfy the pushed-after recency window, and each feature must have
the requested minimum number of references. It also audits the maintained
`projectIndex` so the de-duplicated project list cannot silently drift below
the star floor, fall outside the recency window, lose its GitHub URL, or detach
from all feature ids.

`research coverage` fails when any feature lacks the required number of recent
3K+ star references. Use it as the radar health gate before demo work,
documentation updates, or a new feature implementation so AgentTestBench keeps
searching by capability first instead of drifting back to one-off repository
name searches.

`research matrix` keeps the same feature-first entry point, then explains the
ranked references with project-index metadata: language, matched features, and
evidence reasons. Use it when a feature should be compared against mature OSS
patterns before writing the next CLI behavior or demo.

`research refresh-plan` combines freshness, audit, and coverage checks into a
maintenance plan. It tells agents whether the radar needs refresh, why, which
feature records should be expanded first, and which external radar commands
should run next. Use it before scheduled refreshes or before starting a new CLI
slice from stale radar data.

`research gate` is the pre-implementation guard for an individual CLI slice. It
loads the external feature index, verifies freshness, runs the radar audit,
checks that the matched feature has enough recent 3K+ star references, and can
require a concrete AgentTestBench command path such as `workflow report` or
`case gate`. The JSON report returns `checks`, `referenceGate`, `commandGate`,
ranked references, and verification commands; the command exits non-zero when
any gate fails.

To choose the next implementation or demo slice, ask the CLI to rank roadmap
candidates:

```sh
./bin/agent-testbench.sh research roadmap \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --min-references 3 \
  --limit 5 \
  --reference-limit 2 \
  --json
```

`research roadmap` reuses the same feature coverage gate, then ranks features
by enough references, catalog-verified next commands, implementation-facing
commands, and reference star signal. It outputs a `planCommand` for each
candidate so the next step can immediately become a reviewable `research plan`.

When the next slice should become an execution queue, use `research backlog`:

```sh
./bin/agent-testbench.sh research backlog \
  --radar-index $RADAR_HOME/data/feature-index.json \
  --min-references 3 \
  --limit 5 \
  --reference-limit 2 \
  --json
```

`research backlog` keeps the command stateless. It converts the roadmap into
prioritized tasks with a stable task id, status, top references, implementation
commands, verification commands, and acceptance criteria. Use this as the
handoff between feature search and the next AgentTestBench CLI implementation
slice.

`research feature` also returns `nextCommands`. These are AgentTestBench CLI
commands that make the matched feature actionable after the reference check:
for example `api-test-runner` points to `case run --dry-run`, `quality-gates`
points to `case gate` and `workflow gate`, and `github-radar-generation`
points back to `research features` / `research feature`. Each recommendation is
checked against the current command catalog and includes `commandPath`,
`catalogCommand`, and `available`, so stale recommendations are visible in the
same JSON payload.

For one-shot planning, use `research plan`:

```sh
./bin/agent-testbench.sh research plan \
  --feature "case run" \
  --require-min-matches 3 \
  --json
```

The plan includes the reference gate, ranked references, catalog-verified
`nextCommands`, and `verificationCommands` that can be pasted into a terminal or
used by an agent runbook.

Use Markdown when the research result should be reviewed, pasted into a design
note, or used as a demo artifact:

```sh
./bin/agent-testbench.sh research plan \
  --feature "case run" \
  --require-min-matches 3 \
  --format markdown
```

## Current Seed Features

- `cli-command-ux`
- `api-test-runner`
- `workflow-orchestration`
- `evidence-diagnosis`
- `quality-gates`
- `github-radar-generation`

When adding or redesigning an AgentTestBench CLI capability, add or refine the
feature in `github-feature-radar/data/features.json`, refresh the radar, then
use `agent-testbench research feature` to capture the references that shaped
the implementation.

Recommended pre-design gate:

```sh
./bin/agent-testbench.sh research features --filter "new cli capability"
./bin/agent-testbench.sh research search --query "new cli capability" --limit 5
./bin/agent-testbench.sh research brief --query "new cli capability" --min-references 3 --format markdown
./bin/agent-testbench.sh research status --max-age-hours 72
./bin/agent-testbench.sh research audit --min-references 3
./bin/agent-testbench.sh research coverage --min-references 3
./bin/agent-testbench.sh research matrix --filter "new cli capability" --limit 3
./bin/agent-testbench.sh research refresh-plan --min-references 3 --max-age-hours 72
./bin/agent-testbench.sh research roadmap --min-references 3 --limit 5
./bin/agent-testbench.sh research backlog --min-references 3 --limit 5
./bin/agent-testbench.sh research gate \
  --feature "new cli capability" \
  --require-min-matches 3 \
  --require-command "target command"
./bin/agent-testbench.sh research plan \
  --feature "new cli capability" \
  --require-min-matches 3 \
  --format markdown
```

Then pick one of the returned `nextCommands` with `available=true` as the
verification or demo surface for the implementation slice.

## Reference-Backed CLI Slices

### Feature Index Discovery

Radar gate:

```sh
AGENT_TESTBENCH_FEATURE_RADAR_INDEX=$RADAR_HOME/data/feature-index.json \
  ./bin/agent-testbench.sh research feature \
  --feature "github radar generation" \
  --require-min-matches 3 \
  --limit 5
```

Current qualifying references include Github-Ranking, Star History, and
Top GitHub Users under the `github-radar-generation` feature. AgentTestBench
now exposes a compact feature-catalog query:

```sh
./bin/agent-testbench.sh research features \
  --filter "quality" \
  --json
```

The command reads the external radar index, sorts and filters feature records,
and returns match counts plus ranked reference projects. It is intentionally a
consumer of `feature-index.json`, not a GitHub crawler inside the core CLI.
When you drill into a specific feature, the report includes `nextCommands` so
the research result can become a concrete AgentTestBench CLI action.
`research plan` wraps this into a single payload with verification commands for
the next implementation/demo slice. `research coverage` checks every indexed
feature against the same minimum-reference gate, which makes the feature radar
itself a reusable prerequisite for CLI roadmap work.

### Case Run Dry-Run Preflight

Radar gate:

```sh
AGENT_TESTBENCH_FEATURE_RADAR_INDEX=$RADAR_HOME/data/feature-index.json \
  ./bin/agent-testbench.sh research feature \
  --feature "case run" \
  --require-min-matches 3 \
  --limit 5
```

Current qualifying references include Karate, Playwright, Bruno, Robot
Framework, and Gauge under the `api-test-runner` feature. The AgentTestBench
CLI now exposes a matching no-side-effect preflight path for file-backed API
cases:

```sh
./bin/agent-testbench.sh case run \
  --case examples/demo-services/retail-fulfillment-mesh/create-order.json \
  --base-url http://127.0.0.1:49190 \
  --dry-run \
  --json
```

`--dry-run` loads and validates the case file, applies `--override` values,
builds the planned request URL, summarizes headers/body keys and assertions,
and reports planned Evidence location without sending HTTP, writing Evidence,
or indexing Store records.

### Case Evidence Diagnosis

Radar gate:

```sh
AGENT_TESTBENCH_FEATURE_RADAR_INDEX=$RADAR_HOME/data/feature-index.json \
  ./bin/agent-testbench.sh research feature \
  --feature "evidence diagnosis" \
  --require-min-matches 3 \
  --limit 5
```

Current qualifying references include SigNoz, SkyWalking, and Grafana under the
`evidence-diagnosis` feature. AgentTestBench now exposes a Store-first
diagnosis command for failed API case runs:

```sh
./bin/agent-testbench.sh case diagnose \
  --store local-personal \
  --case-run RUN_ID.case \
  --json
```

`case diagnose` reads the selected case Evidence, parses assertion and response
artifacts when they are present, classifies the failure, emits the primary
finding, exposes compact signals such as `http.status` and
`assertion.error_count`, and suggests the next reproducible CLI action.

### Case Quality Gate

Radar gate:

```sh
AGENT_TESTBENCH_FEATURE_RADAR_INDEX=$RADAR_HOME/data/feature-index.json \
  ./bin/agent-testbench.sh research feature \
  --feature "quality gate" \
  --require-min-matches 3 \
  --limit 5
```

Current qualifying references include Trivy, Semgrep, and OpenSSF Scorecard
under the `quality-gates` feature. AgentTestBench now exposes a CI-oriented
case gate:

```sh
./bin/agent-testbench.sh case gate \
  --store local-personal \
  --run RUN_ID \
  --require-no-failures \
  --require-evidence \
  --min-passed 3 \
  --json
```

The command reads Store case-run facts and Evidence indexes, reports counts,
failed case runs, missing Evidence, gate booleans, and next actions, then exits
non-zero when the selected gate fails. This gives pipelines a hard fail signal
without losing the structured diagnosis payload in logs.

### Workflow Orchestration Gate

Radar gate:

```sh
AGENT_TESTBENCH_FEATURE_RADAR_INDEX=$RADAR_HOME/data/feature-index.json \
  ./bin/agent-testbench.sh research feature \
  --feature "workflow orchestration" \
  --require-min-matches 3 \
  --limit 5
```

Current qualifying references include n8n, Airflow, Prefect, FastGPT, and
Activepieces under the `workflow-orchestration` feature. AgentTestBench now
exposes a workflow-level Store gate:

```sh
./bin/agent-testbench.sh workflow gate \
  --store local-personal \
  --run RUN_ID \
  --require-passed \
  --require-steps \
  --require-evidence \
  --json
```

The command reads a persisted workflow run, its summary steps, linked case
runs, and indexed Evidence. It reports run status, step pass/fail counts,
case-run counts, Evidence completeness, failed steps, missing Evidence, and
next actions such as `workflow step` and `case diagnose`, then exits non-zero
when the selected orchestration gate fails.

### Command Catalog UX

Radar gate:

```sh
AGENT_TESTBENCH_FEATURE_RADAR_INDEX=$RADAR_HOME/data/feature-index.json \
  ./bin/agent-testbench.sh research feature \
  --feature "cli command ux" \
  --require-min-matches 3 \
  --limit 5
```

Current qualifying references include yt-dlp, Gemini CLI, fzf, winget-cli, and
immich-go under the `cli-command-ux` feature. AgentTestBench now exposes a
machine-readable command catalog:

```sh
./bin/agent-testbench.sh commands --filter "gate" --json
```

The command parses the same Usage source as `agent-testbench help`, so command
catalog output stays aligned with the human help screen. It emits command path,
area, usage, Store awareness, and tags, making it easier for agents and local
automation to discover the right command before planning a run.
