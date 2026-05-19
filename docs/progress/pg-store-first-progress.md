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

## 2026-05-19 Subagent Progress Check

Estimated PostgreSQL mainline progress: 84%.

Completed evidence:

- Independent subagent review confirmed the current branch is `test`, the
  worktree is clean, and the branch is ahead of `origin/test` by six commits.
- The subagent agreed with the 84% headline estimate, with a working range of
  83-85% and 84% as the optimistic center.
- PostgreSQL Store-first wiring is established: the release gate requires a
  PostgreSQL smoke DSN, and the smoke/demo paths include PostgreSQL active
  Store coverage with SQLite disabled.
- Recent local commits have gated active or named SQLite Store configs across
  environment catalog, Evidence reads, workflow run reads, and case reads.
- The exact-word guardrail scan has no matches in the current worktree.

Incomplete work:

- More daily or near-daily command families still need explicit resolver
  classification or PostgreSQL proof: sandbox interface/start, executor plan,
  trace topology collect, profile catalog/verify/import/config publish,
  interface-node discover/coverage/report, workflow discover/plan/audit/report,
  baseline get/set, template render, case discover, case suite,
  case incomplete-batches, and serve.
- Latest HEAD still needs a fresh isolated PostgreSQL `npm run release-check`
  before this state can be treated as release-ready.
- Test coverage still contains broad explicit SQLite compatibility cases, so
  daily-path tests need continued migration to named PostgreSQL Stores.

Risk:

- The project is past the architecture backbone stage, but not yet in final
  release hardening. Remaining work is mostly breadth, parity, and proof:
  finishing daily command gating, validating the core 10-step workflow end to
  end, and proving real Evidence plus SkyWalking topology behavior.

## 2026-05-19 Broad Daily Resolver Gate

Estimated PostgreSQL mainline progress: 90%.

Completed evidence:

- `case discover`, `interface-node discover`, and `workflow discover` now share
  the daily Store resolver, so active or named SQLite Store configs are rejected
  before daily discovery reads.
- The same daily resolver is now applied to the remaining clearly daily or
  near-daily command families: sandbox interface registration, sandbox start,
  executor plan, trace topology collection, profile catalog/verify/config
  publish, interface-node case/report loading, workflow report, non-offline
  workflow audit, baseline get/set, template render, case suite report, case
  incomplete batch inspection, and `serve`.
- Remaining generic Store resolution in `cmd/otsandbox` is intentionally scoped
  to Store management commands, offline template package audit helpers, and
  `evidence import` as a legacy runtime migration path.
- Daily commands now reject legacy `--store-url` values that resolve to SQLite,
  including bare local paths. PostgreSQL DSNs can still be resolved, while
  SQLite target Stores remain available only through compatibility/migration
  paths such as `evidence import`.
- CLI flag help now labels `--store-url` as deprecated compatibility usage and
  states that daily commands reject SQLite paths, while command examples
  continue to use `--store NAME_OR_DSN`.
- CLI tests no longer exercise SQLite bare paths through `--store-url` for
  daily command setup or reads. Remaining `--store-url` test references are
  negative assertions or explicit PostgreSQL/legacy compatibility checks.
- An environment-gated named PostgreSQL daily-path test is now available for
  human-machine validation with `OTSANDBOX_TEST_PG_DSN`; it configures an active
  named PostgreSQL Store, publishes config, and runs daily discovery without
  per-command Store flags.
- Evidence topology views now trust saved topology summaries only when they
  explicitly identify SkyWalking as provider/source; otherwise they return the
  unavailable SkyWalking view instead of exposing workflow order or legacy
  summaries as real topology.
- Evidence viewer smoke fixtures now use explicit SkyWalking complete topology
  payloads instead of providerless partial topology examples.
- Frontend Evidence timeline model tests now use the same explicit SkyWalking
  complete topology fixture shape.
- Explicit `--store sqlite://...` compatibility coverage remains green for
  discovery reads, and offline template package review remains available only
  through `--offline-template-package`.
