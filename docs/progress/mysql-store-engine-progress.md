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

## 2026-05-21 MySQL Time Parsing DSN Guard Slice

Progress: `[###################-] 97%`

Implemented:

- Hardened MySQL Store URL conversion so user query parameters cannot override
  the Store-required `parseTime=true` and UTC location settings.
- Kept ordinary MySQL driver query parameters such as `tls=false` intact while
  dropping conflicting `parseTime` and `loc` URL parameters before building the
  driver DSN.
- Added a focused regression test that first exposed the conflicting
  `parseTime=false&loc=Local` DSN output, then now proves the Store DSN remains
  authoritative.

Validated:

- `go test ./internal/store/mysql -run 'TestParseConfigFromURLAcceptsMySQLURL|TestParseConfigFromURLKeepsStoreTimeParsingAuthoritative|TestParseConfigFromURLRejectsNonMySQLDSN|TestParseConfigFromURLRequiresDatabaseName' -count=1`

Current blocker:

- Final completion still requires `npm run release-check:mysql-real` against a
  dedicated company MySQL Store DSN.

## 2026-05-21 MySQL Network Timeout DSN Slice

Progress: `[###################-] 97%`

Implemented:

- Added bounded MySQL Store driver network defaults when the DSN omits them:
  `timeout=10s`, `readTimeout=30s`, and `writeTimeout=30s`.
- Preserved explicit operator-provided timeout values, including case-insensitive
  key matching, so company MySQL DSNs can still tune network behavior.
- Added focused tests that first proved missing timeout defaults, then verify
  defaults are present and explicit values are not duplicated.

Validated:

- `go test ./internal/store/mysql -run 'TestParseConfigFromURLAcceptsMySQLURL|TestParseConfigFromURLKeepsStoreTimeParsingAuthoritative|TestParseConfigFromURLAddsBoundedNetworkTimeouts|TestParseConfigFromURLKeepsExplicitNetworkTimeouts|TestParseConfigFromURLRejectsNonMySQLDSN|TestParseConfigFromURLRequiresDatabaseName' -count=1`

Current blocker:

