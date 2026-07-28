package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// audienceAuthenticated is the `aud` claim Supabase puts on access tokens for
// signed-in users.
const audienceAuthenticated = "authenticated"

// Verifier validates Supabase access tokens.
//
// Verification is purely local: the project's public signing keys are fetched
// once from the JWKS endpoint and refreshed in the background, so checking a
// token is a signature operation rather than a call to the auth server. This is
// what makes per-request authentication cheap enough to sit in middleware.
//
// This requires the project to use asymmetric signing keys (RS256/ES256/EdDSA).
// A project still on the legacy shared HS256 secret publishes no usable public
// key, and tokens will fail to verify — migrate the project to JWT signing keys
// in the Supabase dashboard.
type Verifier struct {
	keys   keyfunc.Keyfunc
	issuer string
}

// NewVerifier starts a background refresh of the project's JWKS. The context
// governs that goroutine's lifetime — cancel it on shutdown.
func NewVerifier(ctx context.Context, cfg Config) (*Verifier, error) {
	keys, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.JWKSURL})
	if err != nil {
		return nil, fmt.Errorf("load Supabase JWKS from %s: %w", cfg.JWKSURL, err)
	}

	return &Verifier{keys: keys, issuer: cfg.Issuer}, nil
}

// SupabaseClaims is the subset of an access token we rely on.
type SupabaseClaims struct {
	jwt.RegisteredClaims

	Email        string         `json:"email"`
	Phone        string         `json:"phone"`
	Role         string         `json:"role"`
	SessionID    string         `json:"session_id"`
	IsAnonymous  bool           `json:"is_anonymous"`
	UserMetadata map[string]any `json:"user_metadata"`
	AppMetadata  map[string]any `json:"app_metadata"`
}

// DisplayName picks the friendliest name available on the token.
func (c *SupabaseClaims) DisplayName() string {
	for _, key := range []string{"display_name", "full_name", "name"} {
		if raw, ok := c.UserMetadata[key]; ok {
			if name, ok := raw.(string); ok && strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name)
			}
		}
	}
	if email := strings.TrimSpace(c.Email); email != "" {
		if local, _, found := strings.Cut(email, "@"); found {
			return local
		}
		return email
	}
	return ""
}

// Verify parses and validates a raw access token, returning its claims.
//
// Signature, expiry, issuer and audience are all enforced. Anonymous sign-ins
// are rejected: every Scholia session is expected to belong to a real invited
// account, and treating an anonymous token as a user would let anyone create
// notes.
func (v *Verifier) Verify(rawToken string) (*SupabaseClaims, error) {
	claims := &SupabaseClaims{}

	_, err := jwt.ParseWithClaims(rawToken, claims, v.keys.Keyfunc,
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(audienceAuthenticated),
		jwt.WithExpirationRequired(),
		// Pin the accepted algorithms. Without this a token could name an
		// algorithm we did not intend to support.
		//
		// These are exactly what Supabase signs asymmetric keys with: ES256
		// (P-256), RS256 (RSA 2048), and EdDSA (Ed25519, announced but not yet
		// generally available). HS256 is deliberately excluded — it is the
		// legacy shared-secret mode, is not recommended for production, and
		// cannot be verified from a public key set anyway.
		jwt.WithValidMethods([]string{"ES256", "RS256", "EdDSA"}),
	)
	if err != nil {
		return nil, fmt.Errorf("invalid access token: %w", err)
	}

	if claims.IsAnonymous {
		return nil, fmt.Errorf("anonymous sessions are not accepted")
	}

	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return nil, fmt.Errorf("access token has no subject claim")
	}

	return claims, nil
}
