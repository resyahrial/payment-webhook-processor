package service

import (
	"context"
	"errors"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	appmetrics "payment-webhook-processor/internal/metrics"
	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/webhook"
)

func TestPaymentProcessorProcessCreatesPaymentForFirstEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := paymentProcessorEvent("evt_401", "pay_401", webhook.EventTypePaymentPending, time.Date(2026, time.June, 17, 10, 30, 0, 0, time.UTC))
	repo := &stubPaymentStateRepository{getErr: repository.ErrPaymentNotFound}
	anomalies := &stubAnomalyRecorder{}

	processorMetrics := appmetrics.New()
	processor := NewPaymentProcessor(repo, anomalies, processorMetrics)
	result, err := processor.Process(ctx, PaymentUpdate{Event: event, WebhookEventID: 101})
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusCreated {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusCreated, result.Status)
	}

	if repo.upsertCalls != 1 {
		t.Fatalf("expected 1 upsert call, got %d", repo.upsertCalls)
	}

	if anomalies.calls != 0 {
		t.Fatalf("expected first event to skip anomaly recording, got %d calls", anomalies.calls)
	}

	assertPaymentState(t, repo.lastUpserted, repository.Payment{
		PaymentID:       event.PaymentID,
		Status:          event.PaymentStatus,
		StatusTimestamp: event.EventTimestamp,
	})

	assertProcessorHistogramCount(t, processorMetrics, "payment_webhook_processing_duration_seconds", map[string]string{"status": "created"}, 1)
}

func TestPaymentProcessorProcessUpdatesPaymentForNewerEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	current := repository.Payment{
		PaymentID:       "pay_402",
		Status:          webhook.PaymentStatusPending,
		StatusTimestamp: time.Date(2026, time.June, 17, 10, 30, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_402", current.PaymentID, webhook.EventTypePaymentPaid, current.StatusTimestamp.Add(5*time.Minute))
	repo := &stubPaymentStateRepository{payment: current}
	anomalies := &stubAnomalyRecorder{}

	processor := NewPaymentProcessor(repo, anomalies, nil)
	result, err := processor.Process(ctx, PaymentUpdate{Event: event, WebhookEventID: 102})
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusUpdated {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusUpdated, result.Status)
	}

	if repo.upsertCalls != 1 {
		t.Fatalf("expected 1 upsert call, got %d", repo.upsertCalls)
	}

	if anomalies.calls != 0 {
		t.Fatalf("expected normal update to skip anomaly recording, got %d calls", anomalies.calls)
	}

	assertPaymentState(t, repo.lastUpserted, repository.Payment{
		PaymentID:       event.PaymentID,
		Status:          event.PaymentStatus,
		StatusTimestamp: event.EventTimestamp,
	})
}

func TestPaymentProcessorProcessIgnoresOlderEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	current := repository.Payment{
		PaymentID:       "pay_403",
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: time.Date(2026, time.June, 17, 11, 30, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_403", current.PaymentID, webhook.EventTypePaymentPending, current.StatusTimestamp.Add(-5*time.Minute))
	repo := &stubPaymentStateRepository{payment: current}
	anomalies := &stubAnomalyRecorder{}

	processorMetrics := appmetrics.New()
	processor := NewPaymentProcessor(repo, anomalies, processorMetrics)
	result, err := processor.Process(ctx, PaymentUpdate{Event: event, WebhookEventID: 103})
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusIgnored {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusIgnored, result.Status)
	}

	if repo.upsertCalls != 0 {
		t.Fatalf("expected older event to skip upsert, got %d calls", repo.upsertCalls)
	}

	if anomalies.calls != 1 {
		t.Fatalf("expected older event anomaly to be recorded once, got %d calls", anomalies.calls)
	}

	if len(anomalies.records) != 1 || anomalies.records[0].AnomalyType != repository.AnomalyTypeOlderEventTimestamp {
		t.Fatalf("expected older timestamp anomaly, got %+v", anomalies.records)
	}

	assertProcessorCounterValue(t, processorMetrics, "payment_webhook_anomalies_total", map[string]string{"anomaly_type": string(repository.AnomalyTypeOlderEventTimestamp)}, 1)
	assertProcessorHistogramCount(t, processorMetrics, "payment_webhook_processing_duration_seconds", map[string]string{"status": "ignored"}, 1)
}

