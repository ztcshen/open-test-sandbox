# God File Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor the largest AgentTestBench god files into cohesive, behavior-preserving modules so the CLI, control plane, store backends, tests, and frontend are easier to navigate and safer for agents to modify.

**Architecture:** Keep package boundaries and public contracts stable during the first pass. Split large files inside their existing Go packages first, then consider deeper package extraction only after the move-only refactor is green. Protect each slice with baseline command/API/test evidence before and after the move.

**Tech Stack:** Go 1.26, standard `flag` and `net/http`, SQL Store packages, React 19, Node.js test runner, esbuild, Playwright smoke scripts.

---

## Current Hotspots

Measured with:

```bash
find /Users/zlh/codes/agent-testbench \
  \( -path '*/.git' -o -path '*/node_modules' -o -path '*/.understand-anything' -o -path '*/.runtime' \) -prune -o \
  -type f \( -name '*.go' -o -name '*.jsx' -o -name '*.js' -o -name '*.mjs' -o -name '*.ts' -o -name '*.tsx' \) -print0 |
  xargs -0 wc -l | sort -nr | head -35
```
Primary refactor targets:

- `cmd/agent-testbench/main.go` - 14,815 lines.
- `cmd/agent-testbench/main_test.go` - 13,097 lines.
- `internal/server/controlplane/server_test.go` - 9,262 lines.
- `internal/server/controlplane/server.go` - 2,812 lines.
- `internal/server/controlplane/test_kit.go` - 2,025 lines.
- `internal/store/sqlite/store.go` - 1,998 lines.
- `internal/server/controlplane/api_case_batch_run.go` - 1,648 lines.
- `internal/store/sqlstore/store_test.go` - 1,308 lines.
- `internal/server/controlplane/workflow_runs.go` - 1,091 lines.
- `internal/store/sqlstore/store.go` - 1,032 lines.
- `control-plane/frontend/src/workflowBlueprintDemo.jsx` - 874 lines.
- `tools/smoke/control-plane-smoke.mjs` - 874 lines.

Generated/runtime folders such as `.runtime`, `node_modules`, and `.understand-anything` should be excluded from architectural analysis and line-budget checks.

## Non-Goals

- Do not rename public CLI commands or flags.
- Do not change JSON output fields, HTTP routes, status codes, or Store schema.
- Do not introduce a third-party CLI framework in this refactor.
- Do not mix feature work, copy edits, or broad style cleanup into move-only commits.
- Do not delete generated/runtime files unless that is handled in a separate generated-state cleanup task.

## Success Criteria

- `cmd/agent-testbench/main.go` is reduced to command dispatch, shared constants, `main`, help, and tiny wiring helpers. Target: under 800 lines.
- No first-party production Go/JS/JSX file remains above 1,200 lines, excluding generated files and intentionally large schema/test fixtures.
- The largest test files are split by behavior area. Target: each test file under 1,500 lines where practical.
- Public CLI, API, Store, and frontend behavior remains unchanged.
- Baseline and final verification commands pass.

Required final verification:

```bash
go test ./...
npm run test:frontend
npm run build:frontend
./bin/agent-testbench.sh version
./bin/agent-testbench.sh commands --json >/tmp/agent-testbench-commands-after.json
```

For slices touching CLI behavior, also capture and compare command catalog output before and after:

```bash
./bin/agent-testbench.sh commands --json >/tmp/agent-testbench-commands-before.json
# make the refactor
./bin/agent-testbench.sh commands --json >/tmp/agent-testbench-commands-after.json
diff -u /tmp/agent-testbench-commands-before.json /tmp/agent-testbench-commands-after.json
```

Expected: no diff unless the slice intentionally fixes an already documented command-catalog bug.

---

## Task 0: Baseline and Working Rules

**Files:**
- Inspect: `README.md`
- Inspect: `package.json`
- Inspect: `go.mod`
- Inspect: `cmd/agent-testbench/main.go`
- Inspect: `internal/server/controlplane/server.go`

**Step 1: Confirm worktree state**

Run:

```bash
git status --short
```

Expected: identify unrelated local artifacts before editing. Do not revert user changes.

**Step 2: Capture baseline commands**

Run:

```bash
go test ./...
npm run test:frontend
npm run build:frontend
./bin/agent-testbench.sh version
./bin/agent-testbench.sh commands --json >/tmp/agent-testbench-commands-before.json
```

