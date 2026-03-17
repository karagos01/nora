package middleware

import "net/http"

// BodyLimit restricts the maximum request body size to 2MB.
func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024) // 2MB
		next.ServeHTTP(w, r)
	})
}