func TestPaymentProcessorProcessIgnoresEqualTimestampEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	timestamp := time.Date(2026, time.June, 17, 12, 30, 0, 0, time.UTC)
	current := repository.Payment{
		PaymentID:       "pay_404",
		Status:          webhook.PaymentStatusPending,
		StatusTimestamp: timestamp,
	}
	event := paymentProcessorEvent("evt_404", current.PaymentID, webhook.EventTypePaymentPaid, timestamp)
	repo := &stubPaymentStateRepository{payment: current}
	anomalies := &stubAnomalyRecorder{}

	processor := NewPaymentProcessor(repo, anomalies, nil)
	result, err := processor.Process(ctx, PaymentUpdate{Event: event, WebhookEventID: 104})
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusIgnored {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusIgnored, result.Status)
	}

	if repo.upsertCalls != 0 {
		t.Fatalf("expected equal timestamp event to skip upsert, got %d calls", repo.upsertCalls)
	}

	if anomalies.calls != 0 {
		t.Fatalf("expected equal timestamp event to skip anomaly recording, got %d calls", anomalies.calls)
	}
}

func TestPaymentProcessorProcessHandlesMultipleValidStatusChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	initialTimestamp := time.Date(2026, time.June, 17, 13, 30, 0, 0, time.UTC)
	repo := &stubPaymentStateRepository{getErr: repository.ErrPaymentNotFound}
	anomalies := &stubAnomalyRecorder{}
	processor := NewPaymentProcessor(repo, anomalies, nil)

	firstEvent := paymentProcessorEvent("evt_405_a", "pay_405", webhook.EventTypePaymentPending, initialTimestamp)
	secondEvent := paymentProcessorEvent("evt_405_b", "pay_405", webhook.EventTypePaymentPaid, initialTimestamp.Add(5*time.Minute))
	thirdEvent := paymentProcessorEvent("evt_405_c", "pay_405", webhook.EventTypePaymentExpired, initialTimestamp.Add(10*time.Minute))

	firstResult, err := processor.Process(ctx, PaymentUpdate{Event: firstEvent, WebhookEventID: 105})
	if err != nil {
		t.Fatalf("process first event: %v", err)
	}

	repo.getErr = nil
	repo.payment = repo.lastUpserted

	secondResult, err := processor.Process(ctx, PaymentUpdate{Event: secondEvent, WebhookEventID: 106})
	if err != nil {
		t.Fatalf("process second event: %v", err)
	}

	repo.payment = repo.lastUpserted

	thirdResult, err := processor.Process(ctx, PaymentUpdate{Event: thirdEvent, WebhookEventID: 107})
	if err != nil {
		t.Fatalf("process third event: %v", err)
	}

	if firstResult.Status != PaymentProcessingStatusCreated {
		t.Fatalf("expected first result status %q, got %q", PaymentProcessingStatusCreated, firstResult.Status)
	}

	if secondResult.Status != PaymentProcessingStatusUpdated {
		t.Fatalf("expected second result status %q, got %q", PaymentProcessingStatusUpdated, secondResult.Status)
	}

	if thirdResult.Status != PaymentProcessingStatusUpdated {
		t.Fatalf("expected third result status %q, got %q", PaymentProcessingStatusUpdated, thirdResult.Status)
	}

	if repo.upsertCalls != 3 {
		t.Fatalf("expected 3 upsert calls, got %d", repo.upsertCalls)
	}

	if anomalies.calls != 0 {
		t.Fatalf("expected valid transitions to skip anomaly recording, got %d calls", anomalies.calls)
	}

	assertPaymentState(t, repo.lastUpserted, repository.Payment{
		PaymentID:       thirdEvent.PaymentID,
		Status:          thirdEvent.PaymentStatus,
		StatusTimestamp: thirdEvent.EventTimestamp,
	})
}

