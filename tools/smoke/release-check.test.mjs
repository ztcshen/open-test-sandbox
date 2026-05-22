import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import os from "node:os";
import path from "node:path";

const rootDir = path.resolve(fileURLToPath(new URL("../..", import.meta.url)));

function configuredTraceIDs(expectedSteps = 3) {
  return JSON.stringify(Object.fromEntries(Array.from({ length: expectedSteps }, (_, index) => {
    const step = `step-${String(index + 1).padStart(2, "0")}`;
    return [step, `trace-${step}`];
  })));
}

function releaseCheckEnv(overrides = {}) {
  const env = { ...process.env };
  delete env.OTS_TRACE_GRAPHQL_URL;
  delete env.OTS_SMOKE_TRACE_IDS;
  delete env.OTSANDBOX_REQUIRE_REAL_SKYWALKING;
  return {
    ...env,
    OTSANDBOX_SMOKE_STORE_DSN: "postgres://user:pass@127.0.0.1:5432/otsandbox_smoke?sslmode=disable",
    ...overrides,
  };
}

function runReleaseCheck(env) {
  return spawnSync("bash", ["tools/release-check.sh"], {
    cwd: rootDir,
    env,
    encoding: "utf8",
    stdio: "pipe",
  });
}

function runRealMySQLWrapper(env) {
  const wrapperEnv = { ...process.env };
  delete wrapperEnv.OTSANDBOX_REAL_MYSQL_STORE_DSN;
  delete wrapperEnv.OTSANDBOX_SMOKE_STORE_DSN;
  delete wrapperEnv.OTSANDBOX_SMOKE_STORE;
  return spawnSync("bash", ["tools/smoke/mysql-real-store-release-check.sh"], {
    cwd: rootDir,
    env: { ...wrapperEnv, ...env },
    encoding: "utf8",
    stdio: "pipe",
  });
}

function runNPM(args, env) {
  return spawnSync("npm", args, {
    cwd: rootDir,
    env: { ...process.env, ...env },
    encoding: "utf8",
    stdio: "pipe",
  });
}

test("release-check blocks tracked private test assets before expensive gates", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "ots-release-index-"));
  try {
    const indexFile = path.join(tempDir, "index");
    const gitEnv = { ...process.env, GIT_INDEX_FILE: indexFile };
    const emptyBlob = spawnSync("git", ["hash-object", "-w", "--stdin"], {
      cwd: rootDir,
      input: "",
      encoding: "utf8",
      stdio: ["pipe", "pipe", "pipe"],
    });
    assert.equal(emptyBlob.status, 0, emptyBlob.stderr);
    const readTree = spawnSync("git", ["read-tree", "HEAD"], {
      cwd: rootDir,
      env: gitEnv,
      encoding: "utf8",
      stdio: "pipe",
    });
    assert.equal(readTree.status, 0, readTree.stderr);
    const addPrivatePath = spawnSync("git", [
      "update-index",
      "--add",
      "--cacheinfo",
      `100644,${emptyBlob.stdout.trim()},test-private/secret-profile.json`,
    ], {
      cwd: rootDir,
      env: gitEnv,
      encoding: "utf8",
      stdio: "pipe",
    });
    assert.equal(addPrivatePath.status, 0, addPrivatePath.stderr);

    const result = runReleaseCheck(releaseCheckEnv({ GIT_INDEX_FILE: indexFile }));

    assert.equal(result.status, 1);
    assert.match(result.stderr, /generated or local-only paths are tracked/);
    assert.match(result.stderr, /test-private\/secret-profile\.json/);
    assert.doesNotMatch(result.stdout, /running Go tests/);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("release-check missing Store guidance lists every supported smoke Store env", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_SMOKE_STORE_DSN: "",
    OTSANDBOX_SMOKE_STORE: "",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /OTSANDBOX_SMOKE_STORE_DSN or OTSANDBOX_SMOKE_STORE is required/);
  assert.match(result.stderr, /SQL Store examples:/);
  assert.match(result.stderr, /PostgreSQL: OTSANDBOX_SMOKE_STORE_DSN='postgres:\/\/user:pass@host:5432\/otsandbox_smoke\?sslmode=disable'/);
  assert.match(result.stderr, /MySQL: OTSANDBOX_SMOKE_STORE='mysql:\/\/user:pass@host:3306\/otsandbox_smoke\?tls=false'/);
  assert.match(result.stderr, /OTSANDBOX_SMOKE_STORE='mysql:\/\/user:pass@host:3306\/otsandbox_smoke\?tls=false'/);
  assert.doesNotMatch(result.stderr, /also supported/i);
  assert.doesNotMatch(result.stdout, /checking SkyWalking smoke provider mode/);
});

