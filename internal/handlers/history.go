package handlers

import (
	"database/sql"
	"net/http"

	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/clementus360/scholia/internal/storage"
	"github.com/go-chi/chi/v5"
)

type HistoryHandler struct {
	db *sql.DB
}

func NewHistoryHandler(db *sql.DB) *HistoryHandler {
	return &HistoryHandler{db: db}
}

func (h *HistoryHandler) GetPerson(w http.ResponseWriter, r *http.Request) {
	personID := httputil.NormalizeID(chi.URLParam(r, "person_id"))
	person, err := storage.GetPersonByID(h.db, personID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if person == nil {
		httputil.Error(w, "Person not found", http.StatusNotFound)
		return
	}
	httputil.Success(w, person, http.StatusOK)
}

func (h *HistoryHandler) GetPersonVerses(w http.ResponseWriter, r *http.Request) {
	personID := httputil.NormalizeID(chi.URLParam(r, "person_id"))
	pagination, err := httputil.ParsePagination(r, 50, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	verses, err := storage.GetPersonVerses(h.db, personID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, map[string]any{"person_id": personID, "verses": verses}, http.StatusOK, httputil.PaginationMeta(pagination, len(verses)))
}

func (h *HistoryHandler) GetGroup(w http.ResponseWriter, r *http.Request) {
	groupID := httputil.NormalizeID(chi.URLParam(r, "group_id"))
	group, err := storage.GetGroupByID(h.db, groupID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if group == nil {
		httputil.Error(w, "Group not found", http.StatusNotFound)
		return
	}
	httputil.Success(w, group, http.StatusOK)
}

func (h *HistoryHandler) GetGroupMembers(w http.ResponseWriter, r *http.Request) {
	groupID := httputil.NormalizeID(chi.URLParam(r, "group_id"))
	pagination, err := httputil.ParsePagination(r, 50, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	members, err := storage.GetGroupMembers(h.db, groupID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, map[string]any{"group_id": groupID, "members": members}, http.StatusOK, httputil.PaginationMeta(pagination, len(members)))
}

func (h *HistoryHandler) GetEvent(w http.ResponseWriter, r *http.Request) {
	eventID := httputil.NormalizeID(chi.URLParam(r, "event_id"))
	event, err := storage.GetEventByID(h.db, eventID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if event == nil {
		httputil.Error(w, "Event not found", http.StatusNotFound)
		return
	}
	httputil.Success(w, event, http.StatusOK)
}

func (h *HistoryHandler) GetEventParticipants(w http.ResponseWriter, r *http.Request) {
	eventID := httputil.NormalizeID(chi.URLParam(r, "event_id"))
	pagination, err := httputil.ParsePagination(r, 50, 500)
	if err != nil {
		httputil.Error(w, "Invalid pagination parameters", http.StatusBadRequest)
		return
	}

	participants, err := storage.GetEventParticipants(h.db, eventID, pagination.Limit, pagination.Offset)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, map[string]any{"event_id": eventID, "participants": participants}, http.StatusOK, map[string]any{
		"limit":        pagination.Limit,
		"offset":       pagination.Offset,
		"people_count": len(participants.People),
		"groups_count": len(participants.Groups),
	})
}
