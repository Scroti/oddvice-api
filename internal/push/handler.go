package push

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/oddvice/api/internal/httpx"
	webpush "github.com/SherClockHolmes/webpush-go"
)

// Handler serves the Web Push subscription management endpoints + native (Expo)
// token registration.
type Handler struct {
	store  *Store
	sender *Sender
	public string
	expo   *ExpoStore
}

// NewHandler constructs a Handler. public is the VAPID public key; when empty
// the Web Push feature is unconfigured and /subscribe will return 503. expo may
// be nil (native push registration then no-ops).
func NewHandler(store *Store, sender *Sender, public string, expo *ExpoStore) *Handler {
	return &Handler{store: store, sender: sender, public: public, expo: expo}
}

// Register mounts the push routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/push/subscribe", h.subscribe)
	mux.HandleFunc("POST /api/v1/push/unsubscribe", h.unsubscribe)
	mux.HandleFunc("GET /api/v1/push/public-key", h.publicKey)
	mux.HandleFunc("POST /api/v1/push/expo", h.registerExpo)
}

func (h *Handler) registerExpo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid body")
		return
	}
	token := strings.TrimSpace(body.Token)
	if !strings.HasPrefix(token, "ExponentPushToken") && !strings.HasPrefix(token, "ExpoPushToken") {
		httpx.WriteError(w, http.StatusBadRequest, "invalid expo push token")
		return
	}
	if err := h.expo.Add(r.Context(), token); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "could not save token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
