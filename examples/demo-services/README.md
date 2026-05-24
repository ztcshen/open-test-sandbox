# Demo Services

These scenarios are neutral showcase assets for explaining AgentTestBench
without relying on private or domain-specific business data. They are designed
to make the CLI story visible: register a target, discover runnable cases, run
API cases or workflows, and inspect Evidence.

## Start the Demo Server

```sh
node tools/examples/demo-service-server.mjs --port 49190
```

The server exposes three multi-service-style demo endpoints:

- `POST /retail/orders`
- `POST /iot/telemetry`
- `POST /moderation/reviews`

It also exposes:

- `GET /health`
- `GET /scenarios`

## Scenario Catalog

`catalog.json` describes the services, CLI tour, and Evidence story for each
scenario:

- `retail-fulfillment-mesh`: checkout to shipment across catalog, cart,
  payment, risk, warehouse, and carrier services.
- `iot-telemetry-control`: telemetry ingest to command dispatch across
  normalizer, anomaly, rules, command, and audit services.
- `content-moderation-pipeline`: content submission to policy/model review,
  escalation, notification, and appeal readiness.

## Why These Examples

The examples mirror mature open-source demo patterns: users get a small
runnable target plus a visual story that explains what commands prove. The
assets stay generic so the public repository can show real product value
without embedding private team workflows.