- TDD evidence captured the behavior gap first:
  `go test ./cmd/otsandbox -run TestDiscoverCommandsRejectActiveSQLiteStore -count=1`
  failed because all three discovery commands succeeded against active SQLite.
- Targeted discovery tests passed after the resolver change:
  `go test ./cmd/otsandbox -run 'Test(DiscoverCommandsRejectActiveSQLiteStore|DiscoverCommandsAcceptStoreFlagAsLocationAgnosticStoreSelector|CaseDiscoverRequiresStoreUnlessOfflineTemplatePackage|InterfaceNodeDiscoverRequiresStoreUnlessOfflineTemplatePackage|WorkflowDiscoverRequiresStoreUnlessOfflineTemplatePackage|CaseDiscoverFiltersByMaintenanceMetadata)' -count=1`.
- A release-check attempt exposed a control-plane async batch test timing issue:
  one test used a local 2 second poll while neighboring batch tests used the
  shared 10 second wait helper. That test now uses the shared helper, and its
  targeted control-plane test passed.
- Following the user's direction to avoid blocking on heavy testing, this slice
  used static/light verification only after the broad resolver sweep:
  `git diff --check` passed and the exact-word guardrail scan had no matches.

Incomplete work:

- `--store-url` still exists as a deprecated compatibility flag on many command
  surfaces for migration and explicit compatibility; daily product examples
  should continue steering users to `--store NAME_OR_DSN`.
- Daily-path test data still needs migration from explicit SQLite stores to
  named PostgreSQL Stores beyond the new env-gated discovery coverage. The
  current test cleanup first moves old `--store-url PATH` calls to explicit
  `--store sqlite://...` compatibility form so the daily path no longer
  normalizes SQLite bare paths.
- The core 10-step smoke, per-interface Evidence completeness, and real
  SkyWalking topology proof still need final human-machine validation against a
  real SkyWalking endpoint.
- Latest HEAD is not release-ready. The latest full release-check attempt
  reached the `cmd/otsandbox` package but hit Go's default 10 minute package
  timeout while the suite was still progressing; full validation is deferred by
  user direction.

## 2026-05-19 Frontend Topology Trust Gate

Estimated PostgreSQL mainline progress: 93%.

Completed evidence:

- Evidence Viewer and Interface Node UI now reuse the same SkyWalking trust
  check used by Workflow Step. Providerless or non-SkyWalking topology payloads
  are rendered as unavailable SkyWalking topology instead of being displayed as
  real call graph evidence.
- Evidence timeline modeling now counts topology as a timeline item only when
  the payload explicitly identifies SkyWalking through `provider` or `source`.
- The API case documentation now describes daily Store indexing through the
  active Store or `--store NAME_OR_DSN`; deprecated `--store-url` is documented
  as migration and legacy compatibility only.
- Light validation passed:
  `node --test control-plane/frontend/src/evidenceTimelineModel.test.mjs control-plane/frontend/src/workflowStepModel.test.mjs`,
  `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- This is still not a 98% state because the core 10-step button-level smoke has
  not been re-run against a real PostgreSQL Store plus real SkyWalking endpoint
  in this slice.
- Broader CLI/API parity and daily-path test migration to named PostgreSQL
  Stores remain open. Existing explicit `--store sqlite://...` tests are now
  compatibility coverage, not proof of the PostgreSQL daily path.
- Full `npm run release-check` remains intentionally deferred by user direction
  to keep momentum on the PG line instead of blocking on the heavy suite.

## 2026-05-19 Smoke Topology Strictness Gate

Estimated PostgreSQL mainline progress: 94%.

Completed evidence:

- A background subagent independently confirmed that `--store-url
  .runtime/store.sqlite` is no longer promoted as a daily example, and flagged
  the remaining high-value gap as smoke topology strictness rather than Store
  routing.
