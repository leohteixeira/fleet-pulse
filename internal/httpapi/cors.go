package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

func withSameOriginCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		write := r.Method == http.MethodPost || r.Method == http.MethodOptions
		if !write {
			next.ServeHTTP(w, r)
			return
		}

		if !originMatchesHost(origin, r.Host) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Idempotency-Key, Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host == host
}
