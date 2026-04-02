package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
)

type NotesHandler struct {
	db *sql.DB
}

func NewNotesHandler(db *sql.DB) *NotesHandler {
	return &NotesHandler{db: db}
}

type noteInput struct {
	Title         string   `json:"title"`
	MainReference string   `json:"main_reference"`
	Content       string   `json:"content"`
	VerseIDs      []string `json:"verse_ids"`
}

func (h *NotesHandler) ListNotes(w http.ResponseWriter, r *http.Request) {
	pagination, err := httputil.ParsePagination(r, 50, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	notes, err := storage.ListNotes(h.db, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, notes, http.StatusOK, httputil.PaginationMeta(pagination, len(notes)))
}

func (h *NotesHandler) GetNote(w http.ResponseWriter, r *http.Request) {
	noteID, err := strconv.ParseInt(chi.URLParam(r, "note_id"), 10, 64)
	if err != nil {
		httputil.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	note, err := storage.GetNoteByID(h.db, noteID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if note == nil {
		httputil.Error(w, "Note not found", http.StatusNotFound)
		return
	}
	httputil.Success(w, note, http.StatusOK)
}

func (h *NotesHandler) CreateNote(w http.ResponseWriter, r *http.Request) {
	var input noteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	note := &storage.Note{
		Title:         input.Title,
		MainReference: input.MainReference,
		Content:       input.Content,
		VerseIDs:      normalizeVerseIDs(input.VerseIDs),
	}

	noteID, err := storage.CreateNote(h.db, note)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	created, err := storage.GetNoteByID(h.db, noteID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, created, http.StatusCreated)
}

func (h *NotesHandler) UpdateNote(w http.ResponseWriter, r *http.Request) {
	noteID, err := strconv.ParseInt(chi.URLParam(r, "note_id"), 10, 64)
	if err != nil {
		httputil.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	var input noteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	err = storage.UpdateNote(h.db, &storage.Note{
		ID:            noteID,
		Title:         input.Title,
		MainReference: input.MainReference,
		Content:       input.Content,
		VerseIDs:      normalizeVerseIDs(input.VerseIDs),
	})
	if err != nil {
		if err == sql.ErrNoRows {
			httputil.Error(w, "Note not found", http.StatusNotFound)
			return
		}
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	note, err := storage.GetNoteByID(h.db, noteID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, note, http.StatusOK)
}

func (h *NotesHandler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	noteID, err := strconv.ParseInt(chi.URLParam(r, "note_id"), 10, 64)
	if err != nil {
		httputil.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	err = storage.DeleteNote(h.db, noteID)
	if err != nil {
		if err == sql.ErrNoRows {
			httputil.Error(w, "Note not found", http.StatusNotFound)
			return
		}
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	httputil.Success(w, map[string]any{"deleted": true, "note_id": noteID}, http.StatusOK)
}

func normalizeVerseIDs(verseIDs []string) []string {
	normalized := make([]string, 0, len(verseIDs))
	seen := map[string]struct{}{}
	for _, verseID := range verseIDs {
		canonical := strings.ToUpper(strings.TrimSpace(verseID))
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	return normalized
}
