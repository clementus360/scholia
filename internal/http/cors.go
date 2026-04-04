package http

import (
	"net/http"
	"os"
	"strings"
)

var defaultCORSOrigins = []string{
	"http://localhost:3000",
	"http://localhost:3001",
	"http://127.0.0.1:3000",
	"http://localhost:4173",
	"http://127.0.0.1:4173",
	"http://localhost:5173",
	"http://127.0.0.1:5173",
	"http://localhost:8080",
	"http://127.0.0.1:8080",
	"https://scholia-web-coral.vercel.app",
}

func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	origins := allowedOrigins
	if len(origins) == 0 {
		origins = parseCORSOrigins(os.Getenv("SCHOLIA_CORS_ORIGINS"))
	}
	if len(origins) == 0 {
		origins = defaultCORSOrigins
	}

	allowAll := false
	originSet := map[string]struct{}{}
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAll = true
			break
		}
		originSet[origin] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				if allowAll || isAllowedOrigin(originSet, origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Credentials", "false")
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, Origin, X-API-Key, X-Requested-With")
			w.Header().Set("Access-Control-Max-Age", "600")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseCORSOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

func isAllowedOrigin(originSet map[string]struct{}, origin string) bool {
	_, ok := originSet[origin]
	return ok
}
