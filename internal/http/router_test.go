package http

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestNewRouterServesHealthz(t *testing.T) {
	router := NewRouter(nil)
	req := httptest.NewRequest(stdhttp.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected content type application/json, got %q", contentType)
	}

	var payload map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected valid json response, got error %v", err)
	}

	if payload["status"] != "ok" {
		t.Fatalf("expected status payload ok, got %q", payload["status"])
	}
}

func TestNewRouterServesWebhookRoute(t *testing.T) {
	handlerCalled := false
	router := NewRouter(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		handlerCalled = true
		w.WriteHeader(stdhttp.StatusNoContent)
	}))
	req := httptest.NewRequest(stdhttp.MethodPost, "/webhooks/payment", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if !handlerCalled {
		t.Fatal("expected webhook handler to be called")
	}

	if recorder.Code != stdhttp.StatusNoContent {
		t.Fatalf("expected status 204, got %d", recorder.Code)
	}
}