Expected: all commands pass. If not, record the failing baseline and do not hide it inside the refactor.

**Step 3: Commit only after each green slice**

Each task below should end with:

```bash
gofmt -w <changed-go-files>
go test ./...
git diff --stat
git commit -m "refactor: split <area>"
```

For frontend slices, also run:

```bash
npm run test:frontend
npm run build:frontend
```

---

## Task 1: Split CLI Environment Restore from `main.go`

This is the best first high-impact split because `environmentRestore*` dominates the middle of `main.go` and is internally cohesive.

**Files:**
- Modify: `cmd/agent-testbench/main.go`
- Create: `cmd/agent-testbench/environment_restore.go`
- Create, if useful: `cmd/agent-testbench/environment_restore_docker.go`
- Create, if useful: `cmd/agent-testbench/environment_restore_report.go`

**Step 1: Move only cohesive declarations**

Move these groups out of `main.go` without changing names or visibility:

- `environmentRestoreAttemptLimit`
- `environmentRestoreReport`
- `environmentRestoreCleanMachinePlan`
- `environmentRestoreCleanMachineSummary`
- `environmentRestoreCleanMachinePrerequisite`
- `environmentRestoreSourcePolicy`
- `environmentRestoreComponentGraph`
- `environmentRestoreComponentAsset`
- `environmentRestorePackageReport`
- `environmentRestorePackageSpec`
- `environmentRestoreRepoReport`
- `environmentRestoreRepoSpec`
- `environmentRestorePreflight`
- `environmentRestoreStartupAsset`
- `environmentRestoreStartupAssetCandidate`
- `environmentRestorePreflightTool`
- `environmentRestoreReadiness`
- `environmentRestoreReadinessItem`
- `environmentRestoreDockerReport`
- `environmentRestoreGeneratedFile`
- `environmentRestoreAppliedAsset`
- `environmentRestoreDockerCleanupReport`
- `environmentRestoreHealthCheckReport`
- `environmentRestoreWorkflowRun`
- `environmentRestoreWorkflowAcceptance`
- `environmentRestoreWorkflowOptions`
- `environmentRestoreDockerCleanupOptions`
- Every function whose name starts with `environmentRestore`
- Every function whose name starts with `runRestore`
- `printEnvironmentRestoreReport`
- Restore-only helpers such as `parseComposeShortVolume`, `parseComposeContainerNames`, `writeEnvironmentRestoreGeneratedEnvFile`, `stringMapFromAny`, and `stringSliceFromAny` when used only by restore code.

Keep package name `main`.

**Step 2: Build and fix imports**

Run:

```bash
gofmt -w cmd/agent-testbench/main.go cmd/agent-testbench/environment_restore*.go
go test ./cmd/agent-testbench
```

Expected: pass. Import errors mean the moved file needs missing imports or `main.go` needs unused imports removed.

**Step 3: Full verification**

Run:

```bash
go test ./...
./bin/agent-testbench.sh commands --json >/tmp/agent-testbench-commands-after.json
diff -u /tmp/agent-testbench-commands-before.json /tmp/agent-testbench-commands-after.json
```

Expected: tests pass and command catalog is unchanged.

**Step 4: Commit**

```bash
git add cmd/agent-testbench/main.go cmd/agent-testbench/environment_restore*.go
git commit -m "refactor: split environment restore cli code"
```

---

## Task 2: Split CLI Command Families from `main.go`

**Files:**
- Modify: `cmd/agent-testbench/main.go`
- Create: `cmd/agent-testbench/flags.go`
- Create: `cmd/agent-testbench/store_commands.go`
- Create: `cmd/agent-testbench/environment_commands.go`
- Create: `cmd/agent-testbench/profile_commands.go`
- Create: `cmd/agent-testbench/interface_node_commands.go`
- Create: `cmd/agent-testbench/workflow_commands.go`
- Create: `cmd/agent-testbench/case_commands.go`
- Create: `cmd/agent-testbench/evidence_commands.go`
- Create: `cmd/agent-testbench/serve.go`
- Create: `cmd/agent-testbench/output.go`

**Step 1: Move generic CLI helpers**

Move from `main.go`:

