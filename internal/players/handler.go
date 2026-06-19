package players

import (
	"log/slog"
	"net/http"

	"github.com/oddvice/api/internal/httpx"
)

// Handler exposes player search over HTTP.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

// NewHandler builds a Handler for the given Service.
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Register attaches the player-search route.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/players/search", h.search)
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		h.logger.Error("player search failed", "error", err)
		list = nil // degrade gracefully — always 200
	}
	if list == nil {
		list = []Player{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"count":   len(list),
		"players": list,
	})
}
