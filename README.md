# Open Test Sandbox

Open Test Sandbox is a local-first integration test workbench for
template-driven Workflows, API Cases, fixtures, and Evidence.

The project is intentionally small at the start. The first milestone is a
neutral core that can load profiles, run tests, and record reproducible
Evidence without baking any one business domain into the product.

## Quick Start

```sh
./bin/otsandbox.sh version
go test ./...
```

Run the default local Store migration:

```sh
tmpdir=$(mktemp -d)
./bin/otsandbox.sh store status --store-url "$tmpdir/store.sqlite"
./bin/otsandbox.sh store migrate --store-url "$tmpdir/store.sqlite"
```

Inspect the empty profile bundle:

```sh
./bin/otsandbox.sh profile inspect --profile profiles/empty
```

Render a dry-run API Case Evidence bundle:

```sh
tmpdir=$(mktemp -d)
./bin/otsandbox.sh case run \
  --case examples/api-cases/create-item.json \
  --dry-run \
  --run-id quickstart \
  --evidence-dir "$tmpdir/evidence"
find "$tmpdir/evidence/quickstart" -maxdepth 1 -type f | sort
```

The API Case JSON format and Evidence output contract are documented in
[docs/api-case-format.md](docs/api-case-format.md).
Store backend support is documented in
[docs/store-backends.md](docs/store-backends.md).

## Direction

- Keep the default developer experience local and lightweight.
- Use SQLite as the default local Store.
- Add PostgreSQL as an optional team or hosted Store.
- Treat profile bundles as reviewable source assets.
- Treat runtime databases and Evidence files as generated state.

## Current Status

The project now has a neutral CLI, SQLite Store, profile loader, generic Control
plane, runtime Evidence import path, and API Case runner. Domain-specific data
belongs in profile/config bundles, not in core source code.