- `parseInterspersedFlags`
- `interspersedFlagArgs`
- `mapFlag`
- `stringListFlag`
- `normalizeStringList`
- generic JSON helpers such as `writeIndentedJSON`, `compactRawJSON`, `compactJSONValue`, `readJSONFile`, and `compactJSON`
- generic string/time helpers only when they are used by multiple command files.

Run:

```bash
gofmt -w cmd/agent-testbench/*.go
go test ./cmd/agent-testbench
```

**Step 2: Move store commands**

Move store-related functions and types:

- `runStore`
- `runStoreDDL`
- `printStoreStatus`
- `storeStatusReport`
- `printPostgresStoreStatus`
- `printMySQLStoreStatus`
- `openStore`

Run:

```bash
gofmt -w cmd/agent-testbench/*.go
go test ./cmd/agent-testbench
```

**Step 3: Move environment commands outside restore**

Move:

- `runEnvironment`
- `runEnvironmentRegister`
- `runEnvironmentDiscover`
- `runEnvironmentInspect`
- `runEnvironmentBootstrap`
- `runEnvironmentRepo`
- `runEnvironmentRepoSet`
- `runEnvironmentStartupFile`
- `runEnvironmentStartupFilePut`
- `runEnvironmentComponents`
- `runEnvironmentComponentsInspect`
- `runEnvironmentComponentsReplace`
- `runEnvironmentAcceptance*`
- `runEnvironmentVerify`
- `runEnvironmentPublishVerified`
- `printEnvironmentCommandResult`
- `environmentPayload*`
- `environmentServices*`
- `environmentComposeConfig`
- `environmentHealthChecks`
- `environmentRepoUpdateMap`
- `applyEnvironmentServiceRepoUpdate`
- `environmentKeyValueMap`

Run:

```bash
gofmt -w cmd/agent-testbench/*.go
go test ./cmd/agent-testbench
```

**Step 4: Move profile/template/import commands**

Move:

- profile import report types near the top of `main.go`
- `runProfile`
- `runTemplatePackage`
- `runProfileGenerationPlan`
- `runProfileOpenAPIGenerationPlan`
- `runProfileImportPlan`
- `runProfileOpenAPIImportPlan`
- `runProfileHTTPCaptureImportPlan`
- profile import/verify/audit functions and print helpers.

Run:

```bash
gofmt -w cmd/agent-testbench/*.go
go test ./cmd/agent-testbench
```

**Step 5: Move interface-node, workflow, baseline, evidence, case, and serve commands**

Move each family in a separate commit if the diff gets large:

- Interface node: `runInterfaceNode*`, `interfaceNode*`, `templateConfigInput`, `caseFileInput`.
- Workflow: `runWorkflow*`, `workflow*`, `printWorkflow*`, `workflowGate*`.
- Baseline/template/evidence: `runBaseline*`, `runTemplate*`, `runEvidence*`, evidence report types.
- Case/case-suite: `runCase*`, `caseSuite*`, `interfaceNodeCaseReport*`, report rendering helpers.
- Serve: `runServe`, `serveConfig`, `serveHandlerFromArgs`, `serveConfigFromArgs`, `serveHandler`, `serveBundle`, `serveStoreInfo`.

Run after each family:

```bash
gofmt -w cmd/agent-testbench/*.go
go test ./cmd/agent-testbench
```

**Step 6: Final CLI verification**

Run:

```bash
go test ./...
./bin/agent-testbench.sh version
./bin/agent-testbench.sh commands --json >/tmp/agent-testbench-commands-after.json
diff -u /tmp/agent-testbench-commands-before.json /tmp/agent-testbench-commands-after.json
```

Expected: pass and no command catalog diff.

---

## Task 3: Split CLI Tests by Command Family

**Files:**
- Modify: `cmd/agent-testbench/main_test.go`
- Create: `cmd/agent-testbench/test_helpers_test.go`
- Create: `cmd/agent-testbench/store_commands_test.go`
- Create: `cmd/agent-testbench/environment_commands_test.go`
- Create: `cmd/agent-testbench/environment_restore_test.go`
- Create: `cmd/agent-testbench/profile_commands_test.go`
- Create: `cmd/agent-testbench/interface_node_commands_test.go`
- Create: `cmd/agent-testbench/workflow_commands_test.go`
- Create: `cmd/agent-testbench/case_commands_test.go`
- Create: `cmd/agent-testbench/evidence_commands_test.go`
- Create: `cmd/agent-testbench/serve_test.go`

