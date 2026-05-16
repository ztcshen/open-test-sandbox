# Open Test Sandbox

Open Test Sandbox is a local-first integration test workbench for
template-driven Workflows, API Cases, fixtures, and Evidence.

The project is intentionally small at the start. The first milestone is a
neutral core that can load external profile bundles, run tests, and record
reproducible Evidence without baking any one business domain into the product.

## Quick Start

```sh
./bin/otsandbox.sh version
go test ./...
npm run demo:api-case
```

Run the default local Store schema upgrade:

```sh
tmpdir=$(mktemp -d)
./bin/otsandbox.sh store status --store-url "$tmpdir/store.sqlite"
./bin/otsandbox.sh store upgrade --store-url "$tmpdir/store.sqlite"
```

Publish an external profile bundle into the local Store:

```sh
PROFILE_DIR=$(mktemp -d)/sample-profile
./bin/otsandbox.sh profile init --output "$PROFILE_DIR" --id sample
./bin/otsandbox.sh profile install --from "$PROFILE_DIR"
./bin/otsandbox.sh profile list
./bin/otsandbox.sh profile pack --profile sample --output "$tmpdir/sample-profile.tar.gz"
./bin/otsandbox.sh profile verify --profile sample --store-url "$tmpdir/store.sqlite"
./bin/otsandbox.sh serve --profile sample --store-url "$tmpdir/store.sqlite"
```

When a profile has runnable API Cases and the local Store already contains run
records, add `--require-case-runs` to make verification fail unless each case's
latest run passed. Add `--require-workflow-runs` to require each declared
Workflow's latest Store run to have passed as well.

The core repository intentionally does not contain bundled profiles. Keep
business or team-specific bundles in a separate location or repository, then
install them into the local profile home, publish them through `profile verify`,
`config publish`, or the Control plane import API, so the UI reads the generated
Store/read-model.
Profile installation copies source profile assets only; local runtime state
such as `.runtime/`, SQLite/database files, logs, and VCS directories is skipped.
Use `profile pack` to create the same clean distributable archive from either a
profile path or an installed profile id. `profile install --from bundle.tar.gz`
installs that archive into another profile home with the same filtering rules;
`profile audit`, `profile import`, `profile verify`, `config publish`, and the
matching Control plane APIs can also accept the archive path and will install it
before auditing or publishing Store/read-model data.

Run an API Case and write an Evidence bundle:

```sh
tmpdir=$(mktemp -d)
./bin/otsandbox.sh case run \
  --case examples/api-cases/create-item.json \
  --base-url http://127.0.0.1:8080 \
  --run-id quickstart \
  --evidence-dir "$tmpdir/evidence"
find "$tmpdir/evidence/quickstart" -maxdepth 1 -type f | sort
```

The API Case JSON format and Evidence output contract are documented in
[docs/api-case-format.md](docs/api-case-format.md).
Store backend support is documented in
[docs/store-backends.md](docs/store-backends.md).

For a complete first-run guide, see [docs/quickstart.md](docs/quickstart.md).
The full documentation map is in [docs/index.md](docs/index.md).
Backend capability coverage is summarized in
[docs/backend-capabilities.md](docs/backend-capabilities.md).
For profile bundle authoring, see
[docs/profile-authoring.md](docs/profile-authoring.md). Agent and CI
integration contracts are summarized in
[docs/cli-api-contracts.md](docs/cli-api-contracts.md).

## Direction

- Keep the default developer experience local and lightweight.
- Use SQLite as the default local Store.
- Add PostgreSQL as an optional team or hosted Store.
- Treat profile bundles as reviewable source assets outside this core repo.
- Treat runtime databases and Evidence files as generated state.

## Current Status

The project now has:

- a neutral CLI and generic Control plane;
- a SQLite Store with schema upgrades, contract tests, Evidence queries, baseline
  gates, and backend URL validation;
- a profile loader for manifest and split-asset bundles;
- external profile bundle initialization, local profile-home installation,
  audit-gated publishing, Store/read-model publishing, and CLI verification;
- API Case execution with reproducible Evidence and Store indexes;
- request template rendering from profile-owned fixture data;
- workflow planning from profile-owned bindings;
- runtime Evidence import with text and JSON reports.

Domain-specific data belongs in profile/config bundles, not in core source code.

## Contributing and Release Gates

Run the full local gate before publishing a change:

```sh
npm run release-check
```

The gate runs generated-state checks, source-domain guardrails, Go tests, the
React build, and browser smoke tests. See [CONTRIBUTING.md](CONTRIBUTING.md)
and [docs/release-checklist.md](docs/release-checklist.md) for the public
workflow.

Open Test Sandbox is licensed under the Apache License 2.0.
