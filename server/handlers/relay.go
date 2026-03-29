package handlers

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"

	"github.com/google/uuid"
)

// RelayEvent handles cross-server message/call relay from unauthenticated clients.
// The sender proves identity via ed25519 signature.
// The recipient must have the sender in their friend list (anti-spam).
func (d *Deps) RelayEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type          string `json:"type"`           // "dm" or "call.*"
		ToPublicKey   string `json:"to_public_key"`
		FromPublicKey string `json:"from_public_key"`
		Signature     string `json:"signature"` // ed25519 sig of payload
		// DM fields
		EncryptedContent string `json:"encrypted_content,omitempty"`
		ReplyToID        string `json:"reply_to_id,omitempty"`
		// Call fields
		Payload json.RawMessage `json:"payload,omitempty"` // call event payload (sdp, candidate, etc.)
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ToPublicKey == "" || req.FromPublicKey == "" {
		util.Error(w, http.StatusBadRequest, "to_public_key and from_public_key required")
		return
	}

	// Find recipient by public key
	recipient, err := d.Users.GetByPublicKey(req.ToPublicKey)
	if err != nil {
		util.Error(w, http.StatusNotFound, "recipient not found on this server")
		return
	}

	// Verify ed25519 signature
	pubKeyBytes, err := hex.DecodeString(req.FromPublicKey)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		util.Error(w, http.StatusBadRequest, "invalid from_public_key")
		return
	}

	// Sign the type + encrypted_content (or payload for calls)
	var signedData []byte
	if req.Type == "dm" {
		signedData = []byte(req.Type + ":" + req.ToPublicKey + ":" + req.EncryptedContent)
	} else {
		signedData = []byte(req.Type + ":" + req.ToPublicKey + ":" + string(req.Payload))
	}
	sigBytes, err := hex.DecodeString(req.Signature)
	if err != nil {
		util.Error(w, http.StatusBadRequest, "invalid signature format")
		return
	}
	if !ed25519.Verify(pubKeyBytes, signedData, sigBytes) {
		util.Error(w, http.StatusForbidden, "signature verification failed")
		return
	}

	// Anti-spam: recipient must have sender in friend list
	// Find sender on this server (might not exist — check by public key in friends)
	senderUser, _ := d.Users.GetByPublicKey(req.FromPublicKey)
	if senderUser != nil {
		areFriends, _ := d.Friends.AreFriends(recipient.ID, senderUser.ID)
		if !areFriends {
			util.Error(w, http.StatusForbidden, "recipient has not friended the sender")
			return
		}
	}
	// If sender doesn't exist on this server, we can't verify friendship server-side.
	// The recipient's client will verify via contacts DB.

	if len(req.EncryptedContent) > 16000 {
		util.Error(w, http.StatusBadRequest, "encrypted_content too large")
		return
	}
	if len(req.Payload) > 50000 {
		util.Error(w, http.StatusBadRequest, "payload too large")
		return
	}

	switch {
	case req.Type == "dm":
		d.handleRelayDM(w, recipient, req.FromPublicKey, req.EncryptedContent, req.ReplyToID)
	case len(req.Type) >= 5 && req.Type[:5] == "call.":
		d.handleRelayCall(w, recipient, req.FromPublicKey, req.Type, req.Payload)
	default:
		util.Error(w, http.StatusBadRequest, "unsupported relay type")
	}
}

func (d *Deps) handleRelayDM(w http.ResponseWriter, recipient *models.User, fromPubKey, encrypted, replyToID string) {
	if encrypted == "" {
		util.Error(w, http.StatusBadRequest, "encrypted_content required for DM relay")
		return
	}

	// Generate message ID
	msgID, _ := uuid.NewV7()
	now := time.Now().UTC()

	// Broadcast DM to recipient via WS (no server-side storage for relay messages)
	event, _ := ws.NewEvent(ws.EventDMMessage, map[string]interface{}{
		"id":                msgID.String(),
		"conversation_id":   "relay:" + fromPubKey, // virtual conversation
		"sender_id":         "relay:" + fromPubKey,
		"sender_public_key": fromPubKey,
		"encrypted_content": encrypted,
		"reply_to_id":       replyToID,
		"created_at":        now,
		"is_relay":          true,
	})
	d.Hub.BroadcastToUser(recipient.ID, event)

	util.JSON(w, http.StatusOK, map[string]string{
		"message_id": msgID.String(),
		"status":     "delivered",
	})
}

func (d *Deps) handleRelayCall(w http.ResponseWriter, recipient *models.User, fromPubKey, eventType string, payload json.RawMessage) {
	// Build call event with recipient's user ID as "to" and sender public key as "from"
	var payloadMap map[string]interface{}
	if payload != nil {
		json.Unmarshal(payload, &payloadMap)
	}
	if payloadMap == nil {
		payloadMap = make(map[string]interface{})
	}
	payloadMap["to"] = recipient.ID
	payloadMap["from"] = "relay:" + fromPubKey
	payloadMap["from_public_key"] = fromPubKey

	wsEventType := ws.EventType(eventType)
	event, _ := ws.NewEvent(wsEventType, payloadMap)
	d.Hub.BroadcastToUser(recipient.ID, event)

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
