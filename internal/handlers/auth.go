package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/clementus360/scholia/internal/auth"
	httputil "github.com/clementus360/scholia/internal/http"
	"github.com/go-chi/chi/v5"
)

type AuthHandler struct {
	users *sql.DB

	// manager is needed so a revoked key can be evicted from the
	// authentication cache immediately rather than lingering until its entry
	// expires.
	manager *auth.Manager
}

func NewAuthHandler(users *sql.DB, manager *auth.Manager) *AuthHandler {
	return &AuthHandler{users: users, manager: manager}
}

// Me reports who the caller is. Unauthenticated requests get a 200 with
// authenticated:false rather than a 401, so the frontend can use it to decide
// what to render without treating a signed-out visitor as an error.
//
// Sign-up, sign-in, OAuth and sign-out are handled entirely by Supabase on the
// client. This API never sees a password; it only validates the resulting
// session.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httputil.Success(w, map[string]any{"authenticated": false}, http.StatusOK)
		return
	}
	httputil.Success(w, principal, http.StatusOK)
}

type createAPIKeyInput struct {
	Label  string   `json:"label"`
	Scopes []string `json:"scopes"`
}

// CreateAPIKey mints a long-lived key for scripts and integrations that cannot
// perform Supabase's refresh-token cycle.
//
// The plaintext token is in the response and nowhere else — only its hash is
// stored, so it cannot be shown again.
func (h *AuthHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.sessionPrincipal(w, r)
	if !ok {
		return
	}

	var input createAPIKeyInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		httputil.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	key, err := auth.CreateAPIKey(r.Context(), h.users, principal.UserID, input.Label, input.Scopes)
	if err != nil {
		httputil.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	httputil.Success(w, key, http.StatusCreated)
}

func (h *AuthHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.sessionPrincipal(w, r)
	if !ok {
		return
	}

	keys, err := auth.ListAPIKeys(r.Context(), h.users, principal.UserID)
	if err != nil {
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	httputil.Success(w, keys, http.StatusOK)
}

func (h *AuthHandler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.sessionPrincipal(w, r)
	if !ok {
		return
	}

	tokenHash, err := auth.RevokeAPIKey(r.Context(), h.users, principal.UserID, chi.URLParam(r, "key_id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httputil.Error(w, "API key not found", http.StatusNotFound)
			return
		}
		httputil.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Evict now so the key stops working on this instance immediately.
	h.manager.InvalidateAPIKey(tokenHash)

	httputil.Success(w, map[string]any{"revoked": true}, http.StatusOK)
}

// sessionPrincipal requires a Supabase session specifically, not an API key.
//
// Key management is deliberately closed to API keys: a leaked key that could
// mint further keys would survive its own revocation, since the attacker would
// already hold a second credential the owner never saw.
func (h *AuthHandler) sessionPrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.UserID == "" {
		httputil.Error(w, "Missing or invalid credentials", http.StatusUnauthorized)
		return auth.Principal{}, false
	}
	if principal.Authentication != "supabase-jwt" {
		httputil.Error(w, "API keys cannot manage API keys; sign in with a session", http.StatusForbidden)
		return auth.Principal{}, false
	}
	return principal, true
}
