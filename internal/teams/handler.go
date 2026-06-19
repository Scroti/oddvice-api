package teams

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/oddvice/api/internal/httpx"
)

// Handler exposes team data over HTTP.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

// NewHandler builds a Handler for the given Service.
func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// Register attaches the feature's routes. The literal "by-name" segment takes
// precedence over the {id} wildcard.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/teams", h.list)
	mux.HandleFunc("GET /api/v1/teams/by-name", h.byName)
	mux.HandleFunc("GET /api/v1/teams/{id}", h.get)
	mux.HandleFunc("GET /api/v1/lineups", h.lineups)
	mux.HandleFunc("GET /api/v1/match-stats", h.matchStats)
	mux.HandleFunc("GET /api/v1/live", h.live)
	mux.HandleFunc("GET /api/v1/events", h.events)
}

func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	matches, err := h.svc.Live(r.Context())
	if err != nil {
		h.logger.Error("live failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "could not load live matches")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"count":   len(matches),
		"matches": matches,
	})
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var (
		events []Event
		err    error
	)
	if fid, e := strconv.Atoi(q.Get("fixture")); e == nil && fid > 0 {
		events, _, err = h.svc.EventsByFixture(r.Context(), fid)
	} else {
		events, _, err = h.svc.Events(r.Context(), q.Get("home"), q.Get("away"), q.Get("date"))
	}
	if err != nil {
		h.logger.Error("events failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "could not load match events")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"count":  len(events),
		"events": events,
	})
}

func (h *Handler) matchStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ms, _, err := h.svc.MatchStats(r.Context(), q.Get("home"), q.Get("away"), q.Get("date"))
	if err != nil {
		h.logger.Error("match-stats failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "could not load match stats")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ms) // empty lines when unavailable
}

func (h *Handler) lineups(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ml, _, err := h.svc.Lineups(r.Context(), q.Get("home"), q.Get("away"), q.Get("date"))
	if err != nil {
		h.logger.Error("lineups failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "could not load lineups")
		return
	}
	// 200 with nulls when not found — the client falls back to the schematic.
	httpx.WriteJSON(w, http.StatusOK, ml)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.All(r.Context())
	if err != nil {
		h.logger.Error("teams list failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "could not load teams")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"count": len(list),
		"teams": list,
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "team id must be numeric")
		return
	}
	detail, found, err := h.svc.Get(r.Context(), id)
	h.respondDetail(w, detail, found, err, "get")
}

func (h *Handler) byName(w http.ResponseWriter, r *http.Request) {
	detail, found, err := h.svc.ByName(r.Context(), r.URL.Query().Get("name"))
	h.respondDetail(w, detail, found, err, "by-name")
}

func (h *Handler) respondDetail(w http.ResponseWriter, detail Detail, found bool, err error, ctx string) {
	if err != nil {
		h.logger.Error("team "+ctx+" failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "could not load team")
		return
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "team not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}