func TestPaymentProcessorProcessReturnsFailedResultOnRepositoryError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := paymentProcessorEvent("evt_406", "pay_406", webhook.EventTypePaymentFailed, time.Date(2026, time.June, 17, 14, 30, 0, 0, time.UTC))
	repoErr := errors.New("database unavailable")
	repo := &stubPaymentStateRepository{getErr: repoErr}
	anomalies := &stubAnomalyRecorder{}

	processorMetrics := appmetrics.New()
	processor := NewPaymentProcessor(repo, anomalies, processorMetrics)
	result, err := processor.Process(ctx, PaymentUpdate{Event: event, WebhookEventID: 108})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repository error %v, got %v", repoErr, err)
	}

	if result.Status != PaymentProcessingStatusFailed {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusFailed, result.Status)
	}

	if repo.upsertCalls != 0 {
		t.Fatalf("expected repository failure to skip upsert, got %d calls", repo.upsertCalls)
	}

	if anomalies.calls != 0 {
		t.Fatalf("expected repository failure to skip anomaly recording, got %d calls", anomalies.calls)
	}

	assertProcessorHistogramCount(t, processorMetrics, "payment_webhook_processing_duration_seconds", map[string]string{"status": "failed"}, 1)
}

func TestPaymentProcessorProcessRecordsSuspiciousTransitionWithoutChangingUpdateOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	current := repository.Payment{
		PaymentID:       "pay_407",
		Status:          webhook.PaymentStatusFailed,
		StatusTimestamp: time.Date(2026, time.June, 17, 15, 30, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_407", current.PaymentID, webhook.EventTypePaymentPaid, current.StatusTimestamp.Add(5*time.Minute))
	repo := &stubPaymentStateRepository{payment: current}
	anomalies := &stubAnomalyRecorder{}

	processorMetrics := appmetrics.New()
	processor := NewPaymentProcessor(repo, anomalies, processorMetrics)
	result, err := processor.Process(ctx, PaymentUpdate{Event: event, WebhookEventID: 109})
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusUpdated {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusUpdated, result.Status)
	}

	if len(anomalies.records) != 1 || anomalies.records[0].AnomalyType != repository.AnomalyTypePaidAfterFailed {
		t.Fatalf("expected paid-after-failed anomaly, got %+v", anomalies.records)
	}

	assertProcessorCounterValue(t, processorMetrics, "payment_webhook_anomalies_total", map[string]string{"anomaly_type": string(repository.AnomalyTypePaidAfterFailed)}, 1)
	assertProcessorHistogramCount(t, processorMetrics, "payment_webhook_processing_duration_seconds", map[string]string{"status": "updated"}, 1)
}

func TestPaymentProcessorProcessContinuesWhenAnomalyRecordingFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	current := repository.Payment{
		PaymentID:       "pay_408",
		Status:          webhook.PaymentStatusPaid,
		StatusTimestamp: time.Date(2026, time.June, 17, 16, 30, 0, 0, time.UTC),
	}
	event := paymentProcessorEvent("evt_408", current.PaymentID, webhook.EventTypePaymentFailed, current.StatusTimestamp.Add(-5*time.Minute))
	repo := &stubPaymentStateRepository{payment: current}
	anomalies := &stubAnomalyRecorder{err: errors.New("anomaly insert failed")}

	processor := NewPaymentProcessor(repo, anomalies, nil)
	result, err := processor.Process(ctx, PaymentUpdate{Event: event, WebhookEventID: 110})
	if err != nil {
		t.Fatalf("expected anomaly failure to be swallowed, got %v", err)
	}

	if result.Status != PaymentProcessingStatusIgnored {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusIgnored, result.Status)
	}
}

type stubPaymentStateRepository struct {
	payment      repository.Payment
	getErr       error
	upsertErr    error
	upsertCalls  int
	lastUpserted repository.Payment
	requestedIDs []string
}

