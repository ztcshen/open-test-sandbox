import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { demoStore } from "./api-case-demo.mjs";

describe("api-case demo Store selection", () => {
  it("uses the active Store when no explicit demo Store is configured", () => {
    assert.deepEqual(demoStore("/tmp/ots-demo", {}), {
      label: "active Store",
      storeArgs: [],
      upgradeArgs: [],
    });
  });

  it("uses an explicit PostgreSQL demo Store", () => {
    const store = demoStore("/tmp/ots-demo", { OTSANDBOX_DEMO_STORE: "postgres://user:secret@example.com:5432/ots?sslmode=disable" });
    assert.equal(store.label, "postgres://user:secret@example.com:5432/ots?sslmode=disable");
    assert.deepEqual(store.storeArgs, ["--store", store.label]);
    assert.deepEqual(store.upgradeArgs, ["--store", store.label]);
  });

  it("uses an explicit MySQL demo Store", () => {
    const store = demoStore("/tmp/ots-demo", { OTSANDBOX_DEMO_STORE: "mysql://user:secret@example.com:3306/ots?tls=false" });
    assert.equal(store.label, "mysql://user:secret@example.com:3306/ots?tls=false");
    assert.deepEqual(store.storeArgs, ["--store", store.label]);
    assert.deepEqual(store.upgradeArgs, ["--store", store.label]);
  });

  it("uses a named Store and lets the CLI resolve it", () => {
    const store = demoStore("/tmp/ots-demo", { OTSANDBOX_DEMO_STORE: "local-personal" });
    assert.equal(store.label, "local-personal");
    assert.deepEqual(store.upgradeArgs, ["--store", "local-personal"]);
  });

  it("rejects an explicit SQLite Store unless compatibility mode is enabled", () => {
    assert.throws(
      () => demoStore("/tmp/ots-demo", { OTSANDBOX_DEMO_STORE: "sqlite:///tmp/ots-demo/store.sqlite" }),
      /OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO/,
    );
  });

  it("keeps SQLite demo behind an explicit compatibility switch", () => {
    const store = demoStore("/tmp/ots-demo", { OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO: "1" });
    assert.equal(store.label, "sqlite:///tmp/ots-demo/store.sqlite");
    assert.equal(store.sqliteCompat, true);
  });

  it("rejects contradictory SQLite compatibility flags", () => {
    assert.throws(
      () => demoStore("/tmp/ots-demo", { OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO: "1", OTSANDBOX_DISABLE_SQLITE_STORE: "1" }),
      /cannot be combined/,
    );
  });
});
