package metrics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

func TestMetricsExportsPrometheusCompatibleSeries(t *testing.T) {
	t.Parallel()

	m := New()
	t.Cleanup(func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown metrics: %v", err)
		}
	})

	m.IncWebhookRequest()
	m.IncWebhookSuccess("processed")
	m.IncWebhookError("internal_error")
	m.IncSignatureFailure()
	m.IncWebhookDuplicate("payment.failed")
	m.IncAnomaly("older_event_timestamp")
	m.IncProviderEvent("payment.paid", "paid")
	m.ObserveWebhookResponseDuration("processed", 25*time.Millisecond)
	m.ObserveDatabaseWriteDuration("upsert_payment", 10*time.Millisecond)
	m.ObservePaymentProcessingDuration("updated", 15*time.Millisecond)

	assertCounterValue(t, m, "payment_webhook_requests_total", nil, 1)
	assertCounterValue(t, m, "payment_webhook_success_total", map[string]string{"result": "processed"}, 1)
	assertCounterValue(t, m, "payment_webhook_errors_total", map[string]string{"result": "internal_error"}, 1)
	assertCounterValue(t, m, "payment_webhook_signature_failures_total", nil, 1)
	assertCounterValue(t, m, "payment_webhook_duplicates_total", map[string]string{"event_type": "payment.failed"}, 1)
	assertCounterValue(t, m, "payment_webhook_anomalies_total", map[string]string{"anomaly_type": "older_event_timestamp"}, 1)
	assertCounterValue(t, m, "payment_webhook_provider_events_total", map[string]string{"event_type": "payment.paid", "status": "paid"}, 1)
	assertHistogramCount(t, m, "payment_webhook_response_duration_seconds", map[string]string{"result": "processed"}, 1)
	assertHistogramCount(t, m, "payment_webhook_database_write_duration_seconds", map[string]string{"operation": "upsert_payment"}, 1)
	assertHistogramCount(t, m, "payment_webhook_processing_duration_seconds", map[string]string{"status": "updated"}, 1)
}

func TestRegisterDatabaseStatsCollectorExportsPoolSeries(t *testing.T) {
	t.Parallel()

	m := New()
	t.Cleanup(func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown metrics: %v", err)
		}
	})

	database := &sql.DB{}
	database.SetMaxOpenConns(12)

	if err := m.RegisterDatabaseStatsCollector(database); err != nil {
		t.Fatalf("register database stats collector: %v", err)
	}

	assertGaugeValue(t, m, "payment_webhook_db_open_connections", nil, 0)
	assertGaugeValue(t, m, "payment_webhook_db_in_use_connections", nil, 0)
	assertGaugeValue(t, m, "payment_webhook_db_idle_connections", nil, 0)
	assertCounterValue(t, m, "payment_webhook_db_wait_count_total", nil, 0)
	assertCounterValue(t, m, "payment_webhook_db_wait_duration_seconds_total", nil, 0)
}

func TestWebhookResponseTimerUsesFinalResultLabel(t *testing.T) {
	t.Parallel()

	m := New()
	t.Cleanup(func() {
		if err := m.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown metrics: %v", err)
		}
	})

	result := "method_not_allowed"
	timer := m.StartWebhookResponseTimer(&result)
	result = "processed"
	timer.Observe()

	assertHistogramCount(t, m, "payment_webhook_response_duration_seconds", map[string]string{"result": "processed"}, 1)
}

func TestNilMetricsTimerIsSafe(t *testing.T) {
	t.Parallel()

	var m *Metrics
	if duration := m.StartDatabaseWriteTimer("insert_webhook_event").Observe(); duration != 0 {
		t.Fatalf("expected zero duration for nil metrics timer, got %s", duration)
	}
}

func assertCounterValue(t *testing.T, metrics *Metrics, metricName string, expectedLabels map[string]string, expectedValue float64) {
	t.Helper()

	metric := findMetric(t, metrics, metricName, expectedLabels)
	if got := metric.GetCounter().GetValue(); got != expectedValue {
		t.Fatalf("expected counter %s with labels %v to be %v, got %v", metricName, expectedLabels, expectedValue, got)
	}
}

func assertHistogramCount(t *testing.T, metrics *Metrics, metricName string, expectedLabels map[string]string, expectedCount uint64) {
	t.Helper()

	metric := findMetric(t, metrics, metricName, expectedLabels)
	if got := metric.GetHistogram().GetSampleCount(); got != expectedCount {
		t.Fatalf("expected histogram %s with labels %v count %d, got %d", metricName, expectedLabels, expectedCount, got)
	}
}

func assertGaugeValue(t *testing.T, metrics *Metrics, metricName string, expectedLabels map[string]string, expectedValue float64) {
	t.Helper()

	metric := findMetric(t, metrics, metricName, expectedLabels)
	if got := metric.GetGauge().GetValue(); got != expectedValue {
		t.Fatalf("expected gauge %s with labels %v to be %v, got %v", metricName, expectedLabels, expectedValue, got)
	}
}

func findMetric(t *testing.T, metrics *Metrics, metricName string, expectedLabels map[string]string) *dto.Metric {
	t.Helper()

	metricFamilies, err := metrics.Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != metricName {
			continue
		}

		for _, metric := range metricFamily.GetMetric() {
			if labelsMatch(metric.GetLabel(), expectedLabels) {
				return metric
			}
		}
	}

	t.Fatalf("metric %s with labels %v not found; gathered metrics: %v", metricName, expectedLabels, gatheredMetricNames(metricFamilies))
	return nil
}

func labelsMatch(metricLabels []*dto.LabelPair, expectedLabels map[string]string) bool {
	if len(expectedLabels) == 0 {
		return len(metricLabels) == 0
	}

	if len(metricLabels) != len(expectedLabels) {
		return false
	}

	for _, label := range metricLabels {
		if expectedLabels[label.GetName()] != label.GetValue() {
			return false
		}
	}

	return true
}

func gatheredMetricNames(metricFamilies []*dto.MetricFamily) []string {
	names := make([]string, 0, len(metricFamilies))
	for _, metricFamily := range metricFamilies {
		names = append(names, metricFamily.GetName())
	}

	return names
}
