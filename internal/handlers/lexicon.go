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
	pagination, err := httputil.ParsePagination(r, 25, 100)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	entry, err := storage.GetLexiconByID(h.db, strongsID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		httputil.Error(w, "Lexicon entry not found", http.StatusNotFound)
		return
	}

	occurrences, err := storage.GetLexiconOccurrencesByID(h.db, strongsID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	httputil.Success(w, struct {
		storage.LexiconEntry
		Occurrences []storage.LexiconOccurrence `json:"occurrences"`
	}{
		LexiconEntry: *entry,
		Occurrences:  occurrences,
	}, http.StatusOK, httputil.PaginationMeta(pagination, len(occurrences)))
}
