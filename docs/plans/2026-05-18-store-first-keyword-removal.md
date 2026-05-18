# Store-first Naming Migration

## Decision

Open Test Sandbox is an API-operated testing workbench. Test engineers consume
the running sandbox through APIs and UI; they do not maintain a second local
project or repeatedly edit external files.

SQLite/Store is the active source of truth for:

- current sandbox state;
- service and runtime registrations;
- workflow catalog and interface cases;
- execution state;
- Evidence indexes;
- validation results.

Portable template packages may exist only for import, export, review, sharing,
or migration. They are not the daily testing surface.

## Migration Rules

- New API routes must use Store-first words such as `catalog`, `state`,
  `template-package`, `service`, `workflow`, `case`, and `runtime`.
- New UI copy must describe actions from the test engineer viewpoint: discover,
  register once, run, inspect, verify.
- New database changes must treat SQLite/Store as active state, not a derived
  index of a file tree.
- Service registration and workflow/interface registration are decoupled.
  Workflows check interface availability and step binding completeness only.
  Interfaces check their own entry service availability when they execute.
  Downstream services are topology and Evidence observations, not registration
  prerequisites.
- Existing legacy routes and fields can remain only behind compatibility
  aliases while callers migrate.
- Do not add new user-facing copy, docs, or prompt rules using the old
  file-package term.

## Required Slices

1. Add Store-first API aliases for current legacy package routes.
2. Update the React workbench to call the Store-first API aliases.
3. Rename user-facing payload fields while accepting legacy fields on input.
4. Rename internal Store models from package-oriented names to catalog/state
   names.
5. Move compatibility loaders into an import/export adapter package.
6. Remove old CLI words after the Store-first commands and tests are stable.
7. Add a guardrail that blocks the legacy word in docs, UI copy, prompts, and
   new source identifiers.

## Verification

- Main-project `npm run release-check` must stay green after every naming slice.
- The local workbench must still expose the core workflow entry path.
- Full runtime validation remains a heavy gate and must be run before declaring
  the core workflow green.

## Implementation Progress

- Store-first template package API aliases now cover import, verify,
  audit-plan, install, installed list, and catalog-index routes under
  `/api/template-packages/*`. Legacy `/api/profile/*` routes remain as
  compatibility aliases.
- The React workbench and headless smoke use the Store-first
  `/api/template-packages/*` routes for daily import, install, verify, and
  catalog-index flows.
