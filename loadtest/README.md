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
- `K6_CAPACITY_START_RATE`: initial requests/second for the capacity ramp, defaults to `10`
- `K6_CAPACITY_PEAK_RATE`: highest requests/second for the capacity ramp, defaults to `250`
- `K6_NOISY_BASELINE_RATE`: baseline requests/second for the noisy-neighbor scenario, defaults to `5`
- `K6_NOISY_SPIKE_START_RATE`: starting requests/second for the noisy-neighbor spike stream, defaults to `20`
- `K6_NOISY_SPIKE_PEAK_RATE`: peak requests/second for the noisy-neighbor spike stream, defaults to `200`
- `K6_HOT_PAYMENT_RATE`: requests/second for the hot-payments scenario, defaults to `150`
- `K6_HOT_PAYMENT_SET_SIZE`: number of shared payment IDs in the hot-payments scenario, defaults to `5`

The app itself also exposes environment variables that shape natural failure behavior:

- `DB_MAX_OPEN_CONNS`: database pool max open connections, defaults to `10`
- `DB_MAX_IDLE_CONNS`: database pool max idle connections, defaults to `5`
- `DB_CONN_MAX_LIFETIME`: database connection lifetime, defaults to `30m`

Observability now comes from two runtime sources:

- app metrics at `http://localhost:8080/metrics`
- PostgreSQL infra metrics from `postgres_exporter`, scraped by Prometheus

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

Capacity boundary discovery:

```bash
k6 run -e SCENARIO=capacity_ramp loadtest/payment_webhooks.js
```

Noisy neighbor:

```bash
k6 run -e SCENARIO=noisy_neighbor loadtest/payment_webhooks.js
```

Hot payments:

```bash
k6 run -e SCENARIO=hot_payments loadtest/payment_webhooks.js
```

Example with explicit target and higher spike rate:

```bash
K6_WEBHOOK_BASE_URL=http://localhost:8080 WEBHOOK_SIGNING_SECRET=dev-webhook-signing-secret k6 run -e SCENARIO=spike -e K6_SPIKE_RATE=60 loadtest/payment_webhooks.js
```

Example capacity discovery tuned for a smaller laptop:

```bash
K6_CAPACITY_START_RATE=10 K6_CAPACITY_PEAK_RATE=120 k6 run -e SCENARIO=capacity_ramp loadtest/payment_webhooks.js
```

## Expected Results

- `normal`: HTTP `200` responses with `{"status":"processed"}` and low failure rate
- `duplicate`: first request processed, later requests accepted with `{"status":"duplicate"}`
- `mixed_signatures`: both `200 processed` and `401 unauthorized` responses appear
- `spike`: latency should increase versus `normal`, dropped iterations are expected at the default spike rate, and slower machines may also show more `500` responses
- `capacity_ramp`: should reveal the request rate where direct synchronous processing breaches the `5s` provider-facing response budget
- `noisy_neighbor`: the baseline traffic should start to slow down while the noisy spike stream is active
- `hot_payments`: should raise webhook latency and database pressure by concentrating many events on a small set of payment IDs

## Failure Interpretation

The hard provider-facing failure boundary is `5s`. A request that eventually returns `200` but takes longer than `5s` still represents operational failure because providers commonly retry or time out on slow acknowledgements.

Use the scenarios to answer different questions:

- `spike`: can a sudden provider burst increase latency or trigger server errors?
- `capacity_ramp`: where is the natural breaking point for the current architecture?
- `noisy_neighbor`: does one traffic source degrade another concurrent source?
- `hot_payments`: does resource contention around shared payment records amplify pressure?

When reading k6 output, distinguish these cases:

- rising `http_req_duration` and dashboard latency: the app or database is slowing down
- rising `payment_webhook_db_wait_count_total` or `payment_webhook_db_wait_duration_seconds_total`: the database pool is saturated and requests are queueing for a connection
- rising `payment_webhook_payment_processing_total{status="ignored"}`: out-of-order or stale events are being accepted but skipped by payment-state logic
- rising `payment_webhook_payment_processing_total{status="failed"}`: payment processing is failing inside the service, not just at the HTTP envelope
- many `dropped_iterations` without corresponding app/database latency growth: the load generator may be under-provisioned
- `401 unauthorized` responses in `mixed_signatures`: expected validation failures, not load failure

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
- `DB Pool Connections`: watch open, in-use, and idle connections converge toward the configured limits
- `DB Pool Wait Rate`: should increase when requests queue for a DB connection
- `DB Pool Wait Duration`: should increase when synchronous processing stalls on DB pool contention
- `Payment Processing Outcomes`: shows `created`, `updated`, `ignored`, and `failed` result rates inside payment-state processing
- `App CPU Usage`, `App RSS Memory`, `App Goroutines`: show whether service-side saturation is CPU, memory, or concurrency driven
- `PostgreSQL Availability`, `PostgreSQL Connections`, `PostgreSQL Transaction Rate`: show whether the database is healthy and how hard it is being driven under load

Local machine performance affects the spike scenario. If the default spike rate is too aggressive for your machine, lower `K6_SPIKE_RATE`. If it is too mild to show degradation, raise it and rerun.
