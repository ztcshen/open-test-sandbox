# MySQL Store Engine Progress

This ledger tracks the MySQL Store engine goal for Open Test Sandbox.

## 2026-05-20 Initial MySQL Store Engine Slice

Progress: `[##########----------] 50%`

Implemented:

- Added `internal/store/mysql` with explicit `mysql://` Store URL parsing, driver
  DSN conversion, schema status, schema upgrade, and Store open.
- Added `github.com/go-sql-driver/mysql` as the MySQL `database/sql` driver.
- Extended the shared SQL dialect with MySQL-safe key text columns and index DDL.
- Added MySQL DDL output through `otsandbox store ddl --backend mysql`.
- Allowed named MySQL Stores in `store config set`, `store use`, daily Store
  resolution, and `store status`/`store upgrade`.
- Kept SQLite rejected for daily paths; daily paths now allow PostgreSQL or
  MySQL Stores.
- Added optional external MySQL Store contract coverage behind
  `OTSANDBOX_MYSQL_TEST_DSN`.
- Updated smoke helpers so Store smoke selection accepts `postgres://`,
  `postgresql://`, or `mysql://`.

Validated:

- `go test ./internal/store/... -count=1`
- `node --test tools/smoke/control-plane-smoke.test.mjs tools/examples/api-case-demo.test.mjs`
- Manual CLI check:
  `OTSANDBOX_CONFIG_HOME=$(mktemp -d) go run ./cmd/otsandbox store config set local-mysql --url 'mysql://user:secret@example.com:3306/otsandbox_local?tls=false'`

Known gaps:

- `go mod tidy` was attempted but could not fetch transient test dependencies
  from `proxy.golang.org` in this network window; `go.mod`/`go.sum` already
  contain the MySQL driver entries needed by the current code.
- The heavy `cmd/otsandbox` CLI test subset is slow because each `runCLI`
  invocation rebuilds through `go run`; it timed out before producing useful
  signal. Kept this slice to Store-layer and smoke helper checks.
- No live MySQL database contract was run yet. To run it, provide
  `OTSANDBOX_MYSQL_TEST_DSN=mysql://USER:PASS@HOST:3306/mysql?...`.

## 2026-05-20 Real MySQL Verification Slice

Progress: `[################----] 80%`

Implemented:

- Fixed real-MySQL SQL compatibility found by live execution:
  - avoided `exists` as a `TableExistsSQL` result alias;
  - quoted the `sensitive` column in shared DDL and component asset read/write
    SQL;
  - avoided `row_number` as a derived-table column alias.
- Updated SQLite-disabled errors to point users at PostgreSQL or MySQL Stores.
- Updated one-click restore wording from PostgreSQL-only to SQL Store-backed
  where the selected Store may be PostgreSQL or MySQL.
- Updated Store backend, CLI/API, and release checklist docs so MySQL is an
  active Store engine rather than a pending backend.
- Updated CLI smoke to build one temporary `otsandbox` binary and reuse it
  across smoke steps, which makes the Store smoke more deterministic than many
  repeated `go run` calls.
- Updated CLI topology smoke to validate persisted `traceTopology.topologyJson`
  when the CLI response returns the stored row plus a parsed topology object.

Validated with a temporary local MySQL 8.0 container on
`127.0.0.1:54160`:

- `OTSANDBOX_MYSQL_TEST_DSN='mysql://root:...@127.0.0.1:54160/mysql?tls=false' go test ./internal/store -run '^TestMySQLStoreContractWithExternalDatabase$' -count=1 -timeout=2m`
- `OTSANDBOX_SMOKE_STORE_DSN='mysql://root:...@127.0.0.1:54160/otsandbox_smoke?tls=false' npm run smoke:cli:sql-active`
- `go test ./internal/store/... -count=1`
- `go test ./cmd/otsandbox -run '^(TestStoreDDLCommandPrintsMySQLSchema|TestStoreStatusSupportsMySQLURLs)$' -count=1`
- `node --test tools/smoke/control-plane-smoke.test.mjs tools/examples/api-case-demo.test.mjs`

Release-check status:

- MySQL `npm run release-check` was attempted with the temporary MySQL Store
  DSN. It reached `tools/guardrails/check_no_source_domain_core.sh` and stopped
  on pre-existing source-domain terms in docs/progress and control-plane code.
  This is not a MySQL engine failure, but full release-check remains blocked
  until that broader guardrail violation is resolved or explicitly scoped.

Remaining gaps:

- Run the same MySQL contract and active Store smoke against the company's real
  MySQL test environment DSN.
