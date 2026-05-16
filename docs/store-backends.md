# Store Backends

Open Test Sandbox uses SQLite as the default local Store. The Store holds
runtime indexes, run records, imported profile indexes, and baseline gate state;
external profile bundles and Evidence files remain file-first source artifacts.

## Supported URLs

These forms currently resolve to the SQLite backend:

- empty `--store-url`: `.runtime/store.sqlite`
- local file paths: `/tmp/otsandbox/store.sqlite`
- `sqlite://PATH`
- `file:PATH`

Unsupported backend schemes fail early with an actionable error. This keeps
optional team or hosted backends explicit instead of silently treating a URL
such as `postgres://...` as a local filename.

## Future Hosted Mode

PostgreSQL is reserved for a future team or hosted Store. Adding it should keep
SQLite as the default and route backend-specific behavior behind the generic
Store interface rather than changing external profile bundle files.