- Control-plane smoke Evidence assertions now reject empty SkyWalking topology
  edges. A topology must include `provider: "skywalking"`, `status:
  "complete"`, the expected trace id, both observed service nodes, and a
  confirmed `service.alpha -> service.worker` edge.
- The core 10-step browser workflow smoke now validates every persisted
  topology row with the same complete SkyWalking evidence rule, not just
  presence of a provider-labeled row.
- CLI active PostgreSQL Store smoke now uses the same topology evidence rule
  after each `trace topology collect`.
- Workflow-step evidence smoke now checks the page renders the complete status,
  trace id, and source/target services, not only that a topology SVG node
  exists.
- Store-first guardrails now reject topology fixtures that set a topology
  status before declaring provider/source, preventing providerless complete
  topology examples from re-entering smoke or docs.
- Light validation passed:
  `node --test tools/smoke/control-plane-smoke.test.mjs`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The smoke provider remains synthetic for local deterministic smoke. It proves
  PostgreSQL Store wiring, Evidence indexing, UI rendering, and topology
  persistence semantics, but the final real SkyWalking endpoint validation is
  still open.
- The main remaining PG-line work is broad named PostgreSQL daily-path test
  migration and env-gated real SkyWalking validation. Full release-check is
  still deferred by user direction.

## 2026-05-19 Real SkyWalking Smoke Hook

Estimated PostgreSQL mainline progress: 95%.

Completed evidence:

- Browser and CLI PostgreSQL smoke now share one trace provider selector:
  `OTS_TRACE_GRAPHQL_URL` uses an external real SkyWalking GraphQL endpoint;
  otherwise the smoke starts the deterministic synthetic provider.
- The 10-step smoke supports per-step real trace ids through
  `OTS_SMOKE_TRACE_IDS`, either as JSON such as
  `{"step-01":"trace-real-01"}` or comma-separated `step-01=trace-real-01`
  mappings.
- Public docs and release checklist now state the boundary clearly: default
  smoke proves PostgreSQL Store wiring, Evidence indexing, topology persistence,
  and UI behavior with a synthetic provider; real SkyWalking proof requires
  `OTS_TRACE_GRAPHQL_URL`.
- `tools/release-check.sh` now prints a SkyWalking provider notice when the
  real GraphQL URL is absent, without blocking the lightweight local release
  gate.

Incomplete work:

- Real SkyWalking validation still requires a live endpoint and trace ids from
  the target environment. This slice adds the hook and documentation, but does
  not execute the external endpoint validation.
- Named PostgreSQL daily-path test migration remains broad and incomplete
  beyond the current env-gated discovery coverage.

## 2026-05-19 Named PostgreSQL Workflow Daily Coverage

Estimated PostgreSQL mainline progress: 96%.

Completed evidence:

- Added env-gated named PostgreSQL coverage for a daily workflow path behind
  `OTSANDBOX_TEST_PG_DSN`. The test configures an active named PostgreSQL
  Store, upgrades it, publishes workflow config, and then runs daily commands
  without per-command `--store` flags.
- The covered no-flag daily commands now include workflow discovery, workflow
  planning, baseline set/get, workflow report execution, case run listing, trace
  topology collection, and case Evidence lookup against the active named
  PostgreSQL Store.
- The new workflow daily coverage also validates SkyWalking topology persistence
  through the CLI path by collecting topology for the PostgreSQL-backed workflow
  run and then reading it back through case Evidence.
- The previous named PostgreSQL discovery coverage now shares the same helper
  for active Store setup, keeping PG daily-path tests consistent.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(DiscoverCommandsUseNamedPostgreSQLActiveStore|DailyWorkflowCommandsUseNamedPostgreSQLActiveStore)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The new coverage is env-gated and skipped without `OTSANDBOX_TEST_PG_DSN`; it
  does not replace a full release-check or live SkyWalking validation.
- Many product-like CLI tests still use explicit `--store sqlite://...` as
  compatibility coverage. More daily-path suites should move to named
  PostgreSQL Store coverage over time.

