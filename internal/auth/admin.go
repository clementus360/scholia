package auth

import (
	"net/http"
	"os"
	"strings"

	httputil "github.com/clementus360/scholia/internal/http"
)

func (m *Manager) RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if m == nil || m.db == nil {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httputil.Error(w, "Authentication not configured", http.StatusServiceUnavailable)
			})
		}

		adminSubject := strings.TrimSpace(os.Getenv("SCHOLIA_ADMIN_SUBJECT"))
		adminUserID := strings.TrimSpace(os.Getenv("SCHOLIA_ADMIN_USER_ID"))

		if adminSubject == "" && adminUserID == "" {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httputil.Error(w, "Admin access not configured", http.StatusServiceUnavailable)
			})
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := m.authenticate(r)
			if !ok {
				httputil.Error(w, "Missing or invalid API key", http.StatusUnauthorized)
				return
			}

			if !principalIsAdmin(principal, adminSubject, adminUserID) {
				httputil.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			r = WithPrincipal(r, principal)
			next.ServeHTTP(w, r)
		})
	}
}

func principalIsAdmin(principal Principal, adminSubject, adminUserID string) bool {
	if strings.TrimSpace(adminUserID) != "" && strings.TrimSpace(principal.UserID) == strings.TrimSpace(adminUserID) {
		return true
	}
	if strings.TrimSpace(adminSubject) != "" && strings.TrimSpace(principal.Subject) == strings.TrimSpace(adminSubject) {
		return true
	}
	return false
}
