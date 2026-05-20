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

## 2026-05-19 Named PostgreSQL Profile Import Verify Coverage

Estimated PostgreSQL mainline progress: 98.4%.

Completed evidence:

- Added env-gated named PostgreSQL coverage for `profile import` and
  `profile verify` behind `OTSANDBOX_TEST_PG_DSN`.
- The new test configures an active named PostgreSQL Store and runs both
  commands without per-command `--store` flags.
- `profile import` now has daily-path proof that it writes profile index,
  catalog index, and read models into the active named PostgreSQL Store.
- `profile verify --require-case-runs` now has daily-path proof that it reads
  existing passed API case run facts from the active PostgreSQL Store, publishes
  the verified profile, and leaves a PostgreSQL-backed profile catalog with the
  expected maintained API cases.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(ProfileImportAndVerifyUseNamedPostgreSQLActiveStore|ProfileVerifyCommandCanRequirePassedAPICaseRuns|ProfileImportCommandIndexesBundleInStore)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The profile import/verify coverage is env-gated and skipped without
  `OTSANDBOX_TEST_PG_DSN`; it does not replace the later human-machine
  PostgreSQL validation pass.
- Remaining PG-line gaps are now mostly `evidence import` as an explicit
  migration path, serve API profile import/verify through the running handler,
  CLI/API parity polish, release preparation, and live SkyWalking endpoint
  validation with real trace ids.

## 2026-05-20 Named PostgreSQL Serve Profile API Coverage

Estimated PostgreSQL mainline progress: 98.6%.

Completed evidence:

- Extended the env-gated named PostgreSQL serve coverage to include POST
  `/api/profile/import` and POST `/api/profile/verify` through the actual
  `serve` handler created from the active named Store.
- The profile import API now has daily-path proof that the running control
  plane writes profile index and read models into the active named PostgreSQL
  Store, not a local SQLite runtime.
- The profile verify API now has daily-path proof that the running control
  plane verifies, publishes, activates, and persists the verified profile
  catalog into the active named PostgreSQL Store.
- The test reopens the PostgreSQL Store after the handler API calls and checks
  the persisted profile index and catalog, so the API proof is Store-backed and
  not only response-shape based.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(ServeAndEvidenceTasksUseNamedPostgreSQLActiveStore|ProfileImportAndVerifyUseNamedPostgreSQLActiveStore)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The serve profile API coverage is env-gated and skipped without
  `OTSANDBOX_TEST_PG_DSN`; it does not replace the later human-machine
  PostgreSQL validation pass.
- Remaining PG-line gaps are now mostly `evidence import` as an explicit
  migration path, CLI/API parity polish, release preparation, and live
  SkyWalking endpoint validation with real trace ids.

## 2026-05-20 Named PostgreSQL Evidence Import Target Coverage

Estimated PostgreSQL mainline progress: 98.7%.

Completed evidence:

- Added env-gated named PostgreSQL coverage for `evidence import` behind
  `OTSANDBOX_TEST_PG_DSN`.
- The new test keeps `evidence import` correctly scoped as a legacy SQLite
  runtime migration path, but proves the target Store can be the active named
  PostgreSQL Store without passing per-command `--store`.
- The test imports a legacy runtime SQLite database into active PostgreSQL,
  then verifies the PostgreSQL Store contains the imported workflow run, parent
  run, API case run, and Evidence record.
- After import, the test runs `evidence list --run ... --json` without
  `--store` to prove daily Evidence reads see the imported PG-backed data.
- The legacy runtime fixture now supports unique imported run ids so repeated
  PostgreSQL validation runs do not collide in a shared test database.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(EvidenceImportUsesNamedPostgreSQLActiveStore|EvidenceImportCommandCanEmitJSONReport|EvidenceListCommandCanEmitJSON)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The evidence import PG target coverage is env-gated and skipped without
  `OTSANDBOX_TEST_PG_DSN`; it does not replace the later human-machine
  PostgreSQL validation pass.
- Remaining PG-line gaps are now mostly CLI/API parity polish, release
  preparation, and live SkyWalking endpoint validation with real trace ids.

## 2026-05-20 Named PostgreSQL Case Suite Daily Test Migration

Estimated PostgreSQL mainline progress: 98.8%.

Completed evidence:

- Migrated the remaining high-priority product-like case-suite daily tests from
  explicit SQLite Stores to active named PostgreSQL Store coverage behind
  `OTSANDBOX_TEST_PG_DSN`.
- The migrated no-flag daily commands now include `case suite coverage`,
  `case suite inspect`, `case suite plan`, `case suite stability`,
  `case suite priority`, `case suite brief`, `case suite impact`, and
  `case suite impact-report`.
- The tests configure an active named PostgreSQL Store, publish profiles through
  the active Store, seed run facts into the same PostgreSQL Store when needed,
  and then run CLI commands without per-command `--store`.
- Shared PostgreSQL test databases are protected from run id collisions by
  unique test ids in the migrated history-dependent tests.
- Light validation passed for each migrated test with targeted `go test`
  selectors, plus `tools/guardrails/check_store_first_contracts.sh`,
  `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- These tests remain env-gated and skip without `OTSANDBOX_TEST_PG_DSN`; they
  are PostgreSQL daily-path proof when the DSN is supplied, not a replacement
  for the later human-machine PostgreSQL validation pass.
- Some lower-priority case-suite quality/report tests still use explicit
  SQLite compatibility Stores and should either migrate to named PostgreSQL or
  be relabeled as compatibility coverage.
- Final confidence still depends on CLI/API parity polish, release preparation,
  and live SkyWalking endpoint validation with real trace ids.

## 2026-05-20 Named PostgreSQL Case Suite Quality Coverage

Estimated PostgreSQL mainline progress: 98.9%.

Completed evidence:

- Migrated the remaining case-suite quality planning tests from explicit
  SQLite Stores to active named PostgreSQL Store coverage behind
  `OTSANDBOX_TEST_PG_DSN`.
- The migrated no-flag daily commands now include `case suite quality`,
  `case suite quality-plan`, and `case suite quality-report`.
- Also migrated the older detailed `case suite report` filter/report test to
  active named PostgreSQL Store usage, so the detailed HTML/JUnit report
  assertions no longer depend on a SQLite daily path.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'TestCaseSuiteQuality(AuditsMaintainedCaseMetadata|PlanSuggestsAuthoringActions|ReportWritesJSONAndHTML)$' -count=1`,
  `go test ./cmd/otsandbox -run 'TestCaseSuiteReportRunsCasesByMaintenanceFilters$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- Remaining explicit SQLite tests are now more heavily concentrated in Store
  management, template/package/profile compatibility, Evidence import/runtime
  migration, serve handler compatibility, and selected direct case execution
  compatibility paths.
- Full release-check and real SkyWalking validation remain intentionally
  deferred by user direction; they are still required before claiming final
  release readiness.

## 2026-05-20 Named PostgreSQL Executor Plan Coverage

Estimated PostgreSQL mainline progress: 99%.

Completed evidence:

- Migrated `executor plan` descriptor coverage from an explicit SQLite Store to
  active named PostgreSQL Store coverage behind `OTSANDBOX_TEST_PG_DSN`.
- The test now seeds executor descriptors directly into the named PostgreSQL
  Store, then runs `executor plan --json` and text output without per-command
  `--store`.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'TestExecutorPlanCommandReportsProfileDescriptors$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- Remaining product-like SQLite tests are now concentrated in sandbox
  start/register, case timing, workflow audit, template render, trace topology
  collect, and selected direct case-run/report compatibility coverage.
- The practical PG line is about 99% by test-surface migration, but final
  release readiness still requires the later human-machine pass with a real
  PostgreSQL DSN, real SkyWalking endpoint, real trace ids, and the 10-step UI
  smoke proof.

## 2026-05-20 Named PostgreSQL Case Timing Coverage

Estimated PostgreSQL mainline progress: 99.1%.

Completed evidence:

- Migrated `case timing` summary coverage from an explicit SQLite Store to
  active named PostgreSQL Store coverage behind `OTSANDBOX_TEST_PG_DSN`.
- The test seeds uniquely named case run timing records into the named
  PostgreSQL Store, then runs `case timing --kind case --json` without
  per-command `--store`.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'TestCaseTimingCommandSummarizesStoredCaseRuns$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- Remaining non-compatibility-looking SQLite daily-path candidates are smaller
  pockets: sandbox start/register, workflow audit, template render, trace
  topology collect, and selected direct case-run/report tests.
- Final release readiness remains blocked on the later real PostgreSQL plus
  real SkyWalking human-machine validation pass.

## 2026-05-20 Named PostgreSQL Template Render Coverage

Estimated PostgreSQL mainline progress: 99.2%.

Completed evidence:

- Migrated `template render` request-preview coverage from explicit SQLite
  Stores to active named PostgreSQL Store coverage behind
  `OTSANDBOX_TEST_PG_DSN`.
- The test now publishes template profile data through the active named Store,
  renders without per-command `--store`, then seeds a Store-only template
  catalog into the same PostgreSQL Store and renders that path without
  per-command `--store`.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'TestTemplateRenderCommandPrintsRequestPreview$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- Remaining product-like SQLite pockets are now mostly sandbox start/register,
  workflow audit, trace topology collect, and selected direct case-run/report
  compatibility tests.
- Final release readiness still requires the later real PostgreSQL plus real
  SkyWalking validation pass.

## 2026-05-20 Named PostgreSQL Workflow Audit Coverage

Estimated PostgreSQL mainline progress: 99.3%.

Completed evidence:

- Migrated workflow audit Store-state coverage from explicit SQLite Stores to
  active named PostgreSQL Store coverage behind `OTSANDBOX_TEST_PG_DSN`.
- The JSON test now publishes workflow config through the active Store, seeds
  unique workflow and API case run facts into the same PostgreSQL Store, and
  runs `workflow audit --json` without per-command `--store`.
- The text summary test now publishes through the active named PostgreSQL Store
  and runs `workflow audit` without per-command `--store`.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'TestWorkflowAuditCommand(EmitsJSONWithScopedStoreState|PrintsTextSummary)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- Remaining product-like SQLite pockets are now mostly sandbox start/register,
  trace topology collect, and selected direct case-run/report compatibility
  tests.
- The PostgreSQL line is very close by command-surface migration, but final
  release readiness still requires a real PostgreSQL DSN, real SkyWalking
  endpoint, real trace ids, and 10-step UI smoke proof in the later
  human-machine validation pass.

## 2026-05-20 Named PostgreSQL Serve Evidence Import API Coverage

Estimated PostgreSQL mainline progress: 98.8%.

Completed evidence:

- Extended the env-gated named PostgreSQL serve coverage to include POST
  `/api/evidence/import` and GET `/api/evidence/list`.
- The new API path coverage imports a legacy runtime SQLite database through
  the running `serve` handler into the active named PostgreSQL Store.
- The same handler then reads the imported run back through `/api/evidence/list`
  and verifies the API case run count, Evidence count, Evidence id, case run id,
  kind, and URI mapping.
- This closes the main Evidence import CLI/API parity gap while preserving
  `evidence import` as an explicit legacy migration path rather than a normal
  daily SQLite path.
- Light validation passed:
  `go test ./cmd/otsandbox -run 'Test(ServeAndEvidenceTasksUseNamedPostgreSQLActiveStore|EvidenceImportUsesNamedPostgreSQLActiveStore)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- The serve Evidence import API coverage is env-gated and skipped without
  `OTSANDBOX_TEST_PG_DSN`; it does not replace the later human-machine
  PostgreSQL validation pass.
