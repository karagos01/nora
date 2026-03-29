package ui

import (
	"crypto/ed25519"
	"encoding/hex"
	"nora-client/api"
	"sort"
)

// UnifiedFriend represents a friend merged across all servers.
type UnifiedFriend struct {
	PublicKey string
	Name      string // resolved from contacts (custom > auto > key)
	AvatarURL string
	Online    bool // online on ANY server
}

// UnifiedDM represents a DM conversation merged across servers by peer PublicKey.
type UnifiedDM struct {
	PeerPublicKey string
	PeerName      string
	PeerAvatar    string
	Online        bool
	TotalUnread   int
	BestServerIdx int    // server index for opening (-1 = relay only)
	BestConvID    string // conversation ID on that server ("" = needs creation)
}

// IsOnlineAnywhere checks if a public key is online on any connected server.
// Caller must hold a.mu (at least RLock).
func (a *App) IsOnlineAnywhere(publicKey string) bool {
	for _, conn := range a.Servers {
		for _, u := range conn.Users {
			if u.PublicKey == publicKey && conn.OnlineUsers[u.ID] {
				return true
			}
		}
	}
	return false
}

// FindUserAcrossServers finds an api.User by PublicKey on any connected server.
// Returns the user and server index. Caller must hold a.mu (at least RLock).
func (a *App) FindUserAcrossServers(publicKey string) (*api.User, int) {
	for srvIdx, conn := range a.Servers {
		for i := range conn.Users {
			if conn.Users[i].PublicKey == publicKey {
				return &conn.Users[i], srvIdx
			}
		}
	}
	return nil, -1
}

// FindBestDMServer finds the best server for a DM conversation with a peer.
// Priority: 1) active server with existing DM, 2) any server with existing DM,
// 3) any server where both exist (for creating DM).
// Caller must hold a.mu (at least RLock).
func (a *App) FindBestDMServer(peerPubKey string) (serverIdx int, convID string, found bool) {
	// Pass 1: active server with existing DM
	if a.ActiveServer >= 0 && a.ActiveServer < len(a.Servers) {
		conn := a.Servers[a.ActiveServer]
		if cid := findDMConvByPubKey(conn, peerPubKey); cid != "" {
			return a.ActiveServer, cid, true
		}
	}

	// Pass 2: any server with existing DM
	for i, conn := range a.Servers {
		if cid := findDMConvByPubKey(conn, peerPubKey); cid != "" {
			return i, cid, true
		}
	}

	// Pass 3: any server where both users exist (for creating new DM)
	for i, conn := range a.Servers {
		peerExists := false
		for _, u := range conn.Users {
			if u.PublicKey == peerPubKey {
				peerExists = true
				break
			}
		}
		if peerExists {
			return i, "", true
		}
	}

	return -1, "", false
}

// findDMConvByPubKey finds a DM conversation ID with a peer on a specific server.
func findDMConvByPubKey(conn *ServerConnection, peerPubKey string) string {
	for _, conv := range conn.DMConversations {
		for _, p := range conv.Participants {
			if p.PublicKey == peerPubKey && p.UserID != conn.UserID {
				return conv.ID
			}
		}
	}
	return ""
}