**Step 1: Extract shared test helpers**

Move reusable helpers, test fixtures, temporary Store setup, and assertion helpers into `test_helpers_test.go`.

Run:

```bash
gofmt -w cmd/agent-testbench/*_test.go
go test ./cmd/agent-testbench
```

**Step 2: Move tests by prefix and subject**

Move tests by the command family they exercise. Use `rg -n '^func Test' cmd/agent-testbench/main_test.go` to keep the move mechanical.

Run after each file move:

```bash
gofmt -w cmd/agent-testbench/*_test.go
go test ./cmd/agent-testbench -run '<moved-test-prefix>'
go test ./cmd/agent-testbench
```

Expected: all moved tests still pass.

**Step 3: Full verification**

```bash
go test ./...
```

---

## Task 4: Split Control Plane Route Registration

`internal/server/controlplane/server.go` already delegates many handlers to separate files, but route registration itself is still a god function.

**Files:**
- Modify: `internal/server/controlplane/server.go`
- Create: `internal/server/controlplane/options.go`
- Create: `internal/server/controlplane/routes.go`
- Create: `internal/server/controlplane/routes_template_packages.go`
- Create: `internal/server/controlplane/routes_catalog.go`
- Create: `internal/server/controlplane/routes_execution.go`
- Create: `internal/server/controlplane/routes_environment.go`
- Create: `internal/server/controlplane/routes_static.go`
- Create: `internal/server/controlplane/http_helpers.go`

**Step 1: Move option and payload types**

Move:

- `Options`
- `StoreInfo`
- `storeCurrentPayload`

Run:

```bash
gofmt -w internal/server/controlplane/*.go
go test ./internal/server/controlplane
```

**Step 2: Introduce route dependencies**

Create:

```go
type routeDeps struct {
	profiles        *profileState
	runtime         store.Store
	collector       traceCollector
	caseBatchRunner *apiCaseBatchRunner
	profileHome     string
	storeInfo       StoreInfo
	staticDir       string
}
```

Keep it unexported in `routes.go`. Do not change handler signatures in this task unless a route helper removes direct duplication safely.

**Step 3: Move route registration into grouped functions**

Create route registration functions:

```go
func registerTemplatePackageRoutes(mux *http.ServeMux, deps routeDeps)
func registerCatalogRoutes(mux *http.ServeMux, deps routeDeps)
func registerExecutionRoutes(mux *http.ServeMux, deps routeDeps)
func registerEnvironmentRoutes(mux *http.ServeMux, deps routeDeps)
func registerStaticRoutes(mux *http.ServeMux, deps routeDeps)
```

`NewWithOptions` should become mostly:

```go
func NewWithOptions(bundle profile.Bundle, options Options) http.Handler {
	mux := http.NewServeMux()
	deps := routeDeps{...}
	registerTemplatePackageRoutes(mux, deps)
	registerCatalogRoutes(mux, deps)
	registerExecutionRoutes(mux, deps)
	registerEnvironmentRoutes(mux, deps)
	registerStaticRoutes(mux, deps)
	return mux
}
```

**Step 4: Add tiny method helper only if it clarifies repeated checks**

Acceptable helper:

```go
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
	return false
}
```

Use it only where it reduces duplicated method checks without changing status behavior.

**Step 5: Verify**

```bash
gofmt -w internal/server/controlplane/*.go
go test ./internal/server/controlplane
go test ./...
```

---

## Task 5: Split Control Plane Tests

**Files:**
- Modify: `internal/server/controlplane/server_test.go`
- Create: `internal/server/controlplane/test_helpers_test.go`
- Create: `internal/server/controlplane/template_package_routes_test.go`
- Create: `internal/server/controlplane/catalog_routes_test.go`
- Create: `internal/server/controlplane/execution_routes_test.go`
- Create: `internal/server/controlplane/environment_routes_test.go`
- Create: `internal/server/controlplane/evidence_routes_test.go`
- Create: `internal/server/controlplane/workflow_routes_test.go`

**Step 1: Extract test setup**

Move common server construction, profile bundles, runtime Store setup, response decoding, and assertion helpers into `test_helpers_test.go`.

Run:

```bash
gofmt -w internal/server/controlplane/*_test.go
go test ./internal/server/controlplane
```

