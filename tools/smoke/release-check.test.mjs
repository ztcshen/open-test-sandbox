import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { chmod, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
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
  delete env.AGENT_TESTBENCH_TRACE_GRAPHQL_URL;
  delete env.AGENT_TESTBENCH_SMOKE_TRACE_IDS;
  delete env.AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING;
  delete env.AGENT_TESTBENCH_RELEASE_CHECK_SCOPE;
  return {
    ...env,
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "postgres://user:pass@127.0.0.1:5432/agent_testbench_smoke?sslmode=disable",
    ...overrides,
  };
}

function runReleaseCheck(env, args = []) {
  return spawnSync("bash", ["tools/release-check.sh", ...args], {
    cwd: rootDir,
    env,
    encoding: "utf8",
    stdio: "pipe",
  });
}

function runRealMySQLWrapper(env) {
  const wrapperEnv = { ...process.env };
  delete wrapperEnv.AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN;
  delete wrapperEnv.AGENT_TESTBENCH_SMOKE_STORE_DSN;
  delete wrapperEnv.AGENT_TESTBENCH_SMOKE_STORE;
  delete wrapperEnv.AGENT_TESTBENCH_RELEASE_CHECK_SCOPE;
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
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-release-index-"));
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

    const result = runReleaseCheck(releaseCheckEnv({ GIT_INDEX_FILE: indexFile }), ["--full"]);

    assert.equal(result.status, 1);
    assert.match(result.stderr, /generated or local-only paths are tracked/);
    assert.match(result.stderr, /test-private\/secret-profile\.json/);
    assert.doesNotMatch(result.stdout, /running Go tests/);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("release-check requires an explicit scope or full sign-off mode", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "sqlite:///tmp/agent-testbench-requires-scope.sqlite",
  }));

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires --scope, --scope-file, or --full/);
  assert.match(result.stderr, /npm run release-check -- --scope/);
  assert.match(result.stderr, /npm run release-check -- --full/);
  assert.doesNotMatch(result.stdout, /checking SQL smoke Store/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check rejects mixed full and scoped modes", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "sqlite:///tmp/agent-testbench-mixed-scope.sqlite",
  }), ["--full", "--scope", "docs"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /cannot combine --full with --scope or --scope-file/);
  assert.doesNotMatch(result.stdout, /checking SQL smoke Store/);
});

test("release-check scope ignores unrelated untracked source-domain matches", async () => {
  const outsideDir = path.join(rootDir, ".scratch", "release-check-scope-test");
  const outsideFile = path.join(outsideDir, "notes.md");
  try {
    await mkdir(outsideDir, { recursive: true });
    await writeFile(outsideFile, ["s", "c", "f"].join(""));

    const result = runReleaseCheck(releaseCheckEnv({
      AGENT_TESTBENCH_SMOKE_STORE_DSN: "sqlite:///tmp/agent-testbench-scope-test.sqlite",
    }), ["--scope", "docs"]);

    assert.equal(result.status, 0, result.stderr || result.stdout);
    assert.match(result.stdout, /checking release scope/);
    assert.match(result.stdout, /no scoped runtime tests selected/);
    assert.doesNotMatch(result.stderr, /core contains source-domain terms/);
    assert.doesNotMatch(result.stdout, /running Go tests/);
  } finally {
    await rm(outsideDir, { recursive: true, force: true });
  }
});

