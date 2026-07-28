package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	httputil "github.com/clementus360/scholia/internal/http"
)

// Principal is the authenticated caller behind a request.
type Principal struct {
	Type           string   `json:"type"`
	UserID         string   `json:"user_id,omitempty"`
	KeyID          string   `json:"key_id,omitempty"`
	Subject        string   `json:"subject"`
	DisplayName    string   `json:"display_name,omitempty"`
	Email          string   `json:"email,omitempty"`
	Scopes         []string `json:"scopes"`
	Authenticated  bool     `json:"authenticated"`
	Authentication string   `json:"authentication"`
}

// sessionScopes are granted to any signed-in Supabase user. Notes are scoped to
// their owner at the query level, so a session needs no more than the ability to
// read and write its own data. Elevated rights come from profiles.role, checked
// separately in RequireAdmin.
var sessionScopes = []string{"read", "write"}

type contextKey struct{}

var principalContextKey = contextKey{}

// Manager authenticates requests via either a Supabase session JWT or a
// long-lived API key.
//
// The two paths have very different costs, which drives the design:
//
//   - A session JWT is verified from its signature alone. No database, no
//     network. This is the path browsers use, and it is the hot one.
//   - An API key must be looked up by hash in Postgres. That is a network round
//     trip, so results are cached briefly.
type Manager struct {
	users    *sql.DB
	verifier *Verifier

	keyCache     *ttlCache[Principal]
	profileCache *ttlCache[profile]
}

type profile struct {
	Role        string
	DisplayName string
}

func NewManager(users *sql.DB, verifier *Verifier) *Manager {
	return &Manager{
		users:        users,
		verifier:     verifier,
		keyCache:     newTTLCache[Principal](60 * time.Second),
		profileCache: newTTLCache[profile](60 * time.Second),
	}
}

func (m *Manager) ready() bool {
	return m != nil && m.users != nil && m.verifier != nil
}

// Optional attaches a principal when credentials are present, but lets
// unauthenticated requests through. Used for public endpoints that show extra
// detail to signed-in users.
func (m *Manager) Optional(next http.Handler) http.Handler {
	if !m.ready() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := m.authenticate(r); ok {
			r = WithPrincipal(r, principal)
		}
		next.ServeHTTP(w, r)
	})
}

// RequireScopes rejects requests that are unauthenticated or lack every named
// scope.
func (m *Manager) RequireScopes(required ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !m.ready() {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httputil.Error(w, "Authentication not configured", http.StatusServiceUnavailable)
			})
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := m.resolvePrincipal(r)
			if !ok {
				httputil.Error(w, "Missing or invalid credentials", http.StatusUnauthorized)
				return
			}
			if !hasAllScopes(principal.Scopes, required) {
				httputil.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, WithPrincipal(r, principal))
		})
	}
}

// RequireAdmin additionally checks profiles.role, replacing the previous
// approach of matching the caller against SCHOLIA_ADMIN_SUBJECT /
// SCHOLIA_ADMIN_USER_ID environment variables. Admin status is now data, so it
// can be granted and revoked without a redeploy.
func (m *Manager) RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !m.ready() {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httputil.Error(w, "Authentication not configured", http.StatusServiceUnavailable)
			})
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := m.resolvePrincipal(r)
			if !ok {
				httputil.Error(w, "Missing or invalid credentials", http.StatusUnauthorized)
				return
			}

			isAdmin, err := m.IsAdmin(r.Context(), principal.UserID)
			if err != nil {
				httputil.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			if !isAdmin {
				httputil.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, WithPrincipal(r, principal))
		})
	}
}

// InvalidateAPIKey evicts a key from the authentication cache so a revocation
// takes effect immediately rather than when the entry expires.
//
// This is per-process. With more than one instance running, the other instances
// keep honouring the key until their own entries expire, so the cache TTL is
// also the worst-case revocation delay across a fleet. Shorten the TTL if that
// window matters more than the saved lookups.
func (m *Manager) InvalidateAPIKey(tokenHash string) {
	if m == nil || m.keyCache == nil || tokenHash == "" {
		return
	}
	m.keyCache.invalidate(tokenHash)
}

// IsAdmin reports whether a user carries the admin role.
func (m *Manager) IsAdmin(ctx context.Context, userID string) (bool, error) {
	if strings.TrimSpace(userID) == "" {
		return false, nil
	}

	prof, err := m.lookupProfile(ctx, userID)
	if err != nil {
		return false, err
	}
	return prof.Role == "admin", nil
}

func (m *Manager) lookupProfile(ctx context.Context, userID string) (profile, error) {
	if cached, ok := m.profileCache.get(userID); ok {
		return cached, nil
	}

	var (
		role        string
		displayName sql.NullString
	)
	err := m.users.QueryRowContext(ctx,
		`SELECT role, display_name FROM profiles WHERE id = $1::uuid`, userID,
	).Scan(&role, &displayName)
	if err == sql.ErrNoRows {
		// No profile row yet. Treat as an ordinary member rather than an error:
		// the on_auth_user_created trigger may not have run for accounts made
		// before it existed.
		prof := profile{Role: "member"}
		m.profileCache.set(userID, prof)
		return prof, nil
	}
	if err != nil {
		return profile{}, err
	}

	prof := profile{Role: role, DisplayName: displayName.String}
	m.profileCache.set(userID, prof)
	return prof, nil
}

