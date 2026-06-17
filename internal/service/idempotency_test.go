package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"payment-webhook-processor/internal/repository"
	"payment-webhook-processor/internal/webhook"
)

func TestIdempotencyServiceProcessStoresNewEventAndUpdatesPaymentState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := testPaymentEvent("evt_301", "pay_301", webhook.EventTypePaymentPending)
	repo := &stubWebhookEventRepository{}
	updater := &stubPaymentUpdater{}

	svc := NewIdempotencyService(repo, updater.Update)
	result, err := svc.Process(ctx, event)
	if err != nil {
		t.Fatalf("process event: %v", err)
	}

	if result.Status != ResultStatusProcessed {
		t.Fatalf("expected result status %q, got %q", ResultStatusProcessed, result.Status)
	}

	if repo.insertCalls != 1 {
		t.Fatalf("expected 1 insert call, got %d", repo.insertCalls)
	}

	if updater.calls != 1 {
		t.Fatalf("expected 1 payment update call, got %d", updater.calls)
	}

	if updater.updates[0].Event.ProviderEventID != event.ProviderEventID {
		t.Fatalf("expected updater to receive provider event id %q, got %q", event.ProviderEventID, updater.updates[0].Event.ProviderEventID)
	}

	if updater.updates[0].WebhookEventID != 501 {
		t.Fatalf("expected updater to receive webhook event id %d, got %d", 501, updater.updates[0].WebhookEventID)
	}
}

func TestIdempotencyServiceProcessTreatsDuplicateEventAsSuccessfulNoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := testPaymentEvent("evt_302", "pay_302", webhook.EventTypePaymentPaid)
	repo := &stubWebhookEventRepository{insertErr: repository.ErrDuplicateProviderEventID}
	updater := &stubPaymentUpdater{}

	svc := NewIdempotencyService(repo, updater.Update)
	result, err := svc.Process(ctx, event)
	if err != nil {
		t.Fatalf("process duplicate event: %v", err)
	}

	if result.Status != ResultStatusDuplicate {
		t.Fatalf("expected result status %q, got %q", ResultStatusDuplicate, result.Status)
	}

	if updater.calls != 0 {
		t.Fatalf("expected duplicate event to skip payment update, got %d calls", updater.calls)
	}
}

func TestIdempotencyServiceProcessDoesNotTreatDifferentProviderEventIDAsDuplicate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &stubWebhookEventRepository{insertedID: 502}
	updater := &stubPaymentUpdater{}
	svc := NewIdempotencyService(repo, updater.Update)

	firstEvent := testPaymentEvent("evt_303_a", "pay_303", webhook.EventTypePaymentPending)
	secondEvent := testPaymentEvent("evt_303_b", "pay_303", webhook.EventTypePaymentPaid)

	firstResult, err := svc.Process(ctx, firstEvent)
	if err != nil {
		t.Fatalf("process first event: %v", err)
	}

	secondResult, err := svc.Process(ctx, secondEvent)
	if err != nil {
		t.Fatalf("process second event: %v", err)
	}

	if firstResult.Status != ResultStatusProcessed {
		t.Fatalf("expected first result status %q, got %q", ResultStatusProcessed, firstResult.Status)
	}

	if secondResult.Status != ResultStatusProcessed {
		t.Fatalf("expected second result status %q, got %q", ResultStatusProcessed, secondResult.Status)
	}

	if updater.calls != 2 {
		t.Fatalf("expected 2 payment update calls, got %d", updater.calls)
	}
}

func TestIdempotencyServiceProcessReturnsRepositoryErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := testPaymentEvent("evt_304", "pay_304", webhook.EventTypePaymentFailed)
	repoErr := errors.New("database unavailable")
	repo := &stubWebhookEventRepository{insertErr: repoErr}
	updater := &stubPaymentUpdater{}

	svc := NewIdempotencyService(repo, updater.Update)
	_, err := svc.Process(ctx, event)
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repository error %v, got %v", repoErr, err)
	}

	if updater.calls != 0 {
		t.Fatalf("expected repository error to skip payment update, got %d calls", updater.calls)
	}
}

func TestIdempotencyServiceProcessReturnsPaymentUpdateErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	event := testPaymentEvent("evt_305", "pay_305", webhook.EventTypePaymentExpired)
	updateErr := errors.New("payment update failed")
	repo := &stubWebhookEventRepository{insertedID: 503}
	updater := &stubPaymentUpdater{err: updateErr}

	svc := NewIdempotencyService(repo, updater.Update)
	_, err := svc.Process(ctx, event)
	if !errors.Is(err, updateErr) {
		t.Fatalf("expected payment update error %v, got %v", updateErr, err)
	}

	if updater.calls != 1 {
		t.Fatalf("expected 1 payment update call, got %d", updater.calls)
	}
}

type stubWebhookEventRepository struct {
	insertCalls int
	insertedID  int64
	insertErr   error
	events      []webhook.PaymentEvent
}

func (s *stubWebhookEventRepository) Insert(ctx context.Context, event webhook.PaymentEvent) (int64, error) {
	s.insertCalls++
	s.events = append(s.events, event)
	if s.insertErr != nil {
		return 0, s.insertErr
	}

	if s.insertedID == 0 {
		s.insertedID = 501
	}

	return s.insertedID, nil
}

type stubPaymentUpdater struct {
	calls   int
	err     error
	updates []PaymentUpdate
}

func (s *stubPaymentUpdater) Update(ctx context.Context, update PaymentUpdate) error {
	s.calls++
	s.updates = append(s.updates, update)
	return s.err
}

func testPaymentEvent(providerEventID, paymentID string, eventType webhook.EventType) webhook.PaymentEvent {
	return webhook.PaymentEvent{
		ProviderEventID: providerEventID,
		PaymentID:       paymentID,
		EventType:       eventType,
		PaymentStatus:   webhook.PaymentStatusPending,
		EventTimestamp:  time.Date(2026, time.June, 17, 15, 30, 0, 0, time.UTC),
		RawPayload:      []byte(`{"provider_event_id":"` + providerEventID + `","payment_id":"` + paymentID + `"}`),
	}
}
