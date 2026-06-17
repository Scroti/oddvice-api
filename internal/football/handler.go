package football

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/oddvice/api/internal/httpx"
)

// Handler exposes the football feature over HTTP.
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
	mux.HandleFunc("GET /api/v1/football/matches/search", h.searchMatches)
}

type searchResponse struct {
	Query   string  `json:"query"`
	Count   int     `json:"count"`
	Matches []Match `json:"matches"`
}

// searchMatches handles GET /api/v1/football/matches/search?q=...
func (h *Handler) searchMatches(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	matches, err := h.svc.SearchMatches(r.Context(), query)
	switch {
	case errors.Is(err, ErrEmptyQuery):
		httpx.WriteError(w, http.StatusBadRequest, "missing required query parameter 'q'")
		return
	case err != nil:
		h.logger.Error("football search failed", "query", query, "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "football provider request failed")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, searchResponse{
		Query:   query,
		Count:   len(matches),
		Matches: matches,
	})
}
