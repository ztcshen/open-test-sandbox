# AgentTestBench Feedback

Durable feedback registered by local Codex sessions. Use
`skills/agent-testbench-feedback/scripts/register_feedback.py` for new entries.

## 2026-05-28 - Environment component graph and compose plan can diverge
- Area: environment restore
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: `environment components inspect` showed required dependency components and app nodes, but the recorded compose execution plan only started the app compose files. Restore generated dependency assets and later failed because a required dependency service was not running.
- Suggestion: Add a restore readiness item that compares required component `composeService` values with the compose service allow-list and compose files, then prints a concrete repair hint before Docker starts.
- Verification: `go test ./cmd/agent-testbench -run TestEnvironmentRestoreRejectsRequiredComposeServiceGaps`

## 2026-05-28 - Sandbox start and environment component graph use different registries
- Area: sandbox cli
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: `sandbox start --service <dependency>` failed even though the environment component graph contained that dependency; other dependency entries could also be skipped because their profile service startup command was empty.
- Suggestion: The missing-service error now explains the registry boundary, and `sandbox service list --environment ENV_ID --include-components` gives a read-only bridge view that shows profile services beside environment component-graph-only dependencies.
- Verification: `go test ./cmd/agent-testbench -run 'TestSandbox(ServiceListCanIncludeEnvironmentComponentGraph|ServiceListReportsRegisteredServicesReadOnly|StartMissingServiceExplainsRegistryBoundary)' -count=1`

## 2026-05-28 - Environment restore health wait needs progress output
- Area: environment restore
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: A target service health URL could already return `200 UP` while restore kept waiting without showing the active probe, latest HTTP status or error, or remaining timeout.
- Suggestion: Emit health-check progress to stderr for non-JSON `environment restore --execute` runs, including the target, latest status/error, and completion state.
- Verification: `go test ./cmd/agent-testbench -run TestEnvironmentRestoreHealthWaitReportsProgress`

## 2026-05-28 - Case run should fail fast for bodyless write requests
- Area: case run
- Severity: P1
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: `case run --case-id ...` produced `body=null` for a POST case and the target returned HTTP 400 even though the case appeared ready.
- Suggestion: After request-template rendering and patching, fail before sending HTTP when POST, PUT, or PATCH has no rendered body, and tell the user to add `caseExecution.body` or a body-rendering request template.
- Verification: `go test ./internal/server/controlplane -run TestServerTestKitRunFailsFastForBodylessWriteRequest`

## 2026-05-28 - hasExecutionConfig should not mean bodyless write cases are runnable
- Area: case suite
- Severity: P1
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: Case suite inspection could report `hasExecutionConfig=true` when the config only contained method/path metadata and no POST body.
- Suggestion: Treat active execution configs as runnable only when they have execution metadata and, for POST/PUT/PATCH, a non-null `caseExecution.body`.
- Verification: `go test ./internal/domain/casesuite -run TestExecutionConfigSetDoesNotMarkBodylessWriteConfigRunnable`

## 2026-05-28 - Local evidence URI lifecycle is unclear
- Area: evidence
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: `case evidence` listed historical passed-run request/response attachment URIs, but the local `/tmp/.../request.json` and `response.json` files had been deleted; `case diagnose` could not read them.
- Suggestion: Mark local file evidence lifecycle in Store metadata and add a command or diagnostic next action to export, copy, or rebuild evidence before temporary files disappear.
- Verification: `go test ./internal/server/controlplane -run TestServerMarksMissingLocalEvidenceLifecycle -count=1`; `go test ./cmd/agent-testbench -run TestCaseDiagnoseReportsExpiredLocalEvidenceNextAction -count=1`

## 2026-05-28 - HTTP 200 alone can hide business failure
- Area: case assertions
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: A case passed by HTTP status while downstream data showed a FAILED business decision because a dependent response lacked an expected decision field.
- Suggestion: Add Store-backed post-run assertions such as SQL checks against application-visible state, so case suite reports can require both transport success and business-state success.
- Verification: `docs/api-case-format.md`; existing gate coverage `go test ./internal/server/controlplane -run TestServerTestKitRunHonorsExpectedResponseContains -count=1`

## 2026-05-29 - Environment restore JSON adoption should fail with bounded health evidence
- Area: environment restore
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-29
- Evidence: A JSON `environment restore --execute --use-existing-containers` run could wait longer than the requested health timeout while a command-style health probe was still running, leaving stdout empty until the process was killed.
- Suggestion: Bound the whole restore health phase, cap command probes with the remaining timeout, and surface the failing health target in the final JSON report plus `summary.lastRestore`.
- Verification: `go test ./cmd/agent-testbench -run TestEnvironmentRestoreCommandHealthTimeoutBoundsSlowProbe`

## 2026-05-29 - Runtime SQL discovery and run-scoped Evidence checks should be in the runbook
- Area: docs
- Severity: P3
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-29
- Evidence: `runtime mysql endpoints --include-tables --json` and `evidence list --run RUN_ID --json` gave enough Store-backed diagnostics to verify runtime database visibility and request/response Evidence before inspecting Docker or local files.
- Suggestion: Document those commands as preferred first checks for sandbox diagnostics.
- Verification: `docs/quickstart.md`

## 2026-05-29 - Sandbox service registration needs a read-only list and startup dry-run
- Area: sandbox cli
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-29
- Evidence: After registering services, operators had no obvious read-only service catalog command and `sandbox start` could execute unrelated startup commands while checking registration state.
- Suggestion: Add `sandbox service list`/`discover --json` and `sandbox start --dry-run` so registration state and startup plans can be inspected without launching services.
- Verification: `go test ./cmd/agent-testbench -run 'TestSandbox(ServiceListReportsRegisteredServicesReadOnly|StartDryRunDoesNotRunStartupCommands)'`

## 2026-05-29 - Workflow creation needs small upsert commands
- Area: workflow cli
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-29
- Evidence: Adding one smoke workflow still requires exporting the full profile catalog, editing `profile.json`, and importing the whole profile with audit.
- Suggestion: Add Store-first `workflow register/upsert` and workflow binding register/upsert commands with `--json` and `--audit` support so small workflow additions do not require whole-profile import.
- Verification: `go test ./cmd/agent-testbench -run 'TestWorkflow(RegisterAndBindingUpsertStoreCatalog|BindingAuditReportsMissingReferences)' -count=1`

## 2026-05-29 - Component MySQL assets need graceful incremental ALTER workflow
- Area: environment migration
- Severity: P2
- Status: fixed
- Source: local AgentTestBench usability note from 2026-05-29
- Evidence: Component-to-MySQL edges could execute SQL assets during restore, but adding one ALTER required whole-graph editing and manual idempotency; there was no small versioned migration command, target history table, checksum guard, or baseline path.
- Suggestion: Add Store-first versioned MySQL migration assets linked from component dependency edges, with add/list/plan/apply/baseline CLI commands and restore integration.
- Verification: `go test ./cmd/agent-testbench -run 'TestEnvironment(Migration|RestoreAppliesMySQLMigration)'`
