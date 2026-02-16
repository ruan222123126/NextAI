package observability

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

var publicAuthBypass = map[string]bool{
	"/healthz": true,
	"/version": true,
}

func APIKey(requiredKey string) func(http.Handler) http.Handler {
	required := strings.TrimSpace(requiredKey)
	if required == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if publicAuthBypass[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			candidate := strings.TrimSpace(r.Header.Get("X-API-Key"))
			if candidate == "" {
				authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
				if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
					candidate = strings.TrimSpace(authHeader[7:])
				}
			}
			if subtle.ConstantTimeCompare([]byte(candidate), []byte(required)) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"code":    "unauthorized",
						"message": "missing or invalid api key",
					},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
