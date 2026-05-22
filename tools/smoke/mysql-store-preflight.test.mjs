import assert from "node:assert/strict";
import { chmod, mkdtemp, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const rootDir = path.resolve(new URL("../..", import.meta.url).pathname);
const scriptPath = path.join(rootDir, "tools/smoke/mysql-store-preflight.sh");

test("MySQL Store preflight writes masked blocked report before Store mutation", async () => {
  const tempDir = await mkdtemp(path.join(os.tmpdir(), "ots-mysql-preflight-"));
  const fakeProbe = path.join(tempDir, "fake-mysql-probe.sh");
  await writeFile(fakeProbe, `#!/usr/bin/env bash
cat <<'JSON'
{"ok":false,"backend":"mysql","host":"10.0.20.108","port":3306,"error":"timed out"}
JSON
exit 2
`);
  await chmod(fakeProbe, 0o755);

  const outputPrefix = path.join(tempDir, "team-mysql");
  const result = spawnSync("bash", [
    scriptPath,
    "--url",
    "mysql://baofoo:secret@10.0.20.108:3306/OTS_SANDBOX_TEST?tls=false",
    "--output-prefix",
    outputPrefix,
  ], {
    cwd: rootDir,
    env: {
      ...process.env,
      OTSANDBOX_MYSQL_HANDSHAKE_PROBE: fakeProbe,
    },
    encoding: "utf8",
  });

  assert.equal(result.status, 2);
  assert.match(result.stderr, /preflight blocked/);
  assert.doesNotMatch(result.stderr, /secret/);

  const blocked = await readFile(`${outputPrefix}-mysql-preflight-blocked.md`, "utf8");
  assert.match(blocked, /mysql:\/\/baofoo:xxxxx@10\.0\.20\.108:3306\/OTS_SANDBOX_TEST/);
  assert.match(blocked, /The script stopped before store provision, schema upgrade, store copy, read-back, or restore/);
  assert.match(blocked, /mysql_error: `timed out`/);
  assert.doesNotMatch(blocked, /secret/);
});
