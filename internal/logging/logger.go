package logging

import (
	"context"
	"io"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

func NewJSONLogger(writer io.Writer) *zerolog.Logger {
	logger := zerolog.New(writer).With().Timestamp().Logger()
	return &logger
}

func WithTraceContext(ctx context.Context, event *zerolog.Event) *zerolog.Event {
	if event == nil {
		return event
	}

	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return event
	}

	return event.Str("trace_id", spanContext.TraceID().String()).Str("span_id", spanContext.SpanID().String())
}
