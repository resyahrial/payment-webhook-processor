# payment-webhook-processor

<<<<<<< HEAD
`baseline-direct-processing` is a deliberately simple payment webhook processor that handles provider events synchronously inside the HTTP request.

This branch is meant to prove two things:

- direct processing is easy to understand and works well at normal traffic levels
- direct processing becomes risky under provider spikes because webhook latency, database pressure, and error rates rise together

## Purpose

This project receives payment webhooks, validates the provider signature, stores raw events, prevents duplicate processing, updates the latest payment state in PostgreSQL, records anomalies, and exposes metrics and logs for operational visibility.

It intentionally does not hide the tradeoff of synchronous processing. Every accepted webhook performs its work inline before returning a response, so the webhook API and PostgreSQL both feel load spikes immediately.

## Architecture

```text
Provider
  -> Webhook API
  -> Signature Validation
  -> Idempotency Check
  -> Direct Payment Processing
  -> PostgreSQL
  -> Metrics / Logs / Dashboard
```

Implemented HTTP endpoints:

- `GET /healthz`
- `POST /webhooks/payment`
- `GET /metrics`

Supported sample payment events:

- `payment.pending`
- `payment.paid`
- `payment.failed`
- `payment.expired`

## In Scope

- payment webhook receiver
- provider signature validation
- idempotency using provider event ID
- direct synchronous processing
- payment state updates in PostgreSQL
- raw webhook event storage
- anomaly recording for suspicious transitions
- structured JSON logging
- Prometheus metrics
- provisioned Grafana dashboard
- k6 load test scenarios

## Out Of Scope

This branch does not implement:

- Message queue
- Background worker
- Retry queue
- DLQ
- Multi-provider support
- Real payment gateway integration

## Prerequisites

- Docker and Docker Compose
- optional: `curl` for sending a sample webhook manually
- optional: `openssl` for generating the webhook signature from the command line
- optional: `k6` for load testing

## Environment Variables

Copy `.env.example` to `.env` if you want to override defaults.

Required by the app:

- `PORT`: HTTP port for the app, default `8080`
- `APP_ENV`: environment label for logs, default `development`
- `WEBHOOK_SIGNING_SECRET`: HMAC secret used to validate `X-Webhook-Signature`
- `DATABASE_URL`: PostgreSQL connection string used by the app

Used by Docker Compose defaults:

- `POSTGRES_DB`: default `payments`
- `POSTGRES_USER`: default `payments`
- `POSTGRES_PASSWORD`: default `payments`
- `GRAFANA_ADMIN_USER`: default `admin`
- `GRAFANA_ADMIN_PASSWORD`: default `admin`

Used by k6:

- `K6_WEBHOOK_BASE_URL`: default `http://localhost:8080`
- `K6_NORMAL_RATE`: default `5`
- `K6_SPIKE_RATE`: default `2000`

## Run Locally

Common shortcuts are available in the root `Makefile`:

```bash
make help
```

Start the full stack:

```bash
make up

# or run the underlying command directly
docker compose up --build
```

This starts:

- PostgreSQL on `localhost:5432`
- app on `http://localhost:8080`
- Prometheus on `http://localhost:9090`
- Grafana on `http://localhost:3000`

Useful URLs after startup:

- health check: `http://localhost:8080/healthz`
- webhook endpoint: `http://localhost:8080/webhooks/payment`
- metrics endpoint: `http://localhost:8080/metrics`
- Prometheus UI: `http://localhost:9090`
- Grafana UI: `http://localhost:3000`

Useful local helper targets:

- `make logs`
- `make health`
- `make metrics`
- `make test`
- `make test-metrics`
- `make server`

Expected Grafana login unless overridden:

- username: `admin`
- password: `admin`

## Send A Sample Webhook

The app expects a raw JSON request body signed with HMAC-SHA256 hex using `WEBHOOK_SIGNING_SECRET` and passed in the `X-Webhook-Signature` header.

Example payload:

```json
{
  "provider_event_id": "evt_demo_001",
  "payment_id": "pay_demo_001",
  "event_type": "payment.paid",
  "event_timestamp": "2026-06-18T10:30:00Z"
}
```

Example request using `openssl` and `curl`:

