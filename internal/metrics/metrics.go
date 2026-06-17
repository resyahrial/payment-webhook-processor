package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "payment_webhook"

type Metrics struct {
	registry *prometheus.Registry

	webhookRequestsTotal      prometheus.Counter
	webhookSuccessTotal       *prometheus.CounterVec
	webhookErrorsTotal        *prometheus.CounterVec
	webhookSignatureFailures  prometheus.Counter
	webhookDuplicatesTotal    *prometheus.CounterVec
	webhookAnomaliesTotal     *prometheus.CounterVec
	providerEventsTotal       *prometheus.CounterVec
	webhookResponseDuration   *prometheus.HistogramVec
	databaseWriteDuration     *prometheus.HistogramVec
	paymentProcessingDuration *prometheus.HistogramVec
}

func New() *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return newWithRegistry(registry)
}

func newWithRegistry(registry *prometheus.Registry) *Metrics {
	m := &Metrics{
		registry: registry,
		webhookRequestsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of webhook requests received.",
		}),
		webhookSuccessTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "success_total",
			Help:      "Total number of successfully handled webhooks.",
		}, []string{"result"}),
		webhookErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "errors_total",
			Help:      "Total number of webhook requests that returned an error.",
		}, []string{"result"}),
		webhookSignatureFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "signature_failures_total",
			Help:      "Total number of webhook signature validation failures.",
		}),
		webhookDuplicatesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "duplicates_total",
			Help:      "Total number of duplicate webhook events.",
		}, []string{"event_type"}),
		webhookAnomaliesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "anomalies_total",
			Help:      "Total number of anomalies detected while processing payments.",
		}, []string{"anomaly_type"}),
		providerEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "provider_events_total",
			Help:      "Total number of provider events accepted for processing.",
		}, []string{"event_type", "status"}),
		webhookResponseDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "response_duration_seconds",
			Help:      "Webhook response latency in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.005, 2, 10),
		}, []string{"result"}),
		databaseWriteDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "database_write_duration_seconds",
			Help:      "Database write latency in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10),
		}, []string{"operation"}),
		paymentProcessingDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "processing_duration_seconds",
			Help:      "Payment processing duration in seconds.",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 10),
		}, []string{"status"}),
	}

	registry.MustRegister(
		m.webhookRequestsTotal,
		m.webhookSuccessTotal,
		m.webhookErrorsTotal,
		m.webhookSignatureFailures,
		m.webhookDuplicatesTotal,
		m.webhookAnomaliesTotal,
		m.providerEventsTotal,
		m.webhookResponseDuration,
		m.databaseWriteDuration,
		m.paymentProcessingDuration,
	)

	return m
}

func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return promhttp.Handler()
	}

	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}

	return m.registry
}

func (m *Metrics) IncWebhookRequest() {
	if m == nil {
		return
	}

	m.webhookRequestsTotal.Inc()
}

func (m *Metrics) IncWebhookSuccess(result string) {
	if m == nil {
		return
	}

	m.webhookSuccessTotal.WithLabelValues(labelValue(result)).Inc()
}

func (m *Metrics) IncWebhookError(result string) {
	if m == nil {
		return
	}

	m.webhookErrorsTotal.WithLabelValues(labelValue(result)).Inc()
}

func (m *Metrics) IncSignatureFailure() {
	if m == nil {
		return
	}

	m.webhookSignatureFailures.Inc()
}

func (m *Metrics) IncWebhookDuplicate(eventType string) {
	if m == nil {
		return
	}

	m.webhookDuplicatesTotal.WithLabelValues(labelValue(eventType)).Inc()
}

func (m *Metrics) IncAnomaly(anomalyType string) {
	if m == nil {
		return
	}

	m.webhookAnomaliesTotal.WithLabelValues(labelValue(anomalyType)).Inc()
}

func (m *Metrics) IncProviderEvent(eventType, status string) {
	if m == nil {
		return
	}

	m.providerEventsTotal.WithLabelValues(labelValue(eventType), labelValue(status)).Inc()
}

func (m *Metrics) ObserveWebhookResponseDuration(result string, duration time.Duration) {
	if m == nil {
		return
	}

	m.webhookResponseDuration.WithLabelValues(labelValue(result)).Observe(duration.Seconds())
}

func (m *Metrics) ObserveDatabaseWriteDuration(operation string, duration time.Duration) {
	if m == nil {
		return
	}

	m.databaseWriteDuration.WithLabelValues(labelValue(operation)).Observe(duration.Seconds())
}

func (m *Metrics) ObservePaymentProcessingDuration(status string, duration time.Duration) {
	if m == nil {
		return
	}

	m.paymentProcessingDuration.WithLabelValues(labelValue(status)).Observe(duration.Seconds())
}

func labelValue(value string) string {
	if value == "" {
		return "unknown"
	}

	return value
}
