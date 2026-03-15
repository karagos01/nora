package ws

import (
	"log/slog"
	"net/http"
	"nora/auth"
	"strings"

	"github.com/coder/websocket"
)

func UpgradeHandler(hub *Hub, jwtSvc *auth.JWTService, isBanned auth.BanChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Token from Authorization header (preferred) or query parameter (fallback)
		token := extractBearerToken(r)
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}

		claims, err := jwtSvc.Validate(token)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		if isBanned != nil && isBanned(claims.UserID) {
			http.Error(w, "banned", http.StatusForbidden)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true, // CORS pro development
		})
		if err != nil {
			slog.Error("ws: upgrade failed", "error", err)
			return
		}

		client := NewClient(hub, conn, claims.UserID)

		// Synchronous registration — client is immediately in the hub + receives broadcasts
		onlineIDs := hub.RegisterSync(client)

		initPayload := map[string]any{
			"online": onlineIDs,
		}
		if statuses := hub.GetUserStatuses(onlineIDs); statuses != nil {
			initPayload["statuses"] = statuses
		}
		initMsg, _ := NewEvent(EventPresenceUpdate, initPayload)
		client.send <- initMsg

		// OnConnect callback — IP refresh for firewall
		if hub.onConnectFn != nil {
			go hub.onConnectFn(claims.UserID, r)
		}

		go client.WritePump()
		go client.ReadPump()
	}
}

// extractBearerToken extracts JWT token from the Authorization header or query parameter
func extractBearerToken(r *http.Request) string {
	if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		return ah[7:]
	}
	return r.URL.Query().Get("token")
}