test("release-check scope-file runs targeted example tests without full Go suite", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-release-scope-"));
  const scopeFile = path.join(tempDir, "scope.txt");
  try {
    await writeFile(scopeFile, "tools/examples/demo-showcase.test.mjs\n");

    const result = runReleaseCheck(releaseCheckEnv({
      AGENT_TESTBENCH_SMOKE_STORE_DSN: "sqlite:///tmp/agent-testbench-scope-file-test.sqlite",
    }), ["--scope-file", scopeFile]);

    assert.equal(result.status, 0, result.stderr || result.stdout);
    assert.match(result.stdout, /running scoped Node tests/);
    assert.match(result.stdout, /tools\/examples\/demo-showcase\.test\.mjs/);
    assert.doesNotMatch(result.stdout, /running Go tests/);
    assert.doesNotMatch(result.stdout, /running generic API case demo/);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("release-check scoped Go selection runs only touched package directories", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-release-go-scope-"));
  const binDir = path.join(tempDir, "bin");
  const goLog = path.join(tempDir, "go.log");
  const fakeGo = path.join(binDir, "go");
  try {
    await mkdir(binDir, { recursive: true });
    await writeFile(fakeGo, `#!/usr/bin/env bash\nprintf '%s\\n' "$*" >> "${goLog}"\n`);
    await chmod(fakeGo, 0o755);

    const result = runReleaseCheck(releaseCheckEnv({
      AGENT_TESTBENCH_SMOKE_STORE_DSN: "sqlite:///tmp/agent-testbench-go-scope-test.sqlite",
      PATH: `${binDir}:${process.env.PATH}`,
    }), ["--scope", "internal/store/mysql/config.go"]);

    assert.equal(result.status, 0, result.stderr || result.stdout);
    assert.match(result.stdout, /running scoped Go tests/);
    assert.match(result.stdout, /go test \.\/internal\/store\/mysql -count=1/);
    assert.doesNotMatch(result.stdout, /go test \.\/\.\.\. -count=1/);
    assert.match(readFileSync(goLog, "utf8"), /^test \.\/internal\/store\/mysql -count=1$/m);

    await writeFile(goLog, "");
    const dirResult = runReleaseCheck(releaseCheckEnv({
      AGENT_TESTBENCH_SMOKE_STORE_DSN: "sqlite:///tmp/agent-testbench-go-dir-scope-test.sqlite",
      PATH: `${binDir}:${process.env.PATH}`,
    }), ["--scope", "internal/store/mysql"]);

    assert.equal(dirResult.status, 0, dirResult.stderr || dirResult.stdout);
    assert.match(dirResult.stdout, /go test \.\/internal\/store\/mysql\/\.\.\. -count=1/);
    assert.doesNotMatch(dirResult.stdout, /go test \.\/\.\.\. -count=1/);
    assert.match(readFileSync(goLog, "utf8"), /^test \.\/internal\/store\/mysql\/\.\.\. -count=1$/m);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("release-check scoped Go selection includes reverse dependent packages", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-release-go-dependents-"));
  const binDir = path.join(tempDir, "bin");
  const goLog = path.join(tempDir, "go.log");
  const fakeGo = path.join(binDir, "go");
  try {
    await mkdir(binDir, { recursive: true });
    await writeFile(fakeGo, `#!/usr/bin/env bash
printf '%s\\n' "$*" >> "${goLog}"
if [[ "$1" == "list" && "$2" == "-f" ]]; then
  printf '%s\\n' "agent-testbench/internal/store/mysql " "agent-testbench/cmd/agent-testbench agent-testbench/internal/store/mysql agent-testbench/internal/store" "agent-testbench/internal/runner/apicase agent-testbench/internal/store"
elif [[ "$1" == "list" ]]; then
  printf '%s\\n' "agent-testbench/internal/store/mysql"
fi
`);
    await chmod(fakeGo, 0o755);

    const result = runReleaseCheck(releaseCheckEnv({
      AGENT_TESTBENCH_SMOKE_STORE_DSN: "sqlite:///tmp/agent-testbench-go-dependents-test.sqlite",
      PATH: `${binDir}:${process.env.PATH}`,
    }), ["--scope", "internal/store/mysql/config.go"]);

    assert.equal(result.status, 0, result.stderr || result.stdout);
    assert.match(result.stdout, /running scoped Go tests/);
    assert.match(result.stdout, /agent-testbench\/cmd\/agent-testbench/);
    assert.match(readFileSync(goLog, "utf8"), /^test \.\/internal\/store\/mysql agent-testbench\/cmd\/agent-testbench -count=1$/m);
  } finally {
    await rm(tempDir, { recursive: true, force: true });
  }
});

test("release-check scoped Go selection covers module metadata and package paths", () => {
  const script = readFileSync(path.join(rootDir, "tools", "release-check.sh"), "utf8");

  assert.match(script, /go\.mod\|go\.sum/);
  assert.match(script, /go_scope_all=1/);
  assert.match(script, /go_scope_packages/);
  assert.match(script, /go test -p 1 \.\/\.\.\. -count=1/);
  assert.match(script, /go test \.\/\.\.\. -count=1/);
});

test("release-check missing Store guidance lists every supported smoke Store env", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "",
    AGENT_TESTBENCH_SMOKE_STORE: "",
  }), ["--full"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /AGENT_TESTBENCH_SMOKE_STORE_DSN or AGENT_TESTBENCH_SMOKE_STORE is required/);
  assert.match(result.stderr, /SQL Store examples:/);
  assert.match(result.stderr, /PostgreSQL: AGENT_TESTBENCH_SMOKE_STORE_DSN='postgres:\/\/user:pass@host:5432\/agent_testbench_smoke\?sslmode=disable' npm run release-check -- --full/);
  assert.match(result.stderr, /MySQL: AGENT_TESTBENCH_SMOKE_STORE='mysql:\/\/user:pass@host:3306\/agent_testbench_smoke\?tls=false' npm run release-check -- --full/);
  assert.match(result.stderr, /AGENT_TESTBENCH_SMOKE_STORE='mysql:\/\/user:pass@host:3306\/agent_testbench_smoke\?tls=false'/);
  assert.match(result.stderr, /SQLite: AGENT_TESTBENCH_SMOKE_STORE='sqlite:\/\/\/tmp\/agent-testbench-smoke\.sqlite' npm run release-check -- --scope PATH/);
  assert.doesNotMatch(result.stderr, /also supported/i);
  assert.doesNotMatch(result.stdout, /checking SkyWalking smoke provider mode/);
});

