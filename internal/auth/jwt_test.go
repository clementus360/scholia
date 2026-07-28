package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testIssuerPath = "/auth/v1"

// jwksServer stands in for a Supabase project's JWKS endpoint, serving the
// public half of a locally generated signing key.
type jwksServer struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	keyID string
}

func newJWKSServer(t *testing.T) *jwksServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	js := &jwksServer{key: key, keyID: "test-key-1"}

	eBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(eBytes, uint64(key.E))
	// Trim leading zero bytes; the exponent is a minimal big-endian integer.
	trimmed := 0
	for trimmed < len(eBytes)-1 && eBytes[trimmed] == 0 {
		trimmed++
	}

	jwks := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": js.keyID,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(eBytes[trimmed:]),
		}},
	}

	js.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(js.srv.Close)

	return js
}

func (j *jwksServer) config() Config {
	return Config{
		ProjectURL: j.srv.URL,
		JWKSURL:    j.srv.URL + "/jwks",
		Issuer:     j.srv.URL + testIssuerPath,
	}
}

// sign mints a token, applying mutate so individual tests can bend one claim.
func (j *jwksServer) sign(t *testing.T, mutate func(claims jwt.MapClaims)) string {
	t.Helper()

	claims := jwt.MapClaims{
		"sub":   "3f9a1c2e-0000-4000-8000-000000000001",
		"iss":   j.srv.URL + testIssuerPath,
		"aud":   audienceAuthenticated,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"email": "reader@example.com",
		"role":  "authenticated",
	}
	if mutate != nil {
		mutate(claims)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = j.keyID

	signed, err := token.SignedString(j.key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func newTestVerifier(t *testing.T, js *jwksServer) *Verifier {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	verifier, err := NewVerifier(ctx, js.config())
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return verifier
}

func TestVerifyAcceptsValidToken(t *testing.T) {
	js := newJWKSServer(t)
	verifier := newTestVerifier(t, js)

	claims, err := verifier.Verify(js.sign(t, nil))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if got, want := claims.Subject, "3f9a1c2e-0000-4000-8000-000000000001"; got != want {
		t.Errorf("subject = %q, want %q", got, want)
	}
	if got, want := claims.Email, "reader@example.com"; got != want {
		t.Errorf("email = %q, want %q", got, want)
	}
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	js := newJWKSServer(t)
	verifier := newTestVerifier(t, js)

	tests := []struct {
		name   string
		mutate func(jwt.MapClaims)
	}{
		{"expired", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{"wrong issuer", func(c jwt.MapClaims) { c["iss"] = "https://attacker.example.com/auth/v1" }},
		{"wrong audience", func(c jwt.MapClaims) { c["aud"] = "anon" }},
		{"no expiry", func(c jwt.MapClaims) { delete(c, "exp") }},
		{"empty subject", func(c jwt.MapClaims) { c["sub"] = "" }},
		// An anonymous sign-in is a real Supabase session but must not be
		// treated as an invited user, or anyone could create notes.
		{"anonymous", func(c jwt.MapClaims) { c["is_anonymous"] = true }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := verifier.Verify(js.sign(t, tc.mutate)); err == nil {
				t.Fatalf("expected %s token to be rejected", tc.name)
			}
		})
	}
}

// TestVerifyRejectsForeignKey is the important one: a token signed by a key the
// project does not publish must fail, even when every claim looks right.
func TestVerifyRejectsForeignKey(t *testing.T) {
	js := newJWKSServer(t)
	verifier := newTestVerifier(t, js)

	attackerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "3f9a1c2e-0000-4000-8000-000000000001",
		"iss": js.srv.URL + testIssuerPath,
		"aud": audienceAuthenticated,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = js.keyID

	signed, err := token.SignedString(attackerKey)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := verifier.Verify(signed); err == nil {
		t.Fatal("expected a token signed by an unknown key to be rejected")
	}
}

// TestVerifyRejectsNoneAlgorithm guards against the classic "alg: none" bypass.
func TestVerifyRejectsNoneAlgorithm(t *testing.T) {
	js := newJWKSServer(t)
	verifier := newTestVerifier(t, js)

	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "3f9a1c2e-0000-4000-8000-000000000001",
		"iss": js.srv.URL + testIssuerPath,
		"aud": audienceAuthenticated,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = js.keyID

	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := verifier.Verify(signed); err == nil {
		t.Fatal(`expected a token using "alg: none" to be rejected`)
	}
}

func TestDisplayNameFallsBackToEmailLocalPart(t *testing.T) {
	tests := []struct {
		name   string
		claims SupabaseClaims
		want   string
	}{
		{
			name:   "prefers display_name metadata",
			claims: SupabaseClaims{Email: "ada@example.com", UserMetadata: map[string]any{"display_name": "Ada"}},
			want:   "Ada",
		},
		{
			name:   "falls back to full_name",
			claims: SupabaseClaims{Email: "ada@example.com", UserMetadata: map[string]any{"full_name": "Ada Lovelace"}},
			want:   "Ada Lovelace",
		},
		{
			name:   "falls back to email local part",
			claims: SupabaseClaims{Email: "ada@example.com"},
			want:   "ada",
		},
		{
			name:   "empty when nothing available",
			claims: SupabaseClaims{},
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.claims.DisplayName(); got != tc.want {
				t.Errorf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}