- Remaining PG-line gaps are now mostly final CLI/API parity documentation,
  release preparation, and live SkyWalking endpoint validation with real trace
  ids.

## 2026-05-20 Real SkyWalking Release Proof Documentation

Estimated PostgreSQL mainline progress: 98.9%.

Completed evidence:

- Tightened README and Store backend documentation so the deterministic
  synthetic SkyWalking GraphQL provider is described only as a local wiring
  smoke aid, not release evidence for a real SkyWalking deployment.
- Documented that real topology validation requires `OTS_TRACE_GRAPHQL_URL`
  and `OTS_SMOKE_TRACE_IDS` so the core 10-step smoke uses real trace ids.
- Documented that when no SkyWalking endpoint is configured, topology
  collection must report unavailable, failed, or skipped status instead of
  inventing topology.
- The wording is aligned with Apache SkyWalking's own model: topology and
  dependency are queried from SkyWalking data built from trace/service traffic,
  not generated as a local substitute.

Incomplete work:

- This is a documentation/parity closure only. The later human-machine
  validation pass still needs a real SkyWalking GraphQL endpoint and real trace
  ids to prove the final 10-step topology chain.
- Full release-check remains deferred by user direction while the PG line is
  being advanced quickly.

## 2026-05-20 CLI/API Topology Absence Contract

Estimated PostgreSQL mainline progress: 99.0%.

Completed evidence:

- Tightened the CLI/API parity matrix for `trace topology collect` and
  `/api/trace-topology/collect`: both surfaces share the same SkyWalking
  GraphQL path, require a real endpoint plus trace ids for real topology proof,
  and must expose unavailable, failed, or skipped status when the provider or
  trace is missing.
- Tightened the release checklist so real topology sign-off requires the
  configured SkyWalking endpoint, the trace ids used by the 10-step workflow,
  and persisted topology rows with provider, trace id, status, nodes, and
  edges.
- This aligns the docs with trace-derived topology systems such as Apache
  SkyWalking and OpenTelemetry service graph processing, where dependency
  topology is derived from observed trace/span data rather than invented by the
  test harness.

Incomplete work:

- This closes a contract/documentation gap. It does not replace the later live
  validation pass against a real SkyWalking endpoint.
- Full release-check remains deferred by user direction.

## 2026-05-20 Topology Documentation Boundary Sweep

Estimated PostgreSQL mainline progress: 99.1%.

Completed evidence:

- Swept the remaining release, quickstart, roadmap, Store backend, CLI/API, and
  release-check script wording that could make real SkyWalking topology sound
  like the default local smoke path.
- Roadmap now distinguishes stored topology review from real SkyWalking
  validation with a live endpoint, in both English and Chinese.
- Quickstart now states that `environment verify --topology-complete` is only a
  recorded completeness signal; real topology must be collected separately
  before publishing a verified environment.
- Store backend and release checklist docs now distinguish verified-environment
  real topology proof from deterministic local smoke wiring.
- CLI/API parity now labels `evidence import` as a legacy runtime SQLite
  migration/compatibility path into the active or named Store, not a normal
  daily SQLite execution path.
- `tools/release-check.sh` now prints "SkyWalking smoke provider mode" and
  explicitly says synthetic smoke is not live topology proof.

Incomplete work:

- Remaining gap is no longer product contract shape; it is final execution
  evidence: run the PG release gate and 10-step smoke against a real SkyWalking
  endpoint with real trace ids in the human-machine validation pass.

## 2026-05-20 Topology Contract Guardrail

Estimated PostgreSQL mainline progress: 99.2%.

Completed evidence:

- Store-first guardrails now require docs to state that deterministic synthetic
  SkyWalking smoke is not live release proof.
- Store-first guardrails now require docs to state that missing SkyWalking
  topology is reported as unavailable, failed, or skipped rather than generated.
- `tools/release-check.sh` is now guarded to keep its synthetic provider message
  distinct from live SkyWalking topology proof.

Incomplete work:

- The remaining gap is still execution evidence against a live SkyWalking
  GraphQL endpoint with real trace ids; this guardrail only prevents contract
  drift while the PG path is being finalized.

## 2026-05-20 Daily Store Resolver Guardrail

Estimated PostgreSQL mainline progress: 99.3%.

Completed evidence:

- Audited the remaining generic Store resolver calls in CLI handlers. The
  remaining direct `resolveStoreReference` uses are Store maintenance,
  offline template package review, or an optional Store context inside an
  offline audit path.
- The only direct `resolveRequiredStoreReference` use in CLI handlers is
  `evidence import`, which is the explicit legacy runtime migration path.
- Store-first guardrails now count these generic resolver call sites so new
  daily commands cannot bypass `resolveRequiredDailyStoreReference` unnoticed.

Incomplete work:

- This adds drift protection for daily Store resolution. The final unresolved
  proof remains a live PostgreSQL release gate plus real SkyWalking 10-step
  validation with real trace ids.

## 2026-05-20 Real SkyWalking Release-Check Mode

Estimated PostgreSQL mainline progress: 99.4%.

Completed evidence:

- `tools/release-check.sh` now supports
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`.
- In that mode, release-check fails early unless both `OTS_TRACE_GRAPHQL_URL`
  and `OTS_SMOKE_TRACE_IDS` are set, so the final 10-step smoke cannot silently
  run against the deterministic synthetic provider.
- The release checklist documents the required env trio for live topology
  sign-off.
- This is consistent with Apache SkyWalking Query Protocol: topology is queried
  through GraphQL from observed SkyWalking data, so release proof must provide
  a real GraphQL endpoint and concrete trace ids.

Incomplete work:

- The live endpoint itself is still not available in this local run, so the
  remaining proof is executing release-check with a PostgreSQL DSN,
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and real
  `OTS_SMOKE_TRACE_IDS`.

## 2026-05-20 Live SkyWalking Release Mode Guardrail

Estimated PostgreSQL mainline progress: 99.5%.

Completed evidence:

- Store-first guardrails now require the explicit
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING` release-check mode to stay present in
  both implementation and release documentation.
- Store-first guardrails now require the live SkyWalking mode to fail before
  expensive gates unless `OTS_TRACE_GRAPHQL_URL` is configured.
- Store-first guardrails now require the same mode to fail before expensive
  gates unless `OTS_SMOKE_TRACE_IDS` is configured for the core 10-step
  workflow.
- This preserves the final validation contract: live topology sign-off must use
  a real SkyWalking GraphQL endpoint and concrete trace ids, not the synthetic
  local provider.

Incomplete work:

- The remaining proof is still external execution: run release-check with a
  PostgreSQL DSN plus `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`,
  `OTS_TRACE_GRAPHQL_URL`, and real `OTS_SMOKE_TRACE_IDS`.

## 2026-05-20 Live SkyWalking Release-Check Test Coverage

Estimated PostgreSQL mainline progress: 99.6%.

Completed evidence:

- Added `tools/smoke/release-check.test.mjs`, which is included by the existing
  release-check smoke harness test glob.