test("release-check refuses unsafe MySQL smoke database names before expensive gates", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "mysql://user:secret@example.com:3306/business_prod?tls=false",
  }), ["--full"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /Refusing to run release-check against MySQL database 'business_prod'/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode requires a GraphQL URL before expensive gates", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
  }), ["--full"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode rejects invalid GraphQL URLs before expensive gates", () => {
  for (const graphQLURL of ["not-a-url", "ftp://skywalking.example/graphql"]) {
    const result = runReleaseCheck(releaseCheckEnv({
      AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
      AGENT_TESTBENCH_TRACE_GRAPHQL_URL: graphQLURL,
      AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
      AGENT_TESTBENCH_SMOKE_TRACE_IDS: configuredTraceIDs(),
    }), ["--full"]);

    assert.equal(result.status, 1, graphQLURL);
    assert.match(result.stderr, /requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL to be an http\/https URL/);
    assert.doesNotMatch(result.stdout, /running Go tests/);
  }
});

test("release-check accepts uppercase SQL Store schemes before expensive gates", () => {
  const mysql = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "MYSQL://user:pass@127.0.0.1:3306/agent_testbench_smoke?tls=false",
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
  }), ["--full"]);
  assert.equal(mysql.status, 1);
  assert.match(mysql.stderr, /requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL/);
  assert.doesNotMatch(mysql.stderr, /must be postgres:\/\/, postgresql:\/\/, or mysql:\/\//);
  assert.doesNotMatch(mysql.stdout, /running Go tests/);

  const postgres = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "POSTGRESQL://user:pass@127.0.0.1:5432/agent_testbench_smoke?sslmode=disable",
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
  }), ["--full"]);
  assert.equal(postgres.status, 1);
  assert.match(postgres.stderr, /requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL/);
  assert.doesNotMatch(postgres.stderr, /must be postgres:\/\/, postgresql:\/\/, or mysql:\/\//);
  assert.doesNotMatch(postgres.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode requires configured workflow trace ids before expensive gates", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
  }), ["--full"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires AGENT_TESTBENCH_SMOKE_TRACE_IDS/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode requires trace ids for every configured workflow step", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
    AGENT_TESTBENCH_SMOKE_TRACE_IDS: "step-01=trace.real.01",
  }), ["--full"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /every configured workflow step/);
  assert.match(result.stderr, /step-02/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("release-check real SkyWalking mode rejects empty workflow step trace ids", () => {
  const result = runReleaseCheck(releaseCheckEnv({
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
    AGENT_TESTBENCH_SMOKE_TRACE_IDS: [
      "step-01=trace.real.01",
      "step-02=",
      "step-03=trace.real.03",
    ].join(","),
  }), ["--full"]);

  assert.equal(result.status, 1);
  assert.match(result.stderr, /every configured workflow step/);
  assert.match(result.stderr, /step-02/);
  assert.doesNotMatch(result.stdout, /running Go tests/);
});

test("real MySQL release wrapper requires a dedicated MySQL Store DSN", () => {
  const missing = runRealMySQLWrapper({
    AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "",
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "",
  });
  assert.equal(missing.status, 1);
  assert.match(missing.stderr, /Set AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN/);

  const postgres = runRealMySQLWrapper({
    AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "postgres://user:secret@example.com:5432/agent_testbench_smoke?sslmode=disable",
  });
  assert.equal(postgres.status, 1);
  assert.match(postgres.stderr, /must be a mysql:\/\/ DSN/);
});

test("real MySQL release wrapper refuses likely business databases", () => {
  const result = runRealMySQLWrapper({
    AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "mysql://user:secret@example.com:3306/business_prod?tls=false",
  });

  assert.equal(result.status, 1);
  assert.match(result.stderr, /Refusing to run release-check/);
  assert.match(result.stderr, /business_prod/);
});

test("real MySQL release wrapper requires real SkyWalking sign-off inputs", () => {
  const result = runRealMySQLWrapper({
    AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "MYSQL://user:secret@example.com:3306/agent_testbench_smoke?tls=false",
    AGENT_TESTBENCH_REAL_MYSQL_RELEASE_DRY_RUN: "1",
  });

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1/);
  assert.doesNotMatch(result.stderr, /secret/);
});

