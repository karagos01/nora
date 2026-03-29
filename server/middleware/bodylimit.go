package middleware

import "net/http"

// BodyLimit restricts the maximum request body size.
// File sync and uploads may need larger bodies; specific handlers set their own limits.
func BodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 100*1024*1024) // 100MB (handlers may set lower)
		next.ServeHTTP(w, r)
	})
}