func (s *stubPaymentStateRepository) GetByPaymentID(ctx context.Context, paymentID string) (repository.Payment, error) {
	s.requestedIDs = append(s.requestedIDs, paymentID)
	if s.getErr != nil {
		return repository.Payment{}, s.getErr
	}

	return s.payment, nil
}

func (s *stubPaymentStateRepository) Upsert(ctx context.Context, payment repository.Payment) error {
	s.upsertCalls++
	s.lastUpserted = payment
	return s.upsertErr
}

type stubAnomalyRecorder struct {
	calls   int
	err     error
	records []repository.Anomaly
}

func (s *stubAnomalyRecorder) Record(ctx context.Context, anomaly repository.Anomaly) error {
	s.calls++
	s.records = append(s.records, anomaly)
	return s.err
}

func paymentProcessorEvent(providerEventID, paymentID string, eventType webhook.EventType, timestamp time.Time) webhook.PaymentEvent {
	status, err := paymentStatusFromEventType(eventType)
	if err != nil {
		panic(err)
	}

	return webhook.PaymentEvent{
		ProviderEventID: providerEventID,
		PaymentID:       paymentID,
		EventType:       eventType,
		PaymentStatus:   status,
		EventTimestamp:  timestamp,
		RawPayload:      []byte(`{"provider_event_id":"` + providerEventID + `","payment_id":"` + paymentID + `"}`),
	}
}

func paymentStatusFromEventType(eventType webhook.EventType) (webhook.PaymentStatus, error) {
	switch eventType {
	case webhook.EventTypePaymentPending:
		return webhook.PaymentStatusPending, nil
	case webhook.EventTypePaymentPaid:
		return webhook.PaymentStatusPaid, nil
	case webhook.EventTypePaymentFailed:
		return webhook.PaymentStatusFailed, nil
	case webhook.EventTypePaymentExpired:
		return webhook.PaymentStatusExpired, nil
	default:
		return "", errors.New("unsupported event type")
	}
}

func assertPaymentState(t *testing.T, actual, expected repository.Payment) {
	t.Helper()

	if actual.PaymentID != expected.PaymentID {
		t.Fatalf("expected payment id %q, got %q", expected.PaymentID, actual.PaymentID)
	}

	if actual.Status != expected.Status {
		t.Fatalf("expected payment status %q, got %q", expected.Status, actual.Status)
	}

	if !actual.StatusTimestamp.Equal(expected.StatusTimestamp) {
		t.Fatalf("expected payment timestamp %s, got %s", expected.StatusTimestamp, actual.StatusTimestamp)
	}
}

func assertProcessorCounterValue(t *testing.T, processorMetrics *appmetrics.Metrics, metricName string, expectedLabels map[string]string, expectedValue float64) {
	t.Helper()

	metric := findProcessorMetric(t, processorMetrics, metricName, expectedLabels)
	if got := metric.GetCounter().GetValue(); got != expectedValue {
		t.Fatalf("expected counter %s with labels %v to be %v, got %v", metricName, expectedLabels, expectedValue, got)
	}
}

func assertProcessorHistogramCount(t *testing.T, processorMetrics *appmetrics.Metrics, metricName string, expectedLabels map[string]string, expectedCount uint64) {
	t.Helper()

	metric := findProcessorMetric(t, processorMetrics, metricName, expectedLabels)
	if got := metric.GetHistogram().GetSampleCount(); got != expectedCount {
		t.Fatalf("expected histogram %s with labels %v count %d, got %d", metricName, expectedLabels, expectedCount, got)
	}
}

func findProcessorMetric(t *testing.T, processorMetrics *appmetrics.Metrics, metricName string, expectedLabels map[string]string) *dto.Metric {
	t.Helper()

	metricFamilies, err := processorMetrics.Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != metricName {
			continue
		}

		for _, metric := range metricFamily.GetMetric() {
			if processorLabelsMatch(metric.GetLabel(), expectedLabels) {
				return metric
			}
		}
	}

	t.Fatalf("metric %s with labels %v not found", metricName, expectedLabels)
	return nil
}

func processorLabelsMatch(metricLabels []*dto.LabelPair, expectedLabels map[string]string) bool {
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
