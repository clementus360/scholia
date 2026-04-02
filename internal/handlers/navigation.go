package handlers

import (
	"database/sql"
	"net/http"

	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
)

type NavigationHandler struct {
	db *sql.DB
}

func NewNavigationHandler(db *sql.DB) *NavigationHandler {
	return &NavigationHandler{db: db}
}

func (h *NavigationHandler) GetBooks(w http.ResponseWriter, r *http.Request) {
	pagination, err := httputil.ParsePagination(r, 100, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	books, err := storage.ListBooks(h.db, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, books, http.StatusOK, httputil.PaginationMeta(pagination, len(books)))
}

func (h *NavigationHandler) GetBookChapters(w http.ResponseWriter, r *http.Request) {
	slug := httputil.NormalizeSlug(chi.URLParam(r, "slug"))
	book, chapters, err := storage.ListBookChaptersBySlug(h.db, slug)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if book == nil {
		httputil.Error(w, "Book not found", http.StatusNotFound)
		return
	}

	httputil.Success(w, map[string]any{
		"book":          book,
		"chapter_count": len(chapters),
		"chapters":      chapters,
	}, http.StatusOK)
}

func (h *NavigationHandler) GetTimeline(w http.ResponseWriter, r *http.Request) {
	pagination, err := httputil.ParsePagination(r, 100, 1000)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	events, err := storage.ListTimeline(h.db, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, events, http.StatusOK, httputil.PaginationMeta(pagination, len(events)))
}