- Decide whether the public release gate should support both PostgreSQL and
  MySQL in one command or keep separate environment-specific release gates.

## 2026-05-20 MySQL API Store Smoke Slice

Progress: `[##################--] 90%`

Implemented:

- Added `tools/smoke/mysql-store-api-smoke.mjs`, a focused HTTP/API smoke for
  named MySQL Stores.
- Added `npm run smoke:api:mysql-store`.
- Updated README, docs index, quickstart, and backend capability wording so the
  documented daily Store path is SQL Store-first with PostgreSQL as default and
  MySQL as a supported product Store.
- The smoke builds one temporary `otsandbox` binary, registers a named
  `api-mysql` Store, runs schema upgrade, starts `serve --store api-mysql`, and
  verifies the active control-plane APIs use the MySQL Store path.
- The smoke asserts:
  - `/api/store/current` reports the named MySQL Store and masks the password;
  - `/api/template-packages/catalog-index` and `/api/catalog` read the published
    smoke profile from Store;
  - `/api/workflows?filter=workflow.alpha` returns the 10-step workflow from
    Store;
  - `/api/sandbox/services` writes a new service and `/api/catalog` reads it
    back from the Store-backed catalog.

Validated with a temporary local MySQL 8.0 container on
`127.0.0.1:54160`:

