import assert from "node:assert/strict";
import test from "node:test";

import { requiredSQLStoreDSN } from "./cli-active-store-smoke.mjs";

test("active SQL Store CLI smoke accepts shared Store env", () => {
  const dsn = "MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false";

  assert.equal(requiredSQLStoreDSN({ OTSANDBOX_SMOKE_STORE: dsn }), dsn);
});

test("active SQL Store CLI smoke documents every supported Store env", () => {
  assert.throws(
    () => requiredSQLStoreDSN({}),
    /OTSANDBOX_CLI_STORE_DSN, OTSANDBOX_SMOKE_STORE_DSN, or OTSANDBOX_SMOKE_STORE/,
  );
});

test("active SQL Store CLI smoke rejects non-SQL shared Store env", () => {
  assert.throws(
    () => requiredSQLStoreDSN({ OTSANDBOX_SMOKE_STORE: "sqlite:///tmp/otsandbox.sqlite" }),
    /requires a PostgreSQL or MySQL DSN/,
  );
});
