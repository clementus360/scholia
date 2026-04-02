package handlers

import (
	"database/sql"
	"net/http"

	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
)

type GeographyHandler struct {
	db *sql.DB
}

func NewGeographyHandler(db *sql.DB) *GeographyHandler {
	return &GeographyHandler{db: db}
}

func (h *GeographyHandler) GetLocation(w http.ResponseWriter, r *http.Request) {
	locationID := httputil.NormalizeID(chi.URLParam(r, "location_id"))
	location, err := storage.GetLocationByID(h.db, locationID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if location == nil {
		httputil.Error(w, "Location not found", http.StatusNotFound)
		return
	}
	httputil.Success(w, location, http.StatusOK)
}

func (h *GeographyHandler) GetLocationVerses(w http.ResponseWriter, r *http.Request) {
	locationID := httputil.NormalizeID(chi.URLParam(r, "location_id"))
	pagination, err := httputil.ParsePagination(r, 50, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	verses, err := storage.GetLocationVerses(h.db, locationID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, map[string]any{"location_id": locationID, "verses": verses}, http.StatusOK, httputil.PaginationMeta(pagination, len(verses)))
}
