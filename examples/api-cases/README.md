# API Case Examples

`create-item.json` is a small generic API Case used by the quickstart and
release smoke. It is intentionally domain-neutral: the case can run against the
temporary local demo server and write Evidence plus Store indexes into either a
PostgreSQL or MySQL SQL Store.

Use it through the demo entrypoint:

```sh
AGENT_TESTBENCH_DEMO_STORE='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run demo:api-case
# or
AGENT_TESTBENCH_DEMO_STORE='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run demo:api-case
```

MySQL demo Stores must use a dedicated sandbox/smoke/test/CI-looking database
name. SQLite is available only when an explicit compatibility flag is used.

The format and Evidence contract are documented in
[../../docs/api-case-format.md](../../docs/api-case-format.md). Broader Store,
workflow, topology, and release-gate behavior is covered by
[../../README.md](../../README.md) and [../../docs/index.md](../../docs/index.md).
