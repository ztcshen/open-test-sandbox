# Open Test Sandbox Agent Guide

Open Test Sandbox is a new open-source-oriented project. Keep the core generic,
profile-driven, and local-first.

## Core Rules

- Do not hardcode a concrete business domain into core packages.
- Keep template configuration as reviewable files first; databases are indexes
  and runtime stores.
- SQLite is the default local Store.
- PostgreSQL is optional for team or hosted mode.
- Runtime Evidence, logs, and local databases must not be committed.
- Prefer small, verifiable slices with tests and a commit per slice.
- Use headless/background verification for local browser checks.

## Project Shape

- `cmd/otsandbox/`: CLI entrypoint.
- `internal/`: future core packages.
- `profiles/`: future profile bundles.
- `docs/`: public docs and migration notes.
- `tools/migration/`: one-time and repeatable migration helpers.

## Naming

The working product name is **Open Test Sandbox**. Keep names easy to change
until the first public release.