test("release-check refuses unsafe MySQL smoke database names before expensive gates", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_SMOKE_STORE_DSN: "mysql://user:secret@example.com:3306/business_prod?tls=false",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /Refusing to run release-check against MySQL database 'business_prod'/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode requires a GraphQL URL before expensive gates", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires OTS_TRACE_GRAPHQL_URL/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode rejects invalid GraphQL URLs before expensive gates", () => {
  for (const graphQLURL of ["not-a-url", "ftp://skywalking.example/graphql"]) {
    const result = runReleaseCheck(releaseCheckEnv({
      OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
      OTS_TRACE_GRAPHQL_URL: graphQLURL,
      OTS_SMOKE_EXPECTED_STEPS: "3",
      OTS_SMOKE_TRACE_IDS: configuredTraceIDs(),
    }));

    assert.equal(result.status, 1, graphQLURL);
    assert.match(result.stderr, /requires OTS_TRACE_GRAPHQL_URL to be an http\/https URL/);
    assert.doesNotMatch(result.stdout, /running Go tests/);
  }
});

test("release-check accepts uppercase SQL Store schemes before expensive gates", () => {
  const mysql = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_SMOKE_STORE_DSN: "MYSQL://user:pass@127.0.0.1:3306/otsandbox_smoke?tls=false",
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
  }));
  assert.equal(mysql.status, 1);
  assert.match(mysql.stderr, /requires OTS_TRACE_GRAPHQL_URL/);
  assert.doesNotMatch(mysql.stderr, /must be postgres:\/\/, postgresql:\/\/, or mysql:\/\//);
  assert.doesNotMatch(mysql.stdout, /running Go tests/);

  const postgres = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_SMOKE_STORE_DSN: "POSTGRESQL://user:pass@127.0.0.1:5432/otsandbox_smoke?sslmode=disable",
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
  }));
  assert.equal(postgres.status, 1);
  assert.match(postgres.stderr, /requires OTS_TRACE_GRAPHQL_URL/);
  assert.doesNotMatch(postgres.stderr, /must be postgres:\/\/, postgresql:\/\/, or mysql:\/\//);
  assert.doesNotMatch(postgres.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode requires configured workflow trace ids before expensive gates", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_EXPECTED_STEPS: "3",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires OTS_SMOKE_TRACE_IDS/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode requires trace ids for every configured workflow step", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_EXPECTED_STEPS: "3",
    OTS_SMOKE_TRACE_IDS: "step-01=trace.real.01",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /every configured workflow step/);
  assert.match(result.stderr, /step-02/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode rejects empty workflow step trace ids", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_EXPECTED_STEPS: "3",
    OTS_SMOKE_TRACE_IDS: [
      "step-01=trace.real.01",
      "step-02=",
      "step-03=trace.real.03",
    ].join(","),
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /every configured workflow step/);
  assert.match(result.stderr, /step-02/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("real MySQL release wrapper requires a dedicated MySQL Store DSN", () => {
  const missing = runRealMySQLWrapper({
    OTSANDBOX_REAL_MYSQL_STORE_DSN: "",
    OTSANDBOX_SMOKE_STORE_DSN: "",
  });
  assert.equal(missing.status, 1);
  assert.match(missing.stderr, /Set OTSANDBOX_REAL_MYSQL_STORE_DSN/);

  const postgres = runRealMySQLWrapper({
    OTSANDBOX_REAL_MYSQL_STORE_DSN: "postgres://user:secret@example.com:5432/otsandbox_smoke?sslmode=disable",
  });
  assert.equal(postgres.status, 1);
  assert.match(postgres.stderr, /must be a mysql:\/\/ DSN/);
});

test("real MySQL release wrapper refuses likely business databases", () => {
  const result = runRealMySQLWrapper({
    OTSANDBOX_REAL_MYSQL_STORE_DSN: "mysql://user:secret@example.com:3306/business_prod?tls=false",
  });

  assert.equal(result.status, 1);
  assert.match(result.stderr, /Refusing to run release-check/);
  assert.match(result.stderr, /business_prod/);
});

test("real MySQL release wrapper requires real SkyWalking sign-off inputs", () => {
  const result = runRealMySQLWrapper({
    OTSANDBOX_REAL_MYSQL_STORE_DSN: "MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false",
    OTSANDBOX_REAL_MYSQL_RELEASE_DRY_RUN: "1",
  });

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires OTSANDBOX_REQUIRE_REAL_SKYWALKING=1/);
  assert.doesNotMatch(result.stderr, /secret/);
});

test("real MySQL release wrapper rejects invalid or non-http SkyWalking GraphQL URLs", () => {
  for (const graphQLURL of ["not-a-url", "ftp://skywalking.example/graphql"]) {
    const result = runRealMySQLWrapper({
      OTSANDBOX_REAL_MYSQL_STORE_DSN: "mysql://user:secret@example.com:3306/otsandbox_smoke?tls=false",
      OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
      OTS_TRACE_GRAPHQL_URL: graphQLURL,
      OTS_SMOKE_EXPECTED_STEPS: "3",
      OTS_SMOKE_TRACE_IDS: configuredTraceIDs(),
      OTSANDBOX_REAL_MYSQL_RELEASE_DRY_RUN: "1",
    });

    assert.equal(result.status, 1, graphQLURL);
    assert.match(result.stderr, /requires OTS_TRACE_GRAPHQL_URL to be an http\/https URL/);
    assert.doesNotMatch(result.stderr, /secret/);
    assert.doesNotMatch(result.stderr, /Would run: npm run release-check/);
  }
});

test("real MySQL release wrapper requires existing-database contract mode", () => {
  const result = runRealMySQLWrapper({
    OTSANDBOX_REAL_MYSQL_STORE_DSN: "mysql://user:secret@example.com:3306/otsandbox_smoke?tls=false",
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_EXPECTED_STEPS: "3",
    OTS_SMOKE_TRACE_IDS: configuredTraceIDs(),
    OTSANDBOX_MYSQL_TEST_DSN_MODE: "create-drop",
    OTSANDBOX_REAL_MYSQL_RELEASE_DRY_RUN: "1",
  });

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires OTSANDBOX_MYSQL_TEST_DSN_MODE=existing/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.doesNotMatch(result.stderr, /Would run: npm run release-check/);
});

test("real MySQL release wrapper dry-run masks credentials and accepts smoke database", () => {
  const result = runRealMySQLWrapper({
    OTSANDBOX_REAL_MYSQL_STORE_DSN: "MYSQL://user:secret@example.com:3306/otsandbox_smoke?tls=false",
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_EXPECTED_STEPS: "3",
    OTS_SMOKE_TRACE_IDS: configuredTraceIDs(),
    OTSANDBOX_REAL_MYSQL_RELEASE_DRY_RUN: "1",
  });

  assert.equal(result.status, 0);
  assert.match(result.stderr, /mysql:\/\/user:xxxxx@example.com:3306\/otsandbox_smoke/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.match(result.stderr, /MySQL Store contract mode: existing/);
  assert.match(result.stderr, /Real SkyWalking release mode: required/);
  assert.match(result.stderr, /Would run: npm run release-check/);
});

test("real MySQL release preflight npm script runs the guarded dry-run", () => {
  const result = runNPM(["run", "release-check:mysql-real:preflight"], {
    OTSANDBOX_REAL_MYSQL_STORE_DSN: "mysql://user:secret@example.com:3306/otsandbox_smoke?tls=false",
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_EXPECTED_STEPS: "3",
    OTS_SMOKE_TRACE_IDS: configuredTraceIDs(),
  });

  assert.equal(result.status, 0);
  assert.match(result.stderr, /mysql:\/\/user:xxxxx@example.com:3306\/otsandbox_smoke/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.match(result.stderr, /Would run: npm run release-check/);
});

test("real MySQL release wrapper accepts shared smoke Store env", () => {
  const result = runRealMySQLWrapper({
    OTSANDBOX_REAL_MYSQL_STORE_DSN: "",
    OTSANDBOX_SMOKE_STORE_DSN: "",
    OTSANDBOX_SMOKE_STORE: "mysql://user:secret@example.com:3306/otsandbox_smoke?tls=false",
    OTSANDBOX_REQUIRE_REAL_SKYWALKING: "1",
    OTS_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    OTS_SMOKE_EXPECTED_STEPS: "3",
    OTS_SMOKE_TRACE_IDS: configuredTraceIDs(),
    OTSANDBOX_REAL_MYSQL_RELEASE_DRY_RUN: "1",
  });

  assert.equal(result.status, 0);
  assert.match(result.stderr, /mysql:\/\/user:xxxxx@example.com:3306\/otsandbox_smoke/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.match(result.stderr, /Would run: npm run release-check/);
});
