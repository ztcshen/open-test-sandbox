# Go Quality Gate

AgentTestBench uses a local Go quality gate to make AI-generated changes safer.
The gate focuses on new and changed code first; historical debt is reported so
it can be reduced deliberately instead of hidden by mechanical refactors.

## What It Checks

- Oversized Go files, long functions, large structs, large interfaces, high
  function count per file, and large packages.
- Duplicate code through `jscpd`, with special attention to business rules,
  workflow skeletons, error handling, validation, and remote-call wrappers.
- Combined risk, such as a large file that also contains duplicate blocks, or a
  long function that overlaps duplicate logic.
- Go import boundaries inferred from this repository's shape: domain does not
  depend on Store/server/runner implementations, internal packages do not depend
  on CLI entrypoints, and public `pkg` code does not depend on `internal`.
- AI repair safety from git diff: large deletion ratio, deleted tests, public
  API touchpoints, sensitive contract files, and removed error handling.
- Basic Go linting through `golangci-lint`, including `govet`, `staticcheck`,
  `errcheck`, `unused`, `revive`, complexity linters, `dupl`, `depguard`, and
  related hygiene linters.

## Local Commands

Run the report-only gate:

```bash
make quality
```

Run strict mode, where blocking issues exit non-zero:

```bash
QUALITY_GATE_STRICT=true make quality
```

Run Go lint:

```bash
golangci-lint run
```

Run the full existing release gate:

```bash
AGENT_TESTBENCH_SMOKE_STORE_DSN='sqlite:///tmp/agent-testbench-smoke.sqlite' npm run release-check -- --scope PATH
```

Reports are written to:

- `build/reports/quality-gate/quality-gate.md`
- `build/reports/quality-gate/quality-gate.json`
- `build/reports/quality-gate/jscpd/`

## CI

GitHub Actions runs `golangci-lint` and then `npm run release-check`. The release
gate calls `scripts/quality-gate.sh` in report-only mode by default. Turn on
blocking enforcement by setting:

```bash
QUALITY_GATE_STRICT=true
```

For pull requests, the release gate passes changed paths into the quality gate so
old issues are visible but scoped issues receive priority.

## Warning Vs Blocking

Warnings mean "review before expanding this pattern." Blocking means "strict mode
fails until this is fixed, scoped, or explicitly accepted."

Warning thresholds:

- Go file effective lines > 400.
- Function lines > 60.
- Struct fields > 25.
- Interface methods > 10.
- Package effective lines > 1500.
- Package Go files > 20.
- Functions per file > 25.
- Duplicate percentage > 5%.
- Deleted lines > added lines * 2.
- More than 8 changed files.
- Public API, migration, proto, OpenAPI, Swagger, Wire, or error-handling
  deletion touchpoints.

Blocking thresholds:

- Go file effective lines > 600.
- Function lines > 100.
- Struct fields > 40.
- Interface methods > 20.
- Package effective lines > 2500.
- Package Go files > 35.
- Functions per file > 45.
- Duplicate percentage > 8%.
- Core duplicate block >= 40 lines.
- A large file also has duplicate blocks.
- A function longer than 80 lines overlaps duplicate logic.
- A large package contains multiple duplicate blocks.
- A package contains three or more workflow-like duplicate blocks.
- Deleted tests.
- Architecture boundary violations.
- New generic `utils`, `common`, `helper`, or `helpers` packages in core paths.

Config, enum, schema, generated, mock, and fixture-heavy areas are treated more
leniently by default. A large config-like file is warning-only.

## Exclusions

The gate excludes:

- `vendor/**`
- `third_party/**`
- `generated/**`
- `gen/**`
- `mocks/**`
- `mock/**`
- `testdata/**`
- `docs/**`
- `migrations/**`
- `scripts/**`
- `.runtime/**`
- `.scratch/**`
- `node_modules/**`
- `control-plane/static/assets/react/**`
- `**/*.pb.go`
- `**/*.pb.gw.go`
- `**/*.gen.go`
- `**/*_mock.go`
- `**/wire_gen.go`
- `**/swagger/**`
- `**/openapi/**`

`jscpd` excludes `*_test.go` unless
`AGENT_TESTBENCH_DUPLICATION_INCLUDE_TESTS=1` is set.

## Temporary Exceptions

Prefer a scoped report over a blanket exemption:

```bash
scripts/quality-gate.sh --scope-file .release-check-scope
```

If a warning is intentional, explain it in the change summary. If a blocking
issue is intentional, keep report-only mode for that slice and add follow-up work
instead of silencing the rule with a generic abstraction.

## How AI Should Fix Failures

- Start from the duplicate category in `quality-gate.json`.
- Business rule duplication belongs in domain methods, policies, validators, or
  calculators.
- Workflow skeleton duplication belongs in a named usecase helper only when the
  sequence is truly the same.
- Repository/client duplication belongs in repository methods or client wrappers.
- Error handling duplication can be centralized only when the same error
  semantics are preserved.
- DTO/request/response assembly and test fixtures may remain duplicated when
  abstraction would reduce clarity.
- Do not remove cases, tests, branches, or errors to make the report smaller.

## When Not To Abstract

Do not abstract when the duplicated code is only structurally similar but means
different things, when the names would be vague, or when callers would need to
understand hidden flags and options to use the helper safely. In Go, a small
amount of clear duplication is often better than a package named `utils`.

## Interfaces

Do not add an interface because one implementation looks reusable. Add an
interface when there are multiple implementations, a test/mock boundary is
needed, or a dependency boundary is important enough to name. Keep interfaces
small; large interfaces are a design smell and are reported by the gate.

## References

- `golangci-lint`: https://golangci-lint.run/
- `jscpd`: https://github.com/kucherenko/jscpd
- Go modules and internal packages: https://go.dev/ref/mod
