package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/webhook"
)

func TestPaymentProcessorProcessCreatesPaymentForFirstEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := paymentProcessorEvent("evt_401", "pay_401", webhook.EventTypePaymentPending, time.Date(2026, time.June, 17, 10, 30, 0, 0, time.UTC))
	repo := &stubPaymentStateRepository{getErr: repository.ErrPaymentNotFound}

	processor := NewPaymentProcessor(repo)
	result, err := processor.Process(ctx, event)
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusCreated {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusCreated, result.Status)
	}

	if repo.upsertCalls != 1 {
		t.Fatalf("expected 1 upsert call, got %d", repo.upsertCalls)
	}

	assertPaymentState(t, repo.lastUpserted, repository.Payment{
		PaymentID:       event.PaymentID,
		Status:          event.PaymentStatus,
		StatusTimestamp: event.EventTimestamp,
	})
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

	processor := NewPaymentProcessor(repo)
	result, err := processor.Process(ctx, event)
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusUpdated {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusUpdated, result.Status)
	}

	if repo.upsertCalls != 1 {
		t.Fatalf("expected 1 upsert call, got %d", repo.upsertCalls)
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

	processor := NewPaymentProcessor(repo)
	result, err := processor.Process(ctx, event)
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusIgnored {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusIgnored, result.Status)
	}

	if repo.upsertCalls != 0 {
		t.Fatalf("expected older event to skip upsert, got %d calls", repo.upsertCalls)
	}
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

	processor := NewPaymentProcessor(repo)
	result, err := processor.Process(ctx, event)
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != PaymentProcessingStatusIgnored {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusIgnored, result.Status)
	}

	if repo.upsertCalls != 0 {
		t.Fatalf("expected equal timestamp event to skip upsert, got %d calls", repo.upsertCalls)
	}
}

func TestPaymentProcessorProcessHandlesMultipleValidStatusChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	initialTimestamp := time.Date(2026, time.June, 17, 13, 30, 0, 0, time.UTC)
	repo := &stubPaymentStateRepository{getErr: repository.ErrPaymentNotFound}
	processor := NewPaymentProcessor(repo)

	firstEvent := paymentProcessorEvent("evt_405_a", "pay_405", webhook.EventTypePaymentPending, initialTimestamp)
	secondEvent := paymentProcessorEvent("evt_405_b", "pay_405", webhook.EventTypePaymentPaid, initialTimestamp.Add(5*time.Minute))
	thirdEvent := paymentProcessorEvent("evt_405_c", "pay_405", webhook.EventTypePaymentExpired, initialTimestamp.Add(10*time.Minute))

	firstResult, err := processor.Process(ctx, firstEvent)
	if err != nil {
		t.Fatalf("process first event: %v", err)
	}

	repo.getErr = nil
	repo.payment = repo.lastUpserted

	secondResult, err := processor.Process(ctx, secondEvent)
	if err != nil {
		t.Fatalf("process second event: %v", err)
	}

	repo.payment = repo.lastUpserted

	thirdResult, err := processor.Process(ctx, thirdEvent)
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

	processor := NewPaymentProcessor(repo)
	result, err := processor.Process(ctx, event)
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repository error %v, got %v", repoErr, err)
	}

	if result.Status != PaymentProcessingStatusFailed {
		t.Fatalf("expected result status %q, got %q", PaymentProcessingStatusFailed, result.Status)
	}

	if repo.upsertCalls != 0 {
		t.Fatalf("expected repository failure to skip upsert, got %d calls", repo.upsertCalls)
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