- The new lightweight tests verify that
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` fails before expensive gates when
  `OTS_TRACE_GRAPHQL_URL` is missing.
- The tests also verify that the same mode fails before expensive gates when
  `OTS_SMOKE_TRACE_IDS` is missing, even if a SkyWalking GraphQL URL is set.
- This makes the live SkyWalking release sign-off contract executable instead
  of only documented and guardrailed.

Incomplete work:

- The final external proof remains a real run with PostgreSQL DSN,
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, a real `OTS_TRACE_GRAPHQL_URL`, and
  real 10-step `OTS_SMOKE_TRACE_IDS`.

## 2026-05-20 Release-Check Doc Guardrail Scope

Estimated PostgreSQL mainline progress: 99.7%.

Completed evidence:

- `SECURITY.md` and `CONTRIBUTING.md` now show release-check with
  `OTSANDBOX_SMOKE_STORE_DSN=postgres://...` instead of a bare
  `npm run release-check`.
- Store-first guardrails now scan `SECURITY.md` and `CONTRIBUTING.md`, so those
  entrypoint docs cannot drift back to a release-check example without a
  PostgreSQL smoke Store DSN.

Incomplete work:

- Remaining proof is still the external live validation run with PostgreSQL DSN,
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, a real SkyWalking GraphQL endpoint,
  and real 10-step trace ids.

## 2026-05-20 README Release Gate Shorthand

Estimated PostgreSQL mainline progress: 99.8%.

Completed evidence:

- README and README.zh-CN now describe the release gate shorthand as
  `OTSANDBOX_SMOKE_STORE_DSN=postgres://... npm run release-check`, not a bare
  `npm run release-check`.
- Store-first guardrails now reject the old English and Chinese shorthand so
  user-facing docs cannot imply the release gate runs without a PostgreSQL
  smoke Store DSN.

Incomplete work:

- The final proof remains the external live validation run with PostgreSQL DSN,
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, a real SkyWalking GraphQL endpoint,
  and real 10-step trace ids.

## 2026-05-20 Quickstart Live Topology Sign-Off

Estimated PostgreSQL mainline progress: 99.9%.

Completed evidence:

- Quick Start now distinguishes ordinary PostgreSQL release-check from final
  live topology sign-off, and names the required env trio:
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS`.
- Share Kit now states that live SkyWalking validation is the stricter sign-off
  mode, while demos without those env vars use deterministic synthetic topology
  wiring for repeatable local smoke.
- The Share Kit note is bilingual, matching the rest of the page.
- README and README.zh-CN now also name
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` beside `OTS_TRACE_GRAPHQL_URL` and
  `OTS_SMOKE_TRACE_IDS`, so the primary user entrypoints distinguish live
  sign-off from synthetic smoke.

Incomplete work:

- The final proof remains the external live validation run with PostgreSQL DSN,
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, a real SkyWalking GraphQL endpoint,
  and real 10-step trace ids.

## 2026-05-20 Named PostgreSQL Sandbox Start Coverage

Estimated PostgreSQL mainline progress: 99.92%.

Completed evidence:

- Added env-gated named PostgreSQL daily-path coverage for `sandbox start`
  behind `OTSANDBOX_TEST_PG_DSN`.
- The test writes startup commands into the active named PostgreSQL Store,
  invokes `sandbox start --service ... --json` without per-command `--store`,
  and verifies the local startup side effect plus JSON report.
- This gives the local execution entrypoint the same active named PostgreSQL
  proof as the broader daily CLI families, while SQLite remains covered only as
  compatibility behavior in the adjacent test.

Incomplete work:

- The test is env-gated and skipped without `OTSANDBOX_TEST_PG_DSN`; final
  release proof still requires the external live validation run with PostgreSQL
  DSN and real SkyWalking trace ids.

## 2026-05-20 Named PostgreSQL Sandbox Registration Coverage

Estimated PostgreSQL mainline progress: 99.94%.

Completed evidence:

- Added env-gated named PostgreSQL daily-path coverage for
  `sandbox service register` and `sandbox interface register`.
- The new test configures an active named PostgreSQL Store, runs both
  registration commands without per-command `--store`, and verifies the service,
  interface node, generated request template, and API case are persisted in the
  PostgreSQL catalog.
- This closes the remaining local execution registration proof beside
  `sandbox start`, while the existing SQLite test remains scoped to
  compatibility coverage.

Incomplete work:

- The new coverage is env-gated behind `OTSANDBOX_TEST_PG_DSN`; final release
  proof still requires the external PostgreSQL release-check plus live
  SkyWalking trace validation.

## 2026-05-20 Completion Audit Checklist

Estimated PostgreSQL mainline progress: 99.95%.

Completed evidence:

- Added a release checklist completion audit section that preserves the full
  objective rather than treating 99.x progress as done.
- The audit requires proof for daily named PostgreSQL Store usage, daily
  SQLite rejection, deprecated `--store-url` scoping, local execution paths,
  the core 10-step workbench smoke, per-interface Evidence, live SkyWalking
  topology, and PostgreSQL release-check.
- The checklist explicitly requires live SkyWalking proof with
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and real
  `OTS_SMOKE_TRACE_IDS` before topology coverage can be claimed.

Incomplete work:

- The checklist is contract documentation. The final missing evidence remains
  executing the PostgreSQL release gate plus live SkyWalking 10-step validation
  against an actual endpoint.

## 2026-05-20 Ten-Step Live Trace ID Preflight

Estimated PostgreSQL mainline progress: 99.96%.

Completed evidence:

- Release-check live SkyWalking mode now requires `OTS_SMOKE_TRACE_IDS` to
  contain trace id mappings for every workflow step from `step-01` through
  `step-10`, instead of accepting a partial trace-id set.
- The smoke test suite now covers missing GraphQL URL, missing trace ids, and
  partial trace-id mappings before release-check reaches expensive gates.
- This keeps synthetic local smoke separate from the final real-topology
  sign-off and prevents a partial 10-step run from being treated as complete.

Incomplete work:

- Final proof still requires running PostgreSQL release-check with
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, a real `OTS_TRACE_GRAPHQL_URL`, and
  real trace ids for all 10 workflow steps against an actual SkyWalking
  endpoint.

## 2026-05-20 Live Trace ID Documentation Guard

Estimated PostgreSQL mainline progress: 99.97%.

Completed evidence:

- README, README.zh-CN, Quick Start, and the release checklist now state that
  final live SkyWalking sign-off needs real trace id mappings for every
  workflow step from `step-01` through `step-10`.
- The Store-first contract guardrail now rejects release-check drift that drops
  the "all 10 workflow steps" requirement from the script or release checklist.
- This aligns the public entrypoints with the stricter release preflight and
  removes the older partial `step-01` example from the live sign-off path.

Incomplete work:

- Final proof still requires a real PostgreSQL release-check and live
  SkyWalking run with all 10 trace ids supplied by the external environment.

## 2026-05-20 Strict Live Trace ID Parsing

Estimated PostgreSQL mainline progress: 99.98%.

Completed evidence:

- Release-check live SkyWalking mode now parses `OTS_SMOKE_TRACE_IDS` using the
  same accepted JSON object or comma-separated `step=trace` formats as the
  smoke harness instead of relying on raw substring matches.
- Empty per-step trace ids are rejected before expensive gates run.
- The smoke test suite now covers partial trace-id mappings and empty step
  values for the 10-step workflow preflight.

Incomplete work:

- This is still a release preflight hardening slice. The remaining external
  proof is the real PostgreSQL plus live SkyWalking 10-step run.

## 2026-05-20 Direct SQLite Store Flag Rejection

Estimated PostgreSQL mainline progress: 99.985%.

Completed evidence:

- Daily Store resolution now rejects direct `--store sqlite://...` and
  `--store file://...` inputs, matching the existing rejection for active,
  named, and deprecated `--store-url` SQLite paths.
- The rejection message points users back to PostgreSQL Store setup and keeps
  SQLite scoped to explicit migration/compatibility commands.
- Added targeted resolver coverage so direct SQLite DSNs cannot re-enter the
  daily command path.

Incomplete work:

- Store maintenance and migration/compatibility paths still intentionally allow
  SQLite. Final external sign-off remains the real PostgreSQL plus live
  SkyWalking 10-step run.

Update:

- This was narrowed after representative CLI tests showed that rejecting every
  direct `--store sqlite://...` also removed explicit compatibility coverage.
  The current contract rejects implicit active/named SQLite Stores and legacy
  `--store-url` SQLite paths for daily commands, while direct
  `--store sqlite://...` remains an explicit compatibility selector.

## 2026-05-20 Smoke Harness Live Mode Enforcement

Estimated PostgreSQL mainline progress: 99.99%.

Completed evidence:

