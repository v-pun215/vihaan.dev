package main

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// noindexAdminRoutes sets X-Robots-Tag so crawlers that support it do not index admin UI or admin APIs.
func noindexAdminRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/admin" || strings.HasPrefix(path, "/admin/") || strings.HasPrefix(path, "/api/admin") {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		next.ServeHTTP(w, r)
	})
}

// corsOriginAllowed returns whether the browser Origin may receive credentialed CORS responses.
// If CORS_ALLOWED_ORIGINS is set (comma-separated), only those exact origins match.
// Otherwise the request host must match the Origin URL host (typical same-site deployment).
func corsOriginAllowed(origin string, r *http.Request) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	env := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if env != "" {
		for _, o := range strings.Split(env, ",") {
			if strings.TrimSpace(o) == origin {
				return true
			}
		}
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if corsOriginAllowed(origin, r) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
