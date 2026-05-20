import assert from "node:assert/strict";
import { test } from "node:test";

import { requiredMySQLDSN } from "./mysql-store-api-smoke.mjs";

test("MySQL API smoke accepts the shared SQL smoke Store env", () => {
  assert.equal(
    requiredMySQLDSN({
      OTSANDBOX_SMOKE_STORE: "MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false",
    }),
    "MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false",
  );
});

test("MySQL API smoke prefers its dedicated DSN over shared smoke Store env", () => {
  assert.equal(
    requiredMySQLDSN({
      OTSANDBOX_MYSQL_API_SMOKE_DSN: "mysql://user:secret@example.com:3306/otsandbox_api?tls=false",
      OTSANDBOX_SMOKE_STORE_DSN: "mysql://user:secret@example.com:3306/otsandbox_release?tls=false",
      OTSANDBOX_SMOKE_STORE: "mysql://user:secret@example.com:3306/otsandbox_legacy?tls=false",
    }),
    "mysql://user:secret@example.com:3306/otsandbox_api?tls=false",
  );
});

test("MySQL API smoke rejects non-MySQL shared Store env", () => {
  assert.throws(
    () => requiredMySQLDSN({
      OTSANDBOX_SMOKE_STORE: "postgres://user:secret@example.com:5432/otsandbox_smoke?sslmode=disable",
    }),
    /requires a mysql:\/\/ Store DSN/,
  );
});
