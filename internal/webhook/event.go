package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type EventType string

const (
	EventTypePaymentPending           EventType = "payment.pending"
	EventTypePaymentAuthorized        EventType = "payment.authorized"
	EventTypePaymentPaid              EventType = "payment.paid"
	EventTypePaymentFailed            EventType = "payment.failed"
	EventTypePaymentExpired           EventType = "payment.expired"
	EventTypePaymentCancelled         EventType = "payment.cancelled"
	EventTypePaymentPartiallyRefunded EventType = "payment.partially_refunded"
	EventTypePaymentRefunded          EventType = "payment.refunded"
	EventTypePaymentDisputed          EventType = "payment.disputed"
	EventTypePaymentChargeback        EventType = "payment.chargeback"
)

type PaymentStatus string

const (
	PaymentStatusPending           PaymentStatus = "pending"
	PaymentStatusAuthorized        PaymentStatus = "authorized"
	PaymentStatusPaid              PaymentStatus = "paid"
	PaymentStatusFailed            PaymentStatus = "failed"
	PaymentStatusExpired           PaymentStatus = "expired"
	PaymentStatusCancelled         PaymentStatus = "cancelled"
	PaymentStatusPartiallyRefunded PaymentStatus = "partially_refunded"
	PaymentStatusRefunded          PaymentStatus = "refunded"
	PaymentStatusDisputed          PaymentStatus = "disputed"
	PaymentStatusChargeback        PaymentStatus = "chargeback"
)

var (
	ErrInvalidJSON            = errors.New("invalid json")
	ErrMissingProviderEventID = errors.New("missing provider event id")
	ErrMissingPaymentID       = errors.New("missing payment id")
	ErrMissingEventType       = errors.New("missing event type")
	ErrMissingEventTimestamp  = errors.New("missing event timestamp")
	ErrUnknownEventType       = errors.New("unknown event type")
	ErrInvalidEventTimestamp  = errors.New("invalid event timestamp")
)

type PaymentEvent struct {
	ProviderEventID string
	PaymentID       string
	EventType       EventType
	PaymentStatus   PaymentStatus
	EventTimestamp  time.Time
	RawPayload      []byte
}

type paymentEventPayload struct {
	ProviderEventID string `json:"provider_event_id"`
	PaymentID       string `json:"payment_id"`
	EventType       string `json:"event_type"`
	EventTimestamp  string `json:"event_timestamp"`
}

func ParseEvent(rawPayload []byte) (PaymentEvent, error) {
	var payload paymentEventPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return PaymentEvent{}, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}

	providerEventID := strings.TrimSpace(payload.ProviderEventID)
	if providerEventID == "" {
		return PaymentEvent{}, ErrMissingProviderEventID
	}

	paymentID := strings.TrimSpace(payload.PaymentID)
	if paymentID == "" {
		return PaymentEvent{}, ErrMissingPaymentID
	}

	eventType := EventType(strings.TrimSpace(payload.EventType))
	if eventType == "" {
		return PaymentEvent{}, ErrMissingEventType
	}

	paymentStatus, err := mapEventTypeToStatus(eventType)
	if err != nil {
		return PaymentEvent{}, err
	}

	eventTimestampRaw := strings.TrimSpace(payload.EventTimestamp)
	if eventTimestampRaw == "" {
		return PaymentEvent{}, ErrMissingEventTimestamp
	}

	eventTimestamp, err := time.Parse(time.RFC3339, eventTimestampRaw)
	if err != nil {
		return PaymentEvent{}, fmt.Errorf("%w: %v", ErrInvalidEventTimestamp, err)
	}

	return PaymentEvent{
		ProviderEventID: providerEventID,
		PaymentID:       paymentID,
		EventType:       eventType,
		PaymentStatus:   paymentStatus,
		EventTimestamp:  eventTimestamp,
		RawPayload:      append([]byte(nil), rawPayload...),
	}, nil
}

func mapEventTypeToStatus(eventType EventType) (PaymentStatus, error) {
	switch eventType {
	case EventTypePaymentPending:
		return PaymentStatusPending, nil
	case EventTypePaymentAuthorized:
		return PaymentStatusAuthorized, nil
	case EventTypePaymentPaid:
		return PaymentStatusPaid, nil
	case EventTypePaymentFailed:
		return PaymentStatusFailed, nil
	case EventTypePaymentExpired:
		return PaymentStatusExpired, nil
	case EventTypePaymentCancelled:
		return PaymentStatusCancelled, nil
	case EventTypePaymentPartiallyRefunded:
		return PaymentStatusPartiallyRefunded, nil
	case EventTypePaymentRefunded:
		return PaymentStatusRefunded, nil
	case EventTypePaymentDisputed:
		return PaymentStatusDisputed, nil
	case EventTypePaymentChargeback:
		return PaymentStatusChargeback, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownEventType, eventType)
	}
}
