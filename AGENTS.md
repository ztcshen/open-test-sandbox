# AgentTestBench Agent Guide

AgentTestBench is a new open-source-oriented project. Keep the core generic,
agent-native, API-operated, Store-first, and local-first.

## Local Workflow

- Do not trigger the Multica issue workflow for this repository by default.
  Treat direct user messages in `/Users/zlh/codes/agent-testbench` as local
  project work unless the user explicitly asks to read, comment on, or update a
  Multica issue.
- Do not post Multica comments, change issue status, or fetch issue context for
  ordinary local AgentTestBench questions and implementation tasks.

## Core Rules

- Do not hardcode a concrete business domain into core packages.
- Source code and default core assets must not contain source-domain terms.
  Put domain-specific names and language only in private validation/config data.
- Treat test engineers and agents as workbench users, not external configuration maintainers.
  Day-to-day testing should be possible through AgentTestBench APIs and UI discovery,
  with minimal one-time registration when a runtime or service must be known.
- SQL Store is the active source of truth for current sandbox configuration,
  runtime facts, workflow catalog, execution state, Evidence indexes, and
  verification results. PostgreSQL and MySQL are supported product Store
  engines; SQLite is compatibility-only.
- The sandbox's own SQL Store/control-plane database must be provisioned
  outside any Docker environment restored for a tested target, and must remain
  separate from target application databases. Environment restore may start
  tested services and their business databases, but it must not start or host
  the sandbox Store itself.
- Environment Catalog entries must be Store-first. Test engineers register,
  discover, inspect, bootstrap, verify, and publish verified environments through
  CLI/API/UI surfaces backed by the active Store or an explicit `--store
  NAME_OR_DSN`.
- An environment may enter the verified discovery list only after its acceptance
  workflow has passed and its Evidence plus real SkyWalking topology are
  complete.
- Portable template packages are optional artifacts for import, export, review,
  migration, and sharing. Do not introduce new mandatory file-package-first
  flows for normal testing.
- Prefer Store-first APIs and UI paths for new behavior. Add file-package
  adapters only as compatibility or import/export bridges.
- PostgreSQL and MySQL are both product Store engines for personal and team
  workflows; teams should pick the engine that matches their operational
  environment.
- SQLite is retained only for legacy migration, compatibility, and tests.
- Runtime Evidence, logs, and local databases must not be committed.
- Prefer small, verifiable slices with tests and a commit per slice.
- Keep first-party source files below the project line budget. When a
  production or test file approaches roughly 1,200 lines, split cohesive
  behavior into package-local files before adding more logic; do not grow
  already-oversized files except in move-only reduction slices.
- Do not treat file splitting alone as a successful refactor. Each oversized
  file slice must also pass a duplicate-code static gate with a mature
  open-source detector such as Go `dupl`/`golangci-lint dupl` or `jscpd`.
  When the gate reports meaningful clones introduced or exposed by the slice,
  extract shared helpers or abstractions before committing instead of leaving
  copy-paste families in separate files.
- Use headless/background verification for local browser checks.
- For any moderately large change, first do web research and ground the design
  in mature open-source projects before editing. This is mandatory when the
  change is expected to touch 3 or more files or exceed roughly 200 lines of
  code. Do not rely on pure inference to generate substantial architecture,
  API, persistence, migration, or workflow code.

## AI Go Quality Gate Rules

- Before adding Go behavior, search for an existing package, interface,
  repository, client, validator, policy, or domain method that already owns the
  concept.
- Do not copy similar service, usecase, handler, or CLI command flows just to
  move faster. If repeated code represents a real rule, move the rule to a
  domain method, policy, validator, calculator, repository method, or client
  wrapper with a business name.
- Do not create `utils`, `common`, `helper`, or `helpers` packages to satisfy a
  duplicate-code gate. Package names must describe the domain or boundary they
  own.
- Do not introduce an interface for reuse alone. In Go, add an interface only
  when there are multiple implementations, a test/mock seam is needed, or the
  interface marks an important dependency boundary.
- Before splitting a large file, state the responsibility boundary. Before
  splitting a large package, state the package semantics. File movement alone
  does not count as a completed refactor.
- When fixing duplication, do not delete business branches, error handling, or
  tests to reduce reported lines. Public functions, interfaces, struct fields,
  HTTP routes, proto/OpenAPI contracts, migrations, and wire files require an
  explicit impact note.
- If keeping duplication is clearer, say why. DTO assembly, generated code,
  large test fixtures, and intentionally parallel request/response shapes can
  stay duplicated when abstraction would hide intent.
- Run `go test ./...` and `make quality` before handing off Go changes. Use
  `QUALITY_GATE_STRICT=true make quality` only when the slice is ready for
  blocking enforcement.

## Project Shape

- `cmd/agent-testbench/`: CLI entrypoint.
- `internal/server/controlplane/`: generic control-plane API and workbench server.
- `internal/runner/`: automation runners, request rendering, report output, and Evidence import helpers.
- `internal/domain/`: generic profile, case-suite, redaction, and audit domain logic.
- `internal/store/`: SQL Store contract, openers, migrations, and backend adapters.
- `docs/`: public docs.
- `tools/guardrails/`: local quality gates and repository checks.

Domain-specific validation data lives outside this core repository. If a
portable template package exists, it is imported into the local Store instead
of becoming the daily maintenance surface.

## Naming

The product name is **AgentTestBench** and the preferred repository slug is
`agent-testbench`. The primary CLI path is `cmd/agent-testbench`, the wrapper is
`bin/agent-testbench.sh`, and public environment variables use the
`AGENT_TESTBENCH_*` namespace.
