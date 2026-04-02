package handlers

import (
	"net/http"

	"github.com/clementus360/scholia/internal/auth"
	httputil "github.com/clementus360/scholia/internal/http"
)

type AuthHandler struct{}

func NewAuthHandler() *AuthHandler {
	return &AuthHandler{}
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httputil.Success(w, map[string]any{"authenticated": false}, http.StatusOK)
		return
	}

	httputil.Success(w, principal, http.StatusOK)
}
