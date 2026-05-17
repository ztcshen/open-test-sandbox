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
- Added `case suite inspect` and `/api/case/suite-inspection` for pre-run
  readiness checks across maintained case sets.
- Added `case suite plan`, `/api/case/suite-plan`, and exact `caseIds` support
  in asynchronous batch runs so agents can turn inspection results into a
  deterministic executable payload.
- Added `case suite quality-report` to export maintained-case quality actions
  as compact JSON and HTML artifacts for agent handoff and team review.
- Added `evidence tasks` and `/api/post-process-tasks` for inspecting stored
  topology, log, and report post-process task status and duration by run.
- Added JUnit XML output for maintained suite reports and asynchronous batch
  runs for CI systems that consume test result artifacts.
- Added asynchronous batch artifact manifests that list JSON, HTML, JUnit, case
  detail, and case Evidence artifacts for archival automation.
- Added asynchronous batch failure summaries so agents can fetch only failed
  cases with detail links, Evidence paths, elapsed time, and assertion errors.
- Added `case suite impact` and `/api/case/suite-impact` so change-aware
  agents can turn changed paths or target hints into a runnable case batch plan.
- Added `case suite impact-report` and `/api/case/suite-impact-runs` for
  one-step impact selection plus report or asynchronous batch execution.
- Added `case suite stability` and `/api/case/suite-stability` to flag
  maintained cases whose recent Store history alternates between pass and fail.
- Added `case suite priority` and `/api/case/suite-priority` to rank maintained
  cases by impact signals, latest Store status, stability, and case priority
  before building a batch request.
- Added `case suite brief` and `/api/case/suite-brief` so agents can fetch
  coverage, readiness, stability, ranked recommendations, and a batch request
  in one call before deciding what to execute.
- Added `interface-node case draft` and expanded `interface-node case apply`
  so agents can generate reviewable maintained-case bundles, then apply API
  case metadata, execution config, and runnable case files to external profile
  bundles without touching the Store directly.
- Added `case suite quality` and `/api/case/suite-quality` to audit maintained
  case authoring quality, including uncovered interface nodes, missing metadata,
  missing runnable sources, and missing execution config.
- Added `case suite quality-plan` and `/api/case/suite-quality-plan` to turn
  maintained-case quality gaps into stable authoring actions for agents.
