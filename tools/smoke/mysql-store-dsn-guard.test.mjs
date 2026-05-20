import assert from "node:assert/strict";
import { test } from "node:test";

import { inspectMySQLStoreDSN, requireSafeMySQLStoreDSN } from "./mysql-store-dsn-guard.mjs";

test("MySQL Store DSN guard accepts dedicated smoke database names", () => {
  const info = requireSafeMySQLStoreDSN("MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false");

  assert.equal(info.scheme, "mysql");
  assert.equal(info.database, "otsandbox_smoke");
  assert.equal(info.safeName, true);
  assert.equal(info.masked, "mysql://user:xxxxx@example.com:3306/otsandbox_smoke?tls=false");
});

test("MySQL Store DSN guard requires a database path", () => {
  assert.throws(
    () => requireSafeMySQLStoreDSN("mysql://user:secret@example.com:3306?tls=false"),
    /requires a mysql:\/\/ Store DSN with a database path/,
  );
});

test("MySQL Store DSN guard rejects non-MySQL DSNs", () => {
  assert.throws(
    () => requireSafeMySQLStoreDSN("postgres://user:secret@example.com:5432/otsandbox_smoke"),
    /requires a mysql:\/\/ Store DSN/,
  );
});

test("MySQL Store DSN guard refuses likely business databases without leaking passwords", () => {
  const dsn = "mysql://user:secret@example.com:3306/business_prod?tls=false";
  assert.throws(
    () => requireSafeMySQLStoreDSN(dsn),
    (error) => /refuses database 'business_prod'/.test(error.message) && !error.message.includes("secret"),
  );

  const info = inspectMySQLStoreDSN(dsn);
  assert.equal(info.safeName, false);
  assert.equal(info.masked, "mysql://user:xxxxx@example.com:3306/business_prod?tls=false");
  assert.doesNotMatch(JSON.stringify(info), /secret/);
});
