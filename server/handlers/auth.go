package handlers

import (
	"encoding/hex"
	"log/slog"
	"net/http"
	"nora/auth"
	"nora/models"
	"nora/util"
	"nora/ws"
	"time"

	"github.com/google/uuid"
)

type challengeRequest struct {
	PublicKey    string `json:"public_key"`
	Username     string `json:"username,omitempty"`
	DeviceID     string `json:"device_id,omitempty"`
	HardwareHash string `json:"hardware_hash,omitempty"`
}

type challengeResponse struct {
	Nonce string `json:"nonce"`
}

type verifyRequest struct {
	PublicKey string `json:"public_key"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
}

type authResponse struct {
	AccessToken     string      `json:"access_token"`
	RefreshToken    string      `json:"refresh_token"`
	User            models.User `json:"user"`
	PendingApproval bool        `json:"pending_approval,omitempty"`
}

// Challenge creates a nonce for ed25519 challenge-response auth.
// If public_key doesn't exist and username is provided, creates a new user.
func (d *Deps) Challenge(w http.ResponseWriter, r *http.Request) {
	var req challengeRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PublicKey == "" {
		util.Error(w, http.StatusBadRequest, "public_key is required")
		return
	}

	// Verify it's a valid hex-encoded ed25519 public key (32 bytes = 64 hex chars)
	pubKeyBytes, err := hex.DecodeString(req.PublicKey)
	if err != nil || len(pubKeyBytes) != 32 {
		util.Error(w, http.StatusBadRequest, "invalid public key (expected 64 hex chars)")
		return
	}

	clientIP := util.GetClientIP(r)

	// Device ban check
	if d.DeviceBans != nil && req.DeviceID != "" {
		if d.DeviceBans.IsDeviceOrHardwareBanned(req.DeviceID, req.HardwareHash) {
			util.Error(w, http.StatusForbidden, "your device is banned from this server")
			return
		}
	}

	// Check if user exists
	var pendingUsername, pendingInvitedBy string
	existingUser, err := d.Users.GetByPublicKey(req.PublicKey)
	if err != nil {
		// User doesn't exist — validate for pending registration (user is created in Verify)
		if req.Username == "" {
			util.Error(w, http.StatusNotFound, "unknown key, provide username to register")
			return
		}

		// Check IP ban before registration
		if d.IPBans.IsBanned(clientIP) {
			util.Error(w, http.StatusForbidden, "your IP address is banned from this server")
			return
		}

		if msg := util.ValidateUsername(req.Username); msg != "" {
			util.Error(w, http.StatusBadRequest, msg)
			return
		}

		// Check registration mode
		regMode := d.RegMode
		if regMode == "" {
			if d.OpenReg {
				regMode = "open"
			} else {
				regMode = "closed"
			}
		}

		regClosed := regMode == "closed" || regMode == "approval"

		// Check invite code if registration is not open
		if regClosed {
			userCount, err := d.Users.Count()
			if err != nil {
				util.Error(w, http.StatusInternalServerError, "database error")
				return
			}
			// First user can register without an invite
			if userCount > 0 {
				inviteCode := r.URL.Query().Get("invite")
				if inviteCode == "" {
					util.Error(w, http.StatusBadRequest, "invite code required")
					return
				}
				invite, err := d.Invites.GetByCode(inviteCode)
				if err != nil {
					util.Error(w, http.StatusBadRequest, "invalid invite code")
					return
				}
				if invite.ExpiresAt != nil && invite.ExpiresAt.Before(time.Now()) {
					util.Error(w, http.StatusBadRequest, "invite code expired")
					return
				}
				if invite.MaxUses > 0 && invite.Uses >= invite.MaxUses {
					util.Error(w, http.StatusBadRequest, "invite code fully used")
					return
				}
				// Invite uses are incremented in Verify after successful user creation
				pendingInvitedBy = invite.CreatorID
			}
		}

		pendingUsername = req.Username
	} else {
		// Existing user — suspicious device detection (sent only to owner)
		if d.SecurityCfg.Device.AlertOnSuspicious && d.UserDevices != nil && req.HardwareHash != "" {
			others, _ := d.UserDevices.GetByHardwareHash(req.HardwareHash)
			for _, other := range others {
				if other.UserID != existingUser.ID && other.DeviceID != req.DeviceID {
					// Same HW hash, different user, different device ID — suspicious
					event, _ := ws.NewEvent(ws.EventDeviceSuspicious, map[string]string{
						"user_id":        existingUser.ID,
						"username":       existingUser.Username,
						"hardware_hash":  req.HardwareHash,
						"other_user_id":  other.UserID,
						"other_username": other.Username,
					})
					// Send only to owner and admins (not all users)
					owner, _ := d.Users.GetOwner()
					if owner != nil {
						d.Hub.BroadcastToUser(owner.ID, event)
					}
					break
				}
			}
		}
	}

	// Generate nonce
	nonce, err := auth.GenerateNonce()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to generate nonce")
		return
	}

	expiresAt := time.Now().Add(d.ChallengeTTL)
	if err := d.AuthChallenges.Create(req.PublicKey, nonce, expiresAt.UTC().Format(time.DateTime), pendingUsername, pendingInvitedBy, req.DeviceID, req.HardwareHash); err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to store challenge")
		return
	}

	util.JSON(w, http.StatusOK, challengeResponse{Nonce: nonce})
}

// Verify verifies the ed25519 signature of the nonce and returns tokens.
func (d *Deps) Verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PublicKey == "" || req.Nonce == "" || req.Signature == "" {
		util.Error(w, http.StatusBadRequest, "public_key, nonce and signature are required")
		return
	}

	// Load challenge
	challenge, err := d.AuthChallenges.GetByPublicKey(req.PublicKey)
	if err != nil {
		util.Error(w, http.StatusUnauthorized, "no pending challenge")
		return
	}

	// Delete challenge (one-time use)
	d.AuthChallenges.Delete(req.PublicKey)

	// Check expiration
	if challenge.ExpiresAt.Before(time.Now()) {
		util.Error(w, http.StatusUnauthorized, "challenge expired")
		return
	}

	// Check nonce
	if challenge.Nonce != req.Nonce {
		util.Error(w, http.StatusUnauthorized, "nonce mismatch")
		return
	}

	// Decode public key and signature
	pubKeyBytes, err := hex.DecodeString(req.PublicKey)
	if err != nil {
		util.Error(w, http.StatusBadRequest, "invalid public key encoding")
		return
	}

	sigBytes, err := hex.DecodeString(req.Signature)
	if err != nil {
		util.Error(w, http.StatusBadRequest, "invalid signature encoding")
		return
	}

	nonceBytes, err := hex.DecodeString(req.Nonce)
	if err != nil {
		util.Error(w, http.StatusBadRequest, "invalid nonce encoding")
		return
	}

	// Verify signature
	if !auth.VerifySignature(pubKeyBytes, nonceBytes, sigBytes) {
		util.Error(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// Ban checks BEFORE creating user (so attacker cannot create phantom records)
	verifyClientIP := util.GetClientIP(r)

	// IP ban check
	if d.IPBans.IsBanned(verifyClientIP) {
		util.Error(w, http.StatusForbidden, "your IP address is banned from this server")
		return
	}

	// Device ban check
	if d.DeviceBans != nil && challenge.DeviceID != "" {
		if d.DeviceBans.IsDeviceOrHardwareBanned(challenge.DeviceID, challenge.HardwareHash) {
			util.Error(w, http.StatusForbidden, "your device is banned from this server")
			return
		}
	}

	// Load or create user
	var pendingApproval bool
	user, err := d.Users.GetByPublicKey(req.PublicKey)
	if err != nil {
		// User doesn't exist — create from pending registration (signature was just verified)
		if challenge.Username == "" {
			util.Error(w, http.StatusUnauthorized, "unknown key")
			return
		}

		userCount, _ := d.Users.Count()
		isOwner := userCount == 0

		id, _ := uuid.NewV7()
		user = &models.User{
			ID:          id.String(),
			Username:    challenge.Username,
			DisplayName: challenge.Username,
			PublicKey:   req.PublicKey,
			IsOwner:     isOwner,
			LastIP:      verifyClientIP,
			InvitedBy:   challenge.InvitedBy,
		}

		if err := d.Users.Create(user); err != nil {
			util.Error(w, http.StatusConflict, "username or public key already taken")
			return
		}

		// Assign everyone role
		d.Roles.AssignToUser(user.ID, "everyone")

		// Atomically increment invite uses (deferred from Challenge to skip failed attempts)
		if inviteCode := r.URL.Query().Get("invite"); inviteCode != "" {
			if inv, err := d.Invites.GetByCode(inviteCode); err == nil {
				if ok, _ := d.Invites.IncrementUses(inv.ID); !ok {
					slog.Warn("invite code fully used (race)", "code", inviteCode)
				}
			}
		}

		// Save device
		if d.UserDevices != nil && challenge.DeviceID != "" {
			d.UserDevices.Upsert(user.ID, challenge.DeviceID, challenge.HardwareHash)
		}

		// Invite chain
		if d.InviteChain != nil && d.SecurityCfg.Invites.TrackInviteChain {
			inviteCode := r.URL.Query().Get("invite")
			d.InviteChain.Create(user.ID, challenge.InvitedBy, inviteCode)
		}

		// Approval mode — waiting for approval
		regMode := d.RegMode
		if regMode == "approval" && !isOwner {
			if d.Approvals != nil {
				d.Approvals.Create(user.ID, user.Username, challenge.InvitedBy)
			}
			pendingApproval = true

			// Broadcast approval.pending for admins
			event, _ := ws.NewEvent(ws.EventApprovalPending, map[string]string{
				"user_id":  user.ID,
				"username": user.Username,
			})
			d.Hub.Broadcast(event)

			slog.Info("user waiting for approval", "username", user.Username, "user_id", user.ID)
		} else {
			// Quarantine (only if not approval mode — approval adds quarantine after approval)
			if d.Quarantine != nil && d.SecurityCfg.Quarantine.Enabled && !isOwner {
				var endsAt *time.Time
				if d.SecurityCfg.Quarantine.DurationDays > 0 {
					t := time.Now().Add(time.Duration(d.SecurityCfg.Quarantine.DurationDays) * 24 * time.Hour)
					endsAt = &t
				}
				d.Quarantine.Create(user.ID, endsAt)
			}

			// Broadcast member join (only if not approval)
			joinMsg, _ := ws.NewEvent(ws.EventMemberJoin, user)
			d.Hub.Broadcast(joinMsg)
		}

		d.logAudit(user.ID, "member.join", "user", user.ID, map[string]string{"username": user.Username})
	} else {
		// Existing user — ban check
		if d.Bans.IsBanned(user.ID) {
			util.Error(w, http.StatusForbidden, "you are banned from this server")
			return
		}

		// Update device
		if d.UserDevices != nil && challenge.DeviceID != "" {
			d.UserDevices.Upsert(user.ID, challenge.DeviceID, challenge.HardwareHash)
		}
	}

	// Update last_ip
	d.Users.UpdateLastIP(user.ID, verifyClientIP)

	// Check pending approval
	if d.Approvals != nil && d.Approvals.IsPending(user.ID) {
		pendingApproval = true
	}

	// Check timeout
	timeout, _ := d.Timeouts.GetActive(user.ID)
	if timeout != nil {
		remaining := time.Until(timeout.ExpiresAt).Round(time.Second)
		util.Error(w, http.StatusForbidden, "you are timed out ("+remaining.String()+" remaining)")
		return
	}

	// Generate access token
	accessToken, err := d.JWTService.Generate(user.ID, user.Username, user.IsOwner)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Generate refresh token (returned in JSON body, not cookie)
	refreshTokenStr, tokenHash, err := auth.GenerateRefreshToken()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to generate refresh token")
		return
	}

	rtID, _ := uuid.NewV7()
	rt := &models.RefreshToken{
		ID:        rtID.String(),
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(d.RefreshTTL),
	}
	d.RefreshTokens.Create(rt)

	util.JSON(w, http.StatusOK, authResponse{
		AccessToken:     accessToken,
		RefreshToken:    refreshTokenStr,
		User:            *user,
		PendingApproval: pendingApproval,
	})
}

// Refresh renews tokens. Refresh token comes in JSON body.
func (d *Deps) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := util.DecodeJSON(r, &req); err != nil {
		util.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RefreshToken == "" {
		util.Error(w, http.StatusUnauthorized, "no refresh token")
		return
	}

	tokenHash := auth.HashToken(req.RefreshToken)
	rt, err := d.RefreshTokens.GetByHash(tokenHash)
	if err != nil {
		util.Error(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	if rt.ExpiresAt.Before(time.Now()) {
		d.RefreshTokens.Delete(rt.ID)
		util.Error(w, http.StatusUnauthorized, "refresh token expired")
		return
	}

	// Token rotation
	d.RefreshTokens.Delete(rt.ID)

	// Check ban during refresh
	if d.Bans.IsBanned(rt.UserID) {
		util.Error(w, http.StatusForbidden, "you are banned from this server")
		return
	}

	user, err := d.Users.GetByID(rt.UserID)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "user not found")
		return
	}

	accessToken, err := d.JWTService.Generate(user.ID, user.Username, user.IsOwner)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	newRefreshToken, newTokenHash, err := auth.GenerateRefreshToken()
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to generate refresh token")
		return
	}

	rtID, _ := uuid.NewV7()
	newRT := &models.RefreshToken{
		ID:        rtID.String(),
		UserID:    user.ID,
		TokenHash: newTokenHash,
		ExpiresAt: time.Now().Add(d.RefreshTTL),
	}
	d.RefreshTokens.Create(newRT)

	util.JSON(w, http.StatusOK, authResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
		User:         *user,
	})
}

func (d *Deps) Logout(w http.ResponseWriter, r *http.Request) {
	// Get user ID from JWT (logout is behind auth middleware)
	user := auth.GetUser(r)

	// Delete ALL user's refresh tokens (not just the specific one)
	if user != nil {
		d.RefreshTokens.DeleteByUserID(user.ID)
	}

	// Disconnect ALL user's WS sessions and broadcast offline
	if user != nil {
		d.Hub.DisconnectUser(user.ID)
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
