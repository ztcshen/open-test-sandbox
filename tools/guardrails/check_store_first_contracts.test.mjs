import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const script = readFileSync("tools/guardrails/check_store_first_contracts.sh", "utf8");

test("Store-first guardrail release-check guidance is SQL Store neutral", () => {
  assert.doesNotMatch(script, /PostgreSQL gate/);
  assert.match(script, /SQL Store gate/);
});
