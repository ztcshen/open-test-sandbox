import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const rootDir = resolve(__dirname, "..", "..");

test("manual MySQL real sign-off runs preflight before full release gate", () => {
  const workflow = readFileSync(join(rootDir, ".github", "workflows", "ci.yml"), "utf8");
  const jobIndex = workflow.indexOf("mysql-real-signoff:");
  assert.notEqual(jobIndex, -1);

  const job = workflow.slice(jobIndex);
  const preflightIndex = job.indexOf("run: npm run release-check:mysql-real:preflight");
  const fullIndex = job.indexOf("run: npm run release-check:mysql-real\n");

  assert.notEqual(preflightIndex, -1);
  assert.notEqual(fullIndex, -1);
  assert.ok(preflightIndex < fullIndex);
  assert.match(job, /AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING:\s*"1"/);
  assert.match(job, /AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN:\s*\$\{\{\s*secrets\.AGENT_TESTBENCH_REAL_MYSQL_STORE_DSN\s*\}\}/);
  assert.match(job, /AGENT_TESTBENCH_TRACE_GRAPHQL_URL:\s*\$\{\{\s*secrets\.AGENT_TESTBENCH_TRACE_GRAPHQL_URL\s*\}\}/);
  assert.match(job, /AGENT_TESTBENCH_SMOKE_TRACE_IDS:\s*\$\{\{\s*secrets\.AGENT_TESTBENCH_SMOKE_TRACE_IDS\s*\}\}/);
});

test("pull request CI passes changed paths into release-check scope", () => {
  const workflow = readFileSync(join(rootDir, ".github", "workflows", "ci.yml"), "utf8");
  const releaseJobIndex = workflow.indexOf("release-check:");
  const signoffJobIndex = workflow.indexOf("mysql-real-signoff:");
  assert.notEqual(releaseJobIndex, -1);
  assert.notEqual(signoffJobIndex, -1);

  const releaseJob = workflow.slice(releaseJobIndex, signoffJobIndex);
  assert.match(releaseJob, /Collect release scope/);
  assert.match(releaseJob, /refs\/remotes\/origin\/\$\{\{\s*github\.base_ref\s*\}\}/);
  assert.match(releaseJob, /git diff --name-only --diff-filter=ACMRT "origin\/\$\{\{\s*github\.base_ref\s*\}\}" HEAD/);
  assert.doesNotMatch(releaseJob, /origin\/\$\{\{\s*github\.base_ref\s*\}\}\.\.\.HEAD/);
  assert.match(releaseJob, /npm run release-check -- --scope-file \.release-check-scope/);
});
