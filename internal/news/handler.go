package news

import (
	"log/slog"
	"net/http"

	"github.com/oddvice/api/internal/httpx"
)

// Handler exposes the news feature over HTTP.
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
	mux.HandleFunc("GET /api/v1/news", h.list)
	mux.HandleFunc("GET /api/v1/news/{id}", h.getByID)
}

type listResponse struct {
	Count    int       `json:"count"`
	Articles []Article `json:"articles"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	articles, err := h.svc.Latest(r.Context(), r.URL.Query().Get("lang"))
	if err != nil {
		h.logger.Error("news list failed", "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "news provider request failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse{
		Count:    len(articles),
		Articles: articles,
	})
}

func (h *Handler) getByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	article, found, err := h.svc.GetByID(r.Context(), id, r.URL.Query().Get("lang"))
	if err != nil {
		h.logger.Error("news lookup failed", "id", id, "error", err)
		httpx.WriteError(w, http.StatusBadGateway, "news provider request failed")
		return
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "article not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, article)
}