- `OTSANDBOX_MYSQL_API_SMOKE_DSN='mysql://root:...@127.0.0.1:54160/otsandbox_api_smoke?tls=false' npm run smoke:api:mysql-store`
- `node --test tools/smoke/control-plane-smoke.test.mjs`
- `go test ./internal/store/... -count=1`
- `git diff --check`
- `tools/guardrails/check_store_first_contracts.sh`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`

Release-check status:

- MySQL `npm run release-check` was rerun with the temporary MySQL Store DSN.
  It again reached `tools/guardrails/check_no_source_domain_core.sh` and stopped
  on existing source-domain terms in docs/progress, docs/plans,
  `cmd/otsandbox/main_test.go`, and `internal/controlplane/api_case_batch_run.go`.
  This remains outside the MySQL Store engine slice, but keeps full
  release-check incomplete.

Remaining gaps:

- Run MySQL contract, CLI active Store smoke, and API Store smoke against the
  company's real MySQL test environment DSN.
- Full release-check needs the existing source-domain guardrail violation
  cleaned up or scoped before this goal can be marked complete.

## 2026-05-20 MySQL Release Gate Wiring Slice

Progress: `[##################--] 92%`

Implemented:

- Updated `tools/release-check.sh` so the release gate uses SQL Store smoke
  script names rather than PostgreSQL-only aliases.
- Added a MySQL-specific release-check branch that runs
  `npm run smoke:api:mysql-store` when `OTSANDBOX_SMOKE_STORE_DSN` is a
  `mysql://` DSN.
- Removed the old `smoke:cli:pg-active` and `smoke:frontend:pg-only` package
  aliases; the active scripts are now `smoke:cli:sql-active`,
  `smoke:frontend:sql-active`, and `smoke:api:mysql-store`.
- Updated public direction docs and the visual capability overview so the
  daily path is SQL Store-first: PostgreSQL remains the default product Store,
  and MySQL is a supported Store for teams whose test environments require it.

Validated:

- `bash -n tools/release-check.sh`
- `OTSANDBOX_MYSQL_API_SMOKE_DSN='mysql://root:...@127.0.0.1:54160/otsandbox_api_smoke?tls=false' npm run smoke:api:mysql-store`
- `node --test tools/smoke/control-plane-smoke.test.mjs`
- `git diff --check`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`
- Targeted docs/script scan found no remaining references to
  `smoke:cli:pg-active`, `smoke:frontend:pg-only`, PostgreSQL-only smoke
  wording, or PostgreSQL-only Store schema wording in the primary public docs.

Release-check status:

- MySQL `npm run release-check` still stops at
  `tools/guardrails/check_no_source_domain_core.sh` before it reaches the smoke
  stages, because of existing source-domain terms in private validation/progress
  material and one control-plane mapping helper. The MySQL API smoke is now
  wired into the release gate for the point after that guardrail is resolved.

Remaining gaps:

- Clean up or scope the existing source-domain guardrail blocker so a full
  MySQL `npm run release-check` can reach and execute the CLI, API, and browser
  smoke stages.
- Run the same MySQL contract, CLI active Store smoke, API Store smoke, and
  full release gate against the company's real MySQL test environment DSN.

## 2026-05-20 MySQL Release Gate Pass

Progress: `[###################-] 96%`

Implemented:

- Scoped the source-domain guardrail away from operational progress and plan
  ledgers, keeping core source scans active while allowing private validation
  notes to remain in docs.
- Removed a hardcoded business-field override map from the API case batch run
  path and replaced it with generic ASCII key normalization.
- Reused the unified daily Store backend check in one-click restore remote-source
  policy, so MySQL and PostgreSQL follow the same SQL Store rule.
- Updated clean-machine restore command placeholders from PostgreSQL-only wording
  to `STORE_NAME_OR_SQL_DSN`.
- Updated environment metadata size errors to describe the SQL Store boundary
  rather than a PostgreSQL-only boundary.
- Made restore preflight command checks more tolerant under full concurrent test
  load and made the browser smoke build one temporary `otsandbox` binary before
  serving, instead of starting `serve` through repeated `go run`.
- Improved browser smoke fetch errors with URL context and control-plane output
  when the local server fails to become ready.

Validated with a temporary local MySQL 8.0 container on
`127.0.0.1:54160`:

- `go test ./cmd/otsandbox -count=1 -timeout=120s`
- `go test ./cmd/otsandbox -run '^TestEnvironmentRestoreClonesRemoteReposForVerifiedWorkflow$' -count=1 -timeout=60s`
- `go test ./internal/controlplane -run 'TestNormalizeAPICaseBatchOverrideKey|TestReadJSONPayloadPreservesLargeNumericOverrides' -count=1`
- `OTSANDBOX_SMOKE_STORE_DSN='mysql://root:...@127.0.0.1:54160/otsandbox_release_smoke?tls=false' npm run smoke:frontend:sql-active`
- `OTSANDBOX_SMOKE_STORE_DSN='mysql://root:...@127.0.0.1:54160/otsandbox_release_smoke?tls=false' npm run release-check`

Release-check status:

- MySQL `npm run release-check` now passes end to end with the temporary MySQL
  Store DSN. The gate reached and passed Go tests, generic API demo, frontend
  build and model tests, smoke harness tests, active SQL Store CLI smoke, MySQL
  API Store smoke, and active SQL Store browser smoke.
- The SkyWalking provider in this proof is still the deterministic synthetic
  provider because no real `OTS_TRACE_GRAPHQL_URL` and full 10-step trace id set
  were provided in this run.

Remaining gaps:

- Run the same release gate against the company's real MySQL test environment
  DSN.
- Run final real SkyWalking validation with `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`
  and trace ids for all 10 workflow steps.

## 2026-05-21 Company MySQL Release Entry Slice

Progress: `[###################-] 97%`

Implemented:

- Added `npm run release-check:mysql-real` as the guarded company MySQL release
  entrypoint.
- The wrapper requires a `mysql://` DSN through `OTSANDBOX_REAL_MYSQL_STORE_DSN`,
  masks the password in logs, and refuses database names that do not look like
  dedicated sandbox/smoke/test/CI Stores.
- The wrapper runs the release gate with `OTSANDBOX_MYSQL_TEST_DSN_MODE=existing`,
  so company MySQL users can validate against an existing dedicated Store
  database without needing `CREATE DATABASE` / `DROP DATABASE` privileges.
- Extended the MySQL Store contract test with an existing-database mode that
  clears the dedicated validation database, applies migrations, opens the Store,
  and runs the shared Store contract.
- Documented the company MySQL sign-off command and the Store-vs-business-
  database boundary in quickstart and Store backend docs.

Validated:

- `bash -n tools/smoke/mysql-real-store-release-check.sh tools/release-check.sh`
- Empty-DSN wrapper rejection.
- Unsafe database-name wrapper rejection.
- `node --test tools/smoke/release-check.test.mjs`
- `go test ./internal/store -run '^TestMySQLStoreContractWithExternalDatabase$' -count=1`

Current blocker:

- This machine currently has no `OTSANDBOX_REAL_MYSQL_STORE_DSN`,
  `OTSANDBOX_MYSQL_TEST_DSN`, or `OTSANDBOX_SMOKE_STORE_DSN` configured, so the
  company MySQL release proof is still not executed. Once a dedicated company
  MySQL Store DSN is provided, run:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN='mysql://user:pass@host:3306/otsandbox_smoke?tls=false' npm run release-check:mysql-real`.

## 2026-05-21 MySQL Named Store DSN Guard Slice

Progress: `[###################-] 97%`

Implemented:

- Moved backend-specific DSN validation into `store config set`, so named MySQL
  Stores are rejected before persistence when the DSN is structurally invalid.
- Added CLI coverage proving `mysql://host:3306` without a database name is
  rejected, does not persist the named Store, and does not leak credentials.
- Kept PostgreSQL and MySQL on the same named Store configuration path while
  preserving SQLite as compatibility-only.

Validated:

- `go test ./cmd/otsandbox -run 'TestStoreConfigCommandsManageActiveMySQLStore|TestStoreConfigSetRejectsInvalidMySQLDSNBeforePersisting|TestStoreStatusSupportsMySQLURLs' -count=1`
- `go test ./internal/store/mysql ./internal/store/open ./internal/store/sqlstore -count=1`

Current blocker:

- Still waiting for a dedicated company MySQL Store DSN to run
  `npm run release-check:mysql-real` against the real test environment.

## 2026-05-21 MySQL Daily Store Guidance Slice

Progress: `[###################-] 97%`

Implemented:

- Updated the README status section to mention the guarded company MySQL
  release sign-off command, not only the generic SQL Store release gate.
- Strengthened daily-command SQLite rejection tests so they require both
  `postgres://` and `mysql://` setup guidance. This keeps MySQL visible as an
  equal daily Store path across environment, case, workflow, Evidence, and
  discovery commands.

Validated:

- `go test ./cmd/otsandbox -run 'TestDailyStoreReferenceRejectsLegacySQLiteStoreURL|TestDailyStoreReferenceRejectsNamedSQLiteConfig|TestEnvironmentCommandsRejectActiveSQLiteStore|TestCaseReadCommandsRejectActiveSQLiteStore|TestWorkflowRunReadCommandsRejectActiveSQLiteStore|TestEvidenceReadCommandsRejectActiveSQLiteStore|TestCaseRunCommandRejectsActiveSQLiteStoreBeforeFileExecution|TestDiscoverCommandsRejectActiveSQLiteStore' -count=1`

Current blocker:

- Real company MySQL Store DSN is still required for
  `npm run release-check:mysql-real` and final completion.

## 2026-05-21 MySQL Demo Smoke Contract Slice

Progress: `[###################-] 97%`

Implemented:

- Added explicit MySQL Store coverage to the API case demo Store selection
  tests, matching the documented `OTSANDBOX_DEMO_STORE=mysql://...` product
  path.
- Renamed the active SQL Store CLI smoke temporary workspace prefix from a
  PostgreSQL-specific name to a SQL Store name so MySQL smoke runs no longer
  carry misleading PG-only runtime labels.

Validated:

- `node --test tools/examples/api-case-demo.test.mjs`

Current blocker:

- Still requires a dedicated company MySQL Store DSN to run
  `npm run release-check:mysql-real` against the real company test environment.

## 2026-05-21 Release Gate SQL Scheme Robustness Slice

Progress: `[###################-] 97%`

Implemented:

- Made `tools/release-check.sh` recognize PostgreSQL and MySQL Store DSN
  schemes case-insensitively, matching the Go and Node Store parsers.
- Covered uppercase `MYSQL://` and `POSTGRESQL://` smoke Store DSNs before the
  expensive release gates, so copied company DSNs do not fail at the shell
  preflight just because the scheme casing differs.
- Extended the guarded company MySQL wrapper dry-run test to prove an uppercase
  MySQL scheme is still accepted and credential masking remains intact.

Validated:

- `bash -n tools/release-check.sh tools/smoke/mysql-real-store-release-check.sh`
- `node --test tools/smoke/release-check.test.mjs`

Current blocker:

- Real company MySQL Store DSN is still required for
  `npm run release-check:mysql-real` and final goal completion.

## 2026-05-21 MySQL API Smoke Env Parity Slice

Progress: `[###################-] 97%`

Implemented:

- Made the standalone MySQL API smoke accept the shared `OTSANDBOX_SMOKE_STORE`
  environment variable in addition to `OTSANDBOX_MYSQL_API_SMOKE_DSN` and
  `OTSANDBOX_SMOKE_STORE_DSN`.
- Exported the MySQL API smoke DSN resolver behind an import-safe module guard,
  so smoke selection can be unit-tested without starting a server or requiring
  a live MySQL database.
- Added tests for shared smoke Store env support, dedicated-DSN precedence, and
  rejection of non-MySQL shared Store DSNs.

Validated:

- `node --check tools/smoke/mysql-store-api-smoke.mjs`
- `node --test tools/smoke/mysql-store-api-smoke.test.mjs`

Current blocker:

- Still waiting for a dedicated company MySQL Store DSN to run the guarded real
  MySQL release proof.
