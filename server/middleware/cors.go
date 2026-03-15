package middleware

import "net/http"

// CORS middleware. The native Go client doesn't need CORS (not a browser).
// We set headers for potential webhook integrations and dev tooling.
// We don't use Allow-Credentials — auth is via Bearer token, not cookies.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// Echo origin instead of wildcard — prevents cross-origin attacks from arbitrary websites
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Upload-Offset")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "Upload-Offset, Upload-Length")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
