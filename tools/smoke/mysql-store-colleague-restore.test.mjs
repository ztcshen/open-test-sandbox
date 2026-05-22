import assert from "node:assert/strict";
import { chmod, mkdir, mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const rootDir = path.resolve(new URL("../..", import.meta.url).pathname);
const scriptPath = path.join(rootDir, "tools/smoke/mysql-store-colleague-restore.sh");

test("MySQL Store colleague restore verifies remote Store, control plane, and acceptance", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "agent-testbench-mysql-colleague-"));
  const fakeOtsandbox = path.join(tempDir, "agent-testbench");
  const fakeProbe = path.join(tempDir, "fake-mysql-probe.sh");
  const logFile = path.join(tempDir, "agent-testbench.log");
  const outputPrefix = path.join(tempDir, "restore");
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
  }));

  await writeFile(fakeOtsandbox, `#!/usr/bin/env bash
printf '%s\\n' "$*" >> "$AGENT_TESTBENCH_TEST_LOG"
case "$1 $2" in
  "store config")
    if [[ "$3" == "set" ]]; then
      echo 'Store configured'
    else
      cat <<'JSON'
{"stores":[{"name":"team-mysql","url":"mysql://tester:secret@127.0.0.1:3306/agent_testbench?tls=false"}]}
JSON
    fi
    ;;
  "store status")
    echo '{"ok":true,"backend":"mysql","version":7,"pending":0}'
    ;;
  "store use")
    echo 'Active Store: team-mysql'
    ;;
  "store current")
    echo '{"ok":true,"name":"team-mysql","backend":"mysql","url":"mysql://tester:xxxxx@127.0.0.1:3306/agent_testbench?tls=false"}'
    ;;
  "environment inspect")
    cat <<'JSON'
{"ok":true,"environment":{"id":"env.verified","status":"verified","verified":true,"evidenceComplete":true,"topologyComplete":true},"componentGraph":{"configured":true,"ok":true,"components":2,"dependencies":1,"assets":3,"inlineAssetBytes":10}}
JSON
    ;;
  "environment restore")
    cat <<'JSON'
{"ok":true,"environment":{"summary":{"lastRestore":{"ok":true}}},"workflow":{"ok":true,"action":"run-acceptance-workflow","acceptance":{"ok":true,"expectedSteps":3,"completedSteps":3,"passedSteps":3,"failedSteps":0,"topologyProvider":"skywalking"}}}
JSON
    ;;
  "environment publish-verified")
    cat <<'JSON'
{"ok":true,"environment":{"id":"env.verified","status":"verified","verified":true,"evidenceComplete":true,"topologyComplete":true}}
JSON
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
    "--store",
    "team-mysql",
    "--store-url",
    "mysql://tester:secret@127.0.0.1:3306/agent_testbench?tls=false",
    "--environment",
    "env.verified",
    "--workspace",
    path.join(tempDir, "workspace"),
    "--server-url",
    `file://${path.join(tempDir, "control-plane")}`,
    "--min-components",
    "2",
    "--min-dependencies",
    "1",
    "--min-assets",
    "3",
    "--min-inline-asset-bytes",
    "10",
    "--min-acceptance-steps",
    "3",
    "--use-existing-containers",
    "--pull",
    "--clean-docker-state",
    "--clean-docker-images",
    "--allow-destructive-docker-cleanup",
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
  assert.match(result.stdout, /colleague restore complete/);

  const log = await readFile(logFile, "utf8");
  assert.match(log, /store config set team-mysql --url mysql:\/\/tester:secret@127\.0\.0\.1:3306\/agent_testbench\?tls=false/);
  assert.match(log, /store status --store team-mysql --json/);
  assert.match(log, /store use team-mysql/);
  assert.match(log, /store current --json/);
  assert.match(log, /environment inspect env\.verified --store team-mysql --json/);
  assert.match(log, /environment restore env\.verified --store team-mysql/);
  assert.match(log, /--run-workflow/);
  assert.match(log, /--pull/);
  assert.match(log, /--clean-docker-state/);
  assert.match(log, /--clean-docker-images/);
  assert.match(log, /--allow-destructive-docker-cleanup/);
  assert.match(log, /environment publish-verified env\.verified --store team-mysql --json/);

  const completion = await readFile(`${outputPrefix}-restore-complete.md`, "utf8");
  assert.match(completion, /restore-assertion\.json/);
  assert.match(completion, /control-plane-store-current-assertion\.json/);
  assert.match(completion, /publish-verified-assertion\.json/);
});
