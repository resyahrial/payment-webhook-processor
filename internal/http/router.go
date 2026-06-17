package http

import (
	"encoding/json"
	stdhttp "net/http"
)

type healthResponse struct {
	Status string `json:"status"`
}

func NewRouter(webhookHandler stdhttp.Handler) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	if webhookHandler != nil {
		mux.Handle("/webhooks/payment", webhookHandler)
	}
	return withRequestID(mux)
}

func healthzHandler(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}