```bash
PAYLOAD='{"provider_event_id":"evt_demo_001","payment_id":"pay_demo_001","event_type":"payment.paid","event_timestamp":"2026-06-18T10:30:00Z"}'
SIGNATURE=$(printf '%s' "$PAYLOAD" | openssl dgst -sha256 -hmac 'dev-webhook-signing-secret' -binary | xxd -p -c 256)

curl -i http://localhost:8080/webhooks/payment \
  -H 'Content-Type: application/json' \
  -H "X-Webhook-Signature: $SIGNATURE" \
  --data "$PAYLOAD"
```

Expected success response:

```json
{"status":"processed"}
```

If you resend the exact same `provider_event_id`, the idempotency layer should accept the request and respond with:

```json
{"status":"duplicate"}
```

If the signature is missing or wrong, the endpoint returns `401` with:

```json
{"status":"unauthorized"}
```

## Run Load Tests

The k6 script targets `POST /webhooks/payment` and exercises the synchronous path under several traffic patterns.

Run one scenario at a time:

```bash
make loadtest-normal
make loadtest-duplicate
make loadtest-mixed-signatures
make loadtest-spike

# or run the underlying k6 commands directly
k6 run -e SCENARIO=normal loadtest/payment_webhooks.js
k6 run -e SCENARIO=duplicate loadtest/payment_webhooks.js
k6 run -e SCENARIO=mixed_signatures loadtest/payment_webhooks.js
k6 run -e SCENARIO=spike loadtest/payment_webhooks.js
```

Example with explicit environment overrides:

```bash
K6_WEBHOOK_BASE_URL=http://localhost:8080 WEBHOOK_SIGNING_SECRET=dev-webhook-signing-secret k6 run -e SCENARIO=spike -e K6_SPIKE_RATE=60 loadtest/payment_webhooks.js
```

Scenario intent:

- `normal`: steady valid traffic, should mostly return `200` with `processed`
- `duplicate`: repeated delivery of the same event, should show `duplicate`
- `mixed_signatures`: mixes valid and invalid signatures, should show both `processed` and `unauthorized`
- `spike`: aggressive arrival rate intended to expose the weakness of direct synchronous processing

For more detail, see `loadtest/README.md`.

## Metrics, Prometheus, And Grafana

Prometheus scrapes the app from `/metrics`, and Grafana is provisioned with a Prometheus datasource plus a dashboard for this project.

Primary observability surfaces:

- metrics endpoint: `http://localhost:8080/metrics`
- Prometheus UI: `http://localhost:9090`
- Grafana UI: `http://localhost:3000`

The dashboard includes these views:

- `Webhook Request Rate`
- `Webhook Success and Error Rate`
- `P95 Webhook Latency`
- `P95 Database Write Latency`
- `Duplicate Event Count`
- `Anomaly Count`
- `Provider Traffic Spike`
- `Payment Event Status Distribution`

Useful metrics exposed by the app include:

- `payment_webhook_requests_total`
- `payment_webhook_success_total`
- `payment_webhook_errors_total`
- `payment_webhook_signature_failures_total`
- `payment_webhook_duplicates_total`
- `payment_webhook_anomalies_total`
- `payment_webhook_provider_events_total`
- `payment_webhook_response_duration_seconds`
- `payment_webhook_database_write_duration_seconds`
- `payment_webhook_processing_duration_seconds`

## Operational Problem Revealed By Direct Processing

This branch is intentionally useful as a baseline, not as the final architecture.

Because each webhook request performs signature validation, raw event storage, idempotency checks, payment updates, anomaly recording, and response generation inline, provider spikes hit the request path and the database at the same time.

Under spike load you should expect symptoms such as:

- increased webhook latency
- increased database latency
- more dropped k6 iterations when the arrival rate exceeds what the app can process synchronously
- more `500` responses on slower machines or at aggressive spike rates
- Grafana panels showing visible degradation compared with the `normal` scenario

This is the main lesson of `baseline-direct-processing`: the implementation is straightforward and observable, but direct synchronous processing does not absorb provider bursts safely.

If the spike scenario is too aggressive for your machine, reduce `K6_SPIKE_RATE`. If it does not show enough degradation, raise it and rerun.

The stack includes Prometheus, Grafana, and PostgreSQL exporter so load tests can be correlated with both service-level and database-level infrastructure signals.