// resolvePrincipal returns the caller, reusing the result of the Optional
// middleware when it has already run.
//
// Optional is installed globally, so without this every protected route would
// verify the same token a second time — a wasted signature check on the JWT
// path and a redundant cache lookup on the API-key path.
func (m *Manager) resolvePrincipal(r *http.Request) (Principal, bool) {
	if principal, ok := PrincipalFromContext(r.Context()); ok && principal.Authenticated {
		return principal, true
	}
	return m.authenticate(r)
}

// authenticate resolves the caller from the request's credentials.
func (m *Manager) authenticate(r *http.Request) (Principal, bool) {
	token := extractToken(r)
	if token == "" {
		return Principal{}, false
	}

	// A Supabase access token is a JWT and therefore has two dots. Anything
	// else is an API key. This avoids paying for a failed signature check on
	// every API-key request (and vice versa).
	if strings.Count(token, ".") == 2 {
		if principal, ok := m.authenticateJWT(token); ok {
			return principal, true
		}
		return Principal{}, false
	}

	return m.authenticateAPIKey(r.Context(), token)
}

func (m *Manager) authenticateJWT(token string) (Principal, bool) {
	claims, err := m.verifier.Verify(token)
	if err != nil {
		return Principal{}, false
	}

	return Principal{
		Type:           "user",
		UserID:         claims.Subject,
		Subject:        claims.Subject,
		DisplayName:    claims.DisplayName(),
		Email:          claims.Email,
		Scopes:         sessionScopes,
		Authenticated:  true,
		Authentication: "supabase-jwt",
	}, true
}

func (m *Manager) authenticateAPIKey(ctx context.Context, token string) (Principal, bool) {
	tokenHash := hashToken(token)

	if cached, ok := m.keyCache.get(tokenHash); ok {
		return cached, true
	}

	var (
		keyID     string
		userID    string
		label     sql.NullString
		scopesCSV string
	)
	// scopes is text[], flattened to a comma-separated string in SQL rather
	// than scanned as an array. Array decoding through database/sql depends on
	// driver-specific behaviour; this works the same everywhere.
	err := m.users.QueryRowContext(ctx, `
		SELECT id::text, user_id::text, label, array_to_string(scopes, ',')
		FROM api_keys
		WHERE token_hash = $1
		  AND active
		  AND (expires_at IS NULL OR expires_at > now())`,
		tokenHash,
	).Scan(&keyID, &userID, &label, &scopesCSV)
	if err != nil {
		return Principal{}, false
	}

	principal := Principal{
		Type:           "api-key",
		UserID:         userID,
		KeyID:          keyID,
		Subject:        userID,
		DisplayName:    label.String,
		Scopes:         normalizeScopes(strings.Split(scopesCSV, ",")),
		Authenticated:  true,
		Authentication: "api-key",
	}

	m.keyCache.set(tokenHash, principal)
	m.touchAPIKey(keyID)

	return principal, true
}

// touchAPIKey records usage without blocking the request. It only runs on a
// cache miss, so a busy key writes at most once per cache TTL rather than once
// per request.
func (m *Manager) touchAPIKey(keyID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Best effort: a failure here must never affect the request that
		// triggered it.
		_, _ = m.users.ExecContext(ctx,
			`UPDATE api_keys SET last_used_at = now() WHERE id = $1::uuid`, keyID)
	}()
}

func extractToken(r *http.Request) string {
	if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" {
		if token, found := strings.CutPrefix(authHeader, "Bearer "); found {
			return strings.TrimSpace(token)
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func normalizeScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	seen := map[string]struct{}{}
	for _, scope := range scopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func hasAllScopes(actual, required []string) bool {
	if len(required) == 0 {
		return true
	}
	actualSet := make(map[string]struct{}, len(actual))
	for _, scope := range actual {
		actualSet[strings.ToLower(strings.TrimSpace(scope))] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := actualSet[strings.ToLower(strings.TrimSpace(scope))]; !ok {
			return false
		}
	}
	return true
}

func WithPrincipal(r *http.Request, principal Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalContextKey, principal))
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(Principal)
	return principal, ok
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ttlCache is a small expiring map. Entries are cheap to recompute, so eviction
// is lazy: a stale entry is simply ignored and overwritten on next use.
type ttlCache[T any] struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]ttlEntry[T]
}

type ttlEntry[T any] struct {
	value     T
	expiresAt time.Time
}

func newTTLCache[T any](ttl time.Duration) *ttlCache[T] {
	return &ttlCache[T]{ttl: ttl, entries: map[string]ttlEntry[T]{}}
}

func (c *ttlCache[T]) get(key string) (T, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok || time.Now().After(entry.expiresAt) {
		var zero T
		return zero, false
	}
	return entry.value, true
}

func (c *ttlCache[T]) set(key string, value T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Bound growth. Revoked keys and logged-out users would otherwise
	// accumulate for the process lifetime.
	if len(c.entries) > 1024 {
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expiresAt) {
				delete(c.entries, k)
			}
		}
	}

	c.entries[key] = ttlEntry[T]{value: value, expiresAt: time.Now().Add(c.ttl)}
}

// invalidate drops a cached entry immediately, so revocation takes effect
// without waiting out the TTL.
func (c *ttlCache[T]) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
