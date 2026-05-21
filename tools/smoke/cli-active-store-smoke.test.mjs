import assert from "node:assert/strict";
import test from "node:test";

import { requiredSQLStoreDSN } from "./cli-active-store-smoke.mjs";

test("active SQL Store CLI smoke accepts shared Store env", () => {
  const dsn = "MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false";

  assert.equal(requiredSQLStoreDSN({ OTSANDBOX_SMOKE_STORE: dsn }), dsn);
});

test("active SQL Store CLI smoke accepts SQLite Store env", () => {
  const dsn = "sqlite:///tmp/otsandbox.sqlite";

  assert.equal(requiredSQLStoreDSN({ OTSANDBOX_SMOKE_STORE: dsn }), dsn);
});

test("active SQL Store CLI smoke documents every supported Store env", () => {
  assert.throws(
    () => requiredSQLStoreDSN({}),
    /OTSANDBOX_CLI_STORE_DSN, OTSANDBOX_SMOKE_STORE_DSN, or OTSANDBOX_SMOKE_STORE/,
  );
});

test("active SQL Store CLI smoke rejects unsupported shared Store env", () => {
  assert.throws(
    () => requiredSQLStoreDSN({ OTSANDBOX_SMOKE_STORE: "redis://127.0.0.1:6379/0" }),
    /requires a PostgreSQL, MySQL, or SQLite DSN/,
  );
});

test("active SQL Store CLI smoke rejects disabled SQLite Store env", () => {
  assert.throws(
    () => requiredSQLStoreDSN({ OTSANDBOX_SMOKE_STORE: "sqlite:///tmp/otsandbox.sqlite", OTSANDBOX_DISABLE_SQLITE_STORE: "1" }),
    /cannot be combined/,
  );
});

test("active SQL Store CLI smoke refuses likely business MySQL databases", () => {
  assert.throws(
    () => requiredSQLStoreDSN({ OTSANDBOX_SMOKE_STORE: "mysql://user:secret@example.com:3306/business_prod?tls=false" }),
    /refuses database 'business_prod'/,
  );
});
