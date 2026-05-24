import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const textFile = (path) => readFile(new URL(`../../${path}`, import.meta.url), 'utf8');
const blockedTermFile = () => textFile('tools/guardrails/source-domain-terms.txt');

test('demo showcase publishes visual and CLI-facing assets', async () => {
  const [docs, services, page] = await Promise.all([
    textFile('docs/demo-gallery.md'),
    textFile('examples/demo-services/catalog.json'),
    textFile('control-plane/static/demo-gallery.html'),
  ]);

  assert.match(docs, /Demo Gallery/);
  assert.match(docs, /agent-testbench case run/);
  assert.match(docs, /agent-testbench workflow report/);
  assert.match(page, /CLI capability map/);
  assert.match(page, /Evidence timeline/);
  assert.match(page, /CLI automation animation/);
  assert.match(page, /data-demo-runbook="autoplay"/);
  assert.match(page, /environment restore/);
  assert.match(page, /case suite priority/);
  assert.match(page, /case run/);
  assert.match(page, /workflow report/);
  assert.match(page, /evidence tasks/);
  assert.match(page, /case suite quality-report/);
  assert.match(page, /Root cause/);
  assert.match(page, /Replay animation/);

  const catalog = JSON.parse(services);
  const blockedTerms = (await blockedTermFile())
    .split(/\r?\n/)
    .map((term) => term.trim())
    .filter(Boolean);
  const blockedPhrases = [['supply', 'chain'].join(' '), ...blockedTerms];
  assert.equal(catalog.version, 1);
  assert.equal(catalog.scenarios.length, 3);

  const ids = catalog.scenarios.map((scenario) => scenario.id).sort();
  assert.deepEqual(ids, [
    'content-moderation-pipeline',
    'iot-telemetry-control',
    'retail-fulfillment-mesh',
  ]);

  for (const scenario of catalog.scenarios) {
    assert.ok(scenario.cliTour.length >= 4, `${scenario.id} should expose a CLI tour`);
    assert.ok(scenario.services.length >= 3, `${scenario.id} should model a multi-service system`);
    assert.ok(scenario.evidence.length >= 3, `${scenario.id} should explain Evidence outputs`);
    const scenarioText = JSON.stringify(scenario).toLowerCase();
    for (const phrase of blockedPhrases) {
      assert.ok(!scenarioText.includes(phrase.toLowerCase()), `${scenario.id} should not contain restricted phrase`);
    }
  }
});
