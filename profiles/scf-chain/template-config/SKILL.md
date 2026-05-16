# Profile Template Configuration Skill

This is the only document a configuration subagent may read for this profile.

## Boundary

- You may edit only profile configuration files listed by `profiles/scf-chain/config-authoring.json`.
- You must not read source code, tests, old task notes, or implementation files to infer schema details.
- Do not ask the main agent for concrete field, key, or value hints. If the business request lacks a concrete value, keep the request business-level and let the tool or existing template defaults resolve it.
- Do not create bespoke pages or scripts. Workflow pages and interface pages must stay shared React templates.
- Keep concrete business words inside this profile. Generic core packages and default assets must remain business-neutral.

## Tool Flow

1. Write or update a business request or case bundle inside `profiles/scf-chain/template-config/`.
2. Apply configuration through supported profile/template tools only. Do not update runtime SQLite stores with ad hoc SQL for authored cases.
3. Validate the profile and run the target workflow:

   ```sh
   .runtime/otsandbox serve --profile profiles/scf-chain --store-url .runtime/acceptance.sqlite --host 127.0.0.1 --port 18191
   curl -fsS -X POST 'http://127.0.0.1:18191/api/test-kit/run' \
     -H 'Content-Type: application/json' \
     -d '{"workflowId":"sandbox.financing_to_repay_result_query"}'
   ```

4. Verify the shared pages in an isolated browser context:
   - `/workflows.html`
   - `/workflow-detail.html?id=sandbox.financing_to_repay_result_query`
   - `/interface-nodes.html`
   - `/agent-test.html`
   - `/evidence-viewer.html`

## Case Authoring Rules

- A reusable interface case belongs in the profile case configuration, not in Go or page JavaScript.
- Dependencies between steps must be expressed through profile fixtures, case dependencies, workflow bindings, and placeholder inputs or exports.
- New required cases should be promoted only after a real run records green evidence.
- Required handoff fields are: changed files, validation commands, runtime evidence, and friction.

## Friction Report

Always include any friction in the handoff:

- missing config model capability;
- unclear interface-document semantics;
- fixture or precondition gap;
- runner assertion limitation;
- slow or flaky execution;
- any place where you had to guess.
