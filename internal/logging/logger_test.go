package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestWithTraceContextAddsTraceFieldsForValidSpanContext(t *testing.T) {
	t.Parallel()

	logs := &bytes.Buffer{}
	logger := NewJSONLogger(logs)
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	WithTraceContext(ctx, logger.Info()).Msg("request completed")

	entry := decodeSingleLogEntry(t, logs)
	if entry["trace_id"] != spanContext.TraceID().String() {
		t.Fatalf("expected trace_id %q, got %v", spanContext.TraceID().String(), entry["trace_id"])
	}
	if entry["span_id"] != spanContext.SpanID().String() {
		t.Fatalf("expected span_id %q, got %v", spanContext.SpanID().String(), entry["span_id"])
	}
}

func TestWithTraceContextSkipsInvalidSpanContext(t *testing.T) {
	t.Parallel()

	logs := &bytes.Buffer{}
	logger := NewJSONLogger(logs)

	WithTraceContext(context.Background(), logger.Info()).Msg("request completed")

	entry := decodeSingleLogEntry(t, logs)
	if _, ok := entry["trace_id"]; ok {
		t.Fatalf("expected trace_id to be omitted, got %v", entry["trace_id"])
	}
	if _, ok := entry["span_id"]; ok {
		t.Fatalf("expected span_id to be omitted, got %v", entry["span_id"])
	}
}

func decodeSingleLogEntry(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()

	decoder := json.NewDecoder(logs)
	var entry map[string]any
	if err := decoder.Decode(&entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}

	return entry
}