// GetUnifiedFriends returns friends from contacts DB with online status from all servers.
func (a *App) GetUnifiedFriends() []UnifiedFriend {
	if a.Contacts == nil {
		return nil
	}

	friends := a.Contacts.GetFriends()
	if len(friends) == 0 {
		return nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]UnifiedFriend, 0, len(friends))
	for _, ct := range friends {
		if ct.Blocked {
			continue
		}
		name := ct.CustomName
		if name == "" {
			name = ct.AutoName
		}
		if name == "" {
			name = ShortenKey(ct.PublicKey)
		}

		uf := UnifiedFriend{
			PublicKey: ct.PublicKey,
			Name:      name,
			Online:    a.IsOnlineAnywhere(ct.PublicKey),
		}

		// Find avatar from any server
		if user, _ := a.FindUserAcrossServers(ct.PublicKey); user != nil {
			uf.AvatarURL = user.AvatarURL
			// Update name from server if no custom name
			if ct.CustomName == "" {
				if user.DisplayName != "" {
					uf.Name = user.DisplayName
				} else if user.Username != "" {
					uf.Name = user.Username
				}
			}
		}

		result = append(result, uf)
	}

	// Sort: online first, then alphabetical
	sort.Slice(result, func(i, j int) bool {
		if result[i].Online != result[j].Online {
			return result[i].Online
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// GetUnifiedDMs returns DM conversations merged across all servers by peer PublicKey.
func (a *App) GetUnifiedDMs() []UnifiedDM {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Merge by peer public key
	dmMap := make(map[string]*UnifiedDM)
	for srvIdx, conn := range a.Servers {
		for _, conv := range conn.DMConversations {
			// Find peer
			var peerPubKey, peerName, peerAvatar string
			for _, p := range conv.Participants {
				if p.UserID != conn.UserID {
					peerPubKey = p.PublicKey
					// Resolve name
					for _, u := range conn.Users {
						if u.ID == p.UserID {
							peerName = u.DisplayName
							if peerName == "" {
								peerName = u.Username
							}
							peerAvatar = u.AvatarURL
							break
						}
					}
					break
				}
			}
			if peerPubKey == "" {
				continue
			}

			existing, ok := dmMap[peerPubKey]
			if !ok {
				dmMap[peerPubKey] = &UnifiedDM{
					PeerPublicKey: peerPubKey,
					PeerName:      peerName,
					PeerAvatar:    peerAvatar,
					TotalUnread:   conn.UnreadDMCount[conv.ID],
					BestServerIdx: srvIdx,
					BestConvID:    conv.ID,
				}
			} else {
				existing.TotalUnread += conn.UnreadDMCount[conv.ID]
				if peerName != "" && existing.PeerName == "" {
					existing.PeerName = peerName
				}
				if peerAvatar != "" && existing.PeerAvatar == "" {
					existing.PeerAvatar = peerAvatar
				}
				// Prefer active server
				if srvIdx == a.ActiveServer {
					existing.BestServerIdx = srvIdx
					existing.BestConvID = conv.ID
				}
			}
		}
	}

	// Check online status
	result := make([]UnifiedDM, 0, len(dmMap))
	for _, dm := range dmMap {
		dm.Online = a.IsOnlineAnywhere(dm.PeerPublicKey)

		// Resolve name from contacts if available
		if a.Contacts != nil {
			ct := a.Contacts.GetContact(dm.PeerPublicKey)
			if ct != nil && ct.CustomName != "" {
				dm.PeerName = ct.CustomName
			}
		}

		result = append(result, *dm)
	}

	// Add relay conversations from local DM history
	if a.DMHistory != nil {
		relayConvs := a.DMHistory.GetRelayConversations()
		for _, rc := range relayConvs {
			if _, exists := dmMap[rc.PeerPublicKey]; exists {
				continue // already have this peer from a server
			}
			peerName := rc.PeerPublicKey[:8] + "..."
			if a.Contacts != nil {
				ct := a.Contacts.GetContact(rc.PeerPublicKey)
				if ct != nil {
					if ct.CustomName != "" {
						peerName = ct.CustomName
					} else if ct.AutoName != "" {
						peerName = ct.AutoName
					}
				}
			}
			result = append(result, UnifiedDM{
				PeerPublicKey: rc.PeerPublicKey,
				PeerName:      peerName,
				Online:        a.IsOnlineAnywhere(rc.PeerPublicKey),
				TotalUnread:   rc.UnreadCount,
				BestServerIdx: -1,
				BestConvID:    "relay:" + rc.PeerPublicKey,
			})
		}
	}

	// Sort: unread first, then online, then alphabetical
	sort.Slice(result, func(i, j int) bool {
		if (result[i].TotalUnread > 0) != (result[j].TotalUnread > 0) {
			return result[i].TotalUnread > 0
		}
		if result[i].Online != result[j].Online {
			return result[i].Online
		}
		return result[i].PeerName < result[j].PeerName
	})

	return result
}

// SignMessage signs data with the user's ed25519 private key and returns hex-encoded signature.
func (a *App) SignMessage(data []byte) string {
	keyBytes, err := hex.DecodeString(a.SecretKey)
	if err != nil || len(keyBytes) != ed25519.PrivateKeySize {
		return ""
	}
	sig := ed25519.Sign(keyBytes, data)
	return hex.EncodeToString(sig)
}

// UnifiedGroup represents a group from any server.
type UnifiedGroup struct {
	GroupID   string
	Name      string
	ServerIdx int
	Unread    bool
}

// GetUnifiedGroups returns groups from ALL connected servers.
func (a *App) GetUnifiedGroups() []UnifiedGroup {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var result []UnifiedGroup
	for srvIdx, conn := range a.Servers {
		for _, g := range conn.Groups {
			result = append(result, UnifiedGroup{
				GroupID:   g.ID,
				Name:      g.Name,
				ServerIdx: srvIdx,
				Unread:    conn.UnreadGroups[g.ID],
			})
		}
	}
	return result
}

// SelectUnifiedGroup switches to the correct server and selects a group.
func (a *App) SelectUnifiedGroup(groupID string, serverIdx int) {
	a.mu.Lock()
	if serverIdx >= 0 && serverIdx < len(a.Servers) {
		a.ActiveServer = serverIdx
		a.Servers[serverIdx].ActiveGroupID = groupID
		a.Mode = ViewGroup
	}
	a.mu.Unlock()
	a.Window.Invalidate()
}

// SyncFriendsToContacts syncs friend relationships from all servers to contacts DB (two-way).
func (a *App) SyncFriendsToContacts() {
	if a.Contacts == nil {
		return
	}
	a.mu.RLock()
	serverFriendKeys := make(map[string]bool)
	for _, conn := range a.Servers {
		for _, f := range conn.Friends {
			if f.PublicKey != "" {
				serverFriendKeys[f.PublicKey] = true
			}
		}
	}
	a.mu.RUnlock()

	// Add new friends
	for key := range serverFriendKeys {
		a.Contacts.SetFriend(key, true)
	}

	// Remove friends no longer on any server
	existingFriends := a.Contacts.GetFriends()
	for _, ct := range existingFriends {
		if !serverFriendKeys[ct.PublicKey] {
			a.Contacts.SetFriend(ct.PublicKey, false)
		}
	}
}
