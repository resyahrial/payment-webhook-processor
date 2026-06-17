package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/otlptranslator"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const namespace = "payment_webhook"

const meterName = "payment-webhook-processor/internal/metrics"

var (
	resultKey      = attribute.Key("result")
	statusKey      = attribute.Key("status")
	operationKey   = attribute.Key("operation")
	eventTypeKey   = attribute.Key("event_type")
	anomalyTypeKey = attribute.Key("anomaly_type")
)

type Metrics struct {
	registry      *prometheus.Registry
	meterProvider *sdkmetric.MeterProvider

	webhookRequestsTotal      otelmetric.Int64Counter
	webhookSuccessTotal       otelmetric.Int64Counter
	webhookErrorsTotal        otelmetric.Int64Counter
	webhookSignatureFailures  otelmetric.Int64Counter
	webhookDuplicatesTotal    otelmetric.Int64Counter
	webhookAnomaliesTotal     otelmetric.Int64Counter
	providerEventsTotal       otelmetric.Int64Counter
	webhookResponseDuration   otelmetric.Float64Histogram
	databaseWriteDuration     otelmetric.Float64Histogram
	paymentProcessingDuration otelmetric.Float64Histogram
}

type Timer struct {
	startedAt time.Time
	observe   func(time.Duration)
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
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(registry),
		otelprom.WithNamespace(namespace),
		otelprom.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
		otelprom.WithoutScopeInfo(),
	)
	if err != nil {
		panic(err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithView(durationHistogramView("response.duration", prometheus.ExponentialBuckets(0.005, 2, 10))),
		sdkmetric.WithView(durationHistogramView("database.write.duration", prometheus.ExponentialBuckets(0.001, 2, 10))),
		sdkmetric.WithView(durationHistogramView("processing.duration", prometheus.ExponentialBuckets(0.001, 2, 10))),
	)
	meter := provider.Meter(meterName)

	webhookRequestsTotal, err := meter.Int64Counter("requests", otelmetric.WithDescription("Total number of webhook requests received."))
	if err != nil {
		panic(err)
	}
	webhookSuccessTotal, err := meter.Int64Counter("success", otelmetric.WithDescription("Total number of successfully handled webhooks."))
	if err != nil {
		panic(err)
	}
	webhookErrorsTotal, err := meter.Int64Counter("errors", otelmetric.WithDescription("Total number of webhook requests that returned an error."))
	if err != nil {
		panic(err)
	}
	webhookSignatureFailures, err := meter.Int64Counter("signature_failures", otelmetric.WithDescription("Total number of webhook signature validation failures."))
	if err != nil {
		panic(err)
	}
	webhookDuplicatesTotal, err := meter.Int64Counter("duplicates", otelmetric.WithDescription("Total number of duplicate webhook events."))
	if err != nil {
		panic(err)
	}
	webhookAnomaliesTotal, err := meter.Int64Counter("anomalies", otelmetric.WithDescription("Total number of anomalies detected while processing payments."))
	if err != nil {
		panic(err)
	}
	providerEventsTotal, err := meter.Int64Counter("provider_events", otelmetric.WithDescription("Total number of provider events accepted for processing."))
	if err != nil {
		panic(err)
	}
	webhookResponseDuration, err := meter.Float64Histogram("response.duration", otelmetric.WithDescription("Webhook response latency in seconds."), otelmetric.WithUnit("s"))
	if err != nil {
		panic(err)
	}
	databaseWriteDuration, err := meter.Float64Histogram("database.write.duration", otelmetric.WithDescription("Database write latency in seconds."), otelmetric.WithUnit("s"))
	if err != nil {
		panic(err)
	}
	paymentProcessingDuration, err := meter.Float64Histogram("processing.duration", otelmetric.WithDescription("Payment processing duration in seconds."), otelmetric.WithUnit("s"))
	if err != nil {
		panic(err)
	}

	return &Metrics{
		registry:                  registry,
		meterProvider:             provider,
		webhookRequestsTotal:      webhookRequestsTotal,
		webhookSuccessTotal:       webhookSuccessTotal,
		webhookErrorsTotal:        webhookErrorsTotal,
		webhookSignatureFailures:  webhookSignatureFailures,
		webhookDuplicatesTotal:    webhookDuplicatesTotal,
		webhookAnomaliesTotal:     webhookAnomaliesTotal,
		providerEventsTotal:       providerEventsTotal,
		webhookResponseDuration:   webhookResponseDuration,
		databaseWriteDuration:     databaseWriteDuration,
		paymentProcessingDuration: paymentProcessingDuration,
	}
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

func (m *Metrics) Shutdown(ctx context.Context) error {
	if m == nil || m.meterProvider == nil {
		return nil
	}

	return m.meterProvider.Shutdown(ctx)
}

func (m *Metrics) IncWebhookRequest() {
	if m == nil {
		return
	}

	m.webhookRequestsTotal.Add(context.Background(), 1)
}

func (m *Metrics) IncWebhookSuccess(result string) {
	if m == nil {
		return
	}

	m.webhookSuccessTotal.Add(context.Background(), 1, otelmetric.WithAttributes(resultKey.String(labelValue(result))))
}

func (m *Metrics) IncWebhookError(result string) {
	if m == nil {
		return
	}

	m.webhookErrorsTotal.Add(context.Background(), 1, otelmetric.WithAttributes(resultKey.String(labelValue(result))))
}

func (m *Metrics) IncSignatureFailure() {
	if m == nil {
		return
	}

	m.webhookSignatureFailures.Add(context.Background(), 1)
}

func (m *Metrics) IncWebhookDuplicate(eventType string) {
	if m == nil {
		return
	}

	m.webhookDuplicatesTotal.Add(context.Background(), 1, otelmetric.WithAttributes(eventTypeKey.String(labelValue(eventType))))
}

func (m *Metrics) IncAnomaly(anomalyType string) {
	if m == nil {
		return
	}

	m.webhookAnomaliesTotal.Add(context.Background(), 1, otelmetric.WithAttributes(anomalyTypeKey.String(labelValue(anomalyType))))
}

func (m *Metrics) IncProviderEvent(eventType, status string) {
	if m == nil {
		return
	}

	m.providerEventsTotal.Add(context.Background(), 1, otelmetric.WithAttributes(
		eventTypeKey.String(labelValue(eventType)),
		statusKey.String(labelValue(status)),
	))
}

func (m *Metrics) ObserveWebhookResponseDuration(result string, duration time.Duration) {
	if m == nil {
		return
	}

	m.webhookResponseDuration.Record(context.Background(), duration.Seconds(), otelmetric.WithAttributes(resultKey.String(labelValue(result))))
}

func (m *Metrics) ObserveDatabaseWriteDuration(operation string, duration time.Duration) {
	if m == nil {
		return
	}

	m.databaseWriteDuration.Record(context.Background(), duration.Seconds(), otelmetric.WithAttributes(operationKey.String(labelValue(operation))))
}

func (m *Metrics) ObservePaymentProcessingDuration(status string, duration time.Duration) {
	if m == nil {
		return
	}

	m.paymentProcessingDuration.Record(context.Background(), duration.Seconds(), otelmetric.WithAttributes(statusKey.String(labelValue(status))))
}

func (m *Metrics) StartWebhookResponseTimer(result *string) Timer {
	if m == nil {
		return Timer{}
	}

	return Timer{
		startedAt: time.Now(),
		observe: func(duration time.Duration) {
			label := "unknown"
			if result != nil {
				label = *result
			}
			m.ObserveWebhookResponseDuration(label, duration)
		},
	}
}

func (m *Metrics) StartDatabaseWriteTimer(operation string) Timer {
	if m == nil {
		return Timer{}
	}

	return Timer{
		startedAt: time.Now(),
		observe: func(duration time.Duration) {
			m.ObserveDatabaseWriteDuration(operation, duration)
		},
	}
}

func (m *Metrics) StartPaymentProcessingTimer(status *string) Timer {
	if m == nil {
		return Timer{}
	}

	return Timer{
		startedAt: time.Now(),
		observe: func(duration time.Duration) {
			label := "unknown"
			if status != nil {
				label = *status
			}
			m.ObservePaymentProcessingDuration(label, duration)
		},
	}
}

func labelValue(value string) string {
	if value == "" {
		return "unknown"
	}

	return value
}

func (t Timer) Observe() time.Duration {
	if t.startedAt.IsZero() {
		return 0
	}

	duration := time.Since(t.startedAt)
	if t.observe != nil {
		t.observe(duration)
	}

	return duration
}

func durationHistogramView(name string, buckets []float64) sdkmetric.View {
	return sdkmetric.NewView(
		sdkmetric.Instrument{Name: name},
		sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: buckets}},
	)
}
