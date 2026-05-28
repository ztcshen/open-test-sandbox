# AgentTestBench Feedback

Durable feedback registered by local Codex sessions. Use
`skills/agent-testbench-feedback/scripts/register_feedback.py` for new entries.

## 2026-05-28 - Environment component graph and compose plan can diverge
- Area: environment restore
- Severity: P2
- Status: backlog
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: `environment components inspect` showed required dependency components and app nodes, but the recorded compose execution plan only started the app compose files. Restore generated dependency assets and later failed because a required dependency service was not running.
- Suggestion: Add a restore/bootstrap readiness item that compares required component `composeService` values with the compose service allow-list and compose files, then prints a concrete `agent-testbench environment register ... --compose-file ... --compose-service ...` repair hint.

## 2026-05-28 - Sandbox start and environment component graph use different registries
- Area: sandbox cli
- Severity: P2
- Status: partly fixed
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: `sandbox start --service <dependency>` failed even though the environment component graph contained that dependency; other dependency entries could also be skipped because their profile service startup command was empty.
- Suggestion: This slice clarified the missing-service error so users see that `sandbox start` reads the profile service registry and `environment restore` reads the environment component graph. A later slice can add a bridge or discovery view if both registries should be unified.
- Verification: `go test ./cmd/agent-testbench -run TestSandboxStartMissingServiceExplainsRegistryBoundary`

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
- Status: backlog
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: `case evidence` listed historical passed-run request/response attachment URIs, but the local `/tmp/.../request.json` and `response.json` files had been deleted; `case diagnose` could not read them.
- Suggestion: Mark local file evidence lifecycle in Store metadata and add a command or diagnostic next action to export, copy, or rebuild evidence before temporary files disappear.

## 2026-05-28 - HTTP 200 alone can hide business failure
- Area: case assertions
- Severity: P2
- Status: backlog
- Source: local AgentTestBench usability note from 2026-05-28
- Evidence: A case passed by HTTP status while downstream data showed a FAILED business decision because a dependent response lacked an expected decision field.
- Suggestion: Add Store-backed post-run assertions such as SQL checks against application-visible state, so case suite reports can require both transport success and business-state success.
