# PostgreSQL Store-first Progress Ledger

This ledger is the source for progress answers on the PostgreSQL Store-first
mainline. When the user asks for progress, update this file first with the
current evidence, then answer from the ledger instead of estimating from memory.

## Progress Check Rule

- Ask a subagent to independently verify the current state when available.
- Inspect the current worktree, recent commits, and release gates before
  reporting a percentage.
- Record completed evidence, incomplete work, and risks in this file.
- Do not count compatibility-only behavior as completed PostgreSQL mainline
  behavior.
- Do not mark the goal complete until release-check and the independent
  validation workflow prove the full objective.

## 2026-05-19 Check

Estimated PostgreSQL mainline progress: 72%.

Completed evidence:

- PostgreSQL Store backend, migrations, and release smoke are active in the
  release gate.
- API case demo, control-plane smoke, and frontend smoke require PostgreSQL
  configuration for the main path.
- Store config, active Store selection, and `--store NAME_OR_DSN` are available
  for daily CLI commands.
- Current slice removes `--store-url PATH` from top-level help so daily command
  discovery points at the Store-first flag.
- Subagent verification confirmed the PostgreSQL backend, release gate DSN
  requirement, active Store CLI smoke, API demo Store selection, UI smoke Store
  selection, and SQLite disable switch are present.
- Main-thread verification ran the full PostgreSQL release gate with
  `OTSANDBOX_SMOKE_STORE_DSN` and `OTSANDBOX_DISABLE_SQLITE_STORE=1`; it passed.

Incomplete work:

- Several CLI subcommands still keep `--store-url` as a compatibility flag.
- Product entrypoints still need stronger proof that PostgreSQL mode cannot
  touch SQLite implicitly.
- Environment catalog, one-command bootstrap, verified environment discovery,
  and local Docker start orchestration are not complete.
- Core 10-step button-level smoke still needs full Evidence and real
  SkyWalking topology assertions.
- CLI/API parity matrix is still incomplete.

Risk:

- The PostgreSQL database layer is mostly in place, but product-level proof is
  not complete until all daily CLI/API/UI paths are checked under PostgreSQL
  with SQLite disabled.

## 2026-05-19 Store Opener Closure

Completed evidence:

- `internal/store/open` now rejects empty references and plain file paths; the
  daily opener requires an explicit backend scheme such as `postgres://`,
  `postgresql://`, or `sqlite://`.
- Deprecated `--store-url PATH` compatibility is normalized at the CLI boundary
  into an explicit `sqlite://PATH` reference, so SQLite compatibility is visible
  before the shared opener sees it.
- Targeted tests passed for Store reference resolution and the shared opener.
- Full PostgreSQL release gate passed with
  `OTSANDBOX_SMOKE_STORE_DSN=postgres://zlh@127.0.0.1:5432/otsandbox_release_pg_smoke?sslmode=disable`.

Remaining risk:

- CLI tests still use SQLite compatibility broadly; future slices should migrate
  daily-path tests to named PostgreSQL Stores and keep SQLite tests scoped to
  migration or compatibility behavior.

Reference pattern:

- Mature Go/open-source products such as Gitea and Grafana expose the database
  engine as explicit configuration and keep engine-specific settings behind
  that selected type. Open Test Sandbox should follow the same direction:
  explicit Store selection, no implicit engine substitution in the active path.

## 2026-05-19 Active SQLite Daily Gate Check

Estimated PostgreSQL mainline progress: 80%.

Completed evidence:

- Subagent review independently estimated the goal at about 78%, or about 80%
  when the current uncommitted Store validation slice is counted.
- Current branch is `test`; HEAD remains `c71850c` and the active local slice
  modifies `cmd/otsandbox/main.go`, `cmd/otsandbox/main_test.go`, and
  `cmd/otsandbox/store_config.go`.
- Current local slice adds `resolveRequiredDailyStoreReference`, rejects
  active or named SQLite Store configs for selected daily commands, and keeps
  direct `sqlite://` references available only for explicit compatibility
  paths.
- Targeted tests passed for the active/named SQLite rejection and adjacent CLI
  behavior.
- The required exact-word guardrail scan passed with no matches:
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.
- Full release gate passed in the main thread using an isolated temporary
  PostgreSQL instance:
  `OTSANDBOX_CONFIG_HOME=/tmp/... OTSANDBOX_SMOKE_STORE_DSN=postgres://zlh@127.0.0.1:55432/otsandbox_release_pg_smoke?sslmode=disable npm run release-check`.
- Release gate evidence included Go tests, API case demo, React build, frontend
  model tests, smoke harness tests, PostgreSQL active Store CLI smoke, and
  PostgreSQL-only browser smoke.
- Smoke harness evidence now covers the core 10-step Store-backed workflow
  shape and Store-backed Evidence plus SkyWalking topology assertions.

Incomplete work:

- Only selected daily commands have been switched to
  `resolveRequiredDailyStoreReference`; more CLI/API daily paths still use the
  generic compatibility resolver.
- `--store-url` remains as a compatibility flag in several commands and still
  needs tighter scoping so bare paths cannot look like a normal daily path.
- Many CLI tests still exercise SQLite for product-like behavior; future slices
  should move daily-path coverage to named PostgreSQL Stores and leave SQLite
  tests for migration/compatibility.
