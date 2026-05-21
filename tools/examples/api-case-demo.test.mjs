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
    const store = demoStore("/tmp/ots-demo", { OTSANDBOX_DEMO_STORE: "mysql://user:secret@example.com:3306/otsandbox_demo?tls=false" });
    assert.equal(store.label, "mysql://user:secret@example.com:3306/otsandbox_demo?tls=false");
    assert.deepEqual(store.storeArgs, ["--store", store.label]);
    assert.deepEqual(store.upgradeArgs, ["--store", store.label]);
  });

  it("refuses likely business MySQL demo databases", () => {
    assert.throws(
      () => demoStore("/tmp/ots-demo", { OTSANDBOX_DEMO_STORE: "mysql://user:secret@example.com:3306/business_prod?tls=false" }),
      /refuses database 'business_prod'/,
    );
  });

  it("uses a named Store and lets the CLI resolve it", () => {
    const store = demoStore("/tmp/ots-demo", { OTSANDBOX_DEMO_STORE: "local-personal" });
    assert.equal(store.label, "local-personal");
    assert.deepEqual(store.upgradeArgs, ["--store", "local-personal"]);
  });

  it("uses an explicit SQLite demo Store", () => {
    const store = demoStore("/tmp/ots-demo", { OTSANDBOX_DEMO_STORE: "sqlite:///tmp/ots-demo/store.sqlite" });
    assert.equal(store.label, "sqlite:///tmp/ots-demo/store.sqlite");
    assert.deepEqual(store.storeArgs, ["--store", store.label]);
    assert.deepEqual(store.upgradeArgs, ["--store", store.label]);
  });

  it("can create a temporary SQLite demo Store", () => {
    const store = demoStore("/tmp/ots-demo", { OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO: "1" });
    assert.equal(store.label, "sqlite:///tmp/ots-demo/store.sqlite");
    assert.deepEqual(store.storeArgs, ["--store", store.label]);
  });

  it("rejects contradictory temporary SQLite flags", () => {
    assert.throws(
      () => demoStore("/tmp/ots-demo", { OTSANDBOX_ALLOW_SQLITE_COMPAT_DEMO: "1", OTSANDBOX_DISABLE_SQLITE_STORE: "1" }),
      /cannot be combined/,
    );
  });
});
