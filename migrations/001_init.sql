CREATE TABLE IF NOT EXISTS payments (
	payment_id TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	status_timestamp TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_events (
	id BIGSERIAL PRIMARY KEY,
	provider_event_id TEXT NOT NULL UNIQUE,
	payment_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	event_timestamp TIMESTAMPTZ NOT NULL,
	raw_payload JSONB NOT NULL,
	processing_status TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS anomalies (
	id BIGSERIAL PRIMARY KEY,
	webhook_event_id BIGINT REFERENCES webhook_events(id) ON DELETE SET NULL,
	payment_id TEXT REFERENCES payments(payment_id) ON DELETE SET NULL,
	anomaly_type TEXT NOT NULL,
	details JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
