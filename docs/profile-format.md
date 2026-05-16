# Profile Bundle Format

A profile bundle is a reviewable directory of configuration assets kept outside
the Open Test Sandbox core repository. The minimum bundle contains a
`profile.json` manifest.

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

Configuration remains file-first outside core. Store records are generated
runtime indexes and read-models used by the Control plane; they are not the
source of truth for profile assets.

Publish a bundle before serving it through the workbench:

```sh
otsandbox profile init --output /path/to/profile-bundle --id sample
otsandbox profile install --from /path/to/profile-bundle
otsandbox profile verify --profile sample --store-url .runtime/store.sqlite
otsandbox serve --profile sample --store-url .runtime/store.sqlite
```

For local bootstrapping, `otsandbox serve --profile /path/to/profile-bundle`
first publishes the external bundle into the Store/read-model, then serves that
indexed view.

The init command refuses output paths under the core repository's `profiles/`
directory. This keeps generated bundles external even during local experiments.
It also writes a bundle-local `.gitignore` for generated runtime state such as
`.runtime/`, SQLite files, database sidecar files, and local logs.

## Standard Local Placement

Installed profile bundles live outside the core repository. By default the CLI
uses `$HOME/.otsandbox/profiles`; set `OTSANDBOX_PROFILE_HOME` or pass
`--profile-home PATH` to use a team checkout, mounted volume, or temporary test
directory.

```sh
otsandbox profile install --from /path/to/profile-bundle
otsandbox profile list
otsandbox profile pack --profile sample --output sample-profile.tar.gz
otsandbox profile inspect --profile sample
otsandbox profile verify --profile sample --store-url .runtime/store.sqlite
```

`profile install` copies the external bundle into the profile home under the
bundle's `id`. It accepts either a profile directory or a `.tar.gz` / `.tgz`
archive created by `profile pack`. The copy is intentionally source-focused:
generated runtime state, local SQLite/database files, logs, and VCS directories
are skipped. Use `--force` to replace an already installed bundle. Commands
that accept profile bundles (`inspect`, `audit`, `verify`, `import`, and
`config publish`) accept either a filesystem path or an installed profile id.
`serve --profile ID --profile-home PATH` follows the same resolution rule.

`profile list` and `GET /api/profile/installed` are tolerant of a mixed profile
home. If one installed directory has a malformed manifest, the list still
returns the other profiles and includes an item with `valid: false` plus an
`error` message for the broken directory. The workbench disables invalid
installed profiles in the selector instead of hiding the problem.

Use `profile pack --profile PATH_OR_ID --output bundle.tar.gz` to create a
clean distributable archive for review or handoff. The command accepts either a
filesystem path or an installed profile id, uses the same runtime/VCS filtering
as `profile install`, and writes profile files under an archive root named after
the profile id. Archive installation is path-safe: entries that would escape the
archive root are rejected. `profile audit`, `profile import`, `profile verify`,
and `config publish` can also accept a packed archive directly; they install
the archive into the configured profile home before auditing or writing
Store/read-model data. Pass `--force` when a matching installed profile should
be replaced.

The Control plane exposes the same local placement surface:

- `GET /api/profile/installed`: list installed profile bundles.
- `POST /api/profile/install`: install a bundle from a local path or packed
  archive into the configured profile home.
- `POST /api/profile/import` and `POST /api/profile/verify`: accept either a
  local path, packed archive, or installed profile id in the `path` field.
  Archive paths are installed into the configured profile home first, then the
  installed bundle is published or verified. Pass `force: true` when a matching
  installed profile should be replaced.

The workbench Profile panel lists installed profiles, can install a bundle from
a local path, and can publish or verify the selected profile id.

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

Use `--require-audit-ok` with `profile import` or `config publish` when the
publish step must fail before Store/read-model writes if reference integrity
issues are found. The Control plane import API exposes the same behavior with
`requireAuditOk: true`.

Use `otsandbox profile verify --profile PATH --store-url PATH` as the standard
local acceptance command for an external bundle. It audits the bundle, publishes
it only if the audit is clean, then checks that the profile index, active config
version, catalog index, and base Control plane read-models were written for the
same published config version. The Control plane exposes the same flow through
`POST /api/profile/verify`, and the workbench Profile panel provides a matching
`验收并发布` action.

Add `--require-case-runs` when acceptance should also prove runtime coverage.
With that gate enabled, `profile verify` checks every API Case declared by the
profile against the Store's latest API Case run records and fails unless each
case has a latest passed run. The Control plane accepts `requireCaseRuns: true`,
and the workbench exposes the same gate as `要求用例已通过`.

Add `--require-workflow-runs` to apply the same acceptance rule to every
declared Workflow. The Control plane accepts `requireWorkflowRuns: true`, and
the workbench exposes the gate as `要求工作流已通过`.

Verification failures are diagnostic reports, not opaque errors. JSON output
from the CLI and non-2xx Control plane responses both include `ok: false`,
`error`, `summary`, and `checks` so a caller can show the exact missing
acceptance gate without re-running the publish step. The workbench renders the
same failed report inline.

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
  "casePath": "cases/case.alpha.json",
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