- The control-plane smoke harness now enforces
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` directly, not only through
  `tools/release-check.sh`.
- Direct smoke runs now fail before starting synthetic topology validation if
  the required real SkyWalking GraphQL URL is missing or if `OTS_SMOKE_TRACE_IDS`
  does not cover every step from `step-01` through `step-10`.
- Targeted smoke harness tests cover missing URL, partial trace ids, and the
  complete 10-step real-mode configuration.

Incomplete work:

- This closes another bypass around real-topology sign-off, but completion
  still requires the external PostgreSQL release gate plus a real SkyWalking
  10-step proof run.

## 2026-05-20 Live Sign-Off Text Cleanup

Estimated PostgreSQL mainline progress: 99.991%.

Completed evidence:

- Store backend docs and Share Kit now state that final live SkyWalking
  sign-off requires `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` plus trace id mappings
  for every workflow step from `step-01` through `step-10`.
- The release-check warning for synthetic smoke now points to the full live
  validation env set instead of calling trace ids optional for the sign-off
  path.

Incomplete work:

- Remaining completion evidence is still the external PostgreSQL release-check
  and live SkyWalking 10-step run against a real endpoint.

## 2026-05-20 Smoke Harness Live Mode Guardrail

Estimated PostgreSQL mainline progress: 99.992%.

Completed evidence:

- Store-first contract guardrails now require the control-plane smoke harness
  itself to reject `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` without
  `OTS_TRACE_GRAPHQL_URL`.
- The guardrail also requires the smoke harness and its tests to keep the
  "all 10 workflow steps" trace-id requirement in place, so direct smoke runs
  cannot drift away from release-check's live topology sign-off contract.

Incomplete work:

- Final completion still depends on the external PostgreSQL release-check and
  live SkyWalking 10-step proof run.

## 2026-05-20 Named PostgreSQL Read Path Test Migration

Estimated PostgreSQL mainline progress: 99.993%.

Completed evidence:

- Migrated representative daily read-path tests for `case runs`,
  `case evidence`, workflow run detail commands, and `evidence list` away from
  explicit SQLite Store flags.
- The migrated tests now configure an active named PostgreSQL Store through
  `OTSANDBOX_TEST_PG_DSN`, seed Store records directly or through CLI execution,
  and invoke the daily commands without per-command Store flags for the primary
  assertions.
- The tests use unique run/workflow ids and `--run` filters where needed so a
  shared PostgreSQL test DSN does not make assertions depend on old rows.

Incomplete work:

- More older SQLite-backed product-like tests remain and should continue moving
  to named PostgreSQL Store coverage or explicit compatibility scopes. Final
  completion still requires the external PostgreSQL release-check and live
  SkyWalking 10-step proof run.

## 2026-05-20 Release Check Runs PG-Gated Go Coverage

Estimated PostgreSQL mainline progress: 99.994%.

Completed evidence:

- `tools/release-check.sh` now exports `OTSANDBOX_TEST_PG_DSN` from the required
  PostgreSQL smoke Store DSN when the caller has not provided a more specific
  test DSN.
- The release gate's `go test ./... -count=1` therefore exercises named
  PostgreSQL daily-path tests instead of silently skipping them while the smoke
  Store DSN is already available.

Incomplete work:

- This wires the release gate for PG-gated unit coverage, but final completion
  still needs the full external release-check and live SkyWalking 10-step proof
  run against a real endpoint.

## 2026-05-20 Named PostgreSQL Planning Gate Test Migration

Estimated PostgreSQL mainline progress: 99.995%.

Completed evidence:

- Migrated representative `baseline get/set` tests from explicit SQLite Store
  flags to active named PostgreSQL Store coverage.
- Migrated `workflow plan` text, JSON, and missing-workflow tests to publish
  catalog data into the active named PostgreSQL Store and invoke the daily
  commands without per-command Store flags.
- The migrated baseline tests use unique subject ids so a shared PostgreSQL
  test DSN does not make assertions depend on existing rows.

Incomplete work:

- Some older product-like tests still use explicit SQLite for compatibility
  coverage. Final completion still requires the external PostgreSQL release
  gate plus live SkyWalking 10-step proof run.

## 2026-05-20 Named PostgreSQL Interface Coverage Test Migration

Estimated PostgreSQL mainline progress: 99.996%.

Completed evidence:

- Migrated `interface-node coverage` and `interface-node coverage-gaps` CLI
  tests from explicit SQLite Store flags to active named PostgreSQL Store
  coverage.
- The tests now publish catalog data into the active named PostgreSQL Store and
  invoke the daily coverage commands without per-command Store flags.
- SQLite read-model materialization tests remain available above this layer as
  explicit Store compatibility coverage rather than daily command proof.

Incomplete work:

- Additional older product-like tests still use explicit SQLite and should keep
  moving to named PostgreSQL coverage or explicit compatibility labeling. Final
  completion still requires the external PostgreSQL release gate plus live
  SkyWalking 10-step proof run.

## 2026-05-20 Named PostgreSQL Case Discovery Test Migration

Estimated PostgreSQL mainline progress: 99.997%.

Completed evidence:

- Migrated the `case discover` maintenance metadata filter test from explicit
  SQLite Store flags to active named PostgreSQL Store coverage.
- The test now publishes catalog data into the active named PostgreSQL Store and
  invokes the daily discovery command without per-command Store flags for both
  metadata and text filter assertions.
- Explicit `--store sqlite://...` discovery coverage remains in the dedicated
  compatibility-selector test rather than being counted as daily path proof.

Incomplete work:

- Remaining completion evidence is the full external PostgreSQL release gate
  and live SkyWalking 10-step proof run. Some legacy SQLite compatibility tests
  remain by design.

## 2026-05-20 Named PostgreSQL Workflow Report Failure Test Migration

Estimated PostgreSQL mainline progress: 99.998%.

Completed evidence:

- Migrated the workflow report failure-path test from explicit SQLite Store
  flags to active named PostgreSQL Store coverage.
- The test now publishes workflow catalog data into the active named
  PostgreSQL Store, discovers the workflow through the daily Store path, runs
  `workflow report` without a per-command Store flag, and still verifies the
  failed-step counts, Evidence handles, and generated HTML report.

Incomplete work:

- The final remaining proof is still the external PostgreSQL release gate plus
  live SkyWalking 10-step validation against a real endpoint.

## 2026-05-20 Named PostgreSQL Incomplete Batch Test Migration

Estimated PostgreSQL mainline progress: 99.9985%.

Completed evidence:

- Migrated the primary `case incomplete-batches` test path from explicit SQLite
  Store flags to active named PostgreSQL Store coverage.
- The test now runs one API case into the active named PostgreSQL Store and
  invokes `case incomplete-batches` without a per-command Store flag while
  preserving the text and JSON assertions for the not-run case.
- The test uses unique case and run ids so a shared PostgreSQL test DSN does not
  let old rows hide incomplete cases.
- The existing store-only SQLite section remains as explicit compatibility
  coverage for catalog-only inspection.

Incomplete work:

- Final completion still requires the external PostgreSQL release gate plus
  live SkyWalking 10-step validation against a real endpoint.

## 2026-05-20 Pause Checkpoint After Workflow Audit Migration

Estimated PostgreSQL mainline progress: 99.9986%.

Completed evidence:

- Current pause checkpoint includes the latest local commits through
  `a43f3d2 Migrate workflow audit tests to named PostgreSQL`.
- The just-finished slice migrated workflow audit JSON and text summary tests
  to active named PostgreSQL Store coverage behind `OTSANDBOX_TEST_PG_DSN`.
- The current branch is `test`, the worktree is clean, and the local branch is
  ahead of `origin/test` by 70 commits.
- Light validation for the final slice passed:
  `go test ./cmd/otsandbox -run 'TestWorkflowAuditCommand(EmitsJSONWithScopedStoreState|PrintsTextSummary)$' -count=1`,
  `tools/guardrails/check_store_first_contracts.sh`, `git diff --check`, and
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.

Incomplete work:

- Stop point per user request. Do not start another slice until resumed.
- Final completion still requires the external PostgreSQL release gate plus
  live SkyWalking 10-step validation against a real endpoint and real trace ids.

## 2026-05-20 Environment Restore Goal Ledger

Estimated overall new-machine environment restore progress: 98.5%.

Completed evidence:

- Added CLI `environment restore` for Store-backed Environment Catalog entries:
  optional Git clone/pull or existing checkout preparation, Docker Compose
  pull/build/up planning and execution, HTTP health checks, and optional
  verification workflow execution via `--execute --run-workflow`.
- Added `store ddl --backend postgres` so the sandbox control-plane PostgreSQL
  Store can be provisioned outside restored Docker target services.
- Documented the hard boundary that the sandbox Store/control-plane database
  must not be hosted by the Docker environment being restored.
- Added API/CLI bootstrap plan parity: `environment bootstrap` and
  `GET /api/environments/{id}/bootstrap` return repository steps, Docker
  commands, health checks, verification workflow, and a
  `pauseBeforeHeavyValidation` marker for UI review.
- Restore now accepts already-present checkouts without a repo URL, rejects
  missing compose files before invoking Docker, runs the recorded verification
  workflow when requested, and records the workflow run/status back into the
  Environment Catalog without marking SkyWalking topology complete or publishing
  the environment as verified.
- Restore preflight now reports required `git`, `docker`, and Docker Compose
  plugin capability through `docker compose version`, and labels heavy Docker
  pull/build/up steps before execution.
- Environment Catalog compose facts now support project name, env files,
  profiles, service selection, and `skipPull`/`skipBuild`; CLI restore and API
  bootstrap both reflect those options in generated Docker Compose commands.
- Restore continues to support the non-compose `startCommand` path for
  environments that record an explicit local start command instead of a compose
  file.
- Environment Catalog repository facts now support `--repo-ref SERVICE=REF` for
  tag/commit/ref pinning; CLI restore detaches cloned repos at the requested ref
  and API bootstrap exposes the same command plan.
- Existing checkouts with a recorded repo URL are now validated as Git work
  trees with matching `origin` and no uncommitted changes before restore uses or
  pulls them.
- Existing checkouts with a recorded `--repo-ref` are now prepared to the
  requested tag/commit/ref in execute mode before Docker startup, after origin
  and clean work tree validation.
- Restore preflight now also reports `git` when existing checkouts require Git
  validation or ref preparation, not only when a missing checkout must be
  cloned.
