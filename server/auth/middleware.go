package auth

import (
	"context"
	"net/http"
	"nora/util"
	"strings"
)

type contextKey string

const UserContextKey contextKey = "user"

type ContextUser struct {
	ID       string
	Username string
	IsOwner  bool
}

// BanChecker kontroluje zda je uživatel zabanovaný
type BanChecker func(userID string) bool

func Middleware(jwtSvc *JWTService, isBanned BanChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				util.Error(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			tokenStr, ok := strings.CutPrefix(authHeader, "Bearer ")
			if !ok {
				util.Error(w, http.StatusUnauthorized, "invalid authorization format")
				return
			}

			claims, err := jwtSvc.Validate(tokenStr)
			if err != nil {
				util.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}

			if isBanned != nil && isBanned(claims.UserID) {
				util.Error(w, http.StatusForbidden, "you are banned")
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, &ContextUser{
				ID:       claims.UserID,
				Username: claims.Username,
				IsOwner:  claims.IsOwner,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUser(r *http.Request) *ContextUser {
	u, _ := r.Context().Value(UserContextKey).(*ContextUser)
	return u
}
