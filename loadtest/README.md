# Load Testing

This branch includes a `k6` script for exercising the synchronous webhook flow at `POST /webhooks/payment`.

## Prerequisites

- `k6` installed locally
- the stack running with `docker compose up --build`
- app reachable at `http://localhost:8080`

## Environment

The script reads these environment variables:

- `K6_WEBHOOK_BASE_URL`: target base URL, defaults to `http://localhost:8080`
- `WEBHOOK_SIGNING_SECRET`: webhook HMAC secret, defaults to `dev-webhook-signing-secret`
- `K6_NORMAL_RATE`: optional requests/second override for the normal scenario
- `K6_SPIKE_RATE`: optional requests/second override for the spike scenario, defaults to `2000`

## Run Scenarios

Run one scenario at a time so the thresholds and dashboard signals stay easy to read.

Normal traffic:

```bash
k6 run -e SCENARIO=normal loadtest/payment_webhooks.js
```

Duplicate delivery:

```bash
k6 run -e SCENARIO=duplicate loadtest/payment_webhooks.js
```

Mixed valid and invalid signatures:

```bash
k6 run -e SCENARIO=mixed_signatures loadtest/payment_webhooks.js
```

Provider spike:

```bash
k6 run -e SCENARIO=spike loadtest/payment_webhooks.js
```

Example with explicit target and higher spike rate:

```bash
K6_WEBHOOK_BASE_URL=http://localhost:8080 WEBHOOK_SIGNING_SECRET=dev-webhook-signing-secret k6 run -e SCENARIO=spike -e K6_SPIKE_RATE=60 loadtest/payment_webhooks.js
```

## Expected Results

- `normal`: HTTP `200` responses with `{"status":"processed"}` and low failure rate
- `duplicate`: first request processed, later requests accepted with `{"status":"duplicate"}`
- `mixed_signatures`: both `200 processed` and `401 unauthorized` responses appear
- `spike`: latency should increase versus `normal`, dropped iterations are expected at the default spike rate, and slower machines may also show more `500` responses

## Dashboard Verification

With `docker compose up --build` running, open:

- Grafana: `http://localhost:3000`
- Prometheus: `http://localhost:9090`
- Metrics endpoint: `http://localhost:8080/metrics`

During each run, verify the provisioned Grafana dashboard reacts:

- `Webhook Request Rate`: moves for every scenario
- `Webhook Success and Error Rate`: mixed signatures should raise unauthorized/error traffic
- `P95 Webhook Latency`: spike should move above normal traffic
- `P95 Database Write Latency`: spike should rise with synchronous writes
- `Duplicate Event Count`: duplicate scenario should increment this panel
- `Provider Traffic Spike`: spike scenario should be visually obvious here
- `Payment Event Status Distribution`: valid scenarios should shift the status mix

Local machine performance affects the spike scenario. If the default spike rate is too aggressive for your machine, lower `K6_SPIKE_RATE`. If it is too mild to show degradation, raise it and rerun.