- Restore can now produce a Compose-scoped clean-machine simulation plan with
  `--clean-docker-state` and optional `--clean-docker-images`; execution is
  blocked unless `--allow-destructive-docker-cleanup` is explicitly supplied.
- Cleanup planning records `docker compose ps`, `docker compose images`, and
  `docker compose config` for human review before `docker compose down
  --remove-orphans`; it does not use global Docker prune commands, delete
  volumes, or treat the review commands as database/runtime backups.
- Documentation now repeats the hard boundary that cleanup targets only the
  recorded Compose project and must not clean or host the sandbox PostgreSQL
  control-plane Store.
- Restore attempts now persist compact diagnostics back into the selected
  Environment Catalog entry: `summary.lastRestore` points at the latest attempt
  and `summary.restoreAttempts` keeps the most recent 20 attempts with restore
  id, phase, preflight, repository actions, Docker/cleanup status, health check
  counts, workflow action, and next actions for later `environment inspect` or
  API review.
- Restore health checks now support Store-backed URL, TCP, workspace command,
  and Docker Compose service probes. Dry-run keeps probes as plan data; execute
  waits for all probes after Docker startup and records failed probe details in
  restore diagnostics.
- Restore now has an explicit repository precheck regression: registered
  business service repositories must clone/fetch/ref-prepare before Docker
  pull/build/up starts, so missing or mismatched code stops before target Docker
  startup.
- Restore reports a Store-persisted `readiness` gate before true
  colleague-machine validation: it checks the sandbox PostgreSQL Store boundary,
  verified workflow anchor, service repository readiness, Compose
  services/middleware plan, health probes, cleanup review, workflow run gate,
  and the required operator pause before deleting containers/images or waiting
  on large image downloads.
- The readiness gate directly covers the "5 business services before Docker"
  scenario: all recorded service repositories must pass clone/checkout/ref
  preparation before Docker startup; middleware such as Apollo or MySQL is
  represented through the recorded Compose service/image plan and the same
  health probe gate.

Latest light validation:

- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore(HonorsComposeOptionsFromStore|ClonesRemoteReposForVerifiedWorkflow)' -count=1`
- `go test ./internal/controlplane -run 'TestServerManagesVerifiedEnvironmentCatalogFromStore' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentCommandsGateVerifiedDiscovery|TestEnvironmentRestore(RejectsExistingCheckoutWithDifferentOrigin|ChecksOutRequestedRefAfterClone|PullsExistingCheckoutWhenRequested|AcceptsExistingCheckoutWithoutRepoURL)' -count=1`
- `go test ./cmd/otsandbox -run 'Test(StoreDDLCommandPrintsPostgreSQLSchema|EnvironmentCommandsUseNamedPostgreSQLActiveStore|EnvironmentRestore)' -count=1`
- `go test ./internal/controlplane -run 'TestServerManagesVerifiedEnvironmentCatalogFromStore' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore(ChecksOutRequestedRefForExistingCheckout|ChecksOutRequestedRefAfterClone|RejectsExistingCheckoutWithDifferentOrigin|PullsExistingCheckoutWhenRequested)' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore(ChecksOutRequestedRefForExistingCheckout|DetachesExistingCheckoutAlreadyAtRef|PreflightRequiresGitForExistingCheckoutRef|ChecksOutRequestedRefAfterClone|RejectsExistingCheckoutWithDifferentOrigin|PullsExistingCheckoutWhenRequested)' -count=1`
- `go test ./cmd/otsandbox -run 'TestTopLevelHelpShowsStoreFlagNotLegacyStoreURL|TestEnvironmentRestore(PlansDockerCleanupWithoutExecuting|BlocksDockerCleanupWithoutExplicitAllow|RunsAllowedDockerCleanupBeforeStartup|HonorsComposeOptionsFromStore|FailsBeforeDockerWhenComposeFileIsMissing)' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore(ClonesRemoteReposForVerifiedWorkflow|BlocksDockerCleanupWithoutExplicitAllow)' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore(RunsMixedHealthProbes|FailsWhenHealthProbeFails|ExecutesDockerComposeWithoutRepository)' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore(StopsBeforeDockerWhenRepositoryPrecheckFails|RunsMixedHealthProbes|FailsWhenHealthProbeFails)' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore' -count=1`
- `go test ./internal/controlplane -run 'TestServerManagesVerifiedEnvironmentCatalogFromStore' -count=1`
- `go test ./cmd/otsandbox -run 'TestEnvironmentRestore(ClonesRemoteReposForVerifiedWorkflow|StopsBeforeDockerWhenRepositoryPrecheckFails|PlansDockerCleanupWithoutExecuting)' -count=1`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`
- `git diff --check`

Incomplete work:

- True new-machine proof remains intentionally paused until the user approves a
  heavy validation pass that backs up/deletes current Docker containers/images
  or otherwise simulates a clean colleague machine.
- Before that proof, keep the current environment running for manual basic
  verification at `http://127.0.0.1:58663/`; do not delete or rebuild the
  current Docker services until the user finishes that self-test.

Manual self-test hold:

- Local PostgreSQL Store `local-pg` was started through Homebrew
  `postgresql@16`, outside Docker.
- `otsandbox serve --store local-pg --host 127.0.0.1 --port 58663` is running
  for manual workbench verification at `http://127.0.0.1:58663/`.
- Existing target Docker containers are still intact: no container/image
  deletion, cleanup, rebuild, or colleague-machine simulation has been run.
- Manual run failure at 2026-05-20T03:14Z was caused by target services being
  stopped, not by the control-plane page: step 1 attempted
  `127.0.0.1:21116` and got connection refused.
- Existing `scf-chain-sandbox` containers were started with Docker Compose
  `start` only. No image pull, build, delete, cleanup, or recreated clean-machine
  simulation was run. Dashboard then reported 9/9 services healthy and
  `127.0.0.1:21116` was reachable.

Acceptance report gate slice:

- 2026-05-20T03:32:27Z: added the environment workflow acceptance report as a
  pure report template, not a PostgreSQL-specific rule. The current template is
  `environment.workflow.skywalking.v1` and explicitly requires real SkyWalking
  topology for every workflow step.
- Async workflow batch runs now persist an `acceptance` section with workflow
  step count, completed/passed/failed counts, each step status and elapsed time,
  indexed Evidence completeness, SkyWalking topology completeness, and the
  final acceptance result.
- When the control plane is configured with a SkyWalking GraphQL provider, async
  workflow acceptance runs now collect real topology during the background
  runner before computing the report result. If the provider is unavailable or
  unset, the run records a trace topology post-process skip/failure and the
  acceptance result remains false.
- Added CLI surfaces for manual asynchronous acceptance flow against a running
  control plane:
  `workflow acceptance start --server-url URL --workflow ID --request-id ID`
  and `workflow acceptance report --server-url URL --run ID`.
- `environment publish-verified` now rejects a verified-ready environment when
  the referenced run has Evidence and topology rows but lacks
  `acceptance.ok=true` for the environment verification workflow.
- Light validation for this slice:
  `go test ./internal/controlplane -run 'TestServerStartsAsyncAPICaseBatchRunForWorkflow|TestServerManagesVerifiedEnvironmentCatalogFromStore'`;
  `go test ./internal/controlplane -run 'TestServerAsyncWorkflowAcceptancePassesWithSkyWalkingTopology'`;
  `go test ./cmd/otsandbox -run 'TestWorkflowAcceptanceCLIStartsAndReadsAsyncReport|TestEnvironmentCommandsGateVerifiedDiscovery|TestEnvironmentCommandsUseNamedPostgreSQLActiveStore'`;
  `rg -n -i 'fall''back' . --glob '!node_modules/**'`.
- Restore still needs richer provider hardening for GitHub/GitLab tokens,
  submodules, and auth prompts.
- Docker restore still needs a real operator-approved clean-machine proof;
  destructive cleanup policy guardrails are now present at CLI level but not
  live-validated against real Docker state.
- Future restore hardening should add explicit dependency ordering and richer
  per-service readiness policies beyond the current Store-backed probes.

Core 10-step workflow green-run slice:

- 2026-05-20T04:18:49Z: the running workbench at
  `http://127.0.0.1:58663/workflow-detail.html?id=sandbox.financing_to_repay_result_query`
  produced a real passed workflow run:
  `run.20260520T041849.493116000Z`.
- The run finished `10/10` steps green with `elapsedMs=4031`, backed by the
  running local PostgreSQL Store `local-pg` and the current Docker target
  services. No Docker image/container deletion or clean-machine simulation was
  run.
- Root causes fixed in the live validation environment:
  `sandbox-scf-gateway` was missing and was started from the existing local fat
  jar because a fresh Maven build was blocked by Nexus 502; the LLT simulator
  had an SM2 envelope edge case that could corrupt response key material when a
  generated ciphertext began with byte `0x04`; the LLT callback workflow step
  was still targeting `/v1/api/dispatcher` and was corrected in the active
  Store catalog to `POST /__sandbox/llt/callback`.
- Evidence from the passed run: all ten steps returned HTTP 200 and passed:
  trial, apply, callback, query, loan-apply, loan-callback, loan-query,
  repay-trial, repay-apply, repay-query.
- Remaining follow-up from this slice: the dashboard health summary still needs
  to stop treating Docker `running` as business healthy; service cards should
  require real HTTP healthcheck success or show unchecked/unhealthy.

Core 10-step workflow acceptance-green slice:

