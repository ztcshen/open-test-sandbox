# Changelog

All notable changes to Open Test Sandbox will be recorded here.

The project is pre-1.0. Public contracts may still change, but breaking changes
should be called out in this file and in the relevant docs.

## Unreleased

- Added local-first Store, profile, workflow, API case, Evidence, report, and
  Control plane foundations.
- Added external profile installation, audit, verification, packing, and
  Store/read-model publishing.
- Added API case run Evidence, asynchronous batch reports, workflow reports,
  and synchronous failed case Evidence lookup.
- Added source-domain guardrails to keep core code generic.
- Added open-source readiness docs, CI gate, and release checklist.
- Added a backend capabilities overview and richer documentation entry points.
- Expanded the repository homepage with English and Chinese introductions,
  capability summaries, architecture notes, and agent workflow guidance.
- Added public exposure materials with use cases, a share kit, and a roadmap.
- Added API case maintenance metadata and `case discover` for searchable case
  catalogs.
- Added `case suite report` for running maintained case sets selected by tag,
  owner, priority, status, node, or text filter.
- Added `case suite coverage` for latest passed, failed, and not-run status
  across maintained case sets without re-running requests.
- Added `/api/case/suite-coverage` so agents, CI, and the workbench can read
  maintained suite coverage through the Control plane.
- Added maintained suite selectors to `/api/cases/batch-runs`, including
  `runStates` for rerunning only failed or not-run cases.
- Added a shared backend case-suite module so CLI reports, Control plane
  coverage, and batch rerun selectors use the same maintenance rules.