test("real MySQL release wrapper rejects invalid or non-http SkyWalking GraphQL URLs", () => {
  for (const graphQLURL of ["not-a-url", "ftp://skywalking.example/graphql"]) {
    const result = runRealMySQLWrapper({
      AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "mysql://user:secret@example.com:3306/agent_testbench_smoke?tls=false",
      AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
      AGENT_TESTBENCH_TRACE_GRAPHQL_URL: graphQLURL,
      AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
      AGENT_TESTBENCH_SMOKE_TRACE_IDS: configuredTraceIDs(),
      AGENT_TESTBENCH_REAL_MYSQL_RELEASE_DRY_RUN: "1",
    });

    assert.equal(result.status, 1, graphQLURL);
    assert.match(result.stderr, /requires AGENT_TESTBENCH_TRACE_GRAPHQL_URL to be an http\/https URL/);
    assert.doesNotMatch(result.stderr, /secret/);
    assert.doesNotMatch(result.stderr, /Would run: npm run release-check -- --full/);
  }
});

test("real MySQL release wrapper requires existing-database contract mode", () => {
  const result = runRealMySQLWrapper({
    AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "mysql://user:secret@example.com:3306/agent_testbench_smoke?tls=false",
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
    AGENT_TESTBENCH_SMOKE_TRACE_IDS: configuredTraceIDs(),
    AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE: "create-drop",
    AGENT_TESTBENCH_REAL_MYSQL_RELEASE_DRY_RUN: "1",
  });

  assert.equal(result.status, 1);
  assert.match(result.stderr, /requires AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE=existing/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.doesNotMatch(result.stderr, /Would run: npm run release-check -- --full/);
});

test("real MySQL release wrapper dry-run masks credentials and accepts smoke database", () => {
  const result = runRealMySQLWrapper({
    AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "MYSQL://user:secret@example.com:3306/agent_testbench_smoke?tls=false",
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
    AGENT_TESTBENCH_SMOKE_TRACE_IDS: configuredTraceIDs(),
    AGENT_TESTBENCH_REAL_MYSQL_RELEASE_DRY_RUN: "1",
  });

  assert.equal(result.status, 0);
  assert.match(result.stderr, /mysql:\/\/user:xxxxx@example.com:3306\/agent_testbench_smoke/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.match(result.stderr, /MySQL Store contract mode: existing/);
  assert.match(result.stderr, /Real SkyWalking release mode: required/);
  assert.match(result.stderr, /Would run: npm run release-check -- --full/);
});

test("real MySQL release preflight npm script runs the guarded dry-run", () => {
  const result = runNPM(["run", "release-check:mysql-real:preflight"], {
    AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "mysql://user:secret@example.com:3306/agent_testbench_smoke?tls=false",
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
    AGENT_TESTBENCH_SMOKE_TRACE_IDS: configuredTraceIDs(),
  });

  assert.equal(result.status, 0);
  assert.match(result.stderr, /mysql:\/\/user:xxxxx@example.com:3306\/agent_testbench_smoke/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.match(result.stderr, /Would run: npm run release-check -- --full/);
});

test("real MySQL release wrapper accepts shared smoke Store env", () => {
  const result = runRealMySQLWrapper({
    AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN: "",
    AGENT_TESTBENCH_SMOKE_STORE_DSN: "",
    AGENT_TESTBENCH_SMOKE_STORE: "mysql://user:secret@example.com:3306/agent_testbench_smoke?tls=false",
    AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING: "1",
    AGENT_TESTBENCH_TRACE_GRAPHQL_URL: "http://skywalking.example/graphql",
    AGENT_TESTBENCH_SMOKE_EXPECTED_STEPS: "3",
    AGENT_TESTBENCH_SMOKE_TRACE_IDS: configuredTraceIDs(),
    AGENT_TESTBENCH_REAL_MYSQL_RELEASE_DRY_RUN: "1",
  });

  assert.equal(result.status, 0);
  assert.match(result.stderr, /mysql:\/\/user:xxxxx@example.com:3306\/agent_testbench_smoke/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.match(result.stderr, /Would run: npm run release-check -- --full/);
});
