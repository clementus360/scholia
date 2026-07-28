package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/clementus360/scholia/internal/auth"
	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
)

// NotesHandler is the one handler that spans both databases: notes are stored
// in Postgres, but the verse references attached to them are validated against
// the read-only Bible corpus.
type NotesHandler struct {
	stores *storage.Stores
}

func NewNotesHandler(stores *storage.Stores) *NotesHandler {
	return &NotesHandler{stores: stores}
}

type noteInput struct {
	Title         string   `json:"title"`
	MainReference string   `json:"main_reference"`
	Content       string   `json:"content"`
	VerseIDs      []string `json:"verse_ids"`
}

func (h *NotesHandler) ListNotes(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		httputil.Error(w, "Missing or invalid credentials", http.StatusUnauthorized)
		return
	}

	pagination, err := httputil.ParsePagination(r, 50, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	notes, err := storage.ListNotes(r.Context(), h.stores.Users, principal.UserID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, notes, http.StatusOK, httputil.PaginationMeta(pagination, len(notes)))
}

func (h *NotesHandler) GetNote(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		httputil.Error(w, "Missing or invalid credentials", http.StatusUnauthorized)
		return
	}

	noteID, err := strconv.ParseInt(chi.URLParam(r, "note_id"), 10, 64)
	if err != nil {
		httputil.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	note, err := storage.GetNoteByID(r.Context(), h.stores.Users, principal.UserID, noteID)
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
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		httputil.Error(w, "Missing or invalid credentials", http.StatusUnauthorized)
		return
	}

	var input noteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	verseIDs, ok := h.resolveVerseIDs(w, input.VerseIDs)
	if !ok {
		return
	}

	noteID, err := storage.CreateNote(r.Context(), h.stores.Users, &storage.Note{
		OwnerUserID:   principal.UserID,
		Title:         input.Title,
		MainReference: input.MainReference,
		Content:       input.Content,
		VerseIDs:      verseIDs,
	})
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	created, err := storage.GetNoteByID(r.Context(), h.stores.Users, principal.UserID, noteID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, created, http.StatusCreated)
}

func (h *NotesHandler) UpdateNote(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		httputil.Error(w, "Missing or invalid credentials", http.StatusUnauthorized)
		return
	}

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

	verseIDs, ok := h.resolveVerseIDs(w, input.VerseIDs)
	if !ok {
		return
	}

	err = storage.UpdateNote(r.Context(), h.stores.Users, principal.UserID, &storage.Note{
		ID:            noteID,
		OwnerUserID:   principal.UserID,
		Title:         input.Title,
		MainReference: input.MainReference,
		Content:       input.Content,
		VerseIDs:      verseIDs,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httputil.Error(w, "Note not found", http.StatusNotFound)
			return
		}
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	note, err := storage.GetNoteByID(r.Context(), h.stores.Users, principal.UserID, noteID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, note, http.StatusOK)
}

func (h *NotesHandler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		httputil.Error(w, "Missing or invalid credentials", http.StatusUnauthorized)
		return
	}

	noteID, err := strconv.ParseInt(chi.URLParam(r, "note_id"), 10, 64)
	if err != nil {
		httputil.Error(w, "Invalid note ID", http.StatusBadRequest)
		return
	}

	if err := storage.DeleteNote(r.Context(), h.stores.Users, principal.UserID, noteID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httputil.Error(w, "Note not found", http.StatusNotFound)
			return
		}
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	httputil.Success(w, map[string]any{"deleted": true, "note_id": noteID}, http.StatusOK)
}

// resolveVerseIDs expands user-supplied references (which may be ranges like
// "John 3:16-18") into concrete OSIS verse IDs, against the Bible database.
//
// This is the substitute for the foreign key that note_verses used to have on
// verses.id. That constraint disappeared when the two datasets moved into
// separate databases, so validation has to happen here instead — otherwise a
// note could reference a verse that does not exist.
//
// It writes the error response itself and reports whether the caller should
// continue.
func (h *NotesHandler) resolveVerseIDs(w http.ResponseWriter, references []string) ([]string, bool) {
	verseIDs, unresolved, err := storage.ExpandVerseReferences(h.stores.Bible, references)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return nil, false
	}
	if len(unresolved) > 0 {
		httputil.Error(w, fmt.Sprintf("Unresolved verse reference(s): %s", unresolved[0]), http.StatusBadRequest)
		return nil, false
	}
	return verseIDs, true
}
