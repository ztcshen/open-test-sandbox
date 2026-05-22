import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const rootDir = path.resolve(new URL("../..", import.meta.url).pathname);
const scriptPath = path.join(rootDir, "tools/smoke/mysql-store-goal-audit.sh");

async function writeFakeOtsandbox(filePath, { targetReady }) {
  await writeFile(filePath, `#!/usr/bin/env bash
case "$1 $2" in
  "store config")
    cat <<'JSON'
{"stores":[{"name":"team-mysql","url":"mysql://tester:secret@127.0.0.1:3306/agent_testbench?tls=false"}]}
JSON
    ;;
  "environment inspect")
    if [[ "$4" == "team-mysql" && "${targetReady}" != "true" ]]; then
      echo '{"ok":false,"error":"not copied"}'
      exit 2
    fi
    cat <<'JSON'
{"ok":true,"environment":{"id":"env.verified","verificationWorkflowId":"workflow.acceptance","status":"verified","verified":true,"evidenceComplete":true,"topologyComplete":true},"componentGraph":{"configured":true,"ok":true,"components":2,"dependencies":1,"assets":3,"inlineAssetBytes":10}}
JSON
    ;;
  "store status")
    echo '{"ok":true,"backend":"mysql","version":7,"pending":0}'
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
  await chmod(filePath, 0o755);
}

test("MySQL Store goal audit reports target handshake blocker after source readiness", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-mysql-goal-audit-blocked-"));
  const fakeOtsandbox = path.join(tempDir, "agent-testbench");
  const fakeProbe = path.join(tempDir, "probe.sh");
  const outputPrefix = path.join(tempDir, "audit");
  await writeFakeOtsandbox(fakeOtsandbox, { targetReady: false });
  await writeFile(fakeProbe, `#!/usr/bin/env bash
echo '{"ok":false,"backend":"mysql","error":"timed out"}'
exit 2
`);
  await chmod(fakeProbe, 0o755);

  const result = spawnSync("bash", [
    scriptPath,
    "--from", "local-pg",
    "--to", "team-mysql",
    "--environment", "env.verified",
    "--workflow", "workflow.acceptance",
    "--min-components", "2",
    "--min-dependencies", "1",
    "--min-assets", "3",
    "--min-inline-asset-bytes", "10",
    "--output-prefix", outputPrefix,
  ], {
    cwd: rootDir,
    env: {
      ...process.env,
      AGENT_TESTBENCH_BIN: fakeOtsandbox,
      AGENT_TESTBENCH_MYSQL_HANDSHAKE_PROBE: fakeProbe,
    },
    encoding: "utf8",
  });

  assert.equal(result.status, 2, `${result.stdout}\n${result.stderr}`);
  assert.match(result.stdout, /blocker: `target-mysql-handshake`/);
  const summary = JSON.parse(await readFile(`${outputPrefix}-summary.json`, "utf8"));
  assert.equal(summary.ok, false);
  assert.equal(summary.blocker, "target-mysql-handshake");
  assert.match(summary.nextAction, /team-mysql-pending-publish-commands\.sh/);
  assert.deepEqual(summary.nextCommand, [".runtime/team-mysql-pending-publish-commands.sh"]);
  assert.equal(summary.nextCommandShell, ".runtime/team-mysql-pending-publish-commands.sh");
  assert.equal(summary.targetDiagnostics.mysqlError, "timed out");
  assert.equal(summary.checks.sourceReady, true);
  assert.equal(summary.checks.targetReachable, false);
});

test("MySQL Store goal audit passes when all read-only gates are ready", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-mysql-goal-audit-ready-"));
  const fakeOtsandbox = path.join(tempDir, "agent-testbench");
  const fakeProbe = path.join(tempDir, "probe.sh");
  const outputPrefix = path.join(tempDir, "audit");
  const controlPlaneDir = path.join(tempDir, "control-plane", "api", "store");
  await writeFakeOtsandbox(fakeOtsandbox, { targetReady: true });
  await writeFile(fakeProbe, `#!/usr/bin/env bash
echo '{"ok":true,"backend":"mysql","host":"127.0.0.1","port":3306}'
`);
  await chmod(fakeProbe, 0o755);
  await mkdir(controlPlaneDir, { recursive: true });
  await writeFile(path.join(controlPlaneDir, "current"), JSON.stringify({
    ok: true,
    configured: true,
    name: "team-mysql",
    backend: "mysql",
  }));

  const result = spawnSync("bash", [
    scriptPath,
    "--from", "local-pg",
    "--to", "team-mysql",
    "--environment", "env.verified",
    "--workflow", "workflow.acceptance",
    "--control-plane-url", `file://${path.join(tempDir, "control-plane")}`,
    "--min-components", "2",
    "--min-dependencies", "1",
    "--min-assets", "3",
    "--min-inline-asset-bytes", "10",
    "--output-prefix", outputPrefix,
  ], {
    cwd: rootDir,
    env: {
      ...process.env,
      AGENT_TESTBENCH_BIN: fakeOtsandbox,
      AGENT_TESTBENCH_MYSQL_HANDSHAKE_PROBE: fakeProbe,
    },
    encoding: "utf8",
  });

  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
  assert.match(result.stdout, /ok: `true`/);
  const summary = JSON.parse(await readFile(`${outputPrefix}-summary.json`, "utf8"));
  assert.equal(summary.ok, true);
  assert.equal(summary.blocker, "none");
  assert.match(summary.nextAction, /team-mysql-colleague-restore-commands\.sh/);
  assert.deepEqual(summary.nextCommand, [".runtime/team-mysql-colleague-restore-commands.sh"]);
  assert.equal(summary.nextCommandShell, ".runtime/team-mysql-colleague-restore-commands.sh");
  assert.equal(summary.checks.controlPlaneIsTarget, true);
});
