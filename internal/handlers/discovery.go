package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
)

type DiscoveryHandler struct {
	db *sql.DB
}

func NewDiscoveryHandler(db *sql.DB) *DiscoveryHandler {
	return &DiscoveryHandler{db: db}
}

func (h *DiscoveryHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httputil.Error(w, "Missing query parameter: q", http.StatusBadRequest)
		return
	}

	pagination, err := httputil.ParsePagination(r, 20, 100)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	searchType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if searchType == "" {
		searchType = "all"
	}

	response := map[string]any{"query": q, "type": searchType}

	if searchType == "verse" || searchType == "all" {
		verses, err := storage.SearchVerses(h.db, q, pagination.Limit, pagination.Offset)
		if err != nil {
			httputil.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		response["verses"] = verses
	}

	if searchType == "entity" || searchType == "all" {
		entities, err := storage.SearchEntities(h.db, q, pagination.Limit, pagination.Offset)
		if err != nil {
			httputil.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		response["entities"] = entities
	}

	meta := httputil.PaginationMeta(pagination, 0)
	if verses, ok := response["verses"].([]storage.SearchVerseResult); ok {
		meta["verses_count"] = len(verses)
	}
	if entities, ok := response["entities"].([]storage.SearchEntityResult); ok {
		meta["entities_count"] = len(entities)
	}
	httputil.Success(w, response, http.StatusOK, meta)
}

func (h *DiscoveryHandler) Suggest(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httputil.Error(w, "Missing query parameter: q", http.StatusBadRequest)
		return
	}

	pagination, err := httputil.ParsePagination(r, 10, 50)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	suggestions, err := storage.Suggest(h.db, q, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	httputil.Success(w, map[string]any{"query": q, "suggestions": suggestions}, http.StatusOK, httputil.PaginationMeta(pagination, len(suggestions)))
}
