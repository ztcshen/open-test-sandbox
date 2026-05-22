import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const rootDir = path.resolve(new URL("../..", import.meta.url).pathname);
const scriptPath = path.join(rootDir, "tools/smoke/mysql-store-publish-verified-env.sh");

test("MySQL Store publish script gates copy, switches active Store, and can restore", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-mysql-publish-"));
  const logFile = path.join(tempDir, "agent-testbench.log");
  const fakeOtsandbox = path.join(tempDir, "agent-testbench");
  const fakeProbe = path.join(tempDir, "fake-mysql-probe.sh");
  const outputPrefix = path.join(tempDir, "publish");
  const controlPlaneDir = path.join(tempDir, "control-plane", "api", "store");

  await writeFile(fakeProbe, `#!/usr/bin/env bash
cat <<'JSON'
{"ok":true,"backend":"mysql","host":"127.0.0.1","port":3306}
JSON
`);
  await chmod(fakeProbe, 0o755);
  await mkdir(controlPlaneDir, { recursive: true });
  await writeFile(path.join(controlPlaneDir, "current"), JSON.stringify({
    ok: true,
    configured: true,
    name: "team-mysql",
    backend: "mysql",
    url: "mysql://tester:xxxxx@127.0.0.1:3306/agent_testbench?tls=false",
    source: "active-store",
  }));

  await writeFile(fakeOtsandbox, `#!/usr/bin/env bash
printf '%s\\n' "$*" >> "$AGENT_TESTBENCH_TEST_LOG"
case "$1 $2" in
  "store config")
    cat <<'JSON'
{"stores":[{"name":"team-mysql","url":"mysql://tester:secret@127.0.0.1:3306/agent_testbench?tls=false"}]}
JSON
    ;;
  "store provision")
    echo '{"ok":true}'
    ;;
  "store status")
    echo '{"ok":true,"backend":"mysql","version":7,"pending":0}'
    ;;
  "store upgrade")
    echo 'Store schema is current'
    ;;
  "store copy")
    cat <<'JSON'
{"ok":true,"profileCatalogs":1,"profileIndexes":1,"configVersions":1,"readModels":["catalog"],"environmentIds":["env.verified"],"environmentRefs":[{"id":"env.verified","verificationWorkflowId":"workflow.acceptance","status":"verified","verified":true,"evidenceComplete":true,"topologyComplete":true}],"componentRefs":[{"environmentId":"env.verified","components":2,"dependencies":1,"assets":3,"inlineAssetBytes":10}]}
JSON
    ;;
  "environment inspect")
    cat <<'JSON'
{"ok":true,"environment":{"id":"env.verified","verificationWorkflowId":"workflow.acceptance","status":"verified","verified":true,"evidenceComplete":true,"topologyComplete":true},"componentGraph":{"configured":true,"ok":true,"components":2,"dependencies":1,"assets":3,"inlineAssetBytes":10}}
JSON
    ;;
  "environment restore")
    cat <<'JSON'
{"ok":true,"environment":{"summary":{"lastRestore":{"ok":true,"workflow":{"acceptance":{"ok":true}}}}}}
JSON
    ;;
  "store use")
    echo 'Active Store: team-mysql'
    ;;
  "store current")
    echo '{"ok":true,"name":"team-mysql","backend":"mysql","url":"mysql://tester:xxxxx@127.0.0.1:3306/agent_testbench?tls=false"}'
    ;;
  *)
    echo "unexpected command: $*" >&2
    exit 9
    ;;
esac
`);
  await chmod(fakeOtsandbox, 0o755);

  const result = spawnSync("bash", [
    scriptPath,
    "--from",
    "local-pg",
    "--to",
    "team-mysql",
    "--environment",
    "env.verified",
    "--workflow",
    "workflow.acceptance",
    "--min-components",
    "2",
    "--min-dependencies",
    "1",
    "--min-assets",
    "3",
    "--min-inline-asset-bytes",
    "10",
    "--restore",
    "--workspace",
    path.join(tempDir, "workspace"),
    "--server-url",
    "http://127.0.0.1:58663",
    "--verify-control-plane-url",
    `file://${path.join(tempDir, "control-plane")}`,
    "--use-existing-containers",
    "--output-prefix",
    outputPrefix,
  ], {
    cwd: rootDir,
    env: {
      ...process.env,
      AGENT_TESTBENCH_BIN: fakeOtsandbox,
      AGENT_TESTBENCH_MYSQL_HANDSHAKE_PROBE: fakeProbe,
      AGENT_TESTBENCH_TEST_LOG: logFile,
    },
    encoding: "utf8",
  });

  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.doesNotMatch(result.stdout, /secret/);
  assert.doesNotMatch(result.stderr, /secret/);
  assert.match(result.stdout, /promotion complete/);

  const log = await readFile(logFile, "utf8");
  assert.match(log, /environment inspect env\.verified --store local-pg --json/);
  assert.match(log, /store provision --store team-mysql --json/);
  assert.match(log, /store copy --from local-pg --to team-mysql/);
  assert.match(log, /--require-environment env\.verified/);
  assert.match(log, /--require-verification-workflow workflow\.acceptance/);
  assert.match(log, /--require-verified-environment/);
  assert.match(log, /--require-min-components 2/);
  assert.match(log, /environment inspect env\.verified --store team-mysql --json/);
  assert.match(log, /store use team-mysql/);
  assert.match(log, /store current --json/);
  assert.match(log, /environment restore env\.verified --store team-mysql/);

  const completion = await readFile(`${outputPrefix}-publish-complete.md`, "utf8");
  assert.match(completion, /active_store_switched: true/);
  assert.match(completion, /restore_executed: true/);
  assert.match(completion, /source-env-inspect-assertion\.json/);
  assert.match(completion, /store-current-assertion\.json/);
  assert.match(completion, /control-plane-store-current-assertion\.json/);
});
