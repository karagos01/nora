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

// Challenge vytvoří nonce pro ed25519 challenge-response auth.
// Pokud public_key neexistuje a je zadán username, vytvoří nového uživatele.
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

	// Ověřit že je to validní hex-encoded ed25519 public key (32 bajtů = 64 hex znaků)
	pubKeyBytes, err := hex.DecodeString(req.PublicKey)
	if err != nil || len(pubKeyBytes) != 32 {
		util.Error(w, http.StatusBadRequest, "invalid public key (expected 64 hex chars)")
		return
	}

	clientIP := util.GetClientIP(r)

	// Device ban kontrola
	if d.DeviceBans != nil && req.DeviceID != "" {
		if d.DeviceBans.IsDeviceOrHardwareBanned(req.DeviceID, req.HardwareHash) {
			util.Error(w, http.StatusForbidden, "your device is banned from this server")
			return
		}
	}

	// Zjistit jestli uživatel existuje
	var pendingUsername, pendingInvitedBy string
	existingUser, err := d.Users.GetByPublicKey(req.PublicKey)
	if err != nil {
		// Uživatel neexistuje — validace pro pending registraci (uživatel se vytvoří až v Verify)
		if req.Username == "" {
			util.Error(w, http.StatusNotFound, "unknown key, provide username to register")
			return
		}

		// Kontrola IP banu před registrací
		if d.IPBans.IsBanned(clientIP) {
			util.Error(w, http.StatusForbidden, "your IP address is banned from this server")
			return
		}

		if msg := util.ValidateUsername(req.Username); msg != "" {
			util.Error(w, http.StatusBadRequest, msg)
			return
		}

		// Kontrola registration mode
		regMode := d.RegMode
		if regMode == "" {
			if d.OpenReg {
				regMode = "open"
			} else {
				regMode = "closed"
			}
		}

		regClosed := regMode == "closed" || regMode == "approval"

		// Kontrola invite kódu pokud registrace není otevřená
		if regClosed {
			userCount, err := d.Users.Count()
			if err != nil {
				util.Error(w, http.StatusInternalServerError, "database error")
				return
			}
			// První uživatel se může registrovat bez invite
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
				// Invite uses se inkrementují až v Verify po úspěšném vytvoření uživatele
				pendingInvitedBy = invite.CreatorID
			}
		}

		pendingUsername = req.Username
	} else {
		// Existující uživatel — detekce podezřelého zařízení (posíláme jen ownerovi)
		if d.SecurityCfg.Device.AlertOnSuspicious && d.UserDevices != nil && req.HardwareHash != "" {
			others, _ := d.UserDevices.GetByHardwareHash(req.HardwareHash)
			for _, other := range others {
				if other.UserID != existingUser.ID && other.DeviceID != req.DeviceID {
					// Stejný HW hash, jiný uživatel, jiný device ID → podezřelé
					event, _ := ws.NewEvent(ws.EventDeviceSuspicious, map[string]string{
						"user_id":        existingUser.ID,
						"username":       existingUser.Username,
						"hardware_hash":  req.HardwareHash,
						"other_user_id":  other.UserID,
						"other_username": other.Username,
					})
					// Poslat jen ownerovi a adminům (ne všem uživatelům)
					owner, _ := d.Users.GetOwner()
					if owner != nil {
						d.Hub.BroadcastToUser(owner.ID, event)
					}
					break
				}
			}
		}
	}

	// Generovat nonce
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

