package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/clementus360/scholia/internal/auth"
	httputil "github.com/clementus360/scholia/internal/http"
)

type AuthHandler struct {
	db *sql.DB
}

func NewAuthHandler(db *sql.DB) *AuthHandler {
	return &AuthHandler{db: db}
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httputil.Success(w, map[string]any{"authenticated": false}, http.StatusOK)
		return
	}

	httputil.Success(w, principal, http.StatusOK)
}

type exchangeInviteCodeInput struct {
	Code string `json:"code"`
}

type exchangeInviteCodeResponse struct {
	auth.Principal
	APIKey string `json:"api_key"`
}

func (h *AuthHandler) ExchangeCode(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.db == nil {
		httputil.Error(w, "Authentication not configured", http.StatusServiceUnavailable)
		return
	}

	var input exchangeInviteCodeInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httputil.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	redemption, err := auth.RedeemInviteCode(h.db, input.Code)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInviteNotFound), errors.Is(err, auth.ErrInviteInvalid):
			httputil.Error(w, "Invalid invite code", http.StatusBadRequest)
		case errors.Is(err, auth.ErrInviteAlreadyRedeem):
			httputil.Error(w, "Invite code already used", http.StatusConflict)
		default:
			httputil.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	httputil.Success(w, exchangeInviteCodeResponse{Principal: redemption.Principal, APIKey: redemption.APIKey}, http.StatusCreated)
}
