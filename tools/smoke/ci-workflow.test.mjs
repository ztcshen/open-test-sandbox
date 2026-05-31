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
  assert.match(releaseJob, /"\$\{\{\s*github\.event_name\s*\}\}" == "pull_request" && -s \.release-check-scope/);
  assert.match(releaseJob, /npm run release-check -- --scope-file \.release-check-scope/);
  assert.match(releaseJob, /npm run release-check -- --full/);
});

test("Go lint entrypoints use the PR-diff lint gate", () => {
  const workflow = readFileSync(join(rootDir, ".github", "workflows", "ci.yml"), "utf8");
  const packageJSON = readFileSync(join(rootDir, "package.json"), "utf8");
  const makefile = readFileSync(join(rootDir, "Makefile"), "utf8");

  assert.match(workflow, /go install github\.com\/golangci\/golangci-lint\/v2\/cmd\/golangci-lint@v2\.12\.2/);
  assert.match(workflow, /make lint/);
  assert.match(workflow, /make lint-full/);
  assert.doesNotMatch(workflow, /golangci\/golangci-lint-action/);
  assert.match(workflow, /AGENT_TESTBENCH_SKIP_GO_LINT:\s*"1"/);
  assert.match(packageJSON, /"lint:go": "bash tools\/go-lint\.sh"/);
  assert.match(makefile, /lint:\n\ttools\/go-lint\.sh/);
});

test("CI includes deterministic secret scan as an independent gate", () => {
  const workflow = readFileSync(join(rootDir, ".github", "workflows", "ci.yml"), "utf8");
  const packageJSON = readFileSync(join(rootDir, "package.json"), "utf8");

  assert.match(workflow, /secret-scan:/);
  assert.match(workflow, /Run deterministic secret scan/);
  assert.match(workflow, /npm run guard:secrets/);
  assert.match(packageJSON, /"guard:secrets": "bash tools\/guardrails\/check_secrets\.sh"/);
});

test("CI includes dependency baseline validation", () => {
  const workflow = readFileSync(join(rootDir, ".github", "workflows", "ci.yml"), "utf8");
  const packageJSON = readFileSync(join(rootDir, "package.json"), "utf8");

  assert.match(workflow, /Run dependency baseline/);
  assert.match(workflow, /npm run guard:dependencies/);
  assert.match(packageJSON, /"guard:dependencies": "bash tools\/guardrails\/check_dependency_baseline\.sh"/);
});

test("pull request template asks for scoped release-check evidence", () => {
  const template = readFileSync(join(rootDir, ".github", "PULL_REQUEST_TEMPLATE.md"), "utf8");

  assert.match(template, /npm run release-check -- --scope (PATH|FILE_OR_DIR)/);
  assert.match(template, /npm run release-check -- --scope-file \.release-check-scope/);
  assert.doesNotMatch(template, /- \[[ xX]\] `npm run release-check`/);
});