## 2026-05-19 Named PostgreSQL Environment Gate Coverage

Estimated PostgreSQL mainline progress: 97%.

Completed evidence:

- Added env-gated named PostgreSQL coverage for the Environment Catalog
  verified discovery lifecycle behind `OTSANDBOX_TEST_PG_DSN`.
- The new test configures an active named PostgreSQL Store and runs the daily
  Environment Catalog chain without per-command `--store`: register, default
  discover exclusion for unverified environments, publish denial before complete
  verification, verify with complete Evidence and topology flags,
  publish-verified, verified discovery, and bootstrap plan retrieval.
- This directly covers the product rule that verified discovery requires a
  passed acceptance workflow plus complete Evidence and SkyWalking topology,
  while using the same local/remote PostgreSQL command shape.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(EnvironmentCommandsUseNamedPostgreSQLActiveStore|DailyWorkflowCommandsUseNamedPostgreSQLActiveStore|DiscoverCommandsUseNamedPostgreSQLActiveStore)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The environment PG coverage is env-gated and skipped without
  `OTSANDBOX_TEST_PG_DSN`; it does not replace full release-check or real
  SkyWalking endpoint validation.
- Remaining product-like SQLite tests are now mostly broader case-suite,
  case-execution/interface-node report, Evidence import/list/tasks, profile
  import/verify, and serve/UI handler coverage.

## 2026-05-19 Named PostgreSQL Case Suite Coverage

Estimated PostgreSQL mainline progress: 97.5%.

Completed evidence:

- Added env-gated named PostgreSQL coverage for the case-suite daily command
  family behind `OTSANDBOX_TEST_PG_DSN`.
- The new test configures an active named PostgreSQL Store, publishes maintained
  case metadata, and runs daily case-suite commands without per-command
  `--store` flags.
- Covered commands now include `case suite report` for the selected positive
  and derived negative cases, `case suite coverage`, `case suite priority`, and
  `case suite brief`.
- The coverage proves case-suite execution writes PostgreSQL-backed case runs
  and reports, and that subsequent suite read/selection commands consume the
  active PostgreSQL Store state with the same CLI shape.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(CaseSuiteCommandsUseNamedPostgreSQLActiveStore|EnvironmentCommandsUseNamedPostgreSQLActiveStore|DailyWorkflowCommandsUseNamedPostgreSQLActiveStore)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The case-suite PG coverage is env-gated and skipped without
  `OTSANDBOX_TEST_PG_DSN`; it does not replace a full release-check.
- Remaining PG-line gaps are now concentrated around live SkyWalking endpoint
  validation, case execution and interface-node report coverage, Evidence
  import/list/tasks, profile import/verify, and serve/UI handler coverage.

## 2026-05-19 Named PostgreSQL Case Execution Coverage

Estimated PostgreSQL mainline progress: 98%.

Completed evidence:

- Added env-gated named PostgreSQL coverage for direct case execution and
  interface-node case reporting behind `OTSANDBOX_TEST_PG_DSN`.
- The new test configures an active named PostgreSQL Store and then runs daily
  commands without per-command `--store` flags.
- Covered commands now include file-based `case run`, `case runs`,
  `case evidence`, `evidence list`, catalog-backed `case run --case-id`,
  `interface-node discover`, and `interface-node case report`.
- The coverage checks both file and Store-catalog case execution paths write
  PostgreSQL-backed run and Evidence records, and that interface-node reporting
  uses the active Store without creating `runtime.sqlite`.
- The interface-node report path also keeps the existing report hygiene checks:
  derived cases run, all cases pass, detail handles are present, and sensitive
  response fields are redacted in previews.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(CaseExecutionAndInterfaceReportUseNamedPostgreSQLActiveStore|CaseSuiteCommandsUseNamedPostgreSQLActiveStore|EnvironmentCommandsUseNamedPostgreSQLActiveStore)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The new case execution coverage is env-gated and skipped without
  `OTSANDBOX_TEST_PG_DSN`; it does not replace full release-check or the later
  human-machine database validation pass.