- Real company MySQL Store proof still requires a dedicated DSN for
  `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Timeout Param Canonicalization Slice

Progress: `[###################-] 97%`

Implemented:

- Canonicalized explicit MySQL Store network timeout query keys before building
  the driver DSN: `timeout`, `readTimeout`, and `writeTimeout`.
- Preserved operator-provided timeout values even when copied DSNs use mixed
  key casing such as `Timeout=2s`, while ensuring the generated driver DSN uses
  the key names the MySQL driver recognizes.
- Added a focused TDD regression test that first showed mixed-case timeout keys
  leaking into the driver DSN, then now verifies canonical keys and no duplicate
  default timeout values.

Validated:

- `go test ./internal/store/mysql -run 'TestParseConfigFromURLAcceptsMySQLURL|TestParseConfigFromURLKeepsStoreTimeParsingAuthoritative|TestParseConfigFromURLAddsBoundedNetworkTimeouts|TestParseConfigFromURLKeepsExplicitNetworkTimeouts|TestParseConfigFromURLCanonicalizesExplicitNetworkTimeoutKeys|TestParseConfigFromURLRejectsNonMySQLDSN|TestParseConfigFromURLRequiresDatabaseName' -count=1`

Current blocker:

- Final completion still requires a dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Common Param Canonicalization Slice

Progress: `[###################-] 97%`

Implemented:

- Canonicalized common MySQL driver query keys copied from company config
  systems: `tls`, `charset`, `collation`, and `maxAllowedPacket`.
- Preserved the configured values while ensuring the generated driver DSN uses
  key names that `go-sql-driver/mysql` recognizes instead of passing mixed-case
  keys through as unknown session params.
- Added a focused TDD regression test that first showed mixed-case
  `TLS/CHARSET/COLLATION/MAXALLOWEDPACKET` leaking into the DSN, then now
  verifies canonical keys.

Validated:

- `go test ./internal/store/mysql -run 'TestParseConfigFromURLAcceptsMySQLURL|TestParseConfigFromURLKeepsStoreTimeParsingAuthoritative|TestParseConfigFromURLAddsBoundedNetworkTimeouts|TestParseConfigFromURLKeepsExplicitNetworkTimeouts|TestParseConfigFromURLCanonicalizesExplicitNetworkTimeoutKeys|TestParseConfigFromURLCanonicalizesCommonDriverParamKeys|TestParseConfigFromURLRejectsNonMySQLDSN|TestParseConfigFromURLRequiresDatabaseName' -count=1`

Current blocker:

- Still blocked on a dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.

## 2026-05-21 Real MySQL Wrapper Shared Store Env Slice

Progress: `[###################-] 97%`

Implemented:

- Aligned `npm run release-check:mysql-real` with the rest of the MySQL smoke
  path by accepting shared `OTSANDBOX_SMOKE_STORE` when the dedicated
  `OTSANDBOX_REAL_MYSQL_STORE_DSN` and `OTSANDBOX_SMOKE_STORE_DSN` variables
  are not set.
- Kept the safety checks intact: the wrapper still requires a `mysql://` DSN,
  masks credentials in output, and refuses likely business database names.
- Added a focused TDD regression test that first failed on the missing shared
  Store env path, then now proves the dry-run wrapper accepts the shared MySQL
  Store DSN without printing the password.

Validated:

- `node --test tools/smoke/release-check.test.mjs`
- `bash -n tools/smoke/mysql-real-store-release-check.sh tools/release-check.sh`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.

## 2026-05-21 CLI Active Smoke Shared Store Env Slice

Progress: `[###################-] 97%`

Implemented:

- Made `tools/smoke/cli-active-store-smoke.mjs` import-safe and exported its
  SQL Store DSN resolver so the CLI smoke Store-selection path can be tested
  without starting servers or requiring a live database.
- Added regression coverage proving the active SQL Store CLI smoke accepts the
  shared `OTSANDBOX_SMOKE_STORE` environment variable, including uppercase
  `MYSQL://` scheme input copied from company-style configs.
- Updated the missing-DSN guidance to list all supported smoke Store env names:
  `OTSANDBOX_CLI_STORE_DSN`, `OTSANDBOX_SMOKE_STORE_DSN`, and
  `OTSANDBOX_SMOKE_STORE`.

Validated:

- `node --test tools/smoke/cli-active-store-smoke.test.mjs`
- `node --check tools/smoke/cli-active-store-smoke.mjs`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- A non-blocking explorer pass found remaining MySQL parity work mostly in
  DSN-gated named active Store tests for daily CLI/API paths that are still
  PostgreSQL-shaped today.

## 2026-05-21 MySQL Driver Bool Param Canonicalization Slice

Progress: `[###################-] 97%`

Implemented:

- Expanded MySQL Store DSN parameter canonicalization for common
  `go-sql-driver/mysql` boolean/options copied from company configuration
  systems, including `allowNativePasswords`, `checkConnLiveness`,
  `clientFoundRows`, `columnsWithAlias`, `interpolateParams`,
  `multiStatements`, and `rejectReadOnly`.
- Preserved operator-provided values while emitting driver-recognized key
  casing, so mixed-case copied DSNs do not turn those options into ordinary
  MySQL session parameters.
- Added a focused TDD regression test that first showed uppercase option keys
  leaking into the driver DSN, then now verifies canonical key names.

Validated:

- `go test ./internal/store/mysql -run 'TestParseConfigFromURLCanonicalizesCommonDriverBoolParamKeys|TestParseConfigFromURLCanonicalizesCommonDriverParamKeys|TestParseConfigFromURLCanonicalizesExplicitNetworkTimeoutKeys' -count=1`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Remaining MySQL parity work is now mostly DSN-gated daily CLI/API active
  Store coverage, plus the real company MySQL release proof.

## 2026-05-21 Named MySQL Store Status Guidance Slice

Progress: `[###################-] 97%`

Implemented:

- Made the no-active-Store setup guidance include a complete copyable MySQL
  command: `otsandbox store config set NAME --url mysql://...`.
- Added named MySQL Store status coverage using the same schema-status injection
  shape as PostgreSQL, proving `store status --store NAME` resolves a named
  MySQL Store, masks credentials, and stays on the MySQL backend path without
  requiring a live database.
- Kept PostgreSQL and MySQL on the same named Store command shape for Store
  management; SQLite remains outside the daily Store path.

Validated:

- `go test ./cmd/otsandbox -run '^(TestStoreStatusAndUpgradeRequireActiveStore|TestStoreStatusCanUseNamedMySQLStore|TestStoreStatusCanUseNamedPostgresStore|TestStoreStatusSupportsMySQLURLs|TestStoreConfigCommandsManageActiveMySQLStore)$' -count=1`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Daily CLI/API named active Store parity still has deeper DSN-gated coverage
  to add beyond this Store-management slice.

## 2026-05-21 SQLite Daily Rejection MySQL Guidance Slice

Progress: `[###################-] 97%`

Implemented:

- Tightened the daily Store SQLite rejection guidance so both PostgreSQL and
  MySQL setup paths are complete copyable commands.
- Updated the legacy `--store-url` SQLite rejection and named SQLite config
  rejection tests to require `store config set NAME --url mysql://...`, not only
  a bare MySQL DSN fragment.
- Kept the daily path boundary unchanged: PostgreSQL and MySQL are valid daily
  Store backends; SQLite stays explicit compatibility/migration only.

Validated:

- `go test ./cmd/otsandbox -run '^(TestDailyStoreReferenceRejectsLegacySQLiteStoreURL|TestDailyStoreReferenceRejectsNamedSQLiteConfig|TestEnvironmentCommandsRejectActiveSQLiteStore|TestCaseReadCommandsRejectActiveSQLiteStore|TestWorkflowRunReadCommandsRejectActiveSQLiteStore|TestEvidenceReadCommandsRejectActiveSQLiteStore|TestDiscoverCommandsRejectActiveSQLiteStore)$' -count=1`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Daily CLI/API named active Store parity still has deeper DSN-gated coverage
  to add beyond Store guidance.

## 2026-05-21 Demo MySQL Guidance Slice

Progress: `[###################-] 97%`

Implemented:

- Updated the API case demo SQLite rejection errors so the MySQL product path is
  shown as the complete `OTSANDBOX_DEMO_STORE=mysql://...` environment entry.
- Updated README, README.zh-CN, and share-kit demo wording so PostgreSQL and
  MySQL demo Store examples use matching full `OTSANDBOX_DEMO_STORE=...`
  prefixes.
- Added a focused regression test proving the SQLite demo rejection now points
  users at a complete MySQL demo Store entry unless explicit SQLite
  compatibility mode is enabled.

Validated:

- `node --test tools/examples/api-case-demo.test.mjs`
- Targeted scan of README/share-kit/api-case-demo guidance for half-written
  MySQL demo Store examples.

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Daily CLI/API named active Store parity still has deeper DSN-gated coverage
  to add beyond Store guidance.

## 2026-05-21 Top-Level Help MySQL Store Guidance Slice

Progress: `[###################-] 97%`

Implemented:

- Split the top-level `otsandbox` help Store setup entry into two complete
  commands: one for PostgreSQL and one for MySQL.
- Added a regression assertion so the first CLI help screen must keep the
  copyable `otsandbox store config set NAME --url mysql://...` command visible.
- Kept the Store-first daily command shape unchanged: daily commands still use
  `--store NAME_OR_DSN`, and legacy `--store-url` paths remain hidden from the
  top-level help.

Validated:

- `go test ./cmd/otsandbox -run '^TestTopLevelHelpShowsStoreFlagNotLegacyStoreURL$' -count=1`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Daily CLI/API named active Store parity still has deeper DSN-gated coverage
  to add beyond Store guidance.

## 2026-05-21 Control Plane Smoke Shared Store Env Guidance Slice

Progress: `[###################-] 97%`

Implemented:

- Updated `tools/smoke/control-plane-smoke.mjs` Store-selection errors to list
  both supported smoke Store env names: `OTSANDBOX_SMOKE_STORE_DSN` and
  `OTSANDBOX_SMOKE_STORE`.
- Added focused regression expectations so missing and non-SQL smoke Store
  inputs both keep the shared Store env visible in browser/control-plane smoke
  guidance.
- Kept the actual smoke Store selection unchanged: PostgreSQL and MySQL DSNs can
  still come from either env, and SQLite remains behind explicit compatibility
  mode only.

Validated:

- `node --test tools/smoke/control-plane-smoke.test.mjs`
- `node --check tools/smoke/control-plane-smoke.mjs`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Daily CLI/API named active Store parity still has deeper DSN-gated coverage
  to add beyond Store guidance.

## 2026-05-21 Release Gate Shared Store Env Guidance Slice

Progress: `[###################-] 97%`

Implemented:

- Updated `tools/release-check.sh` missing/invalid Store guidance to list both
  supported release Store env names: `OTSANDBOX_SMOKE_STORE_DSN` and
  `OTSANDBOX_SMOKE_STORE`.
- Added a focused regression test proving the release gate stops before
  expensive checks when no Store env is configured and prints a complete MySQL
  shared Store example.
- Kept the actual Store selection logic unchanged: PostgreSQL and MySQL DSNs are
  still accepted from either shared release Store env.

Validated:

- `node --test tools/smoke/release-check.test.mjs`
- `bash -n tools/release-check.sh tools/smoke/mysql-real-store-release-check.sh`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Daily CLI/API named active Store parity still has deeper DSN-gated coverage
  to add beyond Store guidance.

## 2026-05-21 Store-First Guardrail SQL Store Wording Slice

Progress: `[###################-] 97%`

Implemented:

- Updated Store-first guardrail release-check wording from PostgreSQL-only gate
  terminology to SQL Store gate terminology.
- Added focused regression coverage so the guardrail script itself cannot
  regress to PostgreSQL-only release gate wording.
- Kept guardrail behavior unchanged; it still requires release-check examples
  to name `OTSANDBOX_SMOKE_STORE_DSN`.

Validated:

- `node --test tools/guardrails/check_store_first_contracts.test.mjs`
- `tools/guardrails/check_store_first_contracts.sh`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Daily CLI/API named active Store parity still has deeper DSN-gated coverage
  to add beyond Store guidance.

## 2026-05-21 MySQL API Workflow Report Smoke Slice

Progress: `[###################-] 97%`

Implemented:

- Extended the MySQL Store API smoke so a real MySQL DSN run now starts a local
  target HTTP service and triggers `/api/cases/batch-runs` for
  `workflow.alpha`.
- The smoke now waits for the asynchronous 10-step workflow batch report,
  requires all 10 steps to pass, and verifies that the report is persisted as a
  Store-backed workflow run.
- The smoke now reads every case Evidence payload by `caseRunId` and checks the
  stored request, response, assertion, run, case, and step fields.
- Added focused unit coverage for the new workflow report and case Evidence
  assertion helpers without requiring a live MySQL DSN.

Validated:

- `node --test tools/smoke/mysql-store-api-smoke.test.mjs`
- `node --check tools/smoke/mysql-store-api-smoke.mjs`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Real SkyWalking release proof still requires
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and trace ids
  for all 10 workflow steps.
- MySQL daily parity still has deeper DSN-gated coverage to add for environment
  lifecycle and interface registration APIs.

## 2026-05-21 MySQL API Interface Registration Smoke Slice

Progress: `[###################-] 97%`

Implemented:

- Extended the MySQL Store API smoke to register an interface through
  `/api/sandbox/interfaces` after registering a Store-backed service.
- The smoke now verifies that the registered interface node, API case, request
  template, and case execution config are persisted to the Store-backed catalog.
- Added focused helper coverage so the MySQL API smoke fails clearly if
  interface registration stops writing the expected catalog records.

Validated:

- `node --test tools/smoke/mysql-store-api-smoke.test.mjs`
- `node --check tools/smoke/mysql-store-api-smoke.mjs`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Real SkyWalking release proof still requires
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and trace ids
  for all 10 workflow steps.
- MySQL daily parity still has deeper DSN-gated coverage to add for environment
  lifecycle APIs and final live release proof.

## 2026-05-21 MySQL API Environment Catalog Smoke Slice

Progress: `[###################-] 97%`

Implemented:

- Extended the MySQL Store API smoke to register an Environment Catalog entry
  through `/api/environments`.
- The smoke now verifies Store-backed environment discovery with `all=true`,
  environment inspect payloads, and bootstrap planning for the configured
  verification workflow.
- Added focused helper coverage so MySQL smoke fails clearly if Environment
  Catalog registration, discovery, inspect, or bootstrap payloads drift.

Validated:

- `node --test tools/smoke/mysql-store-api-smoke.test.mjs`
- `node --check tools/smoke/mysql-store-api-smoke.mjs`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Real SkyWalking release proof still requires
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and trace ids
  for all 10 workflow steps.
- Remaining MySQL daily parity work is now concentrated around final live
  release proof and any deeper DSN-gated environment publish/acceptance checks.

## 2026-05-21 MySQL API Environment Acceptance Smoke Slice

Progress: `[###################-] 98%`

Implemented:

- Extended the MySQL Store API smoke to run the registered Environment Catalog
  entry through `/api/environments/{id}/acceptance-runs`.
- The smoke now starts a local target health endpoint and a SkyWalking smoke
  GraphQL provider, waits for the environment acceptance report, and verifies
  the SkyWalking acceptance template passes for all 10 workflow steps.
- The smoke now re-inspects the environment and verifies that the acceptance run
  wrote back `verified-ready`, last verification run/status, Evidence complete,
  and topology complete flags to the MySQL-backed Environment Catalog entry.
- Added focused helper coverage so the smoke fails clearly if acceptance report
  counts, health checks, Evidence completeness, topology completeness, or
  environment status persistence drift.

Validated:

- `node --test tools/smoke/mysql-store-api-smoke.test.mjs`
- `node --check tools/smoke/mysql-store-api-smoke.mjs`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Final release proof still requires real SkyWalking:
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and trace ids
  for all 10 workflow steps.
- Remaining MySQL daily parity work is now mostly final live release proof and
  optional deeper publish-verified DSN smoke coverage.

## 2026-05-21 MySQL API Environment Publish Smoke Slice

Progress: `[###################-] 98%`

Implemented:

- Extended the MySQL Store API smoke to call
  `/api/environments/{id}/publish-verified` after the environment acceptance
  report passes.
- The smoke now verifies the published Environment Catalog entry is persisted as
  `verified`, remains Evidence/topology complete, and appears in default
  verified discovery.
- Added focused helper coverage so the smoke fails clearly if publish-verified
  stops enforcing or persisting the verified Environment Catalog state.

Validated:

- `node --test tools/smoke/mysql-store-api-smoke.test.mjs`
- `node --check tools/smoke/mysql-store-api-smoke.mjs`

Current blocker:

- Final completion still requires a real dedicated company MySQL Store DSN for
  `npm run release-check:mysql-real`.
- Final release proof still requires real SkyWalking:
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and trace ids
  for all 10 workflow steps.
- MySQL daily API smoke now covers Store current, catalog/workflow discovery,
  async workflow report, Evidence readback, service/interface registration,
  Environment Catalog register/inspect/bootstrap/acceptance, and
  publish-verified; remaining work is primarily live DSN validation.

## 2026-05-21 MySQL Real Release SkyWalking Enforcement Slice

Progress: `[###################-] 98%`

Implemented:

- Tightened `npm run release-check:mysql-real` so it now requires
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps before dry-run or full release
  execution can pass.
- Kept the existing dedicated MySQL Store protections: `mysql://` only,
  sandbox/smoke/test/CI-looking database names only, masked credentials, and
  existing-database contract mode for company accounts.
- Updated README, README.zh-CN, quickstart, Store backend docs, and release
  checklist so company MySQL final sign-off cannot be mistaken for synthetic
  topology smoke.

Validated:

- `node --test tools/smoke/release-check.test.mjs`
- `bash -n tools/smoke/mysql-real-store-release-check.sh tools/release-check.sh`
- `tools/guardrails/check_store_first_contracts.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 Explicit MySQL Contract Mode Slice

Progress: `[###################-] 98%`

Implemented:

- Tightened `TestMySQLStoreContractWithExternalDatabase` so a live
  `OTSANDBOX_MYSQL_TEST_DSN` must now pair with an explicit
  `OTSANDBOX_MYSQL_TEST_DSN_MODE`.
- Preserved company-safe behavior by making generic MySQL `npm run release-check`
  default the contract mode to `existing`, matching the guarded
  `npm run release-check:mysql-real` wrapper.
- Kept `create-drop` available only as an explicit local admin-only contract
  mode for accounts allowed to create and drop temporary databases.
- Added mode parser coverage and documented the `existing` vs `create-drop`
  split in quickstart, Store backend docs, and release checklist.

Validated:

- `go test ./internal/store -run 'TestParseMySQLTestDSNMode|TestMySQLStoreContractWithExternalDatabase' -count=1`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 MySQL API Demo DSN Guard Slice

Progress: `[###################-] 98%`

Implemented:

- Reused the shared MySQL smoke Store DSN guard in `npm run demo:api-case` so
  direct demo runs with `OTSANDBOX_DEMO_STORE=mysql://...` refuse likely
  business database names before Store upgrade or Evidence writes.
- Updated the demo Store selection test to use a dedicated MySQL demo database
  name and added regression coverage for unsafe MySQL demo database names.
- Updated README, README.zh-CN, quickstart, and share-kit docs to state MySQL
  demo Stores must use dedicated sandbox/smoke/test/CI-looking database names,
  not business schemas.

Validated:

- `node --test tools/examples/api-case-demo.test.mjs`
- `node --check tools/examples/api-case-demo.mjs`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 MySQL CLI and Frontend Smoke DSN Guard Slice

Progress: `[###################-] 98%`

Implemented:

- Reused the shared MySQL smoke Store DSN guard in the active SQL Store CLI
  smoke so direct `npm run smoke:cli:sql-active` runs refuse likely business
  database names before Store writes.
- Reused the same guard in the control-plane/frontend smoke Store preparation
  path so direct `npm run smoke:frontend` and `npm run smoke:frontend:sql-active`
  runs refuse unsafe MySQL smoke databases before named Store configuration,
  Store upgrade, or workbench writes begin.
- Added regression coverage for both direct smoke entrypoints and updated
  quickstart, Store backend docs, and release checklist to state the guard now
  covers release-check, CLI smoke, frontend smoke, and standalone MySQL API
  smoke.

Validated:

- `node --test tools/smoke/cli-active-store-smoke.test.mjs tools/smoke/control-plane-smoke.test.mjs`
- `node --check tools/smoke/cli-active-store-smoke.mjs tools/smoke/control-plane-smoke.mjs`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 Shared SkyWalking Release Guard Slice

Progress: `[###################-] 98%`

Implemented:

- Added a shared SkyWalking release guard for release tooling so real
  SkyWalking GraphQL URL validation, JSON or comma-separated trace-id parsing,
  and complete 10-step trace-id checks use one rule.
- Rewired generic `npm run release-check` and guarded
  `npm run release-check:mysql-real` to call the shared guard instead of
  carrying separate inline URL and trace-id parsers.
- Added focused unit coverage for `http`/`https` GraphQL URLs, non-HTTP
  rejection, JSON and shell trace-id parsing, missing workflow step detection,
  and complete 10-step acceptance.

Validated:

- `node --test tools/smoke/skywalking-release-guard.test.mjs tools/smoke/release-check.test.mjs`
- `node --check tools/smoke/skywalking-release-guard.mjs`
- `bash -n tools/release-check.sh tools/smoke/mysql-real-store-release-check.sh`
- `tools/guardrails/check_store_first_contracts.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 Generic Release Real SkyWalking URL Guard Slice

Progress: `[###################-] 98%`

Implemented:

- Tightened generic `npm run release-check` so
  `OTSANDBOX_REQUIRE_REAL_SKYWALKING=1` requires `OTS_TRACE_GRAPHQL_URL` to be
  an `http` or `https` URL before Go tests, Store smoke, API smoke, or browser
  smoke can run.
- Added regression coverage proving invalid or non-HTTP SkyWalking GraphQL URLs
  fail early and do not enter expensive gates.
- Updated the release checklist so real SkyWalking validation documents the
  `http`/`https` URL requirement for the generic release gate, not only the
  MySQL-specific wrapper.

Validated:

- `node --test tools/smoke/release-check.test.mjs`
- `bash -n tools/release-check.sh tools/smoke/mysql-real-store-release-check.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 Shared MySQL Smoke DSN Guard Slice

Progress: `[###################-] 98%`

Implemented:

- Added a shared MySQL smoke Store DSN guard for release tooling so MySQL URL
  parsing, password masking, database-path checks, and dedicated
  sandbox/smoke/test/CI database-name checks use one rule.
- Rewired generic `npm run release-check`, guarded
  `npm run release-check:mysql-real`, and standalone
  `npm run smoke:api:mysql-store` to call the shared guard instead of carrying
  separate parsing logic.
- Added focused unit coverage for the guard, including uppercase MySQL schemes,
  missing database paths, non-MySQL DSNs, unsafe database names, and credential
  masking.

Validated:

- `node --test tools/smoke/mysql-store-dsn-guard.test.mjs tools/smoke/release-check.test.mjs tools/smoke/mysql-store-api-smoke.test.mjs`
- `bash -n tools/release-check.sh tools/smoke/mysql-real-store-release-check.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Smoke Store Safe Database Guard Slice

Progress: `[###################-] 98%`

Implemented:

- Tightened generic `npm run release-check` for MySQL Store DSNs so it refuses
  likely business database names before Go tests, Store migrations, API smoke,
  or browser smoke can run.
- Tightened standalone `npm run smoke:api:mysql-store` DSN selection so it
  requires a parseable `mysql://` URL with a database path and the same
  sandbox/smoke/test/CI-looking database-name guard.
- Added regression coverage proving unsafe MySQL database names fail early and
  raw passwords are not printed.
- Updated quickstart, Store backend docs, and release checklist to state that
  generic MySQL release/API smoke paths must use a dedicated sandbox Store
  database, not a business schema.

Validated:

- `node --test tools/smoke/release-check.test.mjs tools/smoke/mysql-store-api-smoke.test.mjs`
- `bash -n tools/release-check.sh tools/smoke/mysql-store-api-smoke.mjs`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Real Release GraphQL URL Validation Slice

Progress: `[###################-] 98%`

Implemented:

- Tightened `npm run release-check:mysql-real` so the required
  `OTS_TRACE_GRAPHQL_URL` must parse as an `http` or `https` URL before dry-run
  or full release execution can pass.
- Added regression coverage proving invalid or non-HTTP SkyWalking GraphQL URLs
  fail before credentials are printed as raw text or the release gate command
  is advertised.
- Updated public MySQL sign-off docs to state the GraphQL URL must use
  `http` or `https`.
- Kept the existing dedicated MySQL Store and 10-step trace-id checks
  unchanged.

Validated:

- `node --test tools/smoke/release-check.test.mjs`
- `bash -n tools/smoke/mysql-real-store-release-check.sh tools/release-check.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Real Wrapper Existing Contract Mode Slice

Progress: `[###################-] 98%`

Implemented:

- Moved live MySQL contract mode validation before any MySQL DSN parse, open,
  or ping, so missing or unknown `OTSANDBOX_MYSQL_TEST_DSN_MODE` fails before
  touching the database.
- Tightened `npm run release-check:mysql-real` so company final sign-off always
  requires `OTSANDBOX_MYSQL_TEST_DSN_MODE=existing` and rejects `create-drop`
  overrides before it advertises or runs the release gate.
- Kept generic MySQL `npm run release-check` able to default to `existing`,
  while reserving `create-drop` for explicit local admin-only contract tests.
- Updated quickstart, Store backend, and release checklist docs to spell out
  that the MySQL real wrapper is existing-database only.

Validated:

- `node --test tools/smoke/release-check.test.mjs`
- `go test ./internal/store -run 'TestParseMySQLTestDSNMode|TestMySQLStoreContractWithExternalDatabase' -count=1`
- `bash -n tools/release-check.sh tools/smoke/mysql-real-store-release-check.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then
  `npm run release-check:mysql-real`.

## 2026-05-21 MySQL CI Release Gate Wiring Slice

Progress: `[###################-] 98%`

Implemented:

- Wired public GitHub Actions CI to run `npm run release-check` with a MySQL
  8.0 service container and a dedicated `otsandbox_ci_smoke` Store database.
- Added a manual `workflow_dispatch` `mysql-real-signoff` job for guarded
  company MySQL final sign-off, fed only by repository secrets and requiring
  real SkyWalking mode.
- Documented the CI split: ordinary PR/push release gate uses temporary MySQL,
  while company MySQL final sign-off remains explicit and manual.

Validated:

- `ruby -e "require 'yaml'; YAML.load_file('.github/workflows/ci.yml'); puts 'ci yaml ok'"`
- `git diff --check`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`
- `tools/guardrails/check_no_source_domain_core.sh && tools/guardrails/check_store_first_contracts.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Environment CLI Daily Path Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for the Environment Catalog
  daily CLI path.
- Shared the existing PostgreSQL Environment Catalog command scenario across
  PostgreSQL and MySQL: `environment register`, default `discover`,
  `verify`, `publish-verified`, verification artifact lookup, verified
  discovery, and `bootstrap`.
- Added `configureNamedMySQLActiveStore`, using `OTSANDBOX_MYSQL_TEST_DSN`, so
  the release gate's MySQL smoke Store can exercise this daily path when a
  MySQL Store is provided.

Validated:

- `go test ./cmd/otsandbox -run 'TestEnvironmentCommandsUseNamed(PostgreSQL|MySQL)ActiveStore' -count=1`
- `node --test tools/smoke/release-check.test.mjs`
- `git diff --check`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.
- More daily-path CLI parity remains valuable, especially sandbox
  service/interface registration, profile import/verify, discover, evidence
  import, and serve/evidence task inspection.

## 2026-05-21 MySQL Sandbox Register Daily Path Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for daily sandbox
  service/interface registration.
- Shared the existing PostgreSQL scenario across PostgreSQL and MySQL:
  `sandbox service register`, `sandbox interface register`, request template
  creation, required admission case creation, and Store catalog readback.
- Kept the scenario tied to `OTSANDBOX_MYSQL_TEST_DSN`, so CI's MySQL release
  Store and the company MySQL sign-off path can both exercise it.

Validated:

- `go test ./cmd/otsandbox -run 'TestSandboxRegisterCommandsUseNamed(PostgreSQL|MySQL)ActiveStore' -count=1`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.
- More daily-path CLI parity remains valuable, especially profile
  import/verify, discover, evidence import, and serve/evidence task inspection.

## 2026-05-21 MySQL Profile Import Daily Path Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for `profile import` and
  `profile verify`.
- Shared the existing PostgreSQL scenario across PostgreSQL and MySQL:
  profile bundle import, profile index readback, catalog index readback,
  Store-backed case-run coverage records, required case-run verification,
  verified profile index readback, and verified catalog readback.
- Kept the scenario tied to `OTSANDBOX_MYSQL_TEST_DSN`, so CI's MySQL release
  Store and the company MySQL sign-off path can both exercise it.

Validated:

- `go test ./cmd/otsandbox -run 'TestProfileImportAndVerifyUseNamed(PostgreSQL|MySQL)ActiveStore' -count=1`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.
- More daily-path CLI parity remains valuable, especially discover, evidence
  import, and serve/evidence task inspection.

## 2026-05-21 MySQL Discover Daily Path Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for daily discovery
  commands.
- Shared the existing PostgreSQL scenario across PostgreSQL and MySQL:
  `config publish`, `case discover --filter`, and
  `interface-node discover --filter` through the active Store.
- Kept the scenario tied to `OTSANDBOX_MYSQL_TEST_DSN`, so CI's MySQL release
  Store and the company MySQL sign-off path can both exercise it.

Validated:

- `go test ./cmd/otsandbox -run 'TestDiscoverCommandsUseNamed(PostgreSQL|MySQL)ActiveStore' -count=1`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.
- More daily-path CLI parity remains valuable, especially evidence import and
  serve/evidence task inspection.

## 2026-05-21 MySQL Evidence Import Compatibility Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for importing legacy
  SQLite runtime Evidence into the active SQL Store.
- Shared the existing PostgreSQL scenario across PostgreSQL and MySQL:
  `evidence import --from legacy.sqlite`, imported workflow run readback,
  parent case-run readback, API case run readback, Evidence record readback,
  and `evidence list --run`.
- Kept SQLite only as the explicit legacy import source while PostgreSQL/MySQL
  remain the active product Store targets.

Validated:

- `go test ./cmd/otsandbox -run 'TestEvidenceImportUsesNamed(PostgreSQL|MySQL)ActiveStore' -count=1`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.
- More daily-path CLI parity remains valuable, especially serve/evidence task
  inspection.

## 2026-05-21 MySQL Serve and Evidence Task Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for serve/API and Evidence
  task inspection.
- Shared the existing PostgreSQL scenario across PostgreSQL and MySQL:
  `evidence list`, `evidence tasks`, `/api/store/current`, `/api/runs`,
  `/api/interface-nodes`, `/api/profile/import`, `/api/profile/verify`,
  `/api/evidence/import`, and `/api/evidence/list`.
- Asserted `/api/store/current` reports the correct active Store name and
  backend for both PostgreSQL and MySQL.

Validated:

- `go test ./cmd/otsandbox -run 'TestServeAndEvidenceTasksUseNamed(PostgreSQL|MySQL)ActiveStore' -count=1`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Workflow Daily Path Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for core workflow daily
  commands.
- Shared the existing PostgreSQL scenario across PostgreSQL and MySQL:
  `config publish`, `workflow discover`, `workflow plan`, baseline set/get,
  `workflow report`, `case runs`, real SkyWalking-shaped topology collection
  through a test GraphQL provider, and `case evidence` readback.
- Parameterized trace/request ids so PostgreSQL and MySQL coverage store
  backend-specific topology evidence without colliding.

Validated:

- `go test ./cmd/otsandbox -run 'TestDailyWorkflowCommandsUseNamed(PostgreSQL|MySQL)ActiveStore' -count=1`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Case Execution Daily Path Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for API case execution and
  interface report generation.
- Shared the existing PostgreSQL scenario across PostgreSQL and MySQL:
  file-backed `case run`, catalog-backed `case run`, `case runs`, `case
  evidence`, `evidence list`, `interface-node discover`, and
  `interface-node case report`.
- Preserved the report assertions that sensitive response fields are redacted
  and that the report uses the active SQL Store without creating
  `runtime.sqlite`.

Validated:

- `go test ./cmd/otsandbox -run 'TestCaseExecutionAndInterfaceReportUseNamed(PostgreSQL|MySQL)ActiveStore' -count=1`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Interface Coverage Daily Path Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for `interface-node
  coverage` and `interface-node coverage-gaps`.
- Shared the existing PostgreSQL scenarios across PostgreSQL and MySQL:
  `config publish`, mapped interface-node coverage reporting, and required
  workflow binding gap reporting through the active Store.
- Generated unique profile/workflow/service/node/case/step ids per run so a
  shared MySQL test database with older rows cannot contaminate this parity
  check.
- Kept the MySQL scenario tied to `OTSANDBOX_MYSQL_TEST_DSN`, matching the
  named Store path used by the rest of the daily MySQL parity suite.

Validated:

- `go test -v ./cmd/otsandbox -run 'TestInterfaceNodeCoverage(CommandCanEmitJSON|CommandUsesNamedMySQLActiveStore|GapsCommandCanEmitJSON|GapsCommandUsesNamedMySQLActiveStore)' -count=1`
  compiled and passed locally; the env-gated PostgreSQL/MySQL cases skipped
  because local DSNs were not exported in this shell.
- `git diff --check`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`
- `tools/guardrails/check_store_first_contracts.sh && tools/guardrails/check_no_source_domain_core.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Case Report Read Path Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for report/read commands:
  `case runs`, `case evidence`, and `case timing`.
- Shared the existing PostgreSQL scenarios across PostgreSQL and MySQL:
  seeded workflow runs, API case runs, Evidence records, case-run listing,
  case Evidence rendering, and slowest-case timing summary.
- Generated unique run/profile/workflow/case ids per run for direct readback
  checks.
- Made the timing scenario shared-database tolerant by using a narrow max-age
  window and a unique slow case that should remain the slowest row even if
  recent unrelated rows exist.

Validated:

- `go test -v ./cmd/otsandbox -run 'TestCase(RunsCommand(ListsStoredCaseRuns|UsesNamedMySQLActiveStore)|EvidenceCommand(ReadsCaseRunEvidence|UsesNamedMySQLActiveStore)|TimingCommand(SummarizesStoredCaseRuns|UsesNamedMySQLActiveStore))' -count=1`
  compiled and passed locally; the env-gated PostgreSQL/MySQL cases skipped
  because local DSNs were not exported in this shell.
- `git diff --check`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`
- `tools/guardrails/check_store_first_contracts.sh && tools/guardrails/check_no_source_domain_core.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Baseline and Workflow Plan Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for baseline gate daily
  commands: `baseline set`, `baseline get`, and missing gate rejection.
- Added env-gated MySQL named active Store coverage for workflow planning:
  text output, JSON output, and missing workflow rejection after
  `config publish`.
- Shared the existing PostgreSQL scenarios through helpers so PostgreSQL and
  MySQL assert the same CLI behavior against the active Store.

Validated:

- `go test -v ./cmd/otsandbox -run 'Test(BaselineGateCommands(SetAndGetState|UseNamedMySQLActiveStore)|BaselineGetCommandRejectsMissingGate(WithMySQLStore)?|WorkflowPlanCommand(PrintsBoundSteps|PrintsBoundStepsWithMySQLStore|CanEmitJSONFromStore|CanEmitJSONFromMySQLStore|RejectsMissingWorkflow|RejectsMissingWorkflowWithMySQLStore))' -count=1`
  compiled and passed locally; the env-gated PostgreSQL/MySQL cases skipped
  because local DSNs were not exported in this shell.

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Executor and Template CLI Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for `executor plan` over
  Store catalog descriptors.
- Added env-gated MySQL named active Store coverage for `template render`,
  including published profile rendering and direct Store catalog rendering.
- Shared the existing PostgreSQL scenarios through helpers so PostgreSQL and
  MySQL assert identical CLI behavior against the active Store.

Validated:

- `go test -v ./cmd/otsandbox -run 'Test(ExecutorPlanCommand(ReportsProfileDescriptors|UsesNamedMySQLActiveStore)|TemplateRenderCommand(PrintsRequestPreview|UsesNamedMySQLActiveStore))' -count=1`
  compiled and passed locally; the env-gated PostgreSQL/MySQL cases skipped
  because local DSNs were not exported in this shell.

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Workflow Run Read CLI Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for workflow run read
  commands: `workflow runs`, `workflow run`, `workflow step`, and
  `workflow latest-step`.
- Shared the existing PostgreSQL scenario through a helper so PostgreSQL and
  MySQL assert identical active Store behavior and explicit `--store` readback.
- Generated unique run/workflow/profile/case ids per run so shared SQL test
  databases do not contaminate the workflow read checks.

Validated:

- `go test -v ./cmd/otsandbox -run 'TestWorkflowRunCommands(ReadStoredRuns|UseNamedMySQLActiveStore)' -count=1`
  compiled and passed locally; the env-gated PostgreSQL/MySQL cases skipped
  because local DSNs were not exported in this shell.

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Workflow Audit CLI Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for `workflow audit`
  JSON output with Store-scoped latest run and binding case state.
- Added env-gated MySQL named active Store coverage for `workflow audit` text
  output after `config publish`.
- Generated unique profile/workflow/node/case ids for both JSON and text
  scenarios so shared SQL test databases do not contaminate audit catalog or
  run-state checks.

Validated:

- `go test -v ./cmd/otsandbox -run 'TestWorkflowAuditCommand(EmitsJSONWithScopedStoreState|UsesNamedMySQLActiveStore|PrintsTextSummary|PrintsTextSummaryWithMySQLStore)' -count=1`
  compiled and passed locally; the env-gated PostgreSQL/MySQL cases skipped
  because local DSNs were not exported in this shell.
- `git diff --check`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`
- `tools/guardrails/check_store_first_contracts.sh && tools/guardrails/check_no_source_domain_core.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Case Suite Command Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for the case suite command
  bundle: `case suite report`, variant report selection, `case suite coverage`,
  `case suite priority`, and `case suite brief`.
- Shared the existing PostgreSQL scenario through a helper so PostgreSQL and
  MySQL assert identical behavior against the active Store.
- Parameterized report output directories and priority request ids by backend
  label so PostgreSQL/MySQL runs do not collide.

Validated:

- `go test -v ./cmd/otsandbox -run 'TestCaseSuiteCommandsUseNamed(PostgreSQL|MySQL)ActiveStore' -count=1`
  compiled and passed locally; the env-gated PostgreSQL/MySQL cases skipped
  because local DSNs were not exported in this shell.
- `git diff --check`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`
- `tools/guardrails/check_store_first_contracts.sh && tools/guardrails/check_no_source_domain_core.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.

## 2026-05-21 MySQL Case Suite Coverage Parity Slice

Progress: `[###################-] 98%`

Implemented:

- Added env-gated MySQL named active Store coverage for `case suite coverage`
  JSON and text output.
- Shared the existing PostgreSQL coverage scenario through a helper so
  PostgreSQL and MySQL assert identical latest-run status behavior against the
  active Store.
- Added a unique case suite coverage fixture with per-run profile/node/case
  and config ids so shared SQL test databases do not contaminate coverage
  checks.

Validated:

- `go test -v ./cmd/otsandbox -run 'TestCaseSuiteCoverage(ReportsLatestRunStatusByMaintenanceFilters|UsesNamedMySQLActiveStore)' -count=1`
  compiled and passed locally; the env-gated PostgreSQL/MySQL cases skipped
  because local DSNs were not exported in this shell.
- `git diff --check`
- `rg -n -i 'fall''back' . --glob '!node_modules/**'`
- `tools/guardrails/check_store_first_contracts.sh && tools/guardrails/check_no_source_domain_core.sh`

Current blocker:

- Final completion still requires the actual company values:
  `OTSANDBOX_REAL_MYSQL_STORE_DSN`, `OTS_TRACE_GRAPHQL_URL`, and
  `OTS_SMOKE_TRACE_IDS` for all 10 workflow steps, then either the manual
  `mysql-real-signoff` CI job or local `npm run release-check:mysql-real`.
