package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nora-client/api"
	"nora-client/crypto"
	"nora-client/store"
)

func (a *App) handleWSEvents(conn *ServerConnection) {
	for {
		select {
		case <-conn.Ctx.Done():
			return
		case ev := <-conn.WS.Events():
			a.processWSEvent(conn, ev)
		}
	}
}

func (a *App) processWSEvent(conn *ServerConnection, ev api.WSEvent) {
	switch ev.Type {
	case "message.new":
		var msg api.Message
		if json.Unmarshal(ev.Payload, &msg) == nil {
			// Cross-server blocklist: ignore messages from blocked users
			if msg.Author != nil && a.IsBlockedKey(msg.Author.PublicKey) {
				return
			}
			a.mu.Lock()
			var notifyChannel bool
			var notifSender, notifChName, notifContent string
			var notifChID string
			viewing := msg.ChannelID == conn.ActiveChannelID && a.Mode == ViewChannels
			if msg.ChannelID == conn.ActiveChannelID {
				conn.Messages = append(conn.Messages, msg)
			}
			if !viewing {
				conn.UnreadCount[msg.ChannelID]++
				if msg.UserID != conn.UserID {
					notifyChannel = true
					notifSender = a.ResolveNameByID(conn, msg.UserID)
					notifChName = findChannelName(conn, msg.ChannelID)
					notifContent = msg.Content
					notifChID = msg.ChannelID
				}
			}
			// Clear typing for this user in the given channel
			if conn.ChannelTyping[msg.ChannelID] != nil {
				delete(conn.ChannelTyping[msg.ChannelID], msg.UserID)
			}

			// Thread: add reply to the open thread
			if a.ThreadView.Visible && msg.ReplyToID != nil && *msg.ReplyToID == a.ThreadView.ParentID {
				a.ThreadView.AddReply(msg)
			}
			// Thread: increment reply count of the parent message
			if msg.ReplyToID != nil {
				for i, m := range conn.Messages {
					if m.ID == *msg.ReplyToID {
						conn.Messages[i].ReplyCount++
						break
					}
				}
			}
			a.mu.Unlock()

			// Notify outside lock to avoid deadlock (ShouldNotify may RLock a.mu)
			if notifyChannel && a.ShouldNotify(conn, notifChID, notifContent) {
				PlayNotificationSound()
				if a.NotifMgr != nil {
					a.NotifMgr.NotifyMessage(conn.Name, "#"+notifChName, notifContent, notifSender)
				}
			}

		}

	case "message.edit":
		var payload struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			now := time.Now()
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.ID == payload.ID {
					conn.Messages[i].Content = payload.Content
					conn.Messages[i].UpdatedAt = &now
					break
				}
			}
			a.mu.Unlock()
		}

	case "message.delete":
		var payload struct{ ID string `json:"id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.ID == payload.ID {
					conn.Messages = append(conn.Messages[:i], conn.Messages[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
		}

	case "message.pin", "message.unpin":
		var payload struct {
			ID       string `json:"id"`
			IsPinned bool   `json:"is_pinned"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.ID == payload.ID {
					conn.Messages[i].IsPinned = payload.IsPinned
					break
				}
			}
			a.mu.Unlock()

			// Update cached pinned messages
			chID := conn.ActiveChannelID
			go func() {
				pins, err := conn.Client.GetPinnedMessages(chID)
				if err == nil {
					a.MsgView.pinnedMsgs = pins
					a.Window.Invalidate()
				}
			}()
		}

	case "message.hide":
		var payload struct {
			ID       string `json:"id"`
			IsHidden bool   `json:"is_hidden"`
			Content  string `json:"content"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.ID == payload.ID {
					conn.Messages[i].IsHidden = payload.IsHidden
					if !payload.IsHidden && payload.Content != "" {
						conn.Messages[i].Content = payload.Content
					}
					break
				}
			}
			a.mu.Unlock()
		}

	case "messages.hide":
		var payload struct {
			UserID string `json:"user_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.UserID == payload.UserID {
					conn.Messages[i].IsHidden = true
				}
			}
			a.mu.Unlock()
		}

	case "messages.bulk_delete":
		var payload struct {
			UserID string `json:"user_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			var filtered []api.Message
			for _, m := range conn.Messages {
				if m.UserID != payload.UserID {
					filtered = append(filtered, m)
				}
			}
			conn.Messages = filtered
			a.mu.Unlock()
		}

	case "reaction.add":
		var payload struct {
			MessageID string `json:"message_id"`
			UserID    string `json:"user_id"`
			Emoji     string `json:"emoji"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.ID == payload.MessageID {
					found := false
					for j, r := range conn.Messages[i].Reactions {
						if r.Emoji == payload.Emoji {
							conn.Messages[i].Reactions[j].Count++
							conn.Messages[i].Reactions[j].UserIDs = append(conn.Messages[i].Reactions[j].UserIDs, payload.UserID)
							found = true
							break
						}
					}
					if !found {
						conn.Messages[i].Reactions = append(conn.Messages[i].Reactions, api.Reaction{
							MessageID: payload.MessageID,
							Emoji:     payload.Emoji,
							Count:     1,
							UserIDs:   []string{payload.UserID},
						})
					}
					break
				}
			}
			a.mu.Unlock()
		}

	case "reaction.remove":
		var payload struct {
			MessageID string `json:"message_id"`
			UserID    string `json:"user_id"`
			Emoji     string `json:"emoji"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.ID == payload.MessageID {
					for j, r := range conn.Messages[i].Reactions {
						if r.Emoji == payload.Emoji {
							// Remove user from list
							for k, uid := range r.UserIDs {
								if uid == payload.UserID {
									conn.Messages[i].Reactions[j].UserIDs = append(r.UserIDs[:k], r.UserIDs[k+1:]...)
									break
								}
							}
							conn.Messages[i].Reactions[j].Count--
							if conn.Messages[i].Reactions[j].Count <= 0 {
								conn.Messages[i].Reactions = append(conn.Messages[i].Reactions[:j], conn.Messages[i].Reactions[j+1:]...)
							}
							break
						}
					}
					break
				}
			}
			a.mu.Unlock()
		}

	case "dm.message":
		var msg api.DMPendingMessage
		if json.Unmarshal(ev.Payload, &msg) == nil {
			// Skip own messages — we already added them locally when sending
			if msg.SenderID == conn.UserID {
				return
			}

			a.mu.Lock()
			// Find conversation and peer key
			var peerKey string
			convKnown := false
			for _, c := range conn.DMConversations {
				if c.ID == msg.ConversationID {
					convKnown = true
					for _, p := range c.Participants {
						if p.UserID != conn.UserID {
							peerKey = p.PublicKey
							break
						}
					}
					break
				}
			}
			secretKey := a.SecretKey

			// Cross-server blocklist: ignore DM from blocked users (known conversation)
			if convKnown && peerKey != "" && a.IsBlockedKey(peerKey) {
				a.mu.Unlock()
				return
			}

			if !convKnown {
				a.mu.Unlock()
				// New conversation — fetch from server, decrypt and save message
				go func() {
					convs, err := conn.Client.GetDMConversations()
					if err != nil {
						return
					}
					// Find peer key from newly fetched conversations
					var newPeerKey string
					for _, c := range convs {
						if c.ID == msg.ConversationID {
							for _, p := range c.Participants {
								if p.UserID != conn.UserID {
									newPeerKey = p.PublicKey
									break
								}
							}
							break
						}
					}
					// Cross-server blocklist: ignore DM from blocked users (new conversation)
					if newPeerKey != "" && a.IsBlockedKey(newPeerKey) {
						return
					}
					// Decrypt and save to local history
					if newPeerKey != "" && secretKey != "" {
						decrypted, err := crypto.DecryptDM(secretKey, newPeerKey, msg.EncryptedContent)
						if err == nil {
							msg.DecryptedContent = decrypted
							if a.DMHistory != nil {
								a.DMHistory.AddMessage(store.StoredDMMessage{
									ID:             msg.ID,
									ConversationID: msg.ConversationID,
									SenderID:       msg.SenderID,
									Content:        decrypted,
									CreatedAt:      msg.CreatedAt,
								})
								a.DMHistory.Save()
							}
						}
					}
					a.mu.Lock()
					conn.DMConversations = convs
					conn.UnreadDMCount[msg.ConversationID]++
					a.mu.Unlock()
					if a.ShouldNotify(conn, "", "") {
						PlayDMSound()
						if a.NotifMgr != nil {
							a.NotifMgr.NotifyMessage(conn.Name, "Direct Message", msg.DecryptedContent, "")
						}
					}
					a.Window.Invalidate()
				}()
				return
			}

			a.mu.Unlock()

			// Known conversation — decrypt with the correct peer key
			if peerKey != "" && secretKey != "" {
				decrypted, err := crypto.DecryptDM(secretKey, peerKey, msg.EncryptedContent)
				if err == nil {
					msg.DecryptedContent = decrypted
					if a.DMHistory != nil {
						a.DMHistory.AddMessage(store.StoredDMMessage{
							ID:             msg.ID,
							ConversationID: msg.ConversationID,
							SenderID:       msg.SenderID,
							Content:        decrypted,
							CreatedAt:      msg.CreatedAt,
						})
						a.DMHistory.Save()
					}
				} else {
					log.Printf("DM decrypt error: %v", err)
				}
			}

			a.mu.Lock()
			var notifyDM bool
			var dmSender, dmContent string
			viewingDM := msg.ConversationID == conn.ActiveDMID && a.Mode == ViewDM
			if msg.ConversationID == conn.ActiveDMID {
				conn.DMMessages = append(conn.DMMessages, msg)
			}
			if !viewingDM {
				conn.UnreadDMCount[msg.ConversationID]++
				notifyDM = true
				dmSender = a.ResolveNameByID(conn, msg.SenderID)
				dmContent = msg.DecryptedContent
			}
			a.mu.Unlock()

			// Notify outside lock to avoid deadlock (ShouldNotify may RLock a.mu)
			if notifyDM && a.ShouldNotify(conn, "", "") {
				PlayDMSound()
				if a.NotifMgr != nil {
					a.NotifMgr.NotifyMessage(conn.Name, "DM from "+dmSender, dmContent, dmSender)
				}
			}
		}

	case "channel.typing":
		// Channel typing indicator
		var payload struct {
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			if payload.UserID != conn.UserID {
				a.mu.Lock()
				if conn.ChannelTyping[payload.ChannelID] == nil {
					conn.ChannelTyping[payload.ChannelID] = make(map[string]time.Time)
				}
				conn.ChannelTyping[payload.ChannelID][payload.UserID] = time.Now()
				a.mu.Unlock()
			}
		}

	case "typing.start":
		// Legacy typing — DM typing
		var payload struct {
			UserID         string `json:"user_id"`
			ChannelID      string `json:"channel_id"`
			ConversationID string `json:"conversation_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			if payload.ConversationID != "" && payload.ConversationID == conn.ActiveDMID {
				conn.TypingDMUsers[payload.UserID] = time.Now()
			}
			a.mu.Unlock()
		}

	case "dm.delete":
		var payload struct{ ConversationID string `json:"conversation_id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, conv := range conn.DMConversations {
				if conv.ID == payload.ConversationID {
					conn.DMConversations = append(conn.DMConversations[:i], conn.DMConversations[i+1:]...)
					break
				}
			}
			if conn.ActiveDMID == payload.ConversationID {
				conn.ActiveDMID = ""
				conn.ActiveDMPeerKey = ""
				conn.DMMessages = nil
			}
			a.mu.Unlock()

			// Delete local history for this conversation
			if a.DMHistory != nil {
				a.DMHistory.DeleteConversation(payload.ConversationID)
				a.DMHistory.Save()
			}
		}

	case "presence.update":
		// Two formats:
		// 1. Initial batch: {"online": ["user_id_1", "user_id_2"], "statuses": {"uid": {"status":"away","status_text":"brb"}}}
		// 2. Individual:    {"user_id": "xxx", "status": "online"/"offline"}
		var batch struct {
			Online   []string                       `json:"online"`
			Statuses map[string]map[string]string    `json:"statuses"`
		}
		if json.Unmarshal(ev.Payload, &batch) == nil && len(batch.Online) > 0 {
			a.mu.Lock()
			for _, uid := range batch.Online {
				conn.OnlineUsers[uid] = true
			}
			for uid, info := range batch.Statuses {
				if s, ok := info["status"]; ok && s != "" {
					conn.UserStatuses[uid] = s
				}
				if t, ok := info["status_text"]; ok && t != "" {
					conn.UserStatusText[uid] = t
				}
			}
			a.mu.Unlock()
			return
		}

		var individual struct {
			UserID string `json:"user_id"`
			Status string `json:"status"`
		}
		if json.Unmarshal(ev.Payload, &individual) == nil && individual.UserID != "" {
			a.mu.Lock()
			conn.OnlineUsers[individual.UserID] = (individual.Status == "online")
			if individual.Status == "offline" {
				delete(conn.UserStatuses, individual.UserID)
				delete(conn.UserStatusText, individual.UserID)
			}
			a.mu.Unlock()
		}

	case "member.join":
		var user api.User
		if json.Unmarshal(ev.Payload, &user) == nil {
			a.mu.Lock()
			conn.Members = append(conn.Members, user)
			conn.OnlineUsers[user.ID] = true
			a.mu.Unlock()
			// Auto-create contact
			if a.Contacts != nil && user.PublicKey != "" {
				name := user.DisplayName
				if name == "" {
					name = user.Username
				}
				a.Contacts.EnsureContact(user.PublicKey, name, conn.URL, conn.Name)
			}
		}

	case "member.leave":
		var payload struct{ UserID string `json:"user_id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			conn.OnlineUsers[payload.UserID] = false
			a.mu.Unlock()
		}

	case "friend.request":
		var fr api.FriendRequest
		if json.Unmarshal(ev.Payload, &fr) == nil {
			a.mu.Lock()
			conn.FriendRequests = append(conn.FriendRequests, fr)
			a.mu.Unlock()
			PlayFriendRequestSound()
			// Desktop notification for friend request
			if a.NotifMgr != nil {
				fromName := "Someone"
				if fr.FromUser != nil {
					if fr.FromUser.DisplayName != "" {
						fromName = fr.FromUser.DisplayName
					} else {
						fromName = fr.FromUser.Username
					}
				}
				a.NotifMgr.NotifyFriendRequest(conn.Name, fromName)
			}
		}

	case "friend.request.accept", "friend.request.decline":
		var payload struct{ ID string `json:"id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, fr := range conn.FriendRequests {
				if fr.ID == payload.ID {
					conn.FriendRequests = append(conn.FriendRequests[:i], conn.FriendRequests[i+1:]...)
					break
				}
			}
			for i, fr := range conn.SentFriendRequests {
				if fr.ID == payload.ID {
					conn.SentFriendRequests = append(conn.SentFriendRequests[:i], conn.SentFriendRequests[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
		}

	case "friend.add":
		var user api.User
		if json.Unmarshal(ev.Payload, &user) == nil {
			a.mu.Lock()
			conn.Friends = append(conn.Friends, user)
			a.mu.Unlock()
			name := user.DisplayName
			if name == "" {
				name = user.Username
			}
			PlayFriendRequestSound()
			if a.NotifCenter != nil {
				a.NotifCenter.AddAlert(conn.Name, "New Friend", name+" is now your friend!")
			}
		}

	case "friend.remove":
		var payload struct{ UserID string `json:"user_id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, f := range conn.Friends {
				if f.ID == payload.UserID {
					conn.Friends = append(conn.Friends[:i], conn.Friends[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
		}

	case "server.update":
		var payload struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			IconURL     string `json:"icon_url"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			if payload.Name != "" {
				conn.Name = payload.Name
			}
			conn.Description = payload.Description
			oldIconURL := conn.IconURL
			conn.IconURL = payload.IconURL
			a.mu.Unlock()
			if payload.IconURL != "" && payload.IconURL != oldIconURL {
				go a.downloadServerIcon(conn, conn.URL+payload.IconURL)
			} else if payload.IconURL == "" {
				a.mu.Lock()
				conn.Icon = nil
				a.mu.Unlock()
			}
		}

	case "channel.create":
		var ch api.Channel
		if json.Unmarshal(ev.Payload, &ch) == nil {
			a.mu.Lock()
			dup := false
			for _, c := range conn.Channels {
				if c.ID == ch.ID {
					dup = true
					break
				}
			}
			if !dup {
				conn.Channels = append(conn.Channels, ch)
			}
			a.mu.Unlock()
		}

	case "channel.update":
		var ch api.Channel
		if json.Unmarshal(ev.Payload, &ch) == nil {
			a.mu.Lock()
			for i, c := range conn.Channels {
				if c.ID == ch.ID {
					conn.Channels[i] = ch
					break
				}
			}
			if conn.ActiveChannelID == ch.ID {
				conn.ActiveChannelName = ch.Name
			}
			a.mu.Unlock()
		}

	case "channel.delete":
		var payload struct{ ID string `json:"id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, ch := range conn.Channels {
				if ch.ID == payload.ID {
					conn.Channels = append(conn.Channels[:i], conn.Channels[i+1:]...)
					break
				}
			}
			if conn.ActiveChannelID == payload.ID {
				conn.ActiveChannelID = ""
				conn.ActiveChannelName = ""
				conn.Messages = nil
			}
			// Clean up LAN members for deleted channel (if it was a LAN channel)
			delete(conn.LANMembers, payload.ID)
			a.mu.Unlock()
		}

	case "category.create", "category.update", "category.delete":
		// Full refresh — server returns hierarchy with Children
		go func() {
			cats, err := conn.Client.GetCategories()
			if err == nil {
				a.mu.Lock()
				conn.Categories = cats
				a.mu.Unlock()
				a.Window.Invalidate()
			}
		}()

	case "group.message":
		var msg api.GroupMessage
		if json.Unmarshal(ev.Payload, &msg) == nil {
			// Don't process own messages (we already added them)
			if msg.SenderID == conn.UserID {
				return
			}

			// Try to decrypt — tries the current key, then old ones (after rotation)
			if a.GroupHistory != nil {
				allKeys := a.GroupHistory.GetAllKeys(msg.GroupID)
				if len(allKeys) > 0 {
					var decrypted string
					var decryptOK bool
					for _, k := range allKeys {
						d, err := crypto.DecryptGroupMessage(k, msg.EncryptedContent)
						if err == nil {
							decrypted = d
							decryptOK = true
							break
						}
					}
					if decryptOK {
						msg.DecryptedContent = decrypted
						storedMsg := store.StoredGroupMessage{
							ID:        msg.ID,
							GroupID:   msg.GroupID,
							SenderID:  msg.SenderID,
							Content:   decrypted,
							CreatedAt: msg.CreatedAt,
						}
						for _, att := range msg.Attachments {
							storedMsg.Attachments = append(storedMsg.Attachments, store.StoredGroupAttachment{
								Filename: att.Filename,
								URL:      att.URL,
								Size:     att.Size,
								MimeType: att.MimeType,
							})
						}
						a.GroupHistory.AddMessage(storedMsg)
						a.GroupHistory.Save()
					} else {
						log.Printf("group decrypt error: tried %d keys", len(allKeys))
						msg.DecryptedContent = "[decryption failed]"
					}
				} else {
					// No key yet — queue for retry when key arrives
					a.mu.Lock()
					conn.PendingGroupMsgs = append(conn.PendingGroupMsgs, msg)
					a.mu.Unlock()
					return
				}
			}

			a.mu.Lock()
			var notifyGroup bool
			var grpSender, grpName, grpContent string
			viewingGroup := msg.GroupID == conn.ActiveGroupID && a.Mode == ViewGroup
			if msg.GroupID == conn.ActiveGroupID {
				conn.GroupMessages = append(conn.GroupMessages, msg)
			}
			if !viewingGroup {
				conn.UnreadGroups[msg.GroupID] = true
				notifyGroup = true
				grpSender = a.ResolveNameByID(conn, msg.SenderID)
				grpName = findGroupName(conn, msg.GroupID)
				grpContent = msg.DecryptedContent
			}
			a.mu.Unlock()

			// Notify outside lock to avoid deadlock (ShouldNotify may RLock a.mu)
			if notifyGroup && a.ShouldNotify(conn, "", grpContent) {
				PlayNotificationSound()
				if a.NotifMgr != nil {
					a.NotifMgr.NotifyMessage(conn.Name, grpName, grpContent, grpSender)
				}
			}
		}

	case "group.key":
		var payload struct {
			GroupID      string `json:"group_id"`
			EncryptedKey string `json:"encrypted_key"`
			From         string `json:"from"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && a.GroupHistory != nil {
			// Find sender's public key
			senderPub := ""
			a.mu.RLock()
			for _, u := range conn.Users {
				if u.ID == payload.From {
					senderPub = u.PublicKey
					break
				}
			}
			a.mu.RUnlock()

			if senderPub != "" {
				decryptedKey, err := crypto.DecryptGroupKeyFromMember(a.SecretKey, senderPub, payload.EncryptedKey)
				if err == nil {
					a.GroupHistory.SetKey(payload.GroupID, decryptedKey)
					a.GroupHistory.Save()
					log.Printf("Received group key for %s from %s", payload.GroupID, payload.From)

					// Retry pending messages for this group
					a.mu.Lock()
					var remaining []api.GroupMessage
					for _, pendMsg := range conn.PendingGroupMsgs {
						if pendMsg.GroupID != payload.GroupID {
							remaining = append(remaining, pendMsg)
							continue
						}
						d, dErr := crypto.DecryptGroupMessage(decryptedKey, pendMsg.EncryptedContent)
						if dErr == nil {
							pendMsg.DecryptedContent = d
							if pendMsg.GroupID == conn.ActiveGroupID {
								conn.GroupMessages = append(conn.GroupMessages, pendMsg)
							}
							if a.GroupHistory != nil {
								a.GroupHistory.AddMessage(store.StoredGroupMessage{
									ID:        pendMsg.ID,
									GroupID:   pendMsg.GroupID,
									SenderID:  pendMsg.SenderID,
									Content:   d,
									CreatedAt: pendMsg.CreatedAt,
								})
							}
						} else {
							pendMsg.DecryptedContent = "[decryption failed]"
							if pendMsg.GroupID == conn.ActiveGroupID {
								conn.GroupMessages = append(conn.GroupMessages, pendMsg)
							}
						}
					}
					conn.PendingGroupMsgs = remaining
					a.mu.Unlock()
					if a.GroupHistory != nil {
						a.GroupHistory.Save()
					}
					a.Window.Invalidate()
				} else {
					log.Printf("Failed to decrypt group key: %v", err)
				}
			}
		}

	case "group.member.join":
		var payload struct {
			GroupID   string `json:"group_id"`
			UserID   string `json:"user_id"`
			PublicKey string `json:"public_key"`
			Username string `json:"username"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			// Add member to local group
			a.mu.Lock()
			for i, g := range conn.Groups {
				if g.ID == payload.GroupID {
					conn.Groups[i].Members = append(conn.Groups[i].Members, api.GroupMember{
						GroupID:   payload.GroupID,
						UserID:    payload.UserID,
						PublicKey: payload.PublicKey,
						Username:  payload.Username,
					})
					break
				}
			}
			a.mu.Unlock()

			// Distribute group key to new member
			if a.GroupHistory != nil && payload.UserID != conn.UserID {
				groupKey := a.GroupHistory.GetKey(payload.GroupID)
				if groupKey != "" && payload.PublicKey != "" {
					encKey, err := crypto.EncryptGroupKeyForMember(a.SecretKey, payload.PublicKey, groupKey)
					if err == nil {
						keyPayload, _ := json.Marshal(map[string]interface{}{
							"group_id":      payload.GroupID,
							"encrypted_key": encKey,
							"to":            []string{payload.UserID},
						})
						conn.WS.Send(api.WSEvent{
							Type:    "group.key",
							Payload: keyPayload,
						})
					}
				}
			}
		}

	case "group.member.leave":
		var payload struct {
			GroupID string `json:"group_id"`
			UserID  string `json:"user_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			if payload.UserID == conn.UserID {
				// We left the group
				a.mu.Lock()
				for i, g := range conn.Groups {
					if g.ID == payload.GroupID {
						conn.Groups = append(conn.Groups[:i], conn.Groups[i+1:]...)
						break
					}
				}
				if conn.ActiveGroupID == payload.GroupID {
					conn.ActiveGroupID = ""
					conn.GroupMessages = nil
				}
				a.mu.Unlock()

				if a.GroupHistory != nil {
					a.GroupHistory.DeleteGroup(payload.GroupID)
					a.GroupHistory.Save()
				}
			} else {
				// Another member left — remove from local list
				var isCreator bool
				a.mu.Lock()
				for i, g := range conn.Groups {
					if g.ID == payload.GroupID {
						for j, m := range g.Members {
							if m.UserID == payload.UserID {
								conn.Groups[i].Members = append(conn.Groups[i].Members[:j], conn.Groups[i].Members[j+1:]...)
								break
							}
						}
						isCreator = g.CreatorID == conn.UserID
						break
					}
				}
				a.mu.Unlock()

				// Creator rotates group key
				if isCreator && a.GroupHistory != nil {
					go a.rotateGroupKey(conn, payload.GroupID)
				}
			}
		}

	case "group.create":
		var group api.Group
		if json.Unmarshal(ev.Payload, &group) == nil {
			a.mu.Lock()
			dup := false
			for _, g := range conn.Groups {
				if g.ID == group.ID {
					dup = true
					break
				}
			}
			if !dup {
				conn.Groups = append(conn.Groups, group)
			}
			a.mu.Unlock()

			// Pre-generate group key so it's ready for distribution to new members
			if a.GroupHistory != nil && group.CreatorID == conn.UserID {
				if a.GroupHistory.GetKey(group.ID) == "" {
					newKey, err := crypto.GenerateGroupKey()
					if err == nil {
						a.GroupHistory.SetKey(group.ID, newKey)
						a.GroupHistory.Save()
					}
				}
			}
		}

	case "group.delete":
		var payload struct{ GroupID string `json:"group_id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, g := range conn.Groups {
				if g.ID == payload.GroupID {
					conn.Groups = append(conn.Groups[:i], conn.Groups[i+1:]...)
					break
				}
			}
			if conn.ActiveGroupID == payload.GroupID {
				conn.ActiveGroupID = ""
				conn.GroupMessages = nil
			}
			a.mu.Unlock()

			if a.GroupHistory != nil {
				a.GroupHistory.DeleteGroup(payload.GroupID)
				a.GroupHistory.Save()
			}
		}

	case "emoji.create":
		var emoji api.CustomEmoji
		if json.Unmarshal(ev.Payload, &emoji) == nil {
			a.mu.Lock()
			conn.Emojis = append(conn.Emojis, emoji)
			a.mu.Unlock()
		}

	case "emoji.delete":
		var payload struct{ ID string `json:"id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, e := range conn.Emojis {
				if e.ID == payload.ID {
					conn.Emojis = append(conn.Emojis[:i], conn.Emojis[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
		}

	case "invite.create":
		var invite api.Invite
		if json.Unmarshal(ev.Payload, &invite) == nil {
			a.mu.Lock()
			conn.Invites = append(conn.Invites, invite)
			a.mu.Unlock()
		}

	case "invite.delete":
		var payload struct{ ID string `json:"id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, inv := range conn.Invites {
				if inv.ID == payload.ID {
					conn.Invites = append(conn.Invites[:i], conn.Invites[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
		}

	case "role.create":
		var role api.Role
		if json.Unmarshal(ev.Payload, &role) == nil {
			a.mu.Lock()
			conn.Roles = append(conn.Roles, role)
			a.mu.Unlock()
		}

	case "role.update":
		var role api.Role
		if json.Unmarshal(ev.Payload, &role) == nil {
			a.mu.Lock()
			for i, r := range conn.Roles {
				if r.ID == role.ID {
					conn.Roles[i] = role
					break
				}
			}
			a.mu.Unlock()
		}

	case "role.delete":
		var payload struct{ ID string `json:"id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, r := range conn.Roles {
				if r.ID == payload.ID {
					conn.Roles = append(conn.Roles[:i], conn.Roles[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
		}

	case "block.add":
		var user api.User
		if json.Unmarshal(ev.Payload, &user) == nil {
			a.mu.Lock()
			conn.BlockedUsers = append(conn.BlockedUsers, user)
			a.mu.Unlock()
			// Sync to client-side cross-server blocklist
			if a.Contacts != nil && user.PublicKey != "" {
				a.Contacts.SetBlocked(user.PublicKey, true)
			}
		}

	case "block.remove":
		var payload struct{ UserID string `json:"user_id"` }
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, u := range conn.BlockedUsers {
				if u.ID == payload.UserID {
					conn.BlockedUsers = append(conn.BlockedUsers[:i], conn.BlockedUsers[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
			// Sync to client-side cross-server blocklist
			if a.Contacts != nil {
				a.mu.RLock()
				for _, u := range conn.Users {
					if u.ID == payload.UserID && u.PublicKey != "" {
						a.Contacts.SetBlocked(u.PublicKey, false)
						break
					}
				}
				a.mu.RUnlock()
			}
		}

	case "member.update":
		var user api.User
		if json.Unmarshal(ev.Payload, &user) == nil {
			a.mu.Lock()
			for i, m := range conn.Members {
				if m.ID == user.ID {
					// Invalidate cached avatar if URL changed
					if m.AvatarURL != user.AvatarURL {
						if m.AvatarURL != "" {
							a.Images.InvalidatePrefix("avatar_" + user.ID)
						}
					}
					conn.Members[i] = user
					break
				}
			}
			// Also update in Users map
			for i, u := range conn.Users {
				if u.ID == user.ID {
					conn.Users[i] = user
					break
				}
			}
			// Update status maps
			if user.Status != "" {
				conn.UserStatuses[user.ID] = user.Status
			} else {
				delete(conn.UserStatuses, user.ID)
			}
			if user.StatusText != "" {
				conn.UserStatusText[user.ID] = user.StatusText
			} else {
				delete(conn.UserStatusText, user.ID)
			}
			a.mu.Unlock()
			// Update contact
			if a.Contacts != nil && user.PublicKey != "" {
				name := user.DisplayName
				if name == "" {
					name = user.Username
				}
				a.Contacts.UpdateServerName(user.PublicKey, conn.URL, conn.Name, name)
			}
		}

	case "dm.message.edit":
		var payload struct {
			ID               string `json:"id"`
			ConversationID   string `json:"conversation_id"`
			EncryptedContent string `json:"encrypted_content"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			// Find peer key for decryption
			peerKey := ""
			a.mu.RLock()
			for _, c := range conn.DMConversations {
				if c.ID == payload.ConversationID {
					for _, p := range c.Participants {
						if p.UserID != conn.UserID {
							peerKey = p.PublicKey
						}
					}
				}
			}
			secretKey := a.SecretKey
			a.mu.RUnlock()

			decrypted := payload.EncryptedContent
			if peerKey != "" && secretKey != "" {
				if dec, err := crypto.DecryptDM(secretKey, peerKey, payload.EncryptedContent); err == nil {
					decrypted = dec
				}
			}

			// Update in current view
			a.mu.Lock()
			if payload.ConversationID == conn.ActiveDMID {
				for i, m := range conn.DMMessages {
					if m.ID == payload.ID {
						conn.DMMessages[i].DecryptedContent = decrypted
						conn.DMMessages[i].EncryptedContent = payload.EncryptedContent
						break
					}
				}
			}
			a.mu.Unlock()

			// Update local history
			if a.DMHistory != nil {
				a.DMHistory.UpdateMessage(payload.ConversationID, payload.ID, decrypted)
				a.DMHistory.Save()
			}
		}

	case "dm.message.delete":
		var payload struct {
			ID             string `json:"id"`
			ConversationID string `json:"conversation_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			if payload.ConversationID == conn.ActiveDMID {
				for i, m := range conn.DMMessages {
					if m.ID == payload.ID {
						conn.DMMessages = append(conn.DMMessages[:i], conn.DMMessages[i+1:]...)
						break
					}
				}
			}
			a.mu.Unlock()

			// Remove from local history
			if a.DMHistory != nil {
				a.DMHistory.DeleteMessage(payload.ID)
				a.DMHistory.Save()
			}
		}

	case "voice.error":
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			log.Printf("voice error: %s", payload.Error)
			// If "Wrong password" → client shows password prompt
			a.mu.Lock()
			conn.LastVoiceError = payload.Error
			a.mu.Unlock()
			a.Toasts.Error("Voice: " + payload.Error)
		}

	case "voice.state":
		var payload struct {
			ChannelID string   `json:"channel_id"`
			Users     []string `json:"users"`
			Joined    string   `json:"joined"`
			Left      string   `json:"left"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			if payload.Users != nil {
				conn.VoiceState[payload.ChannelID] = payload.Users
			}
			if len(payload.Users) == 0 {
				delete(conn.VoiceState, payload.ChannelID)
			}
			// Clean up screen share state when user leaves voice
			if payload.Left != "" {
				delete(conn.ScreenSharers, payload.Left)
			}
			a.mu.Unlock()

			// Auto-stop watching if the streamer left voice
			if payload.Left != "" && a.StreamViewer != nil && a.StreamViewer.Visible && a.StreamViewer.StreamerID == payload.Left {
				a.StreamViewer.StopWatching()
			}

			// Play join/leave sounds for other users in my voice channel
			if conn.Voice != nil && conn.Voice.IsActive() {
				myChannel, _, _ := conn.Voice.GetState()
				if payload.ChannelID == myChannel {
					if payload.Joined != "" && payload.Joined != conn.UserID {
						PlayVoiceJoinSound()
					}
					if payload.Left != "" && payload.Left != conn.UserID {
						PlayVoiceLeaveSound()
					}
				}
			}

			// Forward to voice manager for WebRTC peer management
			if conn.Voice != nil {
				conn.Voice.HandleVoiceState(payload.ChannelID, payload.Users, payload.Joined, payload.Left)
			}
		}

	case "voice.move":
		// Notification to the moved user — reconnect to the new channel
		var payload struct {
			ChannelID string `json:"channel_id"`
			MovedBy   string `json:"moved_by"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Voice != nil && conn.Voice.IsActive() {
			conn.Voice.Join(payload.ChannelID)
		}

	case "voice.offer":
		var payload struct {
			From string `json:"from"`
			SDP  string `json:"sdp"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Voice != nil {
			conn.Voice.HandleOffer(payload.From, payload.SDP)
		}

	case "voice.answer":
		var payload struct {
			From string `json:"from"`
			SDP  string `json:"sdp"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Voice != nil {
			conn.Voice.HandleAnswer(payload.From, payload.SDP)
		}

	case "voice.ice":
		if conn.Voice != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.Voice.HandleICE(payload.From, ev.Payload)
			}
		}

	case "call.ring":
		var payload struct {
			From           string `json:"from"`
			ConversationID string `json:"conversation_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Call != nil {
			// Ignore blocked users (server-side)
			blocked := false
			a.mu.RLock()
			for _, u := range conn.BlockedUsers {
				if u.ID == payload.From {
					blocked = true
					break
				}
			}
			// Cross-server blocklist: find caller's public key
			if !blocked {
				for _, u := range conn.Users {
					if u.ID == payload.From && u.PublicKey != "" {
						if a.IsBlockedKey(u.PublicKey) {
							blocked = true
						}
						break
					}
				}
			}
			a.mu.RUnlock()
			if !blocked {
				conn.Call.HandleRing(payload.From, payload.ConversationID)
				StartCallRingLoop()
			}
		}

	case "call.accept":
		var payload struct {
			From string `json:"from"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Call != nil {
			StopCallRingLoop()
			conn.Call.HandleAccept(payload.From)
		}

	case "call.decline":
		var payload struct {
			From string `json:"from"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Call != nil {
			StopCallRingLoop()
			conn.Call.HandleDecline(payload.From)
			PlayCallEndSound()
			a.mu.RLock()
			declineName := a.ResolveNameByID(conn, payload.From)
			a.mu.RUnlock()
			a.Toasts.Info(declineName + " declined your call")
		}

	case "call.hangup":
		var payload struct {
			From string `json:"from"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Call != nil {
			StopCallRingLoop()
			conn.Call.HandleHangup(payload.From)
			PlayCallEndSound()
		}

	case "call.offer":
		var payload struct {
			From string `json:"from"`
			SDP  string `json:"sdp"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Call != nil {
			conn.Call.HandleOffer(payload.From, payload.SDP)
		}

	case "call.answer":
		var payload struct {
			From string `json:"from"`
			SDP  string `json:"sdp"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Call != nil {
			conn.Call.HandleAnswer(payload.From, payload.SDP)
		}

	case "call.ice":
		if conn.Call != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.Call.HandleICE(payload.From, ev.Payload)
			}
		}

	case "lan.create":
		var party api.LANParty
		if json.Unmarshal(ev.Payload, &party) == nil {
			a.mu.Lock()
			conn.LANParties = append(conn.LANParties, party)
			a.mu.Unlock()
		}

	case "lan.join":
		var member api.LANPartyMember
		if json.Unmarshal(ev.Payload, &member) == nil {
			a.mu.Lock()
			if conn.LANMembers == nil {
				conn.LANMembers = make(map[string][]api.LANPartyMember)
			}
			conn.LANMembers[member.PartyID] = append(conn.LANMembers[member.PartyID], member)
			a.mu.Unlock()
		}

	case "lan.leave":
		var payload struct {
			PartyID string `json:"party_id"`
			UserID  string `json:"user_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			members := conn.LANMembers[payload.PartyID]
			for i, m := range members {
				if m.UserID == payload.UserID {
					conn.LANMembers[payload.PartyID] = append(members[:i], members[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
		}

	case "file.offer":
		if conn.P2P != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.P2P.HandleOffer(payload.From, ev.Payload)
			}
		}

	case "file.accept":
		if conn.P2P != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.P2P.HandleAccept(payload.From, ev.Payload)
			}
		}

	case "file.ice":
		if conn.P2P != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.P2P.HandleIce(payload.From, ev.Payload)
			}
		}

	case "file.reject":
		if conn.P2P != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.P2P.HandleReject(payload.From, ev.Payload)
			}
		}

	case "file.cancel":
		if conn.P2P != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.P2P.HandleCancel(payload.From, ev.Payload)
			}
		}

	case "file.complete":
		if conn.P2P != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.P2P.HandleComplete(payload.From, ev.Payload)
			}
		}

	case "file.request":
		if conn.P2P != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.P2P.HandleRequest(payload.From, ev.Payload)
			}
		}

	case "lan.delete":
		var payload struct {
			PartyID string `json:"party_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, p := range conn.LANParties {
				if p.ID == payload.PartyID {
					conn.LANParties = append(conn.LANParties[:i], conn.LANParties[i+1:]...)
					break
				}
			}
			delete(conn.LANMembers, payload.PartyID)
			a.mu.Unlock()
		}

	case "screen.share":
		var payload struct {
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
			Sharing   bool   `json:"sharing"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			if payload.Sharing {
				conn.ScreenSharers[payload.UserID] = payload.ChannelID
			} else {
				delete(conn.ScreenSharers, payload.UserID)
			}
			a.mu.Unlock()

			// Auto-stop watching if streamer stopped
			if !payload.Sharing && a.StreamViewer != nil && a.StreamViewer.Visible && a.StreamViewer.StreamerID == payload.UserID {
				a.StreamViewer.StopWatching()
			}
		}

	case "screen.watch":
		var payload struct {
			From     string `json:"from"`
			Watching bool   `json:"watching"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil && conn.Voice != nil {
			if payload.Watching {
				conn.Voice.AddViewer(payload.From)
			} else {
				conn.Voice.RemoveViewer(payload.From)
			}
		}

	case "share.registered":
		var dir api.SharedDirectory
		if json.Unmarshal(ev.Payload, &dir) == nil {
			a.mu.Lock()
			if dir.OwnerID == conn.UserID {
				conn.MyShares = append(conn.MyShares, dir)
			} else {
				conn.SharedWithMe = append(conn.SharedWithMe, dir)
			}
			a.mu.Unlock()
			a.Window.Invalidate()
		}

	case "share.unregistered":
		var payload struct {
			ID      string `json:"id"`
			OwnerID string `json:"owner_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, s := range conn.MyShares {
				if s.ID == payload.ID {
					conn.MyShares = append(conn.MyShares[:i], conn.MyShares[i+1:]...)
					break
				}
			}
			for i, s := range conn.SharedWithMe {
				if s.ID == payload.ID {
					conn.SharedWithMe = append(conn.SharedWithMe[:i], conn.SharedWithMe[i+1:]...)
					break
				}
			}
			if a.SharesView.ActiveShareID == payload.ID {
				a.SharesView.ActiveShareID = ""
			}
			a.mu.Unlock()
			a.Window.Invalidate()
		}

	case "share.updated":
		var dir api.SharedDirectory
		if json.Unmarshal(ev.Payload, &dir) == nil {
			a.mu.Lock()
			for i, s := range conn.MyShares {
				if s.ID == dir.ID {
					conn.MyShares[i] = dir
					break
				}
			}
			for i, s := range conn.SharedWithMe {
				if s.ID == dir.ID {
					conn.SharedWithMe[i] = dir
					break
				}
			}
			a.mu.Unlock()
			a.Window.Invalidate()
		}

	case "share.files_changed":
		var payload struct {
			DirectoryID string `json:"directory_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			if a.SharesView.ActiveShareID == payload.DirectoryID {
				a.SharesView.loadFiles()
			}
		}

	case "share.permission_changed":
		go func() {
			resp, err := conn.Client.GetShares()
			if err != nil {
				return
			}
			a.mu.Lock()
			if resp.Own != nil {
				conn.MyShares = resp.Own
			}
			if resp.Accessible != nil {
				conn.SharedWithMe = resp.Accessible
			}
			if conn.Mounts != nil {
				for _, s := range conn.SharedWithMe {
					conn.Mounts.UpdateCanWrite(s.ID, s.CanWrite)
				}
			}
			a.mu.Unlock()
			a.Window.Invalidate()
		}()

	case "upload.request":
		var payload struct {
			UploadID     string `json:"upload_id"`
			DirectoryID  string `json:"directory_id"`
			FileName     string `json:"file_name"`
			FileSize     int64  `json:"file_size"`
			RelativePath string `json:"relative_path"`
			UploaderID   string `json:"uploader_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			log.Printf("Upload request: %s wants to upload %s to share %s",
				payload.UploaderID, payload.FileName, payload.DirectoryID)

			a.mu.RLock()
			localRoot, ok := conn.SharePaths[payload.DirectoryID]
			a.mu.RUnlock()

			if ok && localRoot != "" && conn.P2P != nil {
				relPath := payload.RelativePath
				if relPath == "/" || relPath == "" {
					relPath = ""
				} else {
					relPath = strings.TrimPrefix(relPath, "/")
				}
				savePath := filepath.Join(localRoot, relPath, payload.FileName)

				// Path traversal protection
				cleanPath := filepath.Clean(savePath)
				if !strings.HasPrefix(cleanPath, filepath.Clean(localRoot)+string(filepath.Separator)) && cleanPath != filepath.Clean(localRoot) {
					log.Printf("Upload request: path traversal attempt: %s", savePath)
				} else {
					// Create directories if they don't exist
					os.MkdirAll(filepath.Dir(cleanPath), 0755)

					// Start P2P download from the uploader
					conn.P2P.RequestDownload(payload.UploaderID, payload.UploadID, cleanPath)

					// After completion, sync file listing
					go func() {
						for i := 0; i < 600; i++ {
							time.Sleep(time.Second)
							if conn.P2P.IsDownloaded(payload.UploadID) {
								log.Printf("Upload complete: %s saved to %s", payload.FileName, cleanPath)
								a.syncShareFiles(conn, payload.DirectoryID, localRoot)
								return
							}
							if conn.P2P.IsUnavailable(payload.UploadID) {
								log.Printf("Upload failed: %s unavailable", payload.UploadID)
								return
							}
						}
						log.Printf("Upload timeout: %s", payload.UploadID)
					}()
				}
			} else {
				log.Printf("Upload request: no local path for share %s", payload.DirectoryID)
			}
		}

	case "file.deleted":
		var payload struct {
			DirectoryID  string `json:"directory_id"`
			RelativePath string `json:"relative_path"`
			FileName     string `json:"file_name"`
			DeletedBy    string `json:"deleted_by"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			log.Printf("File deleted: %s/%s in share %s by %s",
				payload.RelativePath, payload.FileName, payload.DirectoryID, payload.DeletedBy)

			// Owner deletes the file from disk
			a.mu.RLock()
			localRoot, ok := conn.SharePaths[payload.DirectoryID]
			a.mu.RUnlock()

			if ok && localRoot != "" {
				relPath := payload.RelativePath
				if relPath == "/" || relPath == "" {
					relPath = ""
				} else {
					relPath = strings.TrimPrefix(relPath, "/")
				}
				fullPath := filepath.Join(localRoot, relPath, payload.FileName)

				// Path traversal protection
				cleanPath := filepath.Clean(fullPath)
				cleanRoot := filepath.Clean(localRoot)
				if strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator)) || cleanPath == cleanRoot {
					if err := os.Remove(cleanPath); err != nil {
						log.Printf("file.deleted: remove error: %v", err)
					} else {
						log.Printf("file.deleted: removed %s", cleanPath)
					}
					// Sync file listing with the server
					go a.syncShareFiles(conn, payload.DirectoryID, localRoot)
				} else {
					log.Printf("file.deleted: path traversal attempt: %s", fullPath)
				}
			}
		}

	case "poll.vote":
		var poll api.Poll
		if json.Unmarshal(ev.Payload, &poll) == nil {
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.ID == poll.MessageID {
					conn.Messages[i].Poll = &poll
					break
				}
			}
			a.mu.Unlock()
		}

	case "message.linkpreview":
		var payload struct {
			MessageID string          `json:"message_id"`
			Preview   api.LinkPreview `json:"preview"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, m := range conn.Messages {
				if m.ID == payload.MessageID {
					conn.Messages[i].LinkPreview = &payload.Preview
					break
				}
			}
			a.mu.Unlock()
		}

	case "gameserver.create":
		var gs api.GameServerInstance
		if json.Unmarshal(ev.Payload, &gs) == nil {
			a.mu.Lock()
			conn.GameServers = append(conn.GameServers, gs)
			a.mu.Unlock()
		}

	case "gameserver.status", "gameserver.start", "gameserver.stop":
		var gs api.GameServerInstance
		if json.Unmarshal(ev.Payload, &gs) == nil {
			a.mu.Lock()
			for i, s := range conn.GameServers {
				if s.ID == gs.ID {
					conn.GameServers[i] = gs
					break
				}
			}
			a.mu.Unlock()
		}

	case "gameserver.delete":
		var payload struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, s := range conn.GameServers {
				if s.ID == payload.ID {
					conn.GameServers = append(conn.GameServers[:i], conn.GameServers[i+1:]...)
					break
				}
			}
			// Delete members
			if a.GameServers != nil {
				delete(a.GameServers.gsMembers, payload.ID)
			}
			a.mu.Unlock()
		}

	case "gameserver.join":
		var member api.GameServerMember
		if json.Unmarshal(ev.Payload, &member) == nil {
			a.mu.Lock()
			if a.GameServers != nil && a.GameServers.gsMembers != nil {
				a.GameServers.gsMembers[member.GameServerID] = append(a.GameServers.gsMembers[member.GameServerID], member)
			}
			a.mu.Unlock()
		}

	case "gameserver.leave":
		var payload struct {
			GameServerID string `json:"game_server_id"`
			UserID       string `json:"user_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			if a.GameServers != nil && a.GameServers.gsMembers != nil {
				members := a.GameServers.gsMembers[payload.GameServerID]
				for i, m := range members {
					if m.UserID == payload.UserID {
						a.GameServers.gsMembers[payload.GameServerID] = append(members[:i], members[i+1:]...)
						break
					}
				}
			}
			a.mu.Unlock()
		}

	case "tunnel.request":
		var tunnel api.Tunnel
		if json.Unmarshal(ev.Payload, &tunnel) == nil {
			a.mu.Lock()
			conn.Tunnels = append(conn.Tunnels, tunnel)
			a.mu.Unlock()
			// Desktop notification
			if a.NotifMgr != nil {
				a.NotifMgr.NotifyMessage(conn.Name, "VPN Tunnels", "Tunnel request", tunnel.CreatorName)
			}
		}

	case "tunnel.accept":
		var tunnel api.Tunnel
		if json.Unmarshal(ev.Payload, &tunnel) == nil {
			a.mu.Lock()
			for i, t := range conn.Tunnels {
				if t.ID == tunnel.ID {
					conn.Tunnels[i] = tunnel
					break
				}
			}
			a.mu.Unlock()
		}

	case "tunnel.close":
		var payload struct {
			TunnelID string `json:"tunnel_id"`
			ClosedBy string `json:"closed_by"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			for i, t := range conn.Tunnels {
				if t.ID == payload.TunnelID {
					conn.Tunnels = append(conn.Tunnels[:i], conn.Tunnels[i+1:]...)
					break
				}
			}
			a.mu.Unlock()
		}

	case "livewb.start":
		var payload struct {
			ChannelID string `json:"channel_id"`
			StarterID string `json:"starter_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			conn.LiveWhiteboards[payload.ChannelID] = payload.StarterID
			a.mu.Unlock()
		}

	case "livewb.stop":
		var payload struct {
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			delete(conn.LiveWhiteboards, payload.ChannelID)
			a.mu.Unlock()
			// Auto-close live WB view if open for this channel
			if a.LiveWB != nil && a.LiveWB.Visible && a.LiveWB.ChannelID == payload.ChannelID {
				a.LiveWB.Visible = false
				a.LiveWB.ChannelID = ""
				a.LiveWB.StarterID = ""
				a.LiveWB.strokes = nil
			}
		}

	case "livewb.stroke":
		var stroke struct {
			ChannelID string              `json:"channel_id"`
			Stroke    api.WhiteboardStroke `json:"stroke"`
		}
		if json.Unmarshal(ev.Payload, &stroke) == nil {
			if a.LiveWB != nil && a.LiveWB.Visible && a.LiveWB.ChannelID == stroke.ChannelID {
				// Deduplicate: skip if stroke already added locally (optimistic update)
				a.mu.Lock()
				dup := false
				for _, s := range a.LiveWB.strokes {
					if s.ID == stroke.Stroke.ID {
						dup = true
						break
					}
				}
				if !dup {
					a.LiveWB.strokes = append(a.LiveWB.strokes, stroke.Stroke)
				}
				a.mu.Unlock()
			}
		}

	case "livewb.undo":
		var payload struct {
			ChannelID string `json:"channel_id"`
			StrokeID  string `json:"stroke_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			if a.LiveWB != nil && a.LiveWB.Visible && a.LiveWB.ChannelID == payload.ChannelID {
				a.mu.Lock()
				for i, s := range a.LiveWB.strokes {
					if s.ID == payload.StrokeID {
						a.LiveWB.strokes = append(a.LiveWB.strokes[:i], a.LiveWB.strokes[i+1:]...)
						break
					}
				}
				a.mu.Unlock()
			}
		}

	case "livewb.clear":
		var payload struct {
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			if a.LiveWB != nil && a.LiveWB.Visible && a.LiveWB.ChannelID == payload.ChannelID {
				a.mu.Lock()
				a.LiveWB.strokes = nil
				a.mu.Unlock()
			}
		}

	case "livewb.state":
		var payload struct {
			ChannelID string               `json:"channel_id"`
			StarterID string               `json:"starter_id"`
			Strokes   []api.WhiteboardStroke `json:"strokes"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			conn.LiveWhiteboards[payload.ChannelID] = payload.StarterID
			a.mu.Unlock()
			if a.LiveWB != nil && a.LiveWB.Visible && a.LiveWB.ChannelID == payload.ChannelID {
				a.mu.Lock()
				a.LiveWB.strokes = payload.Strokes
				a.mu.Unlock()
			}
		}

	case "whiteboard.stroke":
		var stroke api.WhiteboardStroke
		if json.Unmarshal(ev.Payload, &stroke) == nil {
			a.mu.Lock()
			if a.WhiteboardView != nil && a.WhiteboardView.Visible && stroke.WhiteboardID == a.WhiteboardView.BoardID {
				a.WhiteboardView.strokes = append(a.WhiteboardView.strokes, stroke)
			}
			a.mu.Unlock()
		}

	case "whiteboard.undo":
		var payload struct {
			WhiteboardID string `json:"whiteboard_id"`
			StrokeID     string `json:"stroke_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			if a.WhiteboardView != nil && a.WhiteboardView.Visible && payload.WhiteboardID == a.WhiteboardView.BoardID {
				for i, s := range a.WhiteboardView.strokes {
					if s.ID == payload.StrokeID {
						a.WhiteboardView.strokes = append(a.WhiteboardView.strokes[:i], a.WhiteboardView.strokes[i+1:]...)
						break
					}
				}
			}
			a.mu.Unlock()
		}

	case "whiteboard.clear":
		var payload struct {
			WhiteboardID string `json:"whiteboard_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			a.mu.Lock()
			if a.WhiteboardView != nil && a.WhiteboardView.Visible && payload.WhiteboardID == a.WhiteboardView.BoardID {
				a.WhiteboardView.strokes = nil
			}
			a.mu.Unlock()
		}

	case "swarm.piece_request":
		if conn.Swarm != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.Swarm.HandlePieceRequest(payload.From, ev.Payload)
			}
		}

	case "swarm.piece_offer":
		if conn.Swarm != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.Swarm.HandlePieceOffer(payload.From, ev.Payload)
			}
		}

	case "swarm.piece_accept":
		if conn.Swarm != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.Swarm.HandlePieceAccept(payload.From, ev.Payload)
			}
		}

	case "swarm.piece_ice":
		if conn.Swarm != nil {
			var payload struct {
				From string `json:"from"`
			}
			if json.Unmarshal(ev.Payload, &payload) == nil {
				conn.Swarm.HandlePieceIce(payload.From, ev.Payload)
			}
		}

	case "swarm.download_notify":
		// Server notifies seeders about swarm download — auto-register seed file
		if conn.Swarm != nil {
			var data struct {
				DirectoryID  string `json:"directory_id"`
				FileID       string `json:"file_id"`
				FileName     string `json:"file_name"`
				FileSize     int64  `json:"file_size"`
				RelativePath string `json:"relative_path"`
			}
			if json.Unmarshal(ev.Payload, &data) == nil {
				a.mu.RLock()
				localRoot, hasPath := conn.SharePaths[data.DirectoryID]
				a.mu.RUnlock()

				if hasPath && localRoot != "" {
					relPath := data.RelativePath
					if relPath == "/" || relPath == "" {
						relPath = ""
					} else {
						relPath = strings.TrimPrefix(relPath, "/")
					}
					fullPath := filepath.Join(localRoot, relPath, data.FileName)
					if info, err := os.Stat(fullPath); err == nil && info.Size() == data.FileSize {
						conn.Swarm.RegisterSeedFile(data.DirectoryID, data.FileID, fullPath, data.FileName, data.FileSize)
						log.Printf("swarm: auto-registered seed: %s", data.FileName)
					}
				}
			}
		}

	case "swarm.seed_added", "swarm.seed_removed":
		// Refresh seed counts if we're in shares view
		if a.SharesView.ActiveShareID != "" {
			a.Window.Invalidate()
		}

	case "ban.created":
		// Admin notification — refresh ban list
		log.Printf("WS: ban.created event")

	case "device.suspicious":
		var payload struct {
			UserID       string `json:"user_id"`
			HardwareHash string `json:"hardware_hash"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			log.Printf("WS: suspicious device detected for user %s (hw=%s)", payload.UserID, payload.HardwareHash)
		}

	case "quarantine.ended":
		var payload struct {
			UserID string `json:"user_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			log.Printf("WS: quarantine ended for user %s", payload.UserID)
		}

	case "approval.pending":
		var payload struct {
			UserID   string `json:"user_id"`
			Username string `json:"requested_username"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			log.Printf("WS: new approval pending for %s (%s)", payload.Username, payload.UserID)
		}

	case "approval.resolved":
		var payload struct {
			UserID string `json:"user_id"`
			Status string `json:"status"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			log.Printf("WS: approval resolved for %s: %s", payload.UserID, payload.Status)
		}

	case "kanban.board_create", "kanban.board_delete",
		"kanban.column_create", "kanban.column_update", "kanban.column_delete",
		"kanban.card_create", "kanban.card_update", "kanban.card_move", "kanban.card_delete":
		a.KanbanView.HandleWSEvent(ev.Type, ev.Payload)

	case "lfg.create":
		var listing api.LFGListing
		if json.Unmarshal(ev.Payload, &listing) == nil {
			if a.LFGBoard != nil && listing.ChannelID == conn.ActiveChannelID && a.Mode == ViewChannels {
				a.LFGBoard.HandleWSCreate(listing)
				a.Window.Invalidate()
			}
			// Play LFG sound + desktop notification for other users' listings
			if listing.UserID != conn.UserID {
				PlayLFGSound()
				if a.NotifMgr != nil {
					creatorName := a.ResolveNameByID(conn, listing.UserID)
					a.NotifMgr.NotifyMessage(conn.Name, "LFG", listing.GameName, creatorName)
				}
			}
		}

	case "lfg.delete":
		var data struct {
			ID        string `json:"id"`
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(ev.Payload, &data) == nil {
			if a.LFGBoard != nil && data.ChannelID == conn.ActiveChannelID && a.Mode == ViewChannels {
				a.LFGBoard.HandleWSDelete(data.ID)
				a.Window.Invalidate()
			}
		}

	case "lfg.join", "lfg.leave":
		var data struct {
			ListingID    string     `json:"listing_id"`
			ChannelID    string     `json:"channel_id"`
			Participants []api.User `json:"participants"`
		}
		if json.Unmarshal(ev.Payload, &data) == nil {
			if a.LFGBoard != nil && data.ChannelID == conn.ActiveChannelID && a.Mode == ViewChannels {
				a.LFGBoard.HandleWSParticipants(data.ListingID, data.Participants)
				a.Window.Invalidate()
			}
		}

	case "lfg.apply":
		var data struct {
			ListingID    string               `json:"listing_id"`
			ChannelID    string               `json:"channel_id"`
			Applications []api.LFGApplication `json:"applications"`
		}
		if json.Unmarshal(ev.Payload, &data) == nil {
			// Always update LFGBoard data (even when not viewing)
			if a.LFGBoard != nil {
				a.LFGBoard.HandleWSApplications(data.ListingID, data.Applications)
			}
			a.Window.Invalidate()

			// Count pending + find latest applicant name
			pending := 0
			applicantName := ""
			for _, ap := range data.Applications {
				if ap.Status == "pending" {
					pending++
					if ap.User != nil {
						applicantName = ap.User.DisplayName
						if applicantName == "" {
							applicantName = ap.User.Username
						}
					}
				}
			}
			PlayLFGSound()
			notifMsg := fmt.Sprintf("New LFG application from %s", applicantName)
			if pending > 1 {
				notifMsg = fmt.Sprintf("%d pending LFG applications", pending)
			}
			if a.NotifMgr != nil {
				a.NotifMgr.NotifyMessage(conn.Name, "LFG Application", notifMsg, applicantName)
			}
			if a.NotifCenter != nil {
				a.NotifCenter.AddAlert(conn.Name, "LFG Application", notifMsg)
			}
		}

	case "lfg.accepted":
		var data struct {
			ListingID string `json:"listing_id"`
			GroupID   string `json:"group_id"`
		}
		if json.Unmarshal(ev.Payload, &data) == nil {
			PlayNotificationSound()
			if a.NotifMgr != nil {
				a.NotifMgr.NotifyMessage(conn.Name, "LFG", "Your application was accepted!", "")
			}
			if a.NotifCenter != nil {
				a.NotifCenter.AddAlert(conn.Name, "LFG Accepted", "Your application was accepted!")
			}
			// Load and add the new group
			if data.GroupID != "" {
				go func() {
					if conn := a.Conn(); conn != nil {
						group, err := conn.Client.GetGroup(data.GroupID)
						if err == nil && group != nil {
							a.mu.Lock()
							dup := false
							for _, g := range conn.Groups {
								if g.ID == group.ID {
									dup = true
									break
								}
							}
							if !dup {
								conn.Groups = append(conn.Groups, *group)
							}
							a.mu.Unlock()
							a.Window.Invalidate()
						}
					}
				}()
			}
		}

	case "lfg.rejected":
		var data struct {
			ListingID string `json:"listing_id"`
		}
		if json.Unmarshal(ev.Payload, &data) == nil {
			PlayNotificationSound()
			if a.NotifMgr != nil {
				a.NotifMgr.NotifyMessage(conn.Name, "LFG", "Your application was rejected.", "")
			}
			if a.NotifCenter != nil {
				a.NotifCenter.AddAlert(conn.Name, "LFG Rejected", "Your application was rejected.")
			}
		}

	case "calendar.event_create", "calendar.event_update", "calendar.event_delete":
		a.CalendarView.HandleWSEvent(ev.Type, ev.Payload)

	case "calendar.reminder":
		var data struct {
			EventID  string `json:"event_id"`
			Title    string `json:"title"`
			StartsAt string `json:"starts_at"`
		}
		if json.Unmarshal(ev.Payload, &data) == nil {
			log.Printf("Calendar reminder: %s", data.Title)
			go PlayCalendarSound()
			// Desktop notification for calendar reminder
			if a.NotifMgr != nil {
				a.NotifMgr.NotifyCalendarReminder(conn.Name, data.Title)
			}
		}

	case "transfer.request":
		var payload struct {
			TransferID   string `json:"transfer_id"`
			DirectoryID  string `json:"directory_id"`
			FileID       string `json:"file_id"`
			FileName     string `json:"file_name"`
			FileSize     int64  `json:"file_size"`
			RelativePath string `json:"relative_path"`
			RequesterID  string `json:"requester_id"`
		}
		if json.Unmarshal(ev.Payload, &payload) == nil {
			log.Printf("Transfer request: %s wants file %s from share %s",
				payload.RequesterID, payload.FileName, payload.DirectoryID)

			// Owner auto-respond: find local path and register file for P2P
			a.mu.RLock()
			localRoot, ok := conn.SharePaths[payload.DirectoryID]
			a.mu.RUnlock()

			if ok && localRoot != "" && conn.P2P != nil {
				// Build path: root + relativePath + fileName
				relPath := payload.RelativePath
				if relPath == "/" || relPath == "" {
					relPath = ""
				} else {
					// Remove leading /
					relPath = strings.TrimPrefix(relPath, "/")
				}
				fullPath := filepath.Join(localRoot, relPath, payload.FileName)

				// Path traversal protection
				cleanPath := filepath.Clean(fullPath)
				if !strings.HasPrefix(cleanPath, filepath.Clean(localRoot)+string(filepath.Separator)) && cleanPath != filepath.Clean(localRoot) {
					log.Printf("Transfer request: path traversal attempt: %s", fullPath)
				} else {
					// Verify file existence
					if fi, err := os.Stat(cleanPath); err == nil && !fi.IsDir() {
						// Register file in P2P manager with transferID from server
						conn.P2P.RegisterFileForShare(payload.TransferID, cleanPath, payload.FileName, fi.Size())
						log.Printf("Transfer request: registered %s for transfer %s", cleanPath, payload.TransferID)
					} else {
						log.Printf("Transfer request: file not found: %s", cleanPath)
					}
				}
			} else {
				log.Printf("Transfer request: no local path for share %s", payload.DirectoryID)
			}
		}
	}
}

// findUsername looks up a username in conn.Users (caller must hold a.mu).
func findUsername(conn *ServerConnection, userID string) string {
	for _, u := range conn.Users {
		if u.ID == userID {
			if u.DisplayName != "" {
				return u.DisplayName
			}
			return u.Username
		}
	}
	return "Unknown"
}

// findChannelName looks up a channel name in conn.Channels (caller must hold a.mu).
func findChannelName(conn *ServerConnection, channelID string) string {
	for _, ch := range conn.Channels {
		if ch.ID == channelID {
			return ch.Name
		}
	}
	return "unknown"
}

// findGroupName looks up a group name in conn.Groups (caller must hold a.mu).
func findGroupName(conn *ServerConnection, groupID string) string {
	for _, g := range conn.Groups {
		if g.ID == groupID {
			return g.Name
		}
	}
	return "Group"
}

// syncShareFiles synchronizes file listing with the server (owner-side, after upload).
func (a *App) syncShareFiles(conn *ServerConnection, shareID, localPath string) {
	var files []map[string]interface{}
	filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(localPath, path)
		if rel == "." {
			return nil
		}
		if strings.Contains(rel, "..") {
			return nil
		}
		parent := filepath.Dir(rel)
		if parent == "." {
			parent = "/"
		} else {
			parent = "/" + filepath.ToSlash(parent)
		}
		files = append(files, map[string]interface{}{
			"relative_path": parent,
			"file_name":     info.Name(),
			"file_size":     info.Size(),
			"is_dir":        info.IsDir(),
			"file_hash":     "",
		})
		return nil
	})
	if err := conn.Client.SyncShareFiles(shareID, files); err != nil {
		log.Printf("syncShareFiles error: %v", err)
	}
}

// rotateGroupKey generates a new group key and distributes it to all members.
func (a *App) rotateGroupKey(conn *ServerConnection, groupID string) {
	newKey, err := crypto.GenerateGroupKey()
	if err != nil {
		log.Printf("rotateGroupKey: generate: %v", err)
		return
	}

	a.GroupHistory.RotateKey(groupID, newKey)
	a.GroupHistory.Save()

	a.mu.RLock()
	var members []api.GroupMember
	for _, g := range conn.Groups {
		if g.ID == groupID {
			members = make([]api.GroupMember, len(g.Members))
			copy(members, g.Members)
			break
		}
	}
	secretKey := a.SecretKey
	a.mu.RUnlock()

	for _, m := range members {
		if m.UserID == conn.UserID || m.PublicKey == "" {
			continue
		}
		encKey, err := crypto.EncryptGroupKeyForMember(secretKey, m.PublicKey, newKey)
		if err != nil {
			log.Printf("rotateGroupKey: encrypt for %s: %v", m.UserID, err)
			continue
		}
		keyPayload, _ := json.Marshal(map[string]string{
			"group_id":      groupID,
			"encrypted_key": encKey,
			"to":            m.UserID,
		})
		conn.WS.Send(api.WSEvent{
			Type:    "group.key",
			Payload: keyPayload,
		})
	}
	log.Printf("Rotated group key for %s, distributed to %d members", groupID, len(members)-1)
}