**Step 2: Move tests by route family**

Use:

```bash
rg -n '^func Test' internal/server/controlplane/server_test.go
```

Move related tests to the matching route test file. Keep test names unchanged.

Run after each move:

```bash
gofmt -w internal/server/controlplane/*_test.go
go test ./internal/server/controlplane -run '<moved-test-prefix>'
go test ./internal/server/controlplane
```

**Step 3: Verify**

```bash
go test ./...
```

---

## Task 6: Split SQL Store Implementations by Concern

Start with `internal/store/sqlstore/store.go` because it is the shared SQL implementation for PostgreSQL/MySQL-style stores. Then split SQLite.

**Files:**
- Modify: `internal/store/sqlstore/store.go`
- Create: `internal/store/sqlstore/runs.go`
- Create: `internal/store/sqlstore/api_case_runs.go`
- Create: `internal/store/sqlstore/evidence.go`
- Create: `internal/store/sqlstore/trace.go`
- Create: `internal/store/sqlstore/post_process.go`
- Create: `internal/store/sqlstore/baseline.go`
- Create: `internal/store/sqlstore/profile_catalog.go`
- Create: `internal/store/sqlstore/environment.go`
- Create: `internal/store/sqlstore/scanners.go`
- Modify: `internal/store/sqlite/store.go`
- Create: `internal/store/sqlite/config.go`
- Create: `internal/store/sqlite/schema.go`
- Create: `internal/store/sqlite/runs.go`
- Create: `internal/store/sqlite/api_case_runs.go`
- Create: `internal/store/sqlite/evidence.go`
- Create: `internal/store/sqlite/trace.go`
- Create: `internal/store/sqlite/post_process.go`
- Create: `internal/store/sqlite/baseline.go`
- Create: `internal/store/sqlite/profile_catalog.go`
- Create: `internal/store/sqlite/environment.go`
- Create: `internal/store/sqlite/rows.go`
- Create: `internal/store/sqlite/json_helpers.go`

**Step 1: Split `sqlstore` methods**

Move methods by Store contract area without editing SQL:

- Runs: `CreateRun`, `GetRun`, `ListRuns`.
- API case runs: `RecordAPICaseRun`, `ListAPICaseRuns`, `ListLatestAPICaseRuns`.
- Evidence: `RecordEvidence`, `ListEvidence`.
- Trace: `SaveTraceTopology`, `ListTraceTopologies`.
- Post-process: `RecordPostProcessTask`, `ListPostProcessTasks`.
- Baseline/profile/read models/catalog/environment into dedicated files.
- `scanner`, `scan*`, and row conversion helpers into `scanners.go`.

Run:

```bash
gofmt -w internal/store/sqlstore/*.go
go test ./internal/store/sqlstore ./internal/store
go test ./...
```

**Step 2: Split `sqlite` config and schema**

Move:

- `Config`
- `Resolve`
- `ConfigFromURL`
- `ParseConfigFromURL`
- backend URL helpers
- `SchemaStatusResult`
- `SchemaStatus`
- `UpgradeSchema`
- schema table/version helpers.

Run:

```bash
gofmt -w internal/store/sqlite/*.go
go test ./internal/store/sqlite ./internal/store
```

**Step 3: Split `sqlite` contract methods and rows**

Move Store methods by the same concern groups as `sqlstore`. Move row structs and `toStore` methods into `rows.go`. Move JSON/string/time helpers into `json_helpers.go`.

Run after each concern:

```bash
gofmt -w internal/store/sqlite/*.go
go test ./internal/store/sqlite ./internal/store
```

**Step 4: Full Store verification**

```bash
go test ./internal/store/... ./internal/server/controlplane ./cmd/agent-testbench
go test ./...
```

---

## Task 7: Split Control Plane Heavy Handlers

Only do this after route and test splits are green.

**Files:**
- Modify: `internal/server/controlplane/test_kit.go`
- Create: `internal/server/controlplane/test_kit_requests.go`
- Create: `internal/server/controlplane/test_kit_execution.go`
- Create: `internal/server/controlplane/test_kit_reports.go`
- Modify: `internal/server/controlplane/api_case_batch_run.go`
- Create: `internal/server/controlplane/api_case_batch_runner.go`
- Create: `internal/server/controlplane/api_case_batch_report.go`
- Modify: `internal/server/controlplane/workflow_runs.go`
- Create, if useful: `internal/server/controlplane/workflow_run_payloads.go`

