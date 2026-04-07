package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/clementus360/scholia/internal/auth"
	httputil "github.com/clementus360/scholia/internal/http"
)

type AdminHandler struct {
	db *sql.DB
}

func NewAdminHandler(db *sql.DB) *AdminHandler {
	return &AdminHandler{db: db}
}

type createInviteInput struct {
	Label  string   `json:"label"`
	Scopes []string `json:"scopes"`
}

func (h *AdminHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		httputil.Error(w, "Missing or invalid API key", http.StatusUnauthorized)
		return
	}

	var input createInviteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && err.Error() != "EOF" {
		httputil.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	effectiveLabel := strings.TrimSpace(input.Label)
	if effectiveLabel == "" {
		effectiveLabel = "tester"
	}

	inviteID, code, err := auth.CreateInviteCode(h.db, principal.UserID, effectiveLabel, input.Scopes)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	httputil.Success(w, map[string]any{
		"invite_id": inviteID,
		"code":      code,
		"label":     effectiveLabel,
		"scopes":    input.Scopes,
	}, http.StatusCreated)
}

type exchangeCodeInput struct {
	Code string `json:"code"`
}

type exchangeCodeResponse struct {
	auth.Principal
	APIKey string `json:"api_key"`
}

func (h *AdminHandler) ExchangeCode(w http.ResponseWriter, r *http.Request) {
	var input exchangeCodeInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	redemption, err := auth.RedeemInviteCode(h.db, input.Code)
	if err != nil {
		switch err {
		case auth.ErrInviteNotFound, auth.ErrInviteInvalid:
			httputil.Error(w, "Invalid invite code", http.StatusBadRequest)
		case auth.ErrInviteAlreadyRedeem:
			httputil.Error(w, "Invite code already used", http.StatusConflict)
		default:
			httputil.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	httputil.Success(w, exchangeCodeResponse{Principal: redemption.Principal, APIKey: redemption.APIKey}, http.StatusCreated)
}
