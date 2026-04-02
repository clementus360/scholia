package handlers

import (
	"database/sql"
	"fmt"
	"net/http"

	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
)

type VerseHandler struct {
	db *sql.DB
}

func NewVerseHandler(db *sql.DB) *VerseHandler {
	return &VerseHandler{db: db}
}

// GetVerse handles GET /api/v1/verse/{osis_id}
func (h *VerseHandler) GetVerse(w http.ResponseWriter, r *http.Request) {
	osisID := httputil.NormalizeVerseID(chi.URLParam(r, "osis_id"))

	verse, err := storage.GetVerseByID(h.db, osisID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if verse == nil {
		httputil.Error(w, "Verse not found", http.StatusNotFound)
		return
	}

	httputil.Success(w, verse, http.StatusOK)
}

type VerseContextResponse struct {
	Verse           *storage.Verse               `json:"verse"`
	Analysis        []storage.VerseAnalysisToken `json:"analysis"`
	Lexicon         []storage.LexiconEntry       `json:"lexicon"`
	Locations       []storage.Location           `json:"locations"`
	People          []storage.Person             `json:"people"`
	Groups          []storage.Group              `json:"groups"`
	Events          []storage.Event              `json:"events"`
	CrossReferences []string                     `json:"cross_references"`
	Notes           []storage.Note               `json:"notes"`
}

// GetVerseContext handles GET /api/v1/verse/{osis_id}/context
func (h *VerseHandler) GetVerseContext(w http.ResponseWriter, r *http.Request) {
	osisID := httputil.NormalizeVerseID(chi.URLParam(r, "osis_id"))
	pagination, err := httputil.ParsePagination(r, 100, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	verse, err := storage.GetVerseByID(h.db, osisID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if verse == nil {
		httputil.Error(w, "Verse not found", http.StatusNotFound)
		return
	}

	analysis, err := storage.GetVerseAnalysisByVerseID(h.db, osisID)
	if err != nil {
		httputil.Error(w, fmt.Sprintf("Database error (analysis): %v", err), http.StatusInternalServerError)
		return
	}

	locations, err := storage.GetLocationsByVerseID(h.db, osisID)
	if err != nil {
		httputil.Error(w, fmt.Sprintf("Database error (locations): %v", err), http.StatusInternalServerError)
		return
	}

	people, err := storage.GetPeopleByVerseID(h.db, osisID)
	if err != nil {
		httputil.Error(w, fmt.Sprintf("Database error (people): %v", err), http.StatusInternalServerError)
		return
	}

	groups, err := storage.GetGroupsByVerseID(h.db, osisID)
	if err != nil {
		httputil.Error(w, fmt.Sprintf("Database error (groups): %v", err), http.StatusInternalServerError)
		return
	}

	events, err := storage.GetEventsByVerseID(h.db, osisID)
	if err != nil {
		httputil.Error(w, fmt.Sprintf("Database error (events): %v", err), http.StatusInternalServerError)
		return
	}

	crossReferences, err := storage.GetCrossReferencesByVerseID(h.db, osisID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, fmt.Sprintf("Database error (cross_references): %v", err), http.StatusInternalServerError)
		return
	}

	notes, err := storage.GetNotesByVerseID(h.db, osisID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, fmt.Sprintf("Database error (notes): %v", err), http.StatusInternalServerError)
		return
	}

	lexiconByID := make(map[string]storage.LexiconEntry)
	for _, token := range analysis {
		if token.Lexicon != nil && token.Lexicon.StrongsID != "" {
			lexiconByID[token.Lexicon.StrongsID] = *token.Lexicon
		}
	}

	lexicon := make([]storage.LexiconEntry, 0, len(lexiconByID))
	for _, entry := range lexiconByID {
		lexicon = append(lexicon, entry)
	}

	response := VerseContextResponse{
		Verse:           verse,
		Analysis:        analysis,
		Lexicon:         lexicon,
		Locations:       locations,
		People:          people,
		Groups:          groups,
		Events:          events,
		CrossReferences: crossReferences,
		Notes:           notes,
	}

	httputil.Success(w, response, http.StatusOK, map[string]any{
		"limit":                  pagination.Limit,
		"offset":                 pagination.Offset,
		"cross_references_count": len(crossReferences),
		"notes_count":            len(notes),
	})
}

// GetVerseCrossReferences handles GET /api/v1/verse/{osis_id}/cross-references
func (h *VerseHandler) GetVerseCrossReferences(w http.ResponseWriter, r *http.Request) {
	osisID := httputil.NormalizeVerseID(chi.URLParam(r, "osis_id"))

	verse, err := storage.GetVerseByID(h.db, osisID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if verse == nil {
		httputil.Error(w, "Verse not found", http.StatusNotFound)
		return
	}

	pagination, err := httputil.ParsePagination(r, 50, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	references, err := storage.GetCrossReferencesByVerseID(h.db, osisID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	httputil.Success(w, map[string]any{"verse_id": osisID, "cross_references": references}, http.StatusOK, httputil.PaginationMeta(pagination, len(references)))
}

// GetVerseAnalysis handles GET /api/v1/analysis/{osis_id}
func (h *VerseHandler) GetVerseAnalysis(w http.ResponseWriter, r *http.Request) {
	osisID := httputil.NormalizeVerseID(chi.URLParam(r, "osis_id"))

	verse, err := storage.GetVerseByID(h.db, osisID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if verse == nil {
		httputil.Error(w, "Verse not found", http.StatusNotFound)
		return
	}

	analysis, err := storage.GetAnalysisByVerseID(h.db, osisID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	httputil.Success(w, map[string]interface{}{"verse": verse, "analysis": analysis}, http.StatusOK)
}