- 2026-05-20T05:02:12Z: restarted the local workbench with the real SkyWalking
  OAP GraphQL endpoint:
  `OTS_TRACE_GRAPHQL_URL=http://127.0.0.1:12800/graphql`.
- Manual async acceptance run
  `batch.manual-green-skywalking-fixed-20260520T050212Z.20260520T050212.526123000Z`
  passed the full `sandbox.financing_to_repay_result_query` workflow:
  `completed=10`, `passed=10`, `failed=0`, `acceptance.ok=true`.
- Acceptance report details: template
  `environment.workflow.skywalking.v1`; workflow steps `10/10`; Evidence
  complete for every interface step; real SkyWalking topology complete for
  every step (`topologyComplete=true` on all ten steps).
- Root cause fixed for the previous partial topology result: the trace
  collector persisted rows with an empty topology id when the payload omitted
  `id`, so repeated step topology writes overwrote each other. It now generates
  a stable id from run, step, trace/request/case identity before saving.
- Dashboard health semantics were tightened: Docker `running` without a real
  HTTP healthcheck no longer appears as healthy. The current dashboard summary
  correctly reports only healthchecked services as healthy and marks unchecked
  services as not OK.
- Light validation for this slice:
  `go test ./internal/controlplane -run 'TestTraceTopologyCollectPersistsProviderSpanRefs|TestServerStartsAsyncAPICaseBatchRunForWorkflow|TestServerAsyncWorkflowAcceptancePassesWithSkyWalkingTopology' -count=1`;
  `go test ./cmd/otsandbox -run 'TestEnvironmentAcceptanceCLIStartsAndReadsAsyncReport' -count=1`.

Docker restore acceptance binding slice:

- Current validation is intentionally three-layered: non-destructive dry-run,
  isolated prepare-repos-only pre-Docker gate, then operator-approved real
  Docker pull/build/up plus async acceptance run.
- 2026-05-20T06:30Z: set the active goal to prove that an environment bound to
  a successful async acceptance workflow can be restored from Store metadata
  toward a colleague-machine Docker environment, without putting the sandbox
  PostgreSQL Store inside the target Docker stack.
- `environment restore --run-workflow` now requires `--server-url` and triggers
  the environment async acceptance API (`/api/environments/{id}/acceptance-runs`)
  instead of the older local workflow report path. The restore report records
  `action=run-acceptance-workflow`, `reportUrl`, and the acceptance template
  result; Evidence/topology flags are set only when `acceptance.ok=true`.
- Added multi-file Docker Compose support for Environment Catalog entries:
  repeated `--compose-file` is stored as `composeFiles`, and both CLI restore
  plus API bootstrap render all `-f` arguments.
- Added generated Compose env support through repeated
  `--compose-env KEY=VALUE`. Restore writes `.otsandbox/restore.env` in the
  target workspace and expands `$OTS_WORKSPACE` so Docker mounts the cloned
  colleague-machine repositories instead of this machine's default paths.
- Added `environment restore --execute --prepare-repos-only` as the safe
  pre-Docker gate. It really clones/validates repositories in a workspace and
  then stops before Docker pull/build/up, which lets us prove remote access and
  checkout readiness without touching running containers.
- Registered Store environment `scf-chain-core10-local-docker` in `local-pg`.
  It is bound to `sandbox.financing_to_repay_result_query`, records the two
  validation compose files, the tracing profile, 17 compose services, 7 service
  repositories, generated repo path env entries, and two URL health checks.
- Non-destructive dry-run passed against
  `/Users/zlh/codes/open-test-sandbox-validation`: readiness was
  `ready-for-operator-review`, no readiness items failed, and Docker actions
  were plan-only.
- Isolated colleague-workspace repository precheck passed in
  `/tmp/ots-colleague-restore.g6xYt5`: all 7 repositories cloned successfully
  and Docker was skipped with `action=skipped-after-repository-preparation`.
- Pause point: the next real test would copy/provide the validation compose
  package in the isolated workspace and run Docker pull/build/up. Because the
  compose stack uses fixed container names and may download/build images, this
  requires operator approval before proceeding.

Environment package and PG size-boundary slice:

- 2026-05-20T06:55Z: added Store metadata for an environment package repository
  through `environment register --package-repo [--package-branch]
  [--package-ref]`. Restore now prepares that package before service
  repositories, so a new workspace can obtain the validation compose package
  from Git instead of relying on manual copy from this machine.
- Added `package` details to restore reports and a readiness gate named
  `environment-package`. `--prepare-repos-only` now covers both the environment
  package repository and service repositories, while still stopping before
  Docker.
- Added a Store-layer environment definition metadata cap:
  `store.EnvironmentDefinitionMaxBytes = 65536`; environment summary metadata
  is separately capped at `store.EnvironmentSummaryMaxBytes = 131072`. The
  definition cap covers compact fields such as id/display/description, services,
  repo pointers, compose pointers/env entries, health checks, and workflow
  binding. PostgreSQL must store only restore metadata and acceptance
  summaries/indexes; small service-adjacent cert/key material may be stored as
  bounded, redacted environment metadata when it is required for startup. Code
  packages, Docker images, logs, Evidence payloads, and other large files are
  rejected by this boundary and must live in Git, Docker registry, object
  storage, or the workspace.
- Expected size for the current 10-step Docker environment definition remains
  in the tens-of-KB class at most; the current `scf-chain-core10-local-docker`
  PG row is about 3211 bytes for definition metadata and about 13151 bytes for
  restore summary metadata. The goal is compact metadata, not binary or artifact
  storage in PG.
- Light validation for this slice:
  `go test ./cmd/otsandbox -run
  'TestEnvironment(RegisterRejectsOversizedDefinitionMetadata|RestoreCanPreparePackageRepositoryBeforeDocker)'
  -count=1`;
  `go test ./internal/store/... -run 'Test.*Environment' -count=1`.

Docker service startup gate slice:

- 2026-05-20T07:10Z: shifted the restore gate priority ahead of workflow
  execution: middleware and business containers must first be started and
  reported as ready before the bound workflow acceptance is useful.
- Restore now expands the recorded Compose service allow-list into generated
  `compose-service` health checks. After Docker startup it runs
  `docker compose ps --format json SERVICE` for each recorded service and
  requires the container state to be `running` with no `unhealthy` status.
- For `scf-chain-core10-local-docker`, the dry-run report now includes 19
  readiness checks: 2 URL probes plus 17 generated Compose service checks
  covering Zookeeper, MySQL, Redis, RabbitMQ, Apollo, XXL job, SkyWalking,
  LLT, and the business services. This report was persisted back to the active
  local PostgreSQL Store as restore progress metadata.
- This does not replace real HTTP/API checks where they exist; it adds the
  container startup proof that must pass before running the async workflow
  acceptance report.
- Light validation for this slice:
  `go test ./cmd/otsandbox -run
  'TestEnvironmentRestoreHonorsComposeOptionsFromStore|TestEnvironment(RegisterRejectsOversizedDefinitionMetadata|RestoreCanPreparePackageRepositoryBeforeDocker)'
  -count=1`; dry-run against `local-pg` returned `ok=True`,
  `healthChecks=19`, readiness `ready-for-operator-review`.

Remote source policy slice:

- 2026-05-20T08:20Z: tightened the PostgreSQL-backed one-click environment
  path to require remote Git sources for environment package and service repos.
  SQLite compatibility and unit-test fixtures can still use local bare repos,
  but `local-pg` restore now adds a `remote-git-sources` readiness item and
  blocks Docker startup when package or service sources are local paths.
- Created and pushed public GitHub repo
  `git@github.com:ztcshen/open-test-sandbox-llt-simulator.git`; local push used
  the `github-personal` SSH alias and commit
  `a38c0c9 Keep LLT encrypted fields compatible`.
- Updated active PG environment `scf-chain-core10-local-docker` so LLT now uses
  `git@github-personal:ztcshen/open-test-sandbox-llt-simulator.git`; the other
  six business service repositories remain on company GitLab. This gives the
  same restore plan both GitHub and GitLab source coverage.
- Current remaining source-policy blocker is the validation environment package:
  it still points at local path `/Users/zlh/codes/open-test-sandbox-validation`.
  The package itself now has local commit
  `ce4aedc2b4b9994e7c7bb4324d6f4fbcf6103b66`, but it still needs a remote Git
  URL before real one-click Docker restore can proceed.
- 2026-05-20T08:45Z: audited the validation package before any remote push. The
  tracked package is small enough for Git (`.git` about 5.4 MB, `compose` about
  6.0 MB, 77 tracked files), while `.runtime` is about 250 MB and ignored.
  However the tracked compose/scripts include deterministic local-test startup
  secrets and credentials such as MySQL root password, LLT signing key, Grafana
  admin password, encrypted DB password placeholders, and business initialization
  SQL. These are acceptable as bounded startup metadata for a private validation
  environment, but should not be published to a public GitHub repo as-is.
  Recommended next step: create a private GitHub repo or company GitLab repo for
  `open-test-sandbox-validation`, then update the PG package URL and rerun the
  isolated `--prepare-repos-only` gate. A later cleanup can migrate small
  cert/key/env material into PG redacted metadata while keeping Docker images,
  code packages, logs, and Evidence out of PG.
- 2026-05-20T09:05Z: created private GitHub repo
  `ztcshen/open-test-sandbox-validation` through the browser UI and configured
  the local validation package repo with remote
  `git@github-personal:ztcshen/open-test-sandbox-validation.git`. No package
  content has been pushed yet; pushing it is the next explicit external
  transmission decision because the package contains deterministic local-test
  keys, passwords, SQL, and mappings.
