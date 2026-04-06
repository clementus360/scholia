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

type VerseRangeResponse struct {
	Reference string          `json:"reference"`
	Start     string          `json:"start"`
	End       string          `json:"end"`
	Verses    []storage.Verse `json:"verses"`
}

// GetVerse handles GET /api/v1/verse/{osis_id}
func (h *VerseHandler) GetVerse(w http.ResponseWriter, r *http.Request) {
	reference := chi.URLParam(r, "osis_id")
	result, err := storage.GetVerseRangeByReference(h.db, reference)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if result == nil {
		httputil.Error(w, "Verse not found", http.StatusNotFound)
		return
	}

	if len(result.Verses) == 1 && result.Start == result.End {
		httputil.Success(w, result.Verses[0], http.StatusOK)
		return
	}

	httputil.Success(w, VerseRangeResponse{
		Reference: result.Reference,
		Start:     result.Start,
		End:       result.End,
		Verses:    result.Verses,
	}, http.StatusOK, map[string]any{"count": len(result.Verses)})
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

type VerseRangeContextResponse struct {
	Reference       string                                  `json:"reference"`
	Start           string                                  `json:"start"`
	End             string                                  `json:"end"`
	Verse           *storage.Verse                          `json:"verse,omitempty"`
	Verses          []storage.Verse                         `json:"verses"`
	Analysis        []storage.VerseAnalysisToken            `json:"analysis"`
	AnalysisByVerse map[string][]storage.VerseAnalysisToken `json:"analysis_by_verse"`
	Lexicon         []storage.LexiconEntry                  `json:"lexicon"`
	Locations       []storage.Location                      `json:"locations"`
	People          []storage.Person                        `json:"people"`
	Groups          []storage.Group                         `json:"groups"`
	Events          []storage.Event                         `json:"events"`
	CrossReferences []string                                `json:"cross_references"`
	Notes           []storage.Note                          `json:"notes"`
}

type VerseRangeCrossRefsResponse struct {
	Reference       string   `json:"reference"`
	Start           string   `json:"start"`
	End             string   `json:"end"`
	VerseID         string   `json:"verse_id,omitempty"`
	CrossReferences []string `json:"cross_references"`
}

type VerseRangeAnalysisResponse struct {
	Reference       string                                  `json:"reference"`
	Start           string                                  `json:"start"`
	End             string                                  `json:"end"`
	Verse           *storage.Verse                          `json:"verse,omitempty"`
	Verses          []storage.Verse                         `json:"verses"`
	Analysis        []storage.VerseAnalysisToken            `json:"analysis"`
	AnalysisByVerse map[string][]storage.VerseAnalysisToken `json:"analysis_by_verse"`
}

// GetVerseContext handles GET /api/v1/verse/{osis_id}/context
func (h *VerseHandler) GetVerseContext(w http.ResponseWriter, r *http.Request) {
	reference := chi.URLParam(r, "osis_id")
	pagination, err := httputil.ParsePagination(r, 100, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	rangeResult, err := storage.GetVerseRangeByReference(h.db, reference)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if rangeResult == nil || len(rangeResult.Verses) == 0 {
		httputil.Error(w, "Verse not found", http.StatusNotFound)
		return
	}

	if len(rangeResult.Verses) == 1 && rangeResult.Start == rangeResult.End {
		osisID := rangeResult.Verses[0].ID
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
		return
	}

	analysisByVerse := map[string][]storage.VerseAnalysisToken{}
	flatAnalysis := make([]storage.VerseAnalysisToken, 0)
	lexiconByID := map[string]storage.LexiconEntry{}
	locationByID := map[string]storage.Location{}
	peopleByID := map[string]storage.Person{}
	groupsByID := map[string]storage.Group{}
	eventsByID := map[string]storage.Event{}
	crossReferences := make([]string, 0)
	seenCrossRefs := map[string]struct{}{}
	notesByID := map[int64]storage.Note{}
	notesOrdered := make([]storage.Note, 0)

	for _, verse := range rangeResult.Verses {
		analysis, err := storage.GetVerseAnalysisByVerseID(h.db, verse.ID)
		if err != nil {
			httputil.Error(w, fmt.Sprintf("Database error (analysis): %v", err), http.StatusInternalServerError)
			return
		}
		analysisByVerse[verse.ID] = analysis
		flatAnalysis = append(flatAnalysis, analysis...)
		for _, token := range analysis {
			if token.Lexicon != nil && token.Lexicon.StrongsID != "" {
				lexiconByID[token.Lexicon.StrongsID] = *token.Lexicon
			}
		}

		locations, err := storage.GetLocationsByVerseID(h.db, verse.ID)
		if err != nil {
			httputil.Error(w, fmt.Sprintf("Database error (locations): %v", err), http.StatusInternalServerError)
			return
		}
		for _, item := range locations {
			locationByID[item.ID] = item
		}

		people, err := storage.GetPeopleByVerseID(h.db, verse.ID)
		if err != nil {
			httputil.Error(w, fmt.Sprintf("Database error (people): %v", err), http.StatusInternalServerError)
			return
		}
		for _, item := range people {
			peopleByID[item.ID] = item
		}

		groups, err := storage.GetGroupsByVerseID(h.db, verse.ID)
		if err != nil {
			httputil.Error(w, fmt.Sprintf("Database error (groups): %v", err), http.StatusInternalServerError)
			return
		}
		for _, item := range groups {
			groupsByID[item.ID] = item
		}

		events, err := storage.GetEventsByVerseID(h.db, verse.ID)
		if err != nil {
			httputil.Error(w, fmt.Sprintf("Database error (events): %v", err), http.StatusInternalServerError)
			return
		}
		for _, item := range events {
			eventsByID[item.ID] = item
		}

		refs, err := storage.GetCrossReferencesByVerseID(h.db, verse.ID, 1000, 0)
		if err != nil {
			httputil.Error(w, fmt.Sprintf("Database error (cross_references): %v", err), http.StatusInternalServerError)
			return
		}
		for _, ref := range refs {
			if _, ok := seenCrossRefs[ref]; ok {
				continue
			}
			seenCrossRefs[ref] = struct{}{}
			crossReferences = append(crossReferences, ref)
		}

		noteItems, err := storage.GetNotesByVerseID(h.db, verse.ID, 1000, 0)
		if err != nil {
			httputil.Error(w, fmt.Sprintf("Database error (notes): %v", err), http.StatusInternalServerError)
			return
		}
		for _, note := range noteItems {
			if _, exists := notesByID[note.ID]; exists {
				continue
			}
			notesByID[note.ID] = note
			notesOrdered = append(notesOrdered, note)
		}
	}

	lexicon := make([]storage.LexiconEntry, 0, len(lexiconByID))
	for _, entry := range lexiconByID {
		lexicon = append(lexicon, entry)
	}
	locations := make([]storage.Location, 0, len(locationByID))
	for _, item := range locationByID {
		locations = append(locations, item)
	}
	people := make([]storage.Person, 0, len(peopleByID))
	for _, item := range peopleByID {
		people = append(people, item)
	}
	groups := make([]storage.Group, 0, len(groupsByID))
	for _, item := range groupsByID {
		groups = append(groups, item)
	}
	events := make([]storage.Event, 0, len(eventsByID))
	for _, item := range eventsByID {
		events = append(events, item)
	}

	crossReferences = paginateStrings(crossReferences, pagination.Offset, pagination.Limit)
	notes := paginateNotes(notesOrdered, pagination.Offset, pagination.Limit)

	httputil.Success(w, VerseRangeContextResponse{
		Reference:       rangeResult.Reference,
		Start:           rangeResult.Start,
		End:             rangeResult.End,
		Verse:           &rangeResult.Verses[0],
		Verses:          rangeResult.Verses,
		Analysis:        flatAnalysis,
		AnalysisByVerse: analysisByVerse,
		Lexicon:         lexicon,
		Locations:       locations,
		People:          people,
		Groups:          groups,
		Events:          events,
		CrossReferences: crossReferences,
		Notes:           notes,
	}, http.StatusOK, map[string]any{
		"limit":                  pagination.Limit,
		"offset":                 pagination.Offset,
		"cross_references_count": len(crossReferences),
		"notes_count":            len(notes),
	})
}

// GetVerseCrossReferences handles GET /api/v1/verse/{osis_id}/cross-references
func (h *VerseHandler) GetVerseCrossReferences(w http.ResponseWriter, r *http.Request) {
	reference := chi.URLParam(r, "osis_id")
	rangeResult, err := storage.GetVerseRangeByReference(h.db, reference)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if rangeResult == nil || len(rangeResult.Verses) == 0 {
		httputil.Error(w, "Verse not found", http.StatusNotFound)
		return
	}

	pagination, err := httputil.ParsePagination(r, 50, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	if len(rangeResult.Verses) == 1 && rangeResult.Start == rangeResult.End {
		references, err := storage.GetCrossReferencesByVerseID(h.db, rangeResult.Verses[0].ID, pagination.Limit, pagination.Offset)
		if err != nil {
			httputil.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		httputil.Success(w, map[string]any{"verse_id": rangeResult.Verses[0].ID, "cross_references": references}, http.StatusOK, httputil.PaginationMeta(pagination, len(references)))
		return
	}

	references := make([]string, 0)
	seen := map[string]struct{}{}
	for _, verse := range rangeResult.Verses {
		items, err := storage.GetCrossReferencesByVerseID(h.db, verse.ID, 1000, 0)
		if err != nil {
			httputil.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		for _, ref := range items {
			if _, ok := seen[ref]; ok {
				continue
			}
			seen[ref] = struct{}{}
			references = append(references, ref)
		}
	}

	total := len(references)
	references = paginateStrings(references, pagination.Offset, pagination.Limit)

	httputil.Success(w, VerseRangeCrossRefsResponse{
		Reference:       rangeResult.Reference,
		Start:           rangeResult.Start,
		End:             rangeResult.End,
		VerseID:         rangeResult.Verses[0].ID,
		CrossReferences: references,
	}, http.StatusOK, map[string]any{"limit": pagination.Limit, "offset": pagination.Offset, "count": len(references), "total": total})
}

// GetVerseAnalysis handles GET /api/v1/analysis/{osis_id}
func (h *VerseHandler) GetVerseAnalysis(w http.ResponseWriter, r *http.Request) {
	reference := chi.URLParam(r, "osis_id")
	rangeResult, err := storage.GetVerseRangeByReference(h.db, reference)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if rangeResult == nil || len(rangeResult.Verses) == 0 {
		httputil.Error(w, "Verse not found", http.StatusNotFound)
		return
	}

	if len(rangeResult.Verses) == 1 && rangeResult.Start == rangeResult.End {
		verse := rangeResult.Verses[0]
		analysis, err := storage.GetAnalysisByVerseID(h.db, verse.ID)
		if err != nil {
			httputil.Error(w, "Database error", http.StatusInternalServerError)
			return
		}

		httputil.Success(w, map[string]interface{}{"verse": verse, "analysis": analysis}, http.StatusOK)
		return
	}

	analysisByVerse := map[string][]storage.VerseAnalysisToken{}
	flatAnalysis := make([]storage.VerseAnalysisToken, 0)
	for _, verse := range rangeResult.Verses {
		analysis, err := storage.GetAnalysisByVerseID(h.db, verse.ID)
		if err != nil {
			httputil.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		analysisByVerse[verse.ID] = analysis
		flatAnalysis = append(flatAnalysis, analysis...)
	}

	httputil.Success(w, VerseRangeAnalysisResponse{
		Reference:       rangeResult.Reference,
		Start:           rangeResult.Start,
		End:             rangeResult.End,
		Verse:           &rangeResult.Verses[0],
		Verses:          rangeResult.Verses,
		Analysis:        flatAnalysis,
		AnalysisByVerse: analysisByVerse,
	}, http.StatusOK, map[string]any{"count": len(rangeResult.Verses)})
}

func paginateStrings(items []string, offset, limit int) []string {
	if offset >= len(items) {
		return []string{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func paginateNotes(items []storage.Note, offset, limit int) []storage.Note {
	if offset >= len(items) {
		return []storage.Note{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
