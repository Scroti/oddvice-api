package football

import (
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

// Register attaches the feature's routes to the mux. Literal segments
// (search/upcoming/results) take precedence over the {id} wildcard.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/football/matches", h.list)
	mux.HandleFunc("GET /api/v1/football/matches/search", h.search)
	mux.HandleFunc("GET /api/v1/football/matches/upcoming", h.upcoming)
	mux.HandleFunc("GET /api/v1/football/matches/results", h.results)
	mux.HandleFunc("GET /api/v1/football/matches/{id}", h.getMatch)
}

type listResponse struct {
	Count   int     `json:"count"`
	Matches []Match `json:"matches"`
}

func (h *Handler) respondList(w http.ResponseWriter, matches []Match, err error, ctx string) {
	if err != nil {
		h.logger.Error("football "+ctx+" failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "football provider request failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse{Count: len(matches), Matches: matches})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	matches, err := h.svc.All(r.Context())
	h.respondList(w, matches, err, "list")
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	matches, err := h.svc.Search(r.Context(), r.URL.Query().Get("q"))
	h.respondList(w, matches, err, "search")
}

func (h *Handler) upcoming(w http.ResponseWriter, r *http.Request) {
	matches, err := h.svc.Upcoming(r.Context(), 20)
	h.respondList(w, matches, err, "upcoming")
}

func (h *Handler) results(w http.ResponseWriter, r *http.Request) {
	matches, err := h.svc.Results(r.Context(), 20)
	h.respondList(w, matches, err, "results")
}

func (h *Handler) getMatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	match, found, err := h.svc.GetMatch(r.Context(), id)
	if err != nil {
		h.logger.Error("get match failed", "id", id, "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "football provider request failed")
		return
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "match not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, match)
}
