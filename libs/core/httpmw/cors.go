// Package httpmw holds the small HTTP middlewares the domain services share.
package httpmw

import (
	"net/http"
	"strings"
)

// CORS allows the storefront and ops console, which run on their own origins in
// development, to call these APIs from a browser.
//
// allowed is a comma-separated origin list. Empty disables CORS entirely, which
// is the right default behind a gateway that fronts every service on one origin;
// "*" allows any origin and is for local development only. Credentials are never
// allowed: these APIs authenticate with tokens in the body (the rescue consent
// token), not with cookies, so there is nothing for a hostile origin to ride on.
func CORS(allowed string, next http.Handler) http.Handler {
	if strings.TrimSpace(allowed) == "" {
		return next
	}
	origins := map[string]bool{}
	any := false
	for _, o := range strings.Split(allowed, ",") {
		o = strings.TrimSpace(o)
		switch {
		case o == "":
		case o == "*":
			any = true
		default:
			origins[strings.ToLower(o)] = true
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (any || origins[strings.ToLower(origin)]) {
			if any {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				// Echo the matched origin and vary on it, so a shared cache cannot
				// hand one origin's response to another.
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
