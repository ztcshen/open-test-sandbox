# Profile Bundle Format

A profile bundle is a reviewable directory of configuration assets. The minimum
bundle contains a `profile.json` manifest.

```json
{
  "id": "empty",
  "displayName": "Empty Profile",
  "description": "A neutral profile with no services, workflows, cases, or fixtures.",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}
```

## Manifest Fields

- `id`: stable profile identifier.
- `displayName`: human-readable profile name.
- `description`: optional profile summary.
- `services`: systems or dependencies involved in the profile.
- `workflows`: template-driven sequences of testable steps.
- `interfaceNodes`: observable interfaces that cases can target.
- `apiCases`: runnable interface tests.
- `requestTemplates`: reusable request rendering assets for API cases.
- `caseDependencies`: fixture requirements and mappings for cases.
- `workflowBindings`: links from workflow steps to interface nodes and cases.
- `fixtures`: input or precondition data for cases and workflows. Fixtures can
  include `dataJson` when a profile owns small JSON data needed for request
  template rendering.

Configuration remains file-first. Store records are generated runtime indexes,
not the source of truth for profile assets.

## Audit

Use `otsandbox profile audit --profile PATH` to check a bundle before or after
import. The audit verifies basic reference integrity across workflows, API
Cases, request templates, fixtures, case dependencies, and workflow bindings.
For example, it reports a workflow binding that points to a missing workflow,
an API Case that points to a missing interface node, or a case dependency that
points to a missing fixture.

Add `--store-url PATH` to include the local Store profile index and API Case run
status in the report. Add `--json` when another tool needs a stable
machine-readable report.

## Split Assets

Large bundles can keep assets in deterministic JSON directories next to
`profile.json`:

- `services/*.json`
- `workflows/*.json`
- `interface-nodes/*.json`
- `cases/*.json`
- `request-templates/*.json`
- `case-dependencies/*.json`
- `workflow-bindings/*.json`
- `fixtures/*.json`

The loader reads files in sorted path order and appends them to any assets
declared directly in the manifest.

## API Case Run Fields

API Case assets can optionally declare local run settings used by the control
plane workbench:

```json
{
  "id": "case.alpha",
  "displayName": "Create Item",
  "nodeId": "node.alpha",
  "casePath": "profiles/sample/cases/case.alpha.json",
  "baseUrl": "http://127.0.0.1:18080",
  "evidenceDir": ".runtime/cases",
  "timeoutSeconds": 30,
  "defaultOverrides": {
    "itemId": "item-001"
  }
}
```

- `casePath`: path to the runnable API Case JSON file.
- `baseUrl`: default target URL for live runs.
- `evidenceDir`: optional runtime Evidence output directory.
- `timeoutSeconds`: optional request timeout for the control plane run API.
- `defaultOverrides`: optional profile-owned defaults passed to the page.
