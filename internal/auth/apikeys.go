package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// APIKey describes a key without exposing its secret. The plaintext token is
// returned exactly once, at creation, and only its hash is ever stored.
type APIKey struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Scopes     []string   `json:"scopes"`
	Active     bool       `json:"active"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`

	// Token is populated only by CreateAPIKey. It is omitted everywhere else
	// because the plaintext cannot be recovered after creation.
	Token string `json:"token,omitempty"`
}

const apiKeyPrefix = "sk_scholia_"

// CreateAPIKey mints a long-lived key for a user's scripts and integrations.
//
// Sessions are the normal way in, but an access token expires in an hour and
// refreshing it needs the Supabase client. A key trades that convenience for a
// credential that does not rotate, so it is scoped explicitly and can be
// revoked.
func CreateAPIKey(ctx context.Context, db *sql.DB, userID, label string, scopes []string) (*APIKey, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("api key creation requires an authenticated user")
	}

	label = strings.TrimSpace(label)
	if label == "" {
		label = "api key"
	}

	granted := normalizeScopes(scopes)
	if len(granted) == 0 {
		granted = sessionScopes
	}
	// A key must never out-rank the session that created it.
	for _, scope := range granted {
		if !hasAllScopes(sessionScopes, []string{scope}) {
			return nil, fmt.Errorf("unsupported scope %q: valid scopes are %s", scope, strings.Join(sessionScopes, ", "))
		}
	}

	token, err := generateAPIKeyToken()
	if err != nil {
		return nil, err
	}

	key := &APIKey{Label: label, Scopes: granted, Active: true, Token: token}
	err = db.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, token_hash, label, scopes)
		VALUES ($1::uuid, $2, $3, string_to_array($4, ','))
		RETURNING id::text, created_at`,
		userID, hashToken(token), label, strings.Join(granted, ","),
	).Scan(&key.ID, &key.CreatedAt)
	if err != nil {
		return nil, err
	}

	return key, nil
}

// ListAPIKeys returns a user's keys. Token hashes are never selected — there is
// nothing a caller could do with one except attack it offline.
func ListAPIKeys(ctx context.Context, db *sql.DB, userID string) ([]APIKey, error) {
	if strings.TrimSpace(userID) == "" {
		return []APIKey{}, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT id::text, label, array_to_string(scopes, ','), active, last_used_at, created_at
		FROM api_keys
		WHERE user_id = $1::uuid
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]APIKey, 0)
	for rows.Next() {
		var (
			key        APIKey
			label      sql.NullString
			scopesCSV  string
			lastUsedAt sql.NullTime
		)
		if err := rows.Scan(&key.ID, &label, &scopesCSV, &key.Active, &lastUsedAt, &key.CreatedAt); err != nil {
			return nil, err
		}
		key.Label = label.String
		key.Scopes = normalizeScopes(strings.Split(scopesCSV, ","))
		if lastUsedAt.Valid {
			key.LastUsedAt = &lastUsedAt.Time
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// RevokeAPIKey deactivates a key the user owns. It returns sql.ErrNoRows when
// the key does not exist or belongs to someone else — the two are deliberately
// indistinguishable so the endpoint cannot be used to probe for key IDs.
//
// The key is deactivated rather than deleted so last_used_at survives as an
// audit trail.
//
// It returns the revoked key's token hash so the caller can evict it from the
// authentication cache. Without that, a revoked key keeps working until its
// cache entry expires — the user asks for revocation and gets it a minute
// later, which is not what "revoke" means.
func RevokeAPIKey(ctx context.Context, db *sql.DB, userID, keyID string) (string, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(keyID) == "" {
		return "", sql.ErrNoRows
	}

	var tokenHash string
	err := db.QueryRowContext(ctx, `
		UPDATE api_keys
		SET active = false, updated_at = now()
		WHERE id = $1::uuid AND user_id = $2::uuid AND active
		RETURNING token_hash`, keyID, userID).Scan(&tokenHash)
	if err == sql.ErrNoRows {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", err
	}
	return tokenHash, nil
}

func generateAPIKeyToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return apiKeyPrefix + hex.EncodeToString(buf), nil
}
