package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
)

type ResolveHandler struct {
	db *sql.DB
}

func NewResolveHandler(db *sql.DB) *ResolveHandler {
	return &ResolveHandler{db: db}
}

func (h *ResolveHandler) ResolveRecID(w http.ResponseWriter, r *http.Request) {
	recID := strings.TrimSpace(chi.URLParam(r, "rec_id"))
	resolved, err := storage.ResolveRecID(h.db, recID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if resolved == nil {
		httputil.Error(w, "ID not found", http.StatusNotFound)
		return
	}
	httputil.Success(w, resolved, http.StatusOK)
}
