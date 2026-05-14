# Migration From scf-chain-sandbox

This document tracks how reusable code and data move from
`/Users/zlh/codes/scf-chain-sandbox` into Open Test Sandbox.

## Strategy

- Build a new neutral core project instead of renaming the old repository.
- Treat the old repository as the SCF profile/source repository.
- Move generic capabilities into core only after they are scrubbed of
  domain-specific assumptions.
- Export profile configuration into reviewable files.
- Import runtime history and Evidence indexes through commands, not manual
  SQLite edits.

## Data Reuse

Configuration assets should move through profile export/import:

```sh
old-sandboxctl template-config export-profile --out profiles/scf-chain
otsandbox profile import --from profiles/scf-chain
```

Runtime history should move through Evidence import:

```sh
otsandbox evidence import \
  --from /Users/zlh/codes/scf-chain-sandbox/.runtime/control-plane/sandbox.db \
  --profile scf-chain
```

Large Evidence files stay outside git unless a later command explicitly copies
them into a portable archive.

## Storage Direction

- SQLite remains the default local Store.
- PostgreSQL is optional for team or hosted deployments.
- Profile bundles remain file assets regardless of Store backend.

## First Slice

The first slice only creates the neutral project shell and migration guardrails.
Do not copy old business code yet.