// Verify ověří ed25519 podpis nonce a vrátí tokeny.
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

	// Načíst challenge
	challenge, err := d.AuthChallenges.GetByPublicKey(req.PublicKey)
	if err != nil {
		util.Error(w, http.StatusUnauthorized, "no pending challenge")
		return
	}

	// Smazat challenge (one-time use)
	d.AuthChallenges.Delete(req.PublicKey)

	// Kontrola expirace
	if challenge.ExpiresAt.Before(time.Now()) {
		util.Error(w, http.StatusUnauthorized, "challenge expired")
		return
	}

	// Kontrola nonce
	if challenge.Nonce != req.Nonce {
		util.Error(w, http.StatusUnauthorized, "nonce mismatch")
		return
	}

	// Dekódovat public key a signature
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

	// Ověřit podpis
	if !auth.VerifySignature(pubKeyBytes, nonceBytes, sigBytes) {
		util.Error(w, http.StatusUnauthorized, "invalid signature")
		return
	}

	// Kontroly banu PŘED vytvořením uživatele (aby útočník nemohl vytvářet phantom záznamy)
	verifyClientIP := util.GetClientIP(r)

	// IP ban kontrola
	if d.IPBans.IsBanned(verifyClientIP) {
		util.Error(w, http.StatusForbidden, "your IP address is banned from this server")
		return
	}

	// Device ban kontrola
	if d.DeviceBans != nil && challenge.DeviceID != "" {
		if d.DeviceBans.IsDeviceOrHardwareBanned(challenge.DeviceID, challenge.HardwareHash) {
			util.Error(w, http.StatusForbidden, "your device is banned from this server")
			return
		}
	}

	// Načíst nebo vytvořit uživatele
	var pendingApproval bool
	user, err := d.Users.GetByPublicKey(req.PublicKey)
	if err != nil {
		// Uživatel neexistuje — vytvořit z pending registrace (podpis byl právě ověřen)
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

		// Přiřadit everyone roli
		d.Roles.AssignToUser(user.ID, "everyone")

		// Inkrementovat invite uses (odloženo z Challenge aby se nepočítaly neúspěšné pokusy)
		if inviteCode := r.URL.Query().Get("invite"); inviteCode != "" {
			if inv, err := d.Invites.GetByCode(inviteCode); err == nil {
				d.Invites.IncrementUses(inv.ID)
			}
		}

		// Uložit zařízení
		if d.UserDevices != nil && challenge.DeviceID != "" {
			d.UserDevices.Upsert(user.ID, challenge.DeviceID, challenge.HardwareHash)
		}

		// Invite chain
		if d.InviteChain != nil && d.SecurityCfg.Invites.TrackInviteChain {
			inviteCode := r.URL.Query().Get("invite")
			d.InviteChain.Create(user.ID, challenge.InvitedBy, inviteCode)
		}

		// Approval mode — čeká na schválení
		regMode := d.RegMode
		if regMode == "approval" && !isOwner {
			if d.Approvals != nil {
				d.Approvals.Create(user.ID, user.Username, challenge.InvitedBy)
			}
			pendingApproval = true

			// Broadcast approval.pending pro adminy
			event, _ := ws.NewEvent(ws.EventApprovalPending, map[string]string{
				"user_id":  user.ID,
				"username": user.Username,
			})
			d.Hub.Broadcast(event)

			slog.Info("uživatel čeká na schválení", "username", user.Username, "user_id", user.ID)
		} else {
			// Karanténa (jen pokud ne approval mode — approval přidá karanténu po schválení)
			if d.Quarantine != nil && d.SecurityCfg.Quarantine.Enabled && !isOwner {
				var endsAt *time.Time
				if d.SecurityCfg.Quarantine.DurationDays > 0 {
					t := time.Now().Add(time.Duration(d.SecurityCfg.Quarantine.DurationDays) * 24 * time.Hour)
					endsAt = &t
				}
				d.Quarantine.Create(user.ID, endsAt)
			}

			// Broadcast member join (jen pokud ne approval)
			joinMsg, _ := ws.NewEvent(ws.EventMemberJoin, user)
			d.Hub.Broadcast(joinMsg)
		}

		d.logAudit(user.ID, "member.join", "user", user.ID, map[string]string{"username": user.Username})
	} else {
		// Existující uživatel — kontrola banu
		if d.Bans.IsBanned(user.ID) {
			util.Error(w, http.StatusForbidden, "you are banned from this server")
			return
		}

		// Update device
		if d.UserDevices != nil && challenge.DeviceID != "" {
			d.UserDevices.Upsert(user.ID, challenge.DeviceID, challenge.HardwareHash)
		}
	}

	// Aktualizovat last_ip
	d.Users.UpdateLastIP(user.ID, verifyClientIP)

	// Kontrola pending approval
	if d.Approvals != nil && d.Approvals.IsPending(user.ID) {
		pendingApproval = true
	}

	// Kontrola timeoutu
	timeout, _ := d.Timeouts.GetActive(user.ID)
	if timeout != nil {
		remaining := time.Until(timeout.ExpiresAt).Round(time.Second)
		util.Error(w, http.StatusForbidden, "you are timed out ("+remaining.String()+" remaining)")
		return
	}

	// Generovat access token
	accessToken, err := d.JWTService.Generate(user.ID, user.Username, user.IsOwner)
	if err != nil {
		util.Error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Generovat refresh token (vrátíme v JSON body, ne cookie)
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

// Refresh obnovuje tokeny. Refresh token přijde v JSON body.
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

	// Kontrola banu při refresh
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
	// Zjistit user ID z JWT (logout je za auth middleware)
	user := auth.GetUser(r)

	// Smazat VŠECHNY refresh tokeny uživatele (ne jen ten konkrétní)
	if user != nil {
		d.RefreshTokens.DeleteByUserID(user.ID)
	}

	// Odpojit VŠECHNY WS sessions uživatele a broadcastnout offline
	if user != nil {
		d.Hub.DisconnectUser(user.ID)
	}

	util.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