- Live SkyWalking endpoint validation with real trace ids remains open.
- Remaining 98% to final-release gaps are now mostly Evidence import/tasks,
  profile import/verify, serve/UI handler coverage, CLI/API parity, and release
  preparation rather than core daily PG command shape.

## 2026-05-19 Environment Publish Verification Gate

Estimated PostgreSQL mainline progress: 98%.

Completed evidence:

- `environment publish-verified` now performs an actual selected-Store
  inspection before promotion instead of trusting only previously recorded
  completeness flags.
- CLI and API publish paths share `ValidateEnvironmentPublishable`, which
  requires a passed recorded verification status, a non-empty verification run
  id, a matching passed run row in Store, at least one indexed Evidence record,
  and a complete SkyWalking topology row with provider/source identity, trace
  id, observed nodes, and confirmed edges.
- `environment verify` is documented as a recording step for the result of an
  already-run acceptance workflow. `publish-verified` is documented as the
  Store inspection gate that checks the persisted run, Evidence, and topology
  artifacts.
- Quickstart Environment Catalog commands now use the actual positional
  `ENV_ID` CLI shape and publish from the same Store that contains the verified
  run artifacts.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(EnvironmentCommandsGateVerifiedDiscovery|EnvironmentCommandsUseNamedPostgreSQLActiveStore|CaseExecutionAndInterfaceReportUseNamedPostgreSQLActiveStore)$' -count=1`,
  `go test ./internal/controlplane -run 'TestServerManagesVerifiedEnvironmentCatalogFromStore|TestTraceTopologyCollectPersistsProviderSpanRefs|TestTopologyEvidenceViewForCaseListsOnlySkyWalkingRows$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Reference pattern:

- Mature control planes separate recorded status from observed state. The
  [Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
  and [Argo CD health status model](https://argo-cd.readthedocs.io/en/stable/operator-manual/health/)
  both keep current health/sync state as explicit controller-observed status
  rather than treating requested input as proof. Open Test Sandbox now follows
  the same direction for verified environment publication.

Incomplete work:

- The gate checks persisted artifacts in the selected Store, but live
  SkyWalking endpoint validation with real trace ids still needs the later
  human-machine validation pass.
- Full release-check remains deferred by user direction while the PG line is
  being advanced quickly.

## 2026-05-19 Named PostgreSQL Evidence And Serve Coverage

Estimated PostgreSQL mainline progress: 98.2%.

Completed evidence:

- Added env-gated named PostgreSQL coverage for Evidence read and serve/UI API
  paths behind `OTSANDBOX_TEST_PG_DSN`.
- The new test configures an active named PostgreSQL Store and then runs
  `evidence list` and `evidence tasks` without per-command `--store` flags,
  proving both read paths consume active PostgreSQL run, Evidence, and
  post-process task records.
- The same test builds the `serve` handler without `--store`, then checks
  `/api/store/current`, `/api/runs`, and `/api/interface-nodes` use the active
  named PostgreSQL Store and published Store catalog rather than a local SQLite
  runtime.
- The post-process task fixture is now reusable across SQLite compatibility and
  named PostgreSQL active Store tests, keeping the daily-path proof and
  compatibility proof on the same data shape.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(ServeAndEvidenceTasksUseNamedPostgreSQLActiveStore|EvidenceTasksCommandListsPostProcessTasks|EvidenceListCommandCanEmitJSON)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The new coverage is env-gated and skipped without `OTSANDBOX_TEST_PG_DSN`; it
  does not replace the later human-machine PostgreSQL validation pass.
- Remaining PG-line gaps are now mostly `evidence import` as an explicit legacy
  migration path, profile import/verify active PostgreSQL proof, serve API
  profile import/verify through the running handler, CLI/API parity polish, and
  live SkyWalking endpoint validation with real trace ids.
