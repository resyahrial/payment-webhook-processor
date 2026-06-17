package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const requestIDHeader = "X-Request-ID"

type contextKey string

const requestIDContextKey contextKey = "request_id"

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = newRequestID()
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey, requestID)))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey).(string)
	return requestID
}

func newRequestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}

	return hex.EncodeToString(buffer)
}
