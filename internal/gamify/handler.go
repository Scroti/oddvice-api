package gamify

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/oddvice/api/internal/httpx"
)

// Handler serves the prediction-game endpoints.
type Handler struct {
	store *Store
}

// NewHandler constructs a Handler. A nil store makes write/read endpoints return
// 503 (feature off) while still registering the routes.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// Register mounts the gamify routes.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/predict", h.predict)
	mux.HandleFunc("GET /api/v1/predictions", h.predictions)
	mux.HandleFunc("GET /api/v1/poll/{matchId}", h.poll)
	mux.HandleFunc("GET /api/v1/leaderboard", h.leaderboard)
}

func deviceID(r *http.Request) string {
	d := r.Header.Get("X-Device-Id")
	if d == "" {
		d = r.URL.Query().Get("device")
	}
	return strings.TrimSpace(d)
}

func validPick(p string) bool {
	return p == "home" || p == "draw" || p == "away"
}

func (h *Handler) predict(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "predictions not configured")
		return
	}
	dev := deviceID(r)
	if dev == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing device id")
		return
	}
	var body struct {
		MatchID string `json:"matchId"`
		Pick    string `json:"pick"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.MatchID = strings.TrimSpace(body.MatchID)
	if body.MatchID == "" || !validPick(body.Pick) {
		httpx.WriteError(w, http.StatusBadRequest, "matchId and a valid pick (home|draw|away) are required")
		return
	}
	name := strings.TrimSpace(body.Name)
	if len(name) > 40 {
		name = name[:40]
	}
	if err := h.store.Upsert(r.Context(), dev, name, body.MatchID, body.Pick); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "could not save prediction")
		return
	}
	// Return the fresh poll so the client can show the crowd split immediately.
	poll, _ := h.store.Poll(r.Context(), body.MatchID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "pick": body.Pick, "poll": poll})
}

func (h *Handler) predictions(w http.ResponseWriter, r *http.Request) {
	dev := deviceID(r)
	if dev == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing device id")
		return
	}
	preds, points, streak, err := h.store.ByDevice(r.Context(), dev)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "could not load predictions")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"points":      points,
		"streak":      streak,
		"predictions": preds,
	})
}

func (h *Handler) poll(w http.ResponseWriter, r *http.Request) {
	matchID := strings.TrimSpace(r.PathValue("matchId"))
	if matchID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing match id")
		return
	}
	poll, err := h.store.Poll(r.Context(), matchID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "could not load poll")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, poll)
}

func (h *Handler) leaderboard(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	rows, err := h.store.Leaderboard(r.Context(), limit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "could not load leaderboard")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"count": len(rows), "leaders": rows})
}
