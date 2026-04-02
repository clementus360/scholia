package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	httputil "github.com/clementus360/scholia/internal/http"
)

type Principal struct {
	Type           string   `json:"type"`
	UserID         string   `json:"user_id,omitempty"`
	KeyID          string   `json:"key_id,omitempty"`
	Subject        string   `json:"subject"`
	DisplayName    string   `json:"display_name,omitempty"`
	Scopes         []string `json:"scopes"`
	Authenticated  bool     `json:"authenticated"`
	Authentication string   `json:"authentication"`
}

type Manager struct {
	db *sql.DB
}

type contextKey struct{}

var principalContextKey = contextKey{}

func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

func SeedBootstrapAuth(db *sql.DB) error {
	seeded, err := hasAnyApiKeys(db)
	if err != nil {
		return err
	}
	if seeded {
		return nil
	}

	configured := parseConfiguredKeys(os.Getenv("SCHOLIA_AUTH_KEYS"))
	if len(configured) == 0 {
		if token := strings.TrimSpace(os.Getenv("SCHOLIA_AUTH_TOKEN")); token != "" {
			configured = []seedKey{{Label: "default", Token: token, Scopes: []string{"read", "write"}}}
		}
	}
	if len(configured) == 0 {
		configured = []seedKey{{Label: "dev", Token: "scholia-dev", Scopes: []string{"read", "write"}}}
	}

	for _, key := range configured {
		if err := insertSeedKey(db, key); err != nil {
			return err
		}
	}

	return nil
}

type seedKey struct {
	Label  string
	Token  string
	Scopes []string
}

func parseConfiguredKeys(raw string) []seedKey {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	entries := strings.Split(raw, ";")
	keys := make([]seedKey, 0, len(entries))
	for idx, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		label := ""
		tokenPart := entry
		if left, right, found := strings.Cut(entry, "="); found {
			label = strings.TrimSpace(left)
			tokenPart = strings.TrimSpace(right)
		}

		token := tokenPart
		scopes := []string{"read", "write"}
		if left, right, found := strings.Cut(tokenPart, "|"); found {
			token = strings.TrimSpace(left)
			scopes = parseScopes(right)
		}
		if token == "" {
			continue
		}
		if label == "" {
			label = fmt.Sprintf("key-%d", idx+1)
		}
		keys = append(keys, seedKey{Label: label, Token: token, Scopes: scopes})
	}

	return keys
}

func parseScopes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{"read", "write"}
	}

	parts := strings.Split(raw, ",")
	scopes := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		scope := strings.ToLower(strings.TrimSpace(part))
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	if len(scopes) == 0 {
		return []string{"read", "write"}
	}
	return scopes
}

func hasAnyApiKeys(db *sql.DB) (bool, error) {
	var count int
	if err := db.QueryRow("SELECT COUNT(1) FROM api_keys").Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func insertSeedKey(db *sql.DB, key seedKey) error {
	userID := newID("usr")
	keyID := newID("key")
	tokenHash := hashToken(key.Token)
	scopes := strings.Join(parseScopes(strings.Join(key.Scopes, ",")), ",")

	if _, err := db.Exec(`
		INSERT OR IGNORE INTO users (id, subject, display_name, role)
		VALUES (?, ?, ?, ?)`, userID, key.Label, strings.Title(key.Label), "member"); err != nil {
		return err
	}

	_, err := db.Exec(`
		INSERT OR IGNORE INTO api_keys (id, user_id, token_hash, label, scopes, active)
		VALUES (?, ?, ?, ?, ?, 1)`, keyID, userID, tokenHash, key.Label, scopes)
	return err
}

func (m *Manager) Optional(next http.Handler) http.Handler {
	if m == nil || m.db == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if principal, ok := m.authenticate(r); ok {
			r = WithPrincipal(r, principal)
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Manager) RequireScopes(required ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if m == nil || m.db == nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httputil.Error(w, "Authentication not configured", http.StatusServiceUnavailable)
			})
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := m.authenticate(r)
			if !ok {
				httputil.Error(w, "Missing or invalid API key", http.StatusUnauthorized)
				return
			}
			if len(required) > 0 && !hasAllScopes(principal.Scopes, required) {
				httputil.Error(w, "Insufficient permissions", http.StatusForbidden)
				return
			}
			r = WithPrincipal(r, principal)
			next.ServeHTTP(w, r)
		})
	}
}

func (m *Manager) authenticate(r *http.Request) (Principal, bool) {
	token := extractToken(r)
	if token == "" {
		return Principal{}, false
	}

	var (
		keyID       string
		label       string
		tokenHash   string
		scopes      string
		userID      string
		subject     string
		displayName sql.NullString
	)

	rowErr := m.db.QueryRow(`
		SELECT ak.id, ak.label, ak.token_hash, ak.scopes, u.id, u.subject, u.display_name
		FROM api_keys ak
		INNER JOIN users u ON u.id = ak.user_id
		WHERE ak.token_hash = ? AND ak.active = 1
		LIMIT 1`, hashToken(token)).Scan(&keyID, &label, &tokenHash, &scopes, &userID, &subject, &displayName)
	if rowErr != nil {
		return Principal{}, false
	}

	if subtle.ConstantTimeCompare([]byte(tokenHash), []byte(hashToken(token))) != 1 {
		return Principal{}, false
	}

	principal := Principal{
		Type:           "api-key",
		UserID:         userID,
		KeyID:          keyID,
		Subject:        subject,
		DisplayName:    label,
		Scopes:         parseScopes(scopes),
		Authenticated:  true,
		Authentication: "api-key",
	}
	if displayName.Valid && strings.TrimSpace(displayName.String) != "" {
		principal.DisplayName = displayName.String
	}
	return principal, true
}

func extractToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader != "" {
		if token, found := strings.CutPrefix(authHeader, "Bearer "); found {
			return strings.TrimSpace(token)
		}
	}

	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func hasAllScopes(actual, required []string) bool {
	if len(required) == 0 {
		return true
	}
	actualSet := map[string]struct{}{}
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

func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s_%d", prefix, len(prefix))
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(buf))
}