- Environment Catalog has CLI/API pieces, but one-command bootstrap, local
  Docker orchestration, verified discovery proof, CLI/API parity, Evidence and
  report model polish, and release preparation remain incomplete.

Risk:

- The main risk is breadth: the PostgreSQL Store-first backbone and smoke gates
  are working, but every daily command family still needs either PostgreSQL
  proof or explicit compatibility labeling.
- The current percentage assumes this daily Store validation slice remains
  valid as later command families adopt the same rule.

## 2026-05-19 Environment Catalog Daily Gate

Estimated PostgreSQL mainline progress: 81%.

Completed evidence:

- Environment Catalog CLI commands now share the daily Store resolver through
  `openRequiredCLIStore`, so active or named SQLite Store configs are rejected
  before environment registration, discovery, inspection, bootstrap,
  verification, or verified publication.
- Explicit `--store sqlite://...` remains available for compatibility tests,
  while active/named Store daily usage must resolve to PostgreSQL.
- Targeted environment and sandbox CLI tests passed after the resolver change.
- Release-check exposed a loaded-test timing issue in cached workflow runtime
  log Evidence; the cache lookup now has a covered slower-read path so existing
  runtime log Evidence is used instead of showing pending log collection.
- The exact-word guardrail scan and `git diff --check` passed.
- Full release gate passed with an isolated temporary PostgreSQL instance:
  `OTSANDBOX_CONFIG_HOME=/tmp/... OTSANDBOX_SMOKE_STORE_DSN=postgres://zlh@127.0.0.1:55434/otsandbox_release_pg_smoke?sslmode=disable npm run release-check`.

Incomplete work:

- Several other daily CLI command families still use generic compatibility
  Store resolution and need the same active/named SQLite rejection.
- Daily-path tests still need broader migration from explicit SQLite stores to
  named PostgreSQL stores.

## 2026-05-19 Evidence Read Daily Gate

Estimated PostgreSQL mainline progress: 82%.

Completed evidence:

- `evidence list` and `evidence tasks` now use the daily Store resolver, so
  active or named SQLite Store configs are rejected before daily Evidence
  reads.
- `evidence import` remains on the compatibility resolver because it is a
  legacy runtime import path.
- Targeted CLI tests passed for Evidence read rejection, Evidence list/tasks
  compatibility reads, and adjacent profile/config publish commands.

Incomplete work:

- Case, workflow, baseline, template, executor, and interface-node daily query
  families still need the same Store resolver audit.
- `--store-url PATH` is still accepted by compatibility paths and needs command
  family scoping before it can no longer look like a daily workflow.

## 2026-05-19 Workflow Run Read Daily Gate

Estimated PostgreSQL mainline progress: 83%.

Completed evidence:

- `workflow runs`, `workflow run`, `workflow step`, and
  `workflow latest-step` now use the daily Store resolver, so active or named
  SQLite Store configs are rejected before workflow execution results are read.
- Explicit `--store sqlite://...` compatibility coverage remains green for
  stored workflow run reads.
- Targeted workflow, environment, and sandbox CLI tests passed after the
  resolver change.

Incomplete work:

- Workflow discover/plan/audit/report, case suite/read commands, baseline,
  template, executor, trace topology collection, and interface-node query
  families still need Store resolver audits.

## 2026-05-19 Case Read Daily Gate

Estimated PostgreSQL mainline progress: 84%.

Completed evidence:

- `case runs`, `case evidence`, and `case timing` now use the daily Store
  resolver, so active or named SQLite Store configs are rejected before case
  execution results, Evidence, or timing summaries are read.
- Explicit `--store sqlite://...` compatibility coverage remains green for
  case run, case Evidence, and case timing reads.
- Targeted case read CLI tests passed after the resolver change.
- The exact-word guardrail scan and `git diff --check` passed.

Incomplete work:

- Case discover and case suite commands still need Store resolver audits.
- Workflow discover/plan/audit/report, baseline, template, executor, trace
  topology collection, and interface-node query families remain to be checked.

## 2026-05-19 Independent Progress Recheck

Estimated PostgreSQL mainline progress: 84%.

Completed evidence:

- Independent subagent review confirmed the branch is `test`, the worktree is
  clean, and the local branch is ahead of `origin/test` by five commits.
- The latest local commits are the daily Store gates for active/named SQLite
  rejection across selected daily command families: environment catalog,
  Evidence reads, workflow run reads, and case reads.
- The exact-word guardrail scan has no matches in the current worktree.
- PostgreSQL release and smoke wiring remains present: release-check requires a
  PostgreSQL smoke DSN, and the active Store CLI plus browser smoke paths run
  with SQLite disabled.

Incomplete work:

- Several daily or near-daily command families still use generic Store
  resolution and need explicit resolver classification: sandbox interface/start,
  executor plan, trace topology collect, profile catalog/verify/import/config
  publish, interface-node discover/coverage/report, workflow
  discover/plan/audit/report, baseline get/set, template render, case discover,
  case suite, case incomplete-batches, and serve.
- Current HEAD after the latest five local commits has targeted test evidence,
  but not a fresh full release-check record. Re-run an isolated PostgreSQL
  release-check before treating this state as release-ready.

Risk:

- The headline progress remains 84% because the PostgreSQL-first backbone is in
  place and many daily read paths are now gated, but breadth and full-release
  proof are still incomplete.
