# Contributing

Thanks for helping improve AgentTestBench. The project is local-first and
agent-native and API-operated: the core should stay generic, while team-specific
test assets are stored through AgentTestBench APIs or kept in private
validation data.

## Ground Rules

- Keep business or team language out of core source and default assets.
- Do not add a root directory for team-specific configuration packages.
- Do not commit runtime databases, Evidence bundles, logs, coverage, or local
  browser output.
- Keep changes small enough to verify, but large enough to finish a complete
  user-facing slice.
- Prefer Store-first APIs and generic configuration over hardcoded behavior.

## Local Setup

```sh
npm ci
go test ./...
npm run build:frontend
```

Run the full release gate before opening a pull request:

```sh
AGENT_TESTBENCH_SMOKE_STORE_DSN="postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable" npm run release-check
```

The gate runs formatting hygiene, generated-state checks, source-domain
guardrails, Go tests, the React build, and browser smoke tests.

## Pull Request Checklist

- The change has tests or a clear reason tests are not needed.
- Public CLI, API, Store, or report changes are documented.
- The source-domain guardrail passes.
- Runtime output remains ignored and untracked.
- The README or docs still let a new user complete the quick start.

## Configuration Work

When a change needs new services, workflows, interface nodes, cases, fixtures,
or templates, expose them through Store-first APIs or private validation data.
Core code may read catalog data through Store/read-models, but it should not
bake a specific organization or workflow into package logic.

See [docs/quickstart.md](docs/quickstart.md) for the local workflow.