- 2026-05-20T09:35Z: corrected the restore direction after product review:
  `open-test-sandbox-validation` is not a formal one-click environment service
  source. PostgreSQL restore now treats package repositories as compatibility
  inputs, ignores them in the PostgreSQL daily restore path, and enforces remote
  Git only for business service repositories. New `compose.generatedFiles` /
  `environment register --compose-generated-file TARGET=SOURCE_FILE` support lets
  compact Store metadata write compose/startup files under the restore workspace
  before Docker starts. The top-level restore `ok` now fails when readiness fails.
  Current `local-pg` dry-run no longer reports the validation package as a
  source-policy blocker; it reports the real remaining blocker:
  `store-startup-files` is missing generated Store content for
  `compose/docker-compose.yml` and `compose/docker-compose.apps.yml`.
- Also removed the stale `compose.package` field from the active `local-pg`
  environment row for `scf-chain-core10-local-docker`; current dry-run reports
  `package=not-configured`, `sourcePolicy.ok=true`, and readiness blocked only
  on missing Store startup files.
- 2026-05-20T09:55Z: added `environment startup-file put ENV_ID --file
  TARGET=SOURCE_FILE`, a Store-first way to attach compact startup files to an
  existing Environment Catalog row without re-registering its workflow, repos,
  services, or health checks. Also fixed `--execute --prepare-repos-only` so it
  writes Store-backed startup files while still skipping Docker.
- Updated active `local-pg` environment `scf-chain-core10-local-docker` with
  generated content for `compose/docker-compose.yml` and
  `compose/docker-compose.apps.yml`. The resulting `compose_json` is 19,740
  bytes, below the 64 KB environment definition limit and still only metadata:
  no Docker images, code packages, runtime logs, or Evidence payloads are stored
  in PostgreSQL.
- Non-destructive verification: `environment restore --store local-pg
  --workspace /tmp/ots-policy-check --json scf-chain-core10-local-docker`
  reports `ok=true`, `sourcePolicy.ok=true`, `readiness.ok=true`, and two
  planned Store-generated compose files. Isolated prepare-only execution at
  `/tmp/ots-restore-isolated` reports `ok=true`, cloned seven service repos
  from GitLab/GitHub, wrote both compose files, and stopped before Docker with
  `docker.action=skipped-after-repository-preparation`.
- 2026-05-20T10:15Z: added a non-destructive Docker startup guard. Restore
  preflight now extracts fixed Compose `container_name` values from Store-backed
  compose files and compares them with `docker ps -a`; if existing containers
  would be reused or replaced, full Docker startup is blocked with a
  `docker-container-conflicts` readiness failure. `--execute
  --prepare-repos-only` bypasses this check because it does not start Docker.
- Current host state: 17 existing `sandbox-*` containers are running, so a full
  `local-pg` restore dry-run now correctly fails before Docker with
  `docker.action=skipped-due-to-preflight` and lists the conflicting container
  names. The isolated prepare-only verification still passes, writes both
  Store-backed compose files, clones the seven service repos, and stops before
  Docker. This means real Docker up on this machine now requires an operator
  choice: reuse the existing running environment for acceptance, or approve a
  backup/down/clean-machine simulation step first.
- 2026-05-20T10:30Z: added explicit non-destructive adoption via
  `environment restore --use-existing-containers`. This path writes the
  Store-backed startup files, skips Docker Compose startup, converts
  `compose-service` checks into fixed `docker inspect` container checks, and can
  then trigger the same async acceptance workflow.
- Real restore-driven acceptance on the currently running Docker environment
  passed without Docker lifecycle changes:
  `environment restore --store local-pg --workspace /tmp/ots-restore-isolated
  --execute --use-existing-containers --run-workflow --server-url
  http://127.0.0.1:58663 --acceptance-timeout-seconds 180 --json
  scf-chain-core10-local-docker` reported `docker.action=use-existing-containers`,
  `healthPassed=19/19`, `workflow.action=run-acceptance-workflow`,
  `workflow.ok=true`, `expectedSteps=10`, `passedSteps=10`, and
  `topologyProvider=skywalking`. `environment publish-verified --store local-pg`
  then promoted `scf-chain-core10-local-docker` to `status=verified` with
  `evidenceComplete=true` and `topologyComplete=true`.
- Re-ran the same restore-driven acceptance after the adoption implementation
  changes to keep evidence current. Latest run:
  `batch.restore.scf-chain-core10-local-docker.20260520T070947.377509000Z.20260520T070947.569388000Z`.
  Current-code result remains `ok=true`, `docker.action=use-existing-containers`,
  `healthPassed=19/19`, `expectedSteps=10`, `passedSteps=10`,
  `templateId=environment.workflow.skywalking.v1`, and
  `topologyProvider=skywalking`; `publish-verified` again reports
  `status=verified`, `evidenceComplete=true`, and `topologyComplete=true`.
- 2026-05-20T10:55Z correction: Store inspection and restore evidence for this
  line must come through `otsandbox` CLI/API surfaces, not direct database-client
  SQL. This keeps the Environment Catalog contract Store-neutral if the backing
  implementation later changes from PostgreSQL to another supported database.
- Added a Docker startup-asset preflight to `environment restore`. For any path
  that Docker Compose would bind from the host before service startup, restore
  now checks that the asset already exists under the restore workspace or is
  present in Store-backed `compose.generatedFiles`. The check is skipped for
  `--prepare-repos-only` and `--use-existing-containers` because those paths do
  not start Docker.
- CLI-only inspection of `scf-chain-core10-local-docker` shows the current
  Store-backed startup payload is still just two generated compose files:
  `compose/docker-compose.yml` at 4,973 bytes and
  `compose/docker-compose.apps.yml` at 12,729 bytes, totaling 17,702 bytes.
  This remains compact metadata, but it is not enough for a clean workspace.
- CLI-only clean-machine dry-run:
  `environment restore --store local-pg --workspace /tmp/ots-restore-isolated
  --clean-docker-state --json scf-chain-core10-local-docker` now blocks before
  Docker with `preflight.ok=false`, `docker.action=skipped-due-to-preflight`,
  and a `startup-assets` readiness failure. Missing required assets include
  MySQL init SQL, Redis Sentinel config, WireMock files/mappings, Apollo and
  XXL-Job mappings, Loki/Promtail/Grafana config, and six
  `compose/scripts/run-*.sh` launch scripts.
- Real clean-machine Docker deletion or image removal is still paused. The next
  non-destructive slice should decide how to provide the bounded text assets
  through Store metadata while keeping large binaries, runtime databases, logs,
  Evidence payloads, Docker images, Maven caches, and generated code out of the
  sandbox Store. The current 5.6 MB WireMock dependency jar is especially not a
  Store candidate; it should come from an image, remote artifact, or remote repo
  build path.
- 2026-05-20T11:25Z: landed the first componentized environment schema in Store
  DDL. The shared SQL Store target is now schema version 3, and the SQLite
  compatibility schema is version 16. New tables:
  `environment_components`, `service_dependencies`, and
  `service_config_assets`.
- The schema follows the corrected ownership model: middleware components such
  as MySQL, Apollo, Zookeeper, Redis, RabbitMQ, and SkyWalking provide runtime
  capability and health; business services own the assets they need to consume
  those capabilities, including MySQL DDL/seed, Apollo namespace config, launch
  scripts, env/secret material, and mock mappings.
- Applied the migration to `local-pg` through CLI only:
  `store status --store local-pg` reported version 2 -> target 3, then
  `store upgrade --store local-pg` applied one migration, and the final status
  reported version 3, target 3, pending 0. No direct database-client SQL was
  used for this verification.
- This is structure-only landing. Existing environment rows are not backfilled
  into component rows yet; the next design iteration can still refine component
  fields and asset ownership before adding CLI/API writers and restore readers.
- 2026-05-20T11:40Z design correction: the environment model should be a
  component graph, not a middleware-vs-business-service split. Middleware can
  depend on middleware too: examples include Grafana -> Loki, Promtail -> Loki,
  SkyWalking UI -> OAP, OAP -> storage, Redis Sentinel -> Redis master, Apollo
  -> its metadata store, and XXL-Job admin -> its database.
- Rewrote the clean-machine restore plan around generic component ownership:
  every runtime unit is an `environment_component`; dependencies should become
  consumer -> provider edges; config assets should be owned by the consumer
  component and target either the provider component or the consumer itself.
  The already-landed `service_*` tables are treated as a first-step subset and
  should be generalized to `component_dependencies` and
  `component_config_assets` before restore is wired to the model.
- 2026-05-20T07:55Z design correction: component graph cycle detection and
  topological ordering are now explicitly delegated to a mature Go graph
  library, not project-owned algorithms. The plan names Gonum
  `graph/topo` as the implementation dependency for stable ordering and cycle
  reporting, with Open Test Sandbox code limited to Store/API translation,
  dependency phase filtering, provider-before-consumer graph projection, and
  CLI/API/UI report formatting.
- The restore plan now distinguishes blocking dependency phases
  (`prepare`, `startup`, `readiness`, `acceptance`) from `runtime`
  relationships. Blocking phases must pass the Gonum-backed cycle and ordering
  check before Docker restore proceeds. Runtime cycles are allowed only when
  every involved component has explicit health probes, bounded waits, and
  reportable readiness gates.
