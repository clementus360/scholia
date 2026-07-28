package auth

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config holds what the API needs to validate Supabase sessions.
//
// Deliberately minimal: sign-up, sign-in, OAuth and password reset all happen
// directly between the client and Supabase, so this server never acts on a
// user's behalf and holds no Supabase API keys. It only verifies tokens, using
// public keys. There is no secret here to leak.
type Config struct {
	// ProjectURL is the base project URL, e.g. https://abcdefgh.supabase.co
	ProjectURL string

	// JWKSURL is where the public JWT signing keys are published. Access
	// tokens are verified against these locally, so authenticating a request
	// costs no network round trip.
	JWKSURL string

	// Issuer is the expected `iss` claim: <ProjectURL>/auth/v1
	Issuer string
}

// LoadConfig reads Supabase settings from the environment.
//
// The JWKS path is overridable because Supabase has served it from more than
// one location (/auth/v1/jwks and /auth/v1/.well-known/jwks.json). The
// well-known path is the standards-compliant one and is used by default; if a
// project only answers on the other, set SUPABASE_JWKS_URL rather than patching
// this code.
func LoadConfig() (Config, error) {
	projectURL := strings.TrimRight(strings.TrimSpace(os.Getenv("SUPABASE_URL")), "/")
	if projectURL == "" {
		return Config{}, fmt.Errorf("SUPABASE_URL is not set")
	}
	if _, err := url.ParseRequestURI(projectURL); err != nil {
		return Config{}, fmt.Errorf("SUPABASE_URL is not a valid URL: %w", err)
	}

	jwksURL := strings.TrimSpace(os.Getenv("SUPABASE_JWKS_URL"))
	if jwksURL == "" {
		jwksURL = projectURL + "/auth/v1/.well-known/jwks.json"
	}

	return Config{
		ProjectURL: projectURL,
		JWKSURL:    jwksURL,
		Issuer:     projectURL + "/auth/v1",
	}, nil
}
