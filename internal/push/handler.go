package push

import (
	"encoding/json"
	"net/http"

	"github.com/oddvice/api/internal/httpx"
	webpush "github.com/SherClockHolmes/webpush-go"
)

// Handler serves the Web Push subscription management endpoints.
type Handler struct {
	store  *Store
	sender *Sender
	public string
}

// NewHandler constructs a Handler. public is the VAPID public key; when empty
// the push feature is unconfigured and /subscribe will return 503.
func NewHandler(store *Store, sender *Sender, public string) *Handler {
	return &Handler{store: store, sender: sender, public: public}
}

// Register mounts the push routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/push/subscribe", h.subscribe)
	mux.HandleFunc("POST /api/v1/push/unsubscribe", h.unsubscribe)
	mux.HandleFunc("GET /api/v1/push/public-key", h.publicKey)
}

func (h *Handler) subscribe(w http.ResponseWriter, r *http.Request) {
	if h.public == "" {
		httpx.WriteError(w, http.StatusServiceUnavailable, "push notifications not configured")
		return
	}
	var sub webpush.Subscription
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid subscription body")
		return
	}
	if sub.Endpoint == "" {
		httpx.WriteError(w, http.StatusBadRequest, "endpoint is required")
		return
	}
	h.store.Add(sub)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unsubscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Endpoint != "" {
		h.store.Remove(body.Endpoint)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) publicKey(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"publicKey": h.public})
}
