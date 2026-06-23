package webhook

import (
	"errors"
	"testing"
	"time"
)

func TestParseEventValidPending(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_123","payment_id":"pay_123","event_type":"payment.pending","event_timestamp":"2026-06-17T10:30:00Z"}`)

	event, err := ParseEvent(body)
	if err != nil {
		t.Fatalf("expected valid event, got error %v", err)
	}

	assertEvent(t, event, "evt_123", "pay_123", EventTypePaymentPending, PaymentStatusPending, "2026-06-17T10:30:00Z", body)
}

func TestParseEventValidPaid(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_124","payment_id":"pay_124","event_type":"payment.paid","event_timestamp":"2026-06-17T11:30:00Z"}`)

	event, err := ParseEvent(body)
	if err != nil {
		t.Fatalf("expected valid event, got error %v", err)
	}

	assertEvent(t, event, "evt_124", "pay_124", EventTypePaymentPaid, PaymentStatusPaid, "2026-06-17T11:30:00Z", body)
}

func TestParseEventValidFailed(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_125","payment_id":"pay_125","event_type":"payment.failed","event_timestamp":"2026-06-17T12:30:00Z"}`)

	event, err := ParseEvent(body)
	if err != nil {
		t.Fatalf("expected valid event, got error %v", err)
	}

	assertEvent(t, event, "evt_125", "pay_125", EventTypePaymentFailed, PaymentStatusFailed, "2026-06-17T12:30:00Z", body)
}

func TestParseEventValidExpired(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_126","payment_id":"pay_126","event_type":"payment.expired","event_timestamp":"2026-06-17T13:30:00Z"}`)

	event, err := ParseEvent(body)
	if err != nil {
		t.Fatalf("expected valid event, got error %v", err)
	}

	assertEvent(t, event, "evt_126", "pay_126", EventTypePaymentExpired, PaymentStatusExpired, "2026-06-17T13:30:00Z", body)
}

func TestParseEventValidAdditionalPaymentStatuses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		providerEvent string
		paymentID     string
		eventType     EventType
		paymentStatus PaymentStatus
		timestamp     string
	}{
		{name: "authorized", providerEvent: "evt_126_a", paymentID: "pay_126_a", eventType: EventTypePaymentAuthorized, paymentStatus: PaymentStatusAuthorized, timestamp: "2026-06-17T13:35:00Z"},
		{name: "cancelled", providerEvent: "evt_126_b", paymentID: "pay_126_b", eventType: EventTypePaymentCancelled, paymentStatus: PaymentStatusCancelled, timestamp: "2026-06-17T13:40:00Z"},
		{name: "partially refunded", providerEvent: "evt_126_c", paymentID: "pay_126_c", eventType: EventTypePaymentPartiallyRefunded, paymentStatus: PaymentStatusPartiallyRefunded, timestamp: "2026-06-17T13:45:00Z"},
		{name: "refunded", providerEvent: "evt_126_d", paymentID: "pay_126_d", eventType: EventTypePaymentRefunded, paymentStatus: PaymentStatusRefunded, timestamp: "2026-06-17T13:50:00Z"},
		{name: "disputed", providerEvent: "evt_126_e", paymentID: "pay_126_e", eventType: EventTypePaymentDisputed, paymentStatus: PaymentStatusDisputed, timestamp: "2026-06-17T13:55:00Z"},
		{name: "chargeback", providerEvent: "evt_126_f", paymentID: "pay_126_f", eventType: EventTypePaymentChargeback, paymentStatus: PaymentStatusChargeback, timestamp: "2026-06-17T14:00:00Z"},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			body := []byte(`{"provider_event_id":"` + testCase.providerEvent + `","payment_id":"` + testCase.paymentID + `","event_type":"` + string(testCase.eventType) + `","event_timestamp":"` + testCase.timestamp + `"}`)

			event, err := ParseEvent(body)
			if err != nil {
				t.Fatalf("expected valid event, got error %v", err)
			}

			assertEvent(t, event, testCase.providerEvent, testCase.paymentID, testCase.eventType, testCase.paymentStatus, testCase.timestamp, body)
		})
	}
}

func TestParseEventRejectsUnknownEventType(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_127","payment_id":"pay_127","event_type":"payment.unknown","event_timestamp":"2026-06-17T14:30:00Z"}`)

	_, err := ParseEvent(body)
	if !errors.Is(err, ErrUnknownEventType) {
		t.Fatalf("expected ErrUnknownEventType, got %v", err)
	}
}

func TestParseEventRejectsMissingProviderEventID(t *testing.T) {
	t.Parallel()

	body := []byte(`{"payment_id":"pay_128","event_type":"payment.pending","event_timestamp":"2026-06-17T15:30:00Z"}`)

	_, err := ParseEvent(body)
	if !errors.Is(err, ErrMissingProviderEventID) {
		t.Fatalf("expected ErrMissingProviderEventID, got %v", err)
	}
}

func TestParseEventRejectsMissingPaymentID(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_129","event_type":"payment.pending","event_timestamp":"2026-06-17T16:30:00Z"}`)

	_, err := ParseEvent(body)
	if !errors.Is(err, ErrMissingPaymentID) {
		t.Fatalf("expected ErrMissingPaymentID, got %v", err)
	}
}

func TestParseEventRejectsMissingTimestamp(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_130","payment_id":"pay_130","event_type":"payment.pending"}`)

	_, err := ParseEvent(body)
	if !errors.Is(err, ErrMissingEventTimestamp) {
		t.Fatalf("expected ErrMissingEventTimestamp, got %v", err)
	}
}

func TestParseEventRejectsInvalidTimestamp(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_131","payment_id":"pay_131","event_type":"payment.pending","event_timestamp":"not-a-time"}`)

	_, err := ParseEvent(body)
	if !errors.Is(err, ErrInvalidEventTimestamp) {
		t.Fatalf("expected ErrInvalidEventTimestamp, got %v", err)
	}
}

func TestParseEventRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	body := []byte(`{"provider_event_id":"evt_132"`)

	_, err := ParseEvent(body)
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func assertEvent(t *testing.T, event PaymentEvent, providerEventID, paymentID string, eventType EventType, paymentStatus PaymentStatus, timestamp string, rawPayload []byte) {
	t.Helper()

	expectedTimestamp, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatalf("expected valid timestamp fixture, got %v", err)
	}

	if event.ProviderEventID != providerEventID {
		t.Fatalf("expected provider event id %q, got %q", providerEventID, event.ProviderEventID)
	}

	if event.PaymentID != paymentID {
		t.Fatalf("expected payment id %q, got %q", paymentID, event.PaymentID)
	}

	if event.EventType != eventType {
		t.Fatalf("expected event type %q, got %q", eventType, event.EventType)
	}

	if event.PaymentStatus != paymentStatus {
		t.Fatalf("expected payment status %q, got %q", paymentStatus, event.PaymentStatus)
	}

	if !event.EventTimestamp.Equal(expectedTimestamp) {
		t.Fatalf("expected event timestamp %s, got %s", expectedTimestamp, event.EventTimestamp)
	}

	if string(event.RawPayload) != string(rawPayload) {
		t.Fatalf("expected raw payload %q, got %q", string(rawPayload), string(event.RawPayload))
	}
}
