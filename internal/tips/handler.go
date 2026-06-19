package tips

import (
	"log/slog"
	"net/http"

	"github.com/oddvice/api/internal/httpx"
)

// Handler exposes betting tips over HTTP.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

// NewHandler builds a Handler for the given Service.
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Register attaches the feature's routes to the mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tips", h.list)
	mux.HandleFunc("GET /api/v1/tips/{matchId}", h.forMatch)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	bundles, err := h.svc.List(r.Context(), 20)
	if err != nil {
		h.logger.Error("tips list failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "could not load tips")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"count": len(bundles),
		"tips":  bundles,
	})
}

func (h *Handler) forMatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("matchId")
	bundle, found, err := h.svc.ForMatch(r.Context(), id)
	if err != nil {
		h.logger.Error("tips for match failed", "matchId", id, "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "could not load tips")
		return
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "no tips for this match")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, bundle)
}
