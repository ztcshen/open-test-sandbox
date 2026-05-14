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

## Direction

- Keep the default developer experience local and lightweight.
- Use SQLite as the default local Store.
- Add PostgreSQL as an optional team or hosted Store.
- Treat profile bundles as reviewable source assets.
- Treat runtime databases and Evidence files as generated state.

## Current Status

This repository is the empty project shell. The next slices add the migration
manifest, Store boundary, profile loader, and generic Control plane.
