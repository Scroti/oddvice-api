package highlights

import (
	"net/http"
	"strings"

	"github.com/oddvice/api/internal/httpx"
)

// Handler serves the highlights endpoint.
type Handler struct {
	client *Client
}

// NewHandler constructs a Handler.
func NewHandler(client *Client) *Handler {
	return &Handler{client: client}
}

// Register mounts the highlights route.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/highlights", h.search)
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	home := strings.TrimSpace(r.URL.Query().Get("home"))
	away := strings.TrimSpace(r.URL.Query().Get("away"))
	if home == "" || away == "" {
		httpx.WriteError(w, http.StatusBadRequest, "home and away are required")
		return
	}
	vids, err := h.client.Search(r.Context(), home, away)
	if err != nil {
		// degrade gracefully — empty list, client falls back to the search link
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"count": 0, "videos": []Video{}})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"count": len(vids), "videos": vids})
}