- 2026-05-20T08:07Z implementation slice: Docker runtime units now have a
  first real component-graph Store path. Shared SQL Store schema target is
  version 4, and SQLite compatibility schema target is version 17. The new
  generic tables are `component_dependencies` and `component_config_assets`,
  while existing `environment_components` remains the component registry and
  the earlier `service_*` tables remain compatibility-only structure.
- Added Store API methods to replace and read an environment component graph
  without direct database-client SQL. The graph model covers components,
  consumer -> provider dependency edges with phase/capability, and compact
  component-owned config assets. Inline asset content is bounded at 16 KB per
  asset and 64 KB per graph so images, code, runtime databases, logs, caches,
  and Evidence payloads cannot be smuggled into PostgreSQL.
- Added CLI surfaces:
  `environment components replace ENV_ID --file COMPONENT_GRAPH_JSON` and
  `environment components inspect ENV_ID`, both backed by the active Store.
  `environment restore` now loads the Store component graph and reports a
  `component-graph` readiness item. Required components without Store-backed
  health checks fail this readiness gate.
- Applied the new shared SQL Store migration to `local-pg` through CLI only:
  `store status --store local-pg` showed version 3 -> target 4, then
  `store upgrade --store local-pg` applied one migration, and final status is
  version 4, target 4, pending 0. No direct database-client SQL was used.
- Temporary CLI verification wrote a two-component graph (`mysql` plus
  `service.alpha`) into an isolated SQLite Store via
  `environment components replace`, read it back via
  `environment components inspect`, and confirmed `environment restore --json`
  reports `componentGraph.configured=true`, two components, one blocking edge,
  one asset, 49 inline asset bytes, and a green `component-graph` readiness
  item.
- The real `scf-chain-core10-local-docker` environment on `local-pg` currently
  now has first-pass component graph rows written through CLI:
  17 components, 42 dependency edges, and 19 compact config-asset placeholders.
  Components include middleware (`zookeeper`, `mysql`, `redis-master`,
  `redis-sentinel`, `rabbitmq`), platform services (`apollo`, `xxl-job`),
  mocks (`wiremock`, `llt`), observability (`skywalking-oap`,
  `skywalking-ui`), and the six business services in the Compose allow-list.
- A non-destructive restore plan against `local-pg` now reports
  `componentGraph.configured=true`, `components=17`, `dependencies=42`,
  `blockingDependencies=39`, `runtimeDependencies=3`, `assets=19`,
  `inlineAssetBytes=0`, `requiredHealthChecks=17`, and
  `missingHealthChecks=0`. The new `component-graph` readiness item is green.
  Restore is still correctly blocked by missing startup assets, so the next
  implementation slice should materialize those component-owned assets into
  Store-generated startup files rather than treating the component graph as a
  complete clean-machine restore by itself.
- 2026-05-20T08:19Z implementation slice: `environment restore` now projects
  component config assets with bounded `contentInline` into the restore
  `generatedFiles` map before preflight. This lets component-owned startup
  text, such as run scripts, Redis Sentinel config, Apollo/XXL mock mappings,
  Grafana/Loki/Promtail config, and small MySQL bootstrap SQL, satisfy Compose
  bind-source and `/sandbox/compose/...` command checks without requiring a
  separate local validation folder.
- Rewrote the real `scf-chain-core10-local-docker` component graph through the
  CLI to include the actual Docker Compose service set: 24 components,
  47 dependency edges, and 27 component assets. This fixed the earlier gap
  where Loki, Promtail, Grafana, and demo services existed in compose but were
  not represented as components. Inline asset payload is 27,150 bytes total;
  the largest inline asset is `retail-gateway.run-script` at 7,487 bytes.
- Large business DDL files remain out of PostgreSQL. Four large MySQL DDL
  assets are recorded as remote refs only, with no inline body. This preserves
  the storage boundary: PostgreSQL carries compact deterministic startup text
  and references, not code repositories, Docker images, runtime databases,
  Evidence payloads, logs, caches, jars, or large SQL dumps.
- Non-destructive restore verification against `local-pg` now reports
  `componentGraph.configured=true`, `components=24`, `dependencies=47`,
  `assets=27`, `inlineAssetBytes=27150`, `requiredHealthChecks=20`, and
  `missingHealthChecks=0`. The `component-graph` readiness item is green, and
  `startup-assets` is now green with 15 Compose startup assets available. The
  generated file map visible in the restore report contains 25 entries,
  including the two compose files plus component-projected files under
  `compose/scripts`, `compose/mysql/init`, `compose/redis-sentinel`,
  `compose/platform`, `compose/loki`, `compose/promtail`, `compose/grafana`,
  and `compose/wiremock`.
- Restore still does not proceed by default on this live machine because
  fixed-name Docker containers already exist. That remaining block is the
  intended non-destructive Docker conflict gate, not a component asset gap.
- 2026-05-20T08:24Z implementation slice: component graph readiness now
  validates remote component asset references. An asset that is not stored
  inline must have remote Git URL plus relative path metadata; otherwise the
  `component-graph` readiness item fails before Docker startup. This makes the
  large-DDL path explicit without putting those files into PostgreSQL.
- Updated the active `scf-chain-core10-local-docker` component graph through
  the CLI so the four large MySQL DDL assets point at remote Git source
  `git@github-personal:ztcshen/open-test-sandbox-validation.git` with their
  compose-relative paths. A non-destructive restore plan now reports
  `remoteAssets=4`, `remoteAssetBytes=106813`, and
  `missingRemoteAssetRefs=0`, while keeping inline payload at 27,150 bytes.
- 2026-05-20T08:32Z implementation slice: remote component assets now have a
  restore materialization path. During dry-run, restore reports
  `componentAssets` with `plan-materialize` plus the repo clone action. During
  `--execute`, restore prepares the remote Git checkout under
  `.otsandbox/component-assets/<repo-id>` and writes the referenced file into
  the target workspace path before Docker startup. This keeps large DDL bodies
  outside PostgreSQL while making them available to Docker on a clean machine.
- Verified the active `local-pg` environment with a non-destructive restore
  plan. It reports four remote component assets for the large MySQL DDL files,
  all `ok=true`, all planned from
  `git@github-personal:ztcshen/open-test-sandbox-validation.git`, and all
  targeted under `compose/mysql/init`. The only remaining restore blockers are
  existing fixed-name Docker container conflicts and the derived Docker start
  plan block on this already-running machine.
- 2026-05-20T08:40Z implementation slice: component-level
  `HealthCheckJSON` now feeds the restore health probe list instead of only
  being counted for presence. Restore normalizes component checks from `kind`
  or `type`, supports URL, TCP, command, Compose service, and container probes,
  fills missing Compose service names from the owning component, de-duplicates
  probes, and rejects required components whose health check JSON is invalid
  or missing required fields. Health-check failures now keep the persisted
  restore phase at `health-check` instead of being masked as readiness.
- Verified the active `local-pg` environment with a non-destructive restore
  plan after the health-gate change. It reports 24 components, 20 required
  component health checks, zero missing component health checks, and
  26 Store-backed post-start health probes. The remaining failed readiness
  items are still only `docker-container-conflicts` and `docker-start-plan` on
  this already-running machine.
- 2026-05-20T08:49Z implementation slice: blocking component dependency
  ordering now uses Gonum `graph/topo` instead of project-owned DFS/topology
  code. Restore projects blocking phases (`prepare`, `startup`, `readiness`,
  and `acceptance`, plus empty phases) as provider -> consumer edges, reports a
  deterministic `blockingOrder`, and rejects blocking cycles with reportable
  component paths. Runtime edges remain outside the startup ordering gate and
  are allowed only under the separate component health/readiness model.
- Verified the active `local-pg` environment with a non-destructive restore
  plan after the dependency-gate change. It reports 24 components,
  47 dependency edges, 41 blocking edges, 6 runtime edges, a 24-component
  blocking order, and zero blocking cycles. The only failed readiness items on
  this already-running machine remain `docker-container-conflicts` and
  `docker-start-plan`.
- 2026-05-20T08:56Z implementation slice: component assets now consume the
  Gonum-derived blocking dependency order instead of only reporting it. Restore
  orders component-owned inline startup files and remote component asset
  materialization by provider-before-consumer component order, then owner,
  `applyOrder`, asset id, and target path. The generated file writer now
  honors the Store-derived `generatedFileOrder`, so the written startup files
  follow the same component graph order rather than plain path sorting.
- Verified the active `local-pg` environment with a non-destructive restore
  plan after the asset-order change. It reports a 24-component blocking order,
  25 ordered Store-generated startup files, and four remote component assets
  in materialization order. The remaining failed readiness items are still
  only `docker-container-conflicts` and `docker-start-plan` on this
  already-running machine.
- 2026-05-20T09:01Z implementation slice: `environment components replace`
  now runs the same restore-readiness component graph report before writing to
  Store. Graphs with blocking dependency cycles, invalid required component
  health checks, or remote component assets without Git URL/path metadata are
  rejected before they can become the active environment graph. `environment
  components inspect` and successful replace JSON output now include
  `restoreReadiness` so operators can see the dependency order, cycle status,
  health gate count, and remote asset readiness without running Docker.
- Verified the active `local-pg` component graph through CLI inspect, not
  direct SQL. The restore readiness summary reports 24 components,
  47 dependencies, 41 blocking edges, 6 runtime edges, 27 assets,
  24 ordered blocking components, zero blocking cycles, 20 required health
  checks, zero missing health checks, four remote assets, and zero missing
  remote asset refs.
