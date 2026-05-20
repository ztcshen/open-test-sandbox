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
