package handlers

import (
	"database/sql"
	"net/http"

	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
)

type LexiconHandler struct {
	db *sql.DB
}

func NewLexiconHandler(db *sql.DB) *LexiconHandler {
	return &LexiconHandler{db: db}
}

func (h *LexiconHandler) GetLexicon(w http.ResponseWriter, r *http.Request) {
	strongsID := chi.URLParam(r, "strongs_id")
	entry, err := storage.GetLexiconByID(h.db, strongsID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		httputil.Error(w, "Lexicon entry not found", http.StatusNotFound)
		return
	}
	httputil.Success(w, entry, http.StatusOK)
}
