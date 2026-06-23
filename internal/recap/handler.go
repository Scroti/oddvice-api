package recap

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/oddvice/api/internal/httpx"
)

// Handler serves recaps for the feed.
type Handler struct {
	store *Store
}

// NewHandler constructs a Handler (nil store → empty results).
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// Register mounts the recap route.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/recaps", h.recents)
}

func (h *Handler) recents(w http.ResponseWriter, r *http.Request) {
	lang := strings.TrimSpace(r.URL.Query().Get("lang"))
	if _, ok := langNames[lang]; !ok {
		lang = "en"
	}
	limit := 12
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	recaps, err := h.store.Recent(r.Context(), lang, limit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "could not load recaps")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"count": len(recaps), "recaps": recaps})
}