**Step 1: Split type definitions from orchestration**

Move request/response payload structs into `*_requests.go` or `*_reports.go`. Keep handlers in the original file until tests pass.

**Step 2: Split runner internals**

Move pure runner state and report-building code away from HTTP handler functions. Do not change route handlers yet.

**Step 3: Verify**

```bash
gofmt -w internal/server/controlplane/*.go
go test ./internal/server/controlplane
go test ./...
```

---

## Task 8: Split Frontend Large Views

**Files:**
- Modify: `control-plane/frontend/src/workflowBlueprintDemo.jsx`
- Create: `control-plane/frontend/src/workflowBlueprint/WorkflowBlueprintDemo.jsx`
- Create: `control-plane/frontend/src/workflowBlueprint/WorkflowBlueprintCanvas.jsx`
- Create: `control-plane/frontend/src/workflowBlueprint/WorkflowBlueprintControls.jsx`
- Create: `control-plane/frontend/src/workflowBlueprint/WorkflowBlueprintSummary.jsx`
- Create: `control-plane/frontend/src/workflowBlueprint/workflowBlueprintFixtures.mjs`
- Create: `control-plane/frontend/src/workflowBlueprint/index.js`
- Modify, if imports require it: `control-plane/frontend/src/*.jsx`

**Step 1: Move static demo data**

Move static node/edge/demo data into `workflowBlueprintFixtures.mjs`.

Run:

```bash
npm run test:frontend
npm run build:frontend
```

**Step 2: Extract pure presentational components**

Move canvas, controls, and summary panels into dedicated components. Keep state ownership in the top-level demo first.

Run:

```bash
npm run test:frontend
npm run build:frontend
```

**Step 3: Repeat for next frontend hotspots**

Candidate files:

- `control-plane/frontend/src/interfaceNode.jsx`
- `control-plane/frontend/src/caseRuns.jsx`
- `control-plane/frontend/src/apiCases.jsx`
- `control-plane/frontend/src/sandboxWorkbench.jsx`
- `control-plane/frontend/src/evidenceViewer.jsx`

Use the same pattern: move data/helpers first, then split presentational components, then consider state extraction.

---

## Task 9: Add a Source Size Budget Report

Do this after the first major splits, so the budget can pass.

**Files:**
- Create: `tools/guardrails/source-size-budget.mjs`
- Modify: `package.json`
- Modify: `docs/release-checklist.md`

**Step 1: Add a report script**

The script should:

- Scan first-party files only.
- Exclude `.git`, `node_modules`, `.understand-anything`, `.runtime`, `control-plane/static`, generated assets, and build outputs.
- Report files above 1,200 lines.
- Allow a small explicit allowlist only for schema or fixture files with a comment explaining why.

Suggested command:

```bash
node tools/guardrails/source-size-budget.mjs
```

**Step 2: Add npm script**

Add to `package.json`:

```json
"source-size:check": "node tools/guardrails/source-size-budget.mjs"
```

**Step 3: Verify**

```bash
npm run source-size:check
npm run release-check -- --scope cmd/agent-testbench
```

Expected: size check passes after the refactor. If release-check is too broad for the current branch, record the narrower passing checks.

---

## Recommended Execution Order

1. Task 0 baseline.
2. Task 1 environment restore split.
3. Task 2 CLI family split.
4. Task 3 CLI test split.
5. Task 4 control plane route registration split.
6. Task 5 control plane test split.
7. Task 6 Store split.
8. Task 7 heavy handler split.
9. Task 8 frontend split.
10. Task 9 size-budget guardrail.

This order keeps the riskiest behavior changes out of the early work. Most early commits are mechanical moves inside the same package, which makes failures easier to diagnose.

## Handoff Prompt for the Next Session

Use this prompt in the new session:

```text
In /Users/zlh/codes/agent-testbench, execute docs/plans/2026-05-26-god-file-refactor.md task-by-task.

Use the executing-plans skill. Preserve behavior: no CLI command/flag/output changes, no HTTP route/status/JSON changes, no Store schema changes. Start with Task 0 baseline, then Task 1 only. Commit after each green slice. If a baseline test is already failing, stop and report the exact failure before editing.
```
