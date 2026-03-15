package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Client struct {
	BaseURL        string
	AccessToken    string
	RefreshToken   string
	OnTokenRefresh func(access, refresh string) // callback po auto-refresh
	http           *http.Client
	mu             sync.RWMutex
	refreshMu      sync.Mutex
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		http: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
			},
		},
	}
}

func (c *Client) SetTokens(access, refresh string) {
	c.mu.Lock()
	c.AccessToken = access
	c.RefreshToken = refresh
	c.mu.Unlock()
}

func (c *Client) GetAccessToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AccessToken
}

func (c *Client) GetRefreshToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.RefreshToken
}

// tryRefresh zkusí obnovit access token pomocí refresh tokenu.
// Vrátí true pokud se podařilo.
func (c *Client) tryRefresh() bool {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	c.mu.RLock()
	rt := c.RefreshToken
	c.mu.RUnlock()
	if rt == "" {
		return false
	}

	data, _ := json.Marshal(map[string]string{"refresh_token": rt})
	req, err := http.NewRequest("POST", c.BaseURL+"/api/auth/refresh", bytes.NewReader(data))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	var rr RefreshResponse
	if err := json.Unmarshal(body, &rr); err != nil {
		return false
	}

	c.SetTokens(rr.AccessToken, rr.RefreshToken)
	if c.OnTokenRefresh != nil {
		c.OnTokenRefresh(rr.AccessToken, rr.RefreshToken)
	}
	return true
}

func (c *Client) doJSON(method, path string, body any, result any) error {
	var bodyData []byte
	if body != nil {
		var err error
		bodyData, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		var bodyReader io.Reader
		if bodyData != nil {
			bodyReader = bytes.NewReader(bodyData)
		}

		req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		c.mu.RLock()
		if c.AccessToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.AccessToken)
		}
		c.mu.RUnlock()

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("HTTP chyba: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		// Auto-refresh při 401 (ne pro auth endpointy)
		if resp.StatusCode == 401 && attempt == 0 && !strings.HasPrefix(path, "/api/auth/") {
			if c.tryRefresh() {
				continue
			}
		}

		if resp.StatusCode >= 400 {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
		}

		if result != nil && len(respBody) > 0 {
			return json.Unmarshal(respBody, result)
		}
		return nil
	}
	return fmt.Errorf("request failed after retry")
}

// doHTTP vykoná HTTP request s auto-refresh při 401.
// buildReq musí vracet nový request při každém volání (body se spotřebuje).
func (c *Client) doHTTP(buildReq func(token string) (*http.Request, error)) (int, []byte, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token := c.GetAccessToken()
		req, err := buildReq(token)
		if err != nil {
			return 0, nil, err
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return 0, nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return 0, nil, err
		}

		if resp.StatusCode == 401 && attempt == 0 && c.tryRefresh() {
			continue
		}

		return resp.StatusCode, body, nil
	}
	return 0, nil, fmt.Errorf("request failed after retry")
}

// Auth

func (c *Client) Challenge(publicKey, username, inviteCode, deviceID, hardwareHash string) (*ChallengeResponse, error) {
	body := map[string]string{"public_key": publicKey}
	if username != "" {
		body["username"] = username
	}
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	if hardwareHash != "" {
		body["hardware_hash"] = hardwareHash
	}
	path := "/api/auth/challenge"
	if inviteCode != "" {
		path += "?invite=" + inviteCode
	}
	var resp ChallengeResponse
	err := c.doJSON("POST", path, body, &resp)
	return &resp, err
}

func (c *Client) Verify(publicKey, nonce, signature string) (*VerifyResponse, error) {
	body := map[string]string{
		"public_key": publicKey,
		"nonce":      nonce,
		"signature":  signature,
	}
	var resp VerifyResponse
	err := c.doJSON("POST", "/api/auth/verify", body, &resp)
	return &resp, err
}

func (c *Client) Refresh() (*RefreshResponse, error) {
	c.mu.RLock()
	rt := c.RefreshToken
	c.mu.RUnlock()

	body := map[string]string{"refresh_token": rt}
	var resp RefreshResponse
	err := c.doJSON("POST", "/api/auth/refresh", body, &resp)
	if err != nil {
		return nil, err
	}
	c.SetTokens(resp.AccessToken, resp.RefreshToken)
	return &resp, nil
}

// Server

func (c *Client) GetServerInfo() (*ServerInfo, error) {
	var info ServerInfo
	err := c.doJSON("GET", "/api/server", nil, &info)
	return &info, err
}

// Channels

func (c *Client) GetChannels() ([]Channel, error) {
	var channels []Channel
	err := c.doJSON("GET", "/api/channels", nil, &channels)
	return channels, err
}

func (c *Client) CreateChannel(name, channelType string, categoryID *string) (*Channel, error) {
	body := map[string]interface{}{"name": name, "type": channelType}
	if categoryID != nil {
		body["category_id"] = *categoryID
	}
	var ch Channel
	err := c.doJSON("POST", "/api/channels", body, &ch)
	return &ch, err
}

func (c *Client) UpdateChannel(id string, name, topic string, categoryID *string, slowMode *int) error {
	body := map[string]interface{}{"name": name, "topic": topic}
	if categoryID != nil {
		body["category_id"] = *categoryID
	}
	if slowMode != nil {
		body["slow_mode_seconds"] = *slowMode
	}
	return c.doJSON("PATCH", fmt.Sprintf("/api/channels/%s", id), body, nil)
}

func (c *Client) DeleteChannel(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/channels/%s", id), nil, nil)
}

func (c *Client) ReorderChannels(ids []string) error {
	body := map[string]interface{}{"channel_ids": ids}
	return c.doJSON("POST", "/api/channels/reorder", body, nil)
}

func (c *Client) GetCategories() ([]ChannelCategory, error) {
	var cats []ChannelCategory
	err := c.doJSON("GET", "/api/categories", nil, &cats)
	return cats, err
}

func (c *Client) CreateCategory(name, color string, parentID *string) (*ChannelCategory, error) {
	body := map[string]interface{}{"name": name, "color": color}
	if parentID != nil {
		body["parent_id"] = *parentID
	}
	var cat ChannelCategory
	err := c.doJSON("POST", "/api/categories", body, &cat)
	return &cat, err
}

func (c *Client) UpdateCategory(id, name, color string) error {
	body := map[string]string{"name": name, "color": color}
	return c.doJSON("PATCH", fmt.Sprintf("/api/categories/%s", id), body, nil)
}

func (c *Client) SetCategoryParent(id, parentID string) error {
	body := map[string]string{"parent_id": parentID}
	return c.doJSON("PATCH", fmt.Sprintf("/api/categories/%s", id), body, nil)
}

func (c *Client) DeleteCategory(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/categories/%s", id), nil, nil)
}

func (c *Client) ReorderCategories(ids []string) error {
	body := map[string]interface{}{"category_ids": ids}
	return c.doJSON("POST", "/api/categories/reorder", body, nil)
}

// Messages

func (c *Client) GetMessages(channelID string, before string, limit int) ([]Message, error) {
	path := fmt.Sprintf("/api/channels/%s/messages?limit=%d", channelID, limit)
	if before != "" {
		path += "&before=" + before
	}
	var messages []Message
	err := c.doJSON("GET", path, nil, &messages)
	return messages, err
}

func (c *Client) SendMessage(channelID, content string, replyToID string) (*Message, error) {
	body := map[string]interface{}{"content": content}
	if replyToID != "" {
		body["reply_to_id"] = replyToID
	}
	var msg Message
	err := c.doJSON("POST", fmt.Sprintf("/api/channels/%s/messages", channelID), body, &msg)
	return &msg, err
}

func (c *Client) EditMessage(msgID, content string) error {
	body := map[string]string{"content": content}
	return c.doJSON("PATCH", fmt.Sprintf("/api/messages/%s", msgID), body, nil)
}

func (c *Client) DeleteMessage(msgID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/messages/%s", msgID), nil, nil)
}

func (c *Client) ToggleReaction(msgID, emoji string) error {
	body := map[string]string{"emoji": emoji}
	return c.doJSON("PUT", fmt.Sprintf("/api/messages/%s/reactions", msgID), body, nil)
}

func (c *Client) PinMessage(msgID string, pinned bool) error {
	body := map[string]bool{"pinned": pinned}
	return c.doJSON("PUT", fmt.Sprintf("/api/messages/%s/pin", msgID), body, nil)
}

func (c *Client) HideMessage(msgID string, hidden bool) error {
	body := map[string]bool{"hidden": hidden}
	return c.doJSON("PUT", fmt.Sprintf("/api/messages/%s/hide", msgID), body, nil)
}

func (c *Client) HideUserMessages(userID string) error {
	return c.doJSON("PUT", fmt.Sprintf("/api/users/%s/messages/hide", userID), nil, nil)
}

func (c *Client) DeleteUserMessages(userID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/users/%s/messages", userID), nil, nil)
}

func (c *Client) SearchMessages(channelID, query string, limit, offset int) ([]Message, error) {
	path := fmt.Sprintf("/api/channels/%s/messages/search?q=%s&limit=%d&offset=%d",
		channelID, url.QueryEscape(query), limit, offset)
	var messages []Message
	err := c.doJSON("GET", path, nil, &messages)
	return messages, err
}

func (c *Client) GetPinnedMessages(channelID string) ([]Message, error) {
	var messages []Message
	err := c.doJSON("GET", fmt.Sprintf("/api/channels/%s/pins", channelID), nil, &messages)
	return messages, err
}

func (c *Client) GetMessageThread(messageID string) ([]Message, error) {
	var messages []Message
	err := c.doJSON("GET", fmt.Sprintf("/api/messages/%s/thread", messageID), nil, &messages)
	return messages, err
}

// GetMessageEditHistory vrátí historii editací zprávy.
func (c *Client) GetMessageEditHistory(msgID string) ([]MessageEdit, error) {
	var edits []MessageEdit
	err := c.doJSON("GET", fmt.Sprintf("/api/messages/%s/history", msgID), nil, &edits)
	return edits, err
}

// Users

func (c *Client) GetUsers() ([]User, error) {
	var users []User
	err := c.doJSON("GET", "/api/users", nil, &users)
	return users, err
}

// DM

func (c *Client) GetDMConversations() ([]DMConversation, error) {
	var convs []DMConversation
	err := c.doJSON("GET", "/api/dm", nil, &convs)
	return convs, err
}

func (c *Client) CreateDMConversation(userID string) (*DMConversation, error) {
	body := map[string]string{"user_id": userID}
	var conv DMConversation
	err := c.doJSON("POST", "/api/dm", body, &conv)
	return &conv, err
}

func (c *Client) GetDMPending(convID string) ([]DMPendingMessage, error) {
	var msgs []DMPendingMessage
	err := c.doJSON("GET", fmt.Sprintf("/api/dm/%s/pending", convID), nil, &msgs)
	return msgs, err
}

func (c *Client) SendDM(convID, encryptedContent, replyToID string) (*DMPendingMessage, error) {
	body := map[string]string{"encrypted_content": encryptedContent}
	if replyToID != "" {
		body["reply_to_id"] = replyToID
	}
	var msg DMPendingMessage
	err := c.doJSON("POST", fmt.Sprintf("/api/dm/%s/messages", convID), body, &msg)
	return &msg, err
}

func (c *Client) DeleteDMConversation(convID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/dm/%s", convID), nil, nil)
}

// Friends

func (c *Client) GetFriends() ([]User, error) {
	var users []User
	err := c.doJSON("GET", "/api/friends", nil, &users)
	return users, err
}

func (c *Client) SendFriendRequest(userID string) error {
	body := map[string]string{"user_id": userID}
	return c.doJSON("POST", "/api/friends/requests", body, nil)
}

func (c *Client) RemoveFriend(userID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/friends/%s", userID), nil, nil)
}

func (c *Client) GetFriendRequests() (*FriendRequestList, error) {
	var list FriendRequestList
	err := c.doJSON("GET", "/api/friends/requests", nil, &list)
	return &list, err
}

func (c *Client) AcceptFriendRequest(requestID string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/friends/requests/%s/accept", requestID), nil, nil)
}

func (c *Client) DeclineFriendRequest(requestID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/friends/requests/%s/decline", requestID), nil, nil)
}

// Blocks

func (c *Client) GetBlocks() ([]User, error) {
	var users []User
	err := c.doJSON("GET", "/api/blocks", nil, &users)
	return users, err
}

func (c *Client) BlockUser(userID string) error {
	body := map[string]string{"user_id": userID}
	return c.doJSON("POST", "/api/blocks", body, nil)
}

func (c *Client) UnblockUser(userID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/blocks/%s", userID), nil, nil)
}

// Invites

func (c *Client) GetInvites() ([]Invite, error) {
	var invites []Invite
	err := c.doJSON("GET", "/api/invites", nil, &invites)
	return invites, err
}

func (c *Client) CreateInvite(maxUses, expiresIn int) (*Invite, error) {
	body := map[string]int{"max_uses": maxUses, "expires_in": expiresIn}
	var inv Invite
	err := c.doJSON("POST", "/api/invites", body, &inv)
	return &inv, err
}

func (c *Client) DeleteInvite(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/invites/%s", id), nil, nil)
}

// Roles

func (c *Client) GetRoles() ([]Role, error) {
	var roles []Role
	err := c.doJSON("GET", "/api/roles", nil, &roles)
	return roles, err
}

func (c *Client) CreateRole(name string, permissions int64) (*Role, error) {
	body := map[string]interface{}{"name": name, "permissions": permissions}
	var role Role
	err := c.doJSON("POST", "/api/roles", body, &role)
	return &role, err
}

func (c *Client) UpdateRole(id string, name string, permissions int64, color string) error {
	body := map[string]interface{}{"name": name, "permissions": permissions, "color": color}
	return c.doJSON("PATCH", fmt.Sprintf("/api/roles/%s", id), body, nil)
}

func (c *Client) DeleteRole(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/roles/%s", id), nil, nil)
}

func (c *Client) SwapRolePositions(roleID1, roleID2 string) error {
	body := map[string]string{"role_id_1": roleID1, "role_id_2": roleID2}
	return c.doJSON("POST", "/api/roles/swap", body, nil)
}

func (c *Client) GetUserRoles(userID string) ([]Role, error) {
	var roles []Role
	err := c.doJSON("GET", fmt.Sprintf("/api/users/%s/roles", userID), nil, &roles)
	return roles, err
}

func (c *Client) AssignRole(userID, roleID string) error {
	return c.doJSON("PUT", fmt.Sprintf("/api/users/%s/roles/%s", userID, roleID), nil, nil)
}

func (c *Client) RemoveRole(userID, roleID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/users/%s/roles/%s", userID, roleID), nil, nil)
}

// Timeout & Ban

func (c *Client) TimeoutUser(userID string, duration int, reason string) (*Timeout, error) {
	body := map[string]interface{}{"duration": duration, "reason": reason}
	var t Timeout
	err := c.doJSON("POST", fmt.Sprintf("/api/users/%s/timeout", userID), body, &t)
	return &t, err
}

func (c *Client) BanUser(userID, reason string, banIP, banDevice, revokeInvites, deleteMessages bool, duration int) error {
	body := map[string]interface{}{
		"user_id":         userID,
		"reason":          reason,
		"ban_ip":          banIP,
		"ban_device":      banDevice,
		"revoke_invites":  revokeInvites,
		"delete_messages": deleteMessages,
		"duration":        duration,
	}
	return c.doJSON("POST", "/api/bans", body, nil)
}

func (c *Client) UnbanUser(userID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/bans/%s", userID), nil, nil)
}

func (c *Client) GetBans() ([]Ban, error) {
	var bans []Ban
	err := c.doJSON("GET", "/api/bans", nil, &bans)
	return bans, err
}

// Groups

func (c *Client) GetGroups() ([]Group, error) {
	var groups []Group
	err := c.doJSON("GET", "/api/groups", nil, &groups)
	return groups, err
}

func (c *Client) CreateGroup(name string) (*Group, error) {
	body := map[string]string{"name": name}
	var group Group
	err := c.doJSON("POST", "/api/groups", body, &group)
	return &group, err
}

func (c *Client) GetGroup(id string) (*Group, error) {
	var group Group
	err := c.doJSON("GET", fmt.Sprintf("/api/groups/%s", id), nil, &group)
	return &group, err
}

func (c *Client) DeleteGroup(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/groups/%s", id), nil, nil)
}

func (c *Client) LeaveGroup(id string, userID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/groups/%s/members/%s", id, userID), nil, nil)
}

func (c *Client) JoinGroupByInvite(groupID, inviteCode string) (*Group, error) {
	body := map[string]string{"invite_code": inviteCode}
	var group Group
	err := c.doJSON("POST", fmt.Sprintf("/api/groups/%s/members", groupID), body, &group)
	return &group, err
}

func (c *Client) RelayGroupMessage(groupID, encryptedContent string) error {
	body := map[string]string{"encrypted_content": encryptedContent}
	return c.doJSON("POST", fmt.Sprintf("/api/groups/%s/messages", groupID), body, nil)
}

// GroupAttachmentPayload pro odesílání group zpráv s přílohami.
type GroupAttachmentPayload struct {
	Filename string `json:"filename"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
}

func (c *Client) RelayGroupMessageWithAttachments(groupID, encryptedContent string, attachments []GroupAttachmentPayload) error {
	body := map[string]any{
		"encrypted_content": encryptedContent,
	}
	if len(attachments) > 0 {
		body["attachments"] = attachments
	}
	return c.doJSON("POST", fmt.Sprintf("/api/groups/%s/messages", groupID), body, nil)
}

func (c *Client) CreateGroupInvite(groupID string, maxUses, expiresIn int) (*GroupInvite, error) {
	body := map[string]int{"max_uses": maxUses, "expires_in": expiresIn}
	var inv GroupInvite
	err := c.doJSON("POST", fmt.Sprintf("/api/groups/%s/invites", groupID), body, &inv)
	return &inv, err
}

func (c *Client) GetGroupInvites(groupID string) ([]GroupInvite, error) {
	var invites []GroupInvite
	err := c.doJSON("GET", fmt.Sprintf("/api/groups/%s/invites", groupID), nil, &invites)
	return invites, err
}

// Emojis

func (c *Client) GetEmojis() ([]CustomEmoji, error) {
	var emojis []CustomEmoji
	err := c.doJSON("GET", "/api/emojis", nil, &emojis)
	return emojis, err
}

// User Profile

func (c *Client) UpdateDisplayName(displayName string) error {
	return c.doJSON("PATCH", "/api/users/me", map[string]string{"display_name": displayName}, nil)
}

func (c *Client) UpdateStatus(status, statusText string) error {
	return c.doJSON("PATCH", "/api/users/me", map[string]string{"status": status, "status_text": statusText}, nil)
}

// Server Settings

func (c *Client) GetServerSettings() (map[string]interface{}, error) {
	var settings map[string]interface{}
	err := c.doJSON("GET", "/api/server/settings", nil, &settings)
	return settings, err
}

func (c *Client) UpdateServerSettings(settings map[string]interface{}) error {
	return c.doJSON("PATCH", "/api/server/settings", settings, nil)
}

// Upload

func (c *Client) UploadFile(filename string, data []byte) (*Attachment, error) {
	status, respBody, err := c.doHTTP(func(token string) (*http.Request, error) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", filename)
		part.Write(data)
		writer.Close()
		req, err := http.NewRequest("POST", c.BaseURL+"/api/upload", body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(respBody))
	}
	var att Attachment
	if err := json.Unmarshal(respBody, &att); err != nil {
		return nil, err
	}
	return &att, nil
}

// CreateEmoji uploads a custom emoji.
func (c *Client) CreateEmoji(emojiName string, filename string, data []byte) (*CustomEmoji, error) {
	status, respBody, err := c.doHTTP(func(token string) (*http.Request, error) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.WriteField("name", emojiName)
		part, _ := writer.CreateFormFile("file", filename)
		part.Write(data)
		writer.Close()
		req, err := http.NewRequest("POST", c.BaseURL+"/api/emojis", body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("upload emoji: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(respBody))
	}
	var emoji CustomEmoji
	if err := json.Unmarshal(respBody, &emoji); err != nil {
		return nil, err
	}
	return &emoji, nil
}

func (c *Client) DeleteEmoji(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/emojis/%s", id), nil, nil)
}

// UploadResult je výsledek uploadu (odpovídá serveru).
type UploadResult struct {
	Filename    string `json:"filename"`
	Original    string `json:"original"`
	URL         string `json:"url"`
	MimeType    string `json:"mime_type"`
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
}

// InitChunkedUpload zahájí chunked upload session.
func (c *Client) InitChunkedUpload(filename string, size int64) (uploadID string, chunkSize int, err error) {
	body := map[string]interface{}{"filename": filename, "size": size}
	var resp struct {
		UploadID  string `json:"upload_id"`
		ChunkSize int    `json:"chunk_size"`
	}
	err = c.doJSON("POST", "/api/upload/init", body, &resp)
	return resp.UploadID, resp.ChunkSize, err
}

// UploadChunk pošle jeden chunk; vrátí nový offset a pokud hotovo, UploadResult.
func (c *Client) UploadChunk(uploadID string, offset int64, data []byte) (newOffset int64, result *UploadResult, err error) {
	status, respBody, err := c.doHTTP(func(token string) (*http.Request, error) {
		req, err := http.NewRequest("PATCH", c.BaseURL+fmt.Sprintf("/api/upload/%s", uploadID), bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Upload-Offset", fmt.Sprintf("%d", offset))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req, nil
	})
	if err != nil {
		return 0, nil, fmt.Errorf("upload chunk: %w", err)
	}
	if status >= 400 {
		return 0, nil, fmt.Errorf("HTTP %d: %s", status, string(respBody))
	}

	if status == 201 {
		var res UploadResult
		if err := json.Unmarshal(respBody, &res); err != nil {
			return 0, nil, err
		}
		return 0, &res, nil
	}

	var partial struct {
		Offset int64 `json:"offset"`
	}
	if err := json.Unmarshal(respBody, &partial); err != nil {
		return 0, nil, err
	}
	return partial.Offset, nil, nil
}

// UploadFileChunked — high-level wrapper: init + loop chunků + progress callback.
func (c *Client) UploadFileChunked(filename string, data []byte, onProgress func(sent, total int64)) (*UploadResult, error) {
	size := int64(len(data))

	uploadID, chunkSize, err := c.InitChunkedUpload(filename, size)
	if err != nil {
		return nil, fmt.Errorf("init upload: %w", err)
	}
	if chunkSize <= 0 {
		chunkSize = 256 * 1024
	}

	var offset int64
	for offset < size {
		end := offset + int64(chunkSize)
		if end > size {
			end = size
		}
		chunk := data[offset:end]

		newOffset, result, err := c.UploadChunk(uploadID, offset, chunk)
		if err != nil {
			return nil, fmt.Errorf("chunk at offset %d: %w", offset, err)
		}

		if result != nil {
			if onProgress != nil {
				onProgress(size, size)
			}
			return result, nil
		}

		offset = newOffset
		if onProgress != nil {
			onProgress(offset, size)
		}
	}

	return nil, fmt.Errorf("upload completed without final response")
}

// PollCreateRequest popisuje anketu pro odeslání se zprávou.
type PollCreateRequest struct {
	Question  string     `json:"question"`
	PollType  string     `json:"poll_type"`
	Options   []string   `json:"options"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// SendMessageWithAttachments sends a message with attachment objects and optional poll.
func (c *Client) SendMessageWithAttachments(channelID, content string, replyToID string, attachments []UploadResult) (*Message, error) {
	return c.SendMessageFull(channelID, content, replyToID, attachments, nil)
}

// SendMessageFull sends a message with optional attachments and poll.
func (c *Client) SendMessageFull(channelID, content string, replyToID string, attachments []UploadResult, poll *PollCreateRequest) (*Message, error) {
	body := map[string]interface{}{"content": content}
	if replyToID != "" {
		body["reply_to_id"] = replyToID
	}
	if len(attachments) > 0 {
		atts := make([]map[string]interface{}, len(attachments))
		for i, a := range attachments {
			atts[i] = map[string]interface{}{
				"filename":     a.Original,
				"url":          a.URL,
				"mime_type":    a.MimeType,
				"size":         a.Size,
				"content_hash": a.ContentHash,
			}
		}
		body["attachments"] = atts
	}
	if poll != nil {
		body["poll"] = poll
	}
	var msg Message
	err := c.doJSON("POST", fmt.Sprintf("/api/channels/%s/messages", channelID), body, &msg)
	return &msg, err
}

// VotePoll toggles a vote on a poll option.
func (c *Client) VotePoll(pollID, optionID string) (*Poll, error) {
	body := map[string]string{"option_id": optionID}
	var poll Poll
	err := c.doJSON("PUT", fmt.Sprintf("/api/polls/%s/vote", pollID), body, &poll)
	return &poll, err
}

func (c *Client) UploadServerIcon(filename string, data []byte) (string, error) {
	status, respBody, err := c.doHTTP(func(token string) (*http.Request, error) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("icon", filename)
		part.Write(data)
		writer.Close()
		req, err := http.NewRequest("POST", c.BaseURL+"/api/server/icon", body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req, nil
	})
	if err != nil {
		return "", fmt.Errorf("upload icon: %w", err)
	}
	if status >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", status, string(respBody))
	}
	var result struct {
		IconURL string `json:"icon_url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}
	return result.IconURL, nil
}

func (c *Client) DeleteServerIcon() error {
	return c.doJSON("DELETE", "/api/server/icon", nil, nil)
}

// Avatar

func (c *Client) UploadAvatar(filename string, data []byte) (*User, error) {
	status, respBody, err := c.doHTTP(func(token string) (*http.Request, error) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", filename)
		part.Write(data)
		writer.Close()
		req, err := http.NewRequest("POST", c.BaseURL+"/api/users/me/avatar", body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("upload avatar: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(respBody))
	}
	var user User
	if err := json.Unmarshal(respBody, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (c *Client) DeleteAvatar() (*User, error) {
	var user User
	err := c.doJSON("DELETE", "/api/users/me/avatar", nil, &user)
	return &user, err
}

// LAN Party

func (c *Client) GetLANParties() (*LANPartiesResponse, error) {
	var resp LANPartiesResponse
	err := c.doJSON("GET", "/api/lan", nil, &resp)
	return &resp, err
}

func (c *Client) CreateLANParty(name string) (*LANParty, error) {
	body := map[string]string{"name": name}
	var party LANParty
	err := c.doJSON("POST", "/api/lan", body, &party)
	return &party, err
}

func (c *Client) JoinLANParty(partyID, publicKey string) (*JoinLANResponse, error) {
	body := map[string]string{"public_key": publicKey}
	var resp JoinLANResponse
	err := c.doJSON("POST", fmt.Sprintf("/api/lan/%s/join", partyID), body, &resp)
	return &resp, err
}

func (c *Client) LeaveLANParty(partyID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/lan/%s/leave", partyID), nil, nil)
}

func (c *Client) DeleteLANParty(partyID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/lan/%s", partyID), nil, nil)
}

// Gallery

func (c *Client) GetGallery(mimeType, channelID, userID, search string, limit int) ([]GalleryItem, error) {
	path := fmt.Sprintf("/api/gallery?limit=%d", limit)
	if mimeType != "" {
		path += "&type=" + url.QueryEscape(mimeType)
	}
	if channelID != "" {
		path += "&channel_id=" + url.QueryEscape(channelID)
	}
	if userID != "" {
		path += "&user_id=" + url.QueryEscape(userID)
	}
	if search != "" {
		path += "&search=" + url.QueryEscape(search)
	}
	var items []GalleryItem
	err := c.doJSON("GET", path, nil, &items)
	return items, err
}

// File Storage

func (c *Client) ListStorageFolders(parentID *string) ([]StorageFolder, error) {
	path := "/api/storage/folders"
	if parentID != nil {
		path += "?parent_id=" + url.QueryEscape(*parentID)
	}
	var folders []StorageFolder
	err := c.doJSON("GET", path, nil, &folders)
	return folders, err
}

func (c *Client) ListStorageFiles(folderID *string) ([]StorageFile, error) {
	path := "/api/storage/files"
	if folderID != nil {
		path += "?folder_id=" + url.QueryEscape(*folderID)
	}
	var files []StorageFile
	err := c.doJSON("GET", path, nil, &files)
	return files, err
}

// Voice

type VoiceStateResponse struct {
	Channels      map[string][]string `json:"channels"`
	ScreenSharers map[string]string   `json:"screen_sharers"`
}

func (c *Client) GetVoiceState() (*VoiceStateResponse, error) {
	var resp VoiceStateResponse
	err := c.doJSON("GET", "/api/voice/state", nil, &resp)
	return &resp, err
}

// VoiceMove přesune uživatele do jiného voice kanálu (vyžaduje PermKick)
func (c *Client) VoiceMove(userID, channelID string) error {
	return c.doJSON("POST", "/api/voice/move", map[string]string{
		"user_id":    userID,
		"channel_id": channelID,
	}, nil)
}

// File Sharing

func (c *Client) GetShares() (*ShareListResponse, error) {
	var resp ShareListResponse
	err := c.doJSON("GET", "/api/shares", nil, &resp)
	return &resp, err
}

func (c *Client) CreateShare(pathHash, displayName string) (*SharedDirectory, error) {
	body := map[string]string{"path_hash": pathHash, "display_name": displayName}
	var dir SharedDirectory
	err := c.doJSON("POST", "/api/shares", body, &dir)
	return &dir, err
}

func (c *Client) GetShare(id string) (*ShareDetailResponse, error) {
	var resp ShareDetailResponse
	err := c.doJSON("GET", fmt.Sprintf("/api/shares/%s", id), nil, &resp)
	return &resp, err
}

func (c *Client) UpdateShare(id string, body map[string]interface{}) (*SharedDirectory, error) {
	var dir SharedDirectory
	err := c.doJSON("PATCH", fmt.Sprintf("/api/shares/%s", id), body, &dir)
	return &dir, err
}

func (c *Client) DeleteShare(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/shares/%s", id), nil, nil)
}

func (c *Client) GetShareStats(shareID string) (totalSize int64, filesCount int, err error) {
	var resp struct {
		TotalSize  int64 `json:"total_size"`
		FilesCount int   `json:"files_count"`
	}
	err = c.doJSON("GET", fmt.Sprintf("/api/shares/%s/stats", shareID), nil, &resp)
	return resp.TotalSize, resp.FilesCount, err
}

func (c *Client) GetSharePermissions(shareID string) ([]SharePermission, error) {
	var perms []SharePermission
	err := c.doJSON("GET", fmt.Sprintf("/api/shares/%s/permissions", shareID), nil, &perms)
	return perms, err
}

func (c *Client) SetSharePermission(shareID string, granteeID *string, canRead, canWrite, canDelete, isBlocked bool) ([]SharePermission, error) {
	body := map[string]interface{}{
		"grantee_id": granteeID,
		"can_read":   canRead,
		"can_write":  canWrite,
		"can_delete": canDelete,
		"is_blocked": isBlocked,
	}
	var perms []SharePermission
	err := c.doJSON("POST", fmt.Sprintf("/api/shares/%s/permissions", shareID), body, &perms)
	return perms, err
}

func (c *Client) DeleteSharePermission(shareID, permID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/shares/%s/permissions/%s", shareID, permID), nil, nil)
}

func (c *Client) GetShareFiles(shareID, path string) ([]SharedFileEntry, error) {
	u := fmt.Sprintf("/api/shares/%s/files", shareID)
	if path != "" {
		u += "?path=" + url.QueryEscape(path)
	}
	var files []SharedFileEntry
	err := c.doJSON("GET", u, nil, &files)
	return files, err
}

func (c *Client) SyncShareFiles(shareID string, files []map[string]interface{}) error {
	body := map[string]interface{}{"files": files}
	return c.doJSON("POST", fmt.Sprintf("/api/shares/%s/files/sync", shareID), body, nil)
}

func (c *Client) RequestUpload(shareID, fileName string, fileSize int64, relativePath string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"file_name":     fileName,
		"file_size":     fileSize,
		"relative_path": relativePath,
	}
	var resp map[string]interface{}
	err := c.doJSON("POST", fmt.Sprintf("/api/shares/%s/upload/request", shareID), body, &resp)
	return resp, err
}

func (c *Client) DeleteShareFile(shareID, relativePath, fileName string) error {
	body := map[string]string{
		"relative_path": relativePath,
		"file_name":     fileName,
	}
	return c.doJSON("DELETE", fmt.Sprintf("/api/shares/%s/files", shareID), body, nil)
}

func (c *Client) RequestTransfer(shareID, fileID string) (map[string]interface{}, error) {
	body := map[string]string{"file_id": fileID}
	var resp map[string]interface{}
	err := c.doJSON("POST", fmt.Sprintf("/api/shares/%s/transfer/request", shareID), body, &resp)
	return resp, err
}

// Swarm P2P sharing

func (c *Client) SwarmAddSeed(shareID, fileID string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/shares/%s/swarm/seed", shareID), map[string]string{"file_id": fileID}, nil)
}

func (c *Client) SwarmRemoveSeed(shareID, fileID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/shares/%s/swarm/seed/%s", shareID, fileID), nil, nil)
}

func (c *Client) SwarmSources(shareID, fileID string) (*SwarmSourcesResponse, error) {
	var resp SwarmSourcesResponse
	err := c.doJSON("GET", fmt.Sprintf("/api/shares/%s/swarm/sources/%s", shareID, fileID), nil, &resp)
	return &resp, err
}

func (c *Client) SwarmCounts(shareID string) (map[string]int, error) {
	var resp struct {
		Counts map[string]int `json:"counts"`
	}
	err := c.doJSON("GET", fmt.Sprintf("/api/shares/%s/swarm/counts", shareID), nil, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Counts, nil
}

func (c *Client) SwarmRequest(shareID, fileID string) (*SwarmRequestResponse, error) {
	var resp SwarmRequestResponse
	err := c.doJSON("POST", fmt.Sprintf("/api/shares/%s/swarm/request", shareID), map[string]string{"file_id": fileID}, &resp)
	return &resp, err
}

// Game Servers

func (c *Client) GetGameServers() ([]GameServerInstance, error) {
	var servers []GameServerInstance
	err := c.doJSON("GET", "/api/gameservers", nil, &servers)
	return servers, err
}

func (c *Client) GetGameServerPresets() ([]GameServerPreset, error) {
	var presets []GameServerPreset
	err := c.doJSON("GET", "/api/gameservers/presets", nil, &presets)
	return presets, err
}

func (c *Client) CreateGameServer(name, preset string) (*GameServerInstance, error) {
	body := map[string]string{"name": name, "preset": preset}
	var gs GameServerInstance
	err := c.doJSON("POST", "/api/gameservers", body, &gs)
	return &gs, err
}

func (c *Client) DeleteGameServer(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/gameservers/%s", id), nil, nil)
}

func (c *Client) StartGameServer(id string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/gameservers/%s/start", id), nil, nil)
}

func (c *Client) StopGameServer(id string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/gameservers/%s/stop", id), nil, nil)
}

func (c *Client) RestartGameServer(id string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/gameservers/%s/restart", id), nil, nil)
}

func (c *Client) GetGameServerStats(id string) (*GameServerStats, error) {
	var stats GameServerStats
	err := c.doJSON("GET", fmt.Sprintf("/api/gameservers/%s/stats", id), nil, &stats)
	return &stats, err
}

func (c *Client) ListGameServerFiles(id, path string) ([]GameServerFileEntry, error) {
	var entries []GameServerFileEntry
	endpoint := fmt.Sprintf("/api/gameservers/%s/files", id)
	if path != "" {
		endpoint += "?path=" + url.QueryEscape(path)
	}
	err := c.doJSON("GET", endpoint, nil, &entries)
	return entries, err
}

func (c *Client) ReadGameServerFile(id, path string) (string, error) {
	var resp struct {
		Content string `json:"content"`
	}
	endpoint := fmt.Sprintf("/api/gameservers/%s/files/content?path=%s", id, url.QueryEscape(path))
	err := c.doJSON("GET", endpoint, nil, &resp)
	return resp.Content, err
}

func (c *Client) WriteGameServerFile(id, path, content string) error {
	body := map[string]string{
		"path":    path,
		"content": content,
	}
	return c.doJSON("PUT", fmt.Sprintf("/api/gameservers/%s/files/content", id), body, nil)
}

func (c *Client) DeleteGameServerFile(id, path string) error {
	endpoint := fmt.Sprintf("/api/gameservers/%s/files?path=%s", id, url.QueryEscape(path))
	return c.doJSON("DELETE", endpoint, nil, nil)
}

func (c *Client) MkdirGameServer(id, path string) error {
	body := map[string]string{"path": path}
	return c.doJSON("POST", fmt.Sprintf("/api/gameservers/%s/files/mkdir", id), body, nil)
}

func (c *Client) ListGameServerFilesRecursive(id, path string) ([]GameServerRecursiveEntry, error) {
	var entries []GameServerRecursiveEntry
	endpoint := fmt.Sprintf("/api/gameservers/%s/files/recursive", id)
	if path != "" {
		endpoint += "?path=" + url.QueryEscape(path)
	}
	err := c.doJSON("GET", endpoint, nil, &entries)
	return entries, err
}

func (c *Client) GameServerFileDownloadURL(gsID, path string) string {
	return fmt.Sprintf("/api/gameservers/%s/files/download?path=%s", gsID, url.QueryEscape(path))
}

func (c *Client) UploadGameServerFile(id, filePath, filename string, data []byte) error {
	endpoint := fmt.Sprintf("/api/gameservers/%s/files/upload?path=%s", id, url.QueryEscape(filePath))
	status, respBody, err := c.doHTTP(func(token string) (*http.Request, error) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		part, _ := w.CreateFormFile("file", filename)
		part.Write(data)
		w.Close()
		req, err := http.NewRequest("POST", c.BaseURL+endpoint, &buf)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return req, nil
	})
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("upload failed: %s", string(respBody))
	}
	return nil
}

// DownloadGameServerFile stáhne soubor z game serveru do lokálního souboru.
func (c *Client) DownloadGameServerFile(gsID, filePath, savePath string) error {
	endpoint := c.BaseURL + c.GameServerFileDownloadURL(gsID, filePath)
	for attempt := 0; attempt < 2; attempt++ {
		token := c.GetAccessToken()
		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			return err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == 401 && attempt == 0 && c.tryRefresh() {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
		}
		defer resp.Body.Close()
		out, err := os.Create(savePath)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, resp.Body)
		out.Close()
		if err != nil {
			os.Remove(savePath)
			return err
		}
		return nil
	}
	return fmt.Errorf("download failed after retries")
}

// Game server room (access control)

func (c *Client) JoinGameServer(id string) error {
	return c.doJSON("POST", "/api/gameservers/"+id+"/join", nil, nil)
}

func (c *Client) LeaveGameServer(id string) error {
	return c.doJSON("POST", "/api/gameservers/"+id+"/leave", nil, nil)
}

func (c *Client) GetGameServerMembers(id string) ([]GameServerMember, error) {
	var members []GameServerMember
	err := c.doJSON("GET", "/api/gameservers/"+id+"/members", nil, &members)
	return members, err
}

func (c *Client) SetGameServerAccess(id, mode string) error {
	return c.doJSON("PUT", "/api/gameservers/"+id+"/access", map[string]string{"mode": mode}, nil)
}

// GameServerRCON vykoná RCON příkaz na běžícím game serveru (Source RCON protokol)
func (c *Client) GameServerRCON(id, command string) (string, error) {
	var resp struct {
		Response string `json:"response"`
	}
	err := c.doJSON("POST", fmt.Sprintf("/api/gameservers/%s/rcon", id), map[string]string{"command": command}, &resp)
	return resp.Response, err
}

// VPN Tunnels

type CreateTunnelResponse struct {
	Tunnel  Tunnel            `json:"tunnel"`
	WGConfig map[string]string `json:"wg_config"`
}

type AcceptTunnelResponse struct {
	Tunnel   Tunnel            `json:"tunnel"`
	WGConfig map[string]string `json:"wg_config"`
}

func (c *Client) GetTunnels() ([]Tunnel, error) {
	var tunnels []Tunnel
	err := c.doJSON("GET", "/api/tunnels", nil, &tunnels)
	return tunnels, err
}

func (c *Client) CreateTunnel(targetID, wgPubKey string) (*CreateTunnelResponse, error) {
	var resp CreateTunnelResponse
	err := c.doJSON("POST", "/api/tunnels", map[string]string{
		"target_id": targetID,
		"wg_pubkey": wgPubKey,
	}, &resp)
	return &resp, err
}

func (c *Client) AcceptTunnel(id, wgPubKey string) (*AcceptTunnelResponse, error) {
	var resp AcceptTunnelResponse
	err := c.doJSON("POST", fmt.Sprintf("/api/tunnels/%s/accept", id), map[string]string{
		"wg_pubkey": wgPubKey,
	}, &resp)
	return &resp, err
}

func (c *Client) CloseTunnel(id string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/tunnels/%s/close", id), nil, nil)
}

// Server Storage (owner only)

func (c *Client) GetServerStorage() (*ServerStorageInfo, error) {
	var info ServerStorageInfo
	err := c.doJSON("GET", "/api/server/storage", nil, &info)
	return &info, err
}

func (c *Client) UpdateServerStorage(maxMB *int, historyLimit *int) error {
	body := map[string]interface{}{}
	if maxMB != nil {
		body["max_mb"] = *maxMB
	}
	if historyLimit != nil {
		body["channel_history_limit"] = *historyLimit
	}
	return c.doJSON("PATCH", "/api/server/storage", body, nil)
}

// Docker management

func (c *Client) GetDockerStatus() (map[string]any, error) {
	var resp map[string]any
	err := c.doJSON("GET", "/api/gameservers/docker-status", nil, &resp)
	return resp, err
}

func (c *Client) InstallDocker() error {
	return c.doJSON("POST", "/api/gameservers/install-docker", nil, nil)
}

// Whiteboard

func (c *Client) GetWhiteboards() ([]Whiteboard, error) {
	var boards []Whiteboard
	err := c.doJSON("GET", "/api/whiteboards", nil, &boards)
	return boards, err
}

func (c *Client) CreateWhiteboard(name string) (*Whiteboard, error) {
	body := map[string]string{"name": name}
	var wb Whiteboard
	err := c.doJSON("POST", "/api/whiteboards", body, &wb)
	return &wb, err
}

func (c *Client) DeleteWhiteboard(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/whiteboards/%s", id), nil, nil)
}

func (c *Client) GetWhiteboardStrokes(id string) ([]WhiteboardStroke, error) {
	var strokes []WhiteboardStroke
	err := c.doJSON("GET", fmt.Sprintf("/api/whiteboards/%s/strokes", id), nil, &strokes)
	return strokes, err
}

func (c *Client) AddWhiteboardStroke(boardID string, stroke WhiteboardStroke) (*WhiteboardStroke, error) {
	body := map[string]interface{}{
		"path_data": stroke.PathData,
		"color":     stroke.Color,
		"width":     stroke.Width,
		"tool":      stroke.Tool,
	}
	var result WhiteboardStroke
	err := c.doJSON("POST", fmt.Sprintf("/api/whiteboards/%s/strokes", boardID), body, &result)
	return &result, err
}

func (c *Client) UndoWhiteboardStroke(boardID string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/whiteboards/%s/undo", boardID), nil, nil)
}

func (c *Client) ClearWhiteboard(boardID string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/whiteboards/%s/clear", boardID), nil, nil)
}

// Scheduled messages

func (c *Client) ScheduleMessage(channelID, content, replyToID string, scheduledAt time.Time) (*ScheduledMessage, error) {
	body := map[string]interface{}{
		"content":      content,
		"scheduled_at": scheduledAt.UTC(),
	}
	if replyToID != "" {
		body["reply_to_id"] = replyToID
	}
	var msg ScheduledMessage
	err := c.doJSON("POST", fmt.Sprintf("/api/channels/%s/messages/schedule", channelID), body, &msg)
	return &msg, err
}

func (c *Client) ListScheduledMessages() ([]ScheduledMessage, error) {
	var msgs []ScheduledMessage
	err := c.doJSON("GET", "/api/scheduled-messages", nil, &msgs)
	return msgs, err
}

func (c *Client) DeleteScheduledMessage(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/scheduled-messages/%s", id), nil, nil)
}

// Kanban

func (c *Client) ListKanbanBoards() ([]KanbanBoard, error) {
	var boards []KanbanBoard
	err := c.doJSON("GET", "/api/kanban", nil, &boards)
	return boards, err
}

func (c *Client) CreateKanbanBoard(name, desc string) (*KanbanBoard, error) {
	body := map[string]string{"name": name, "description": desc}
	var board KanbanBoard
	err := c.doJSON("POST", "/api/kanban", body, &board)
	return &board, err
}

func (c *Client) GetKanbanBoard(id string) (*KanbanBoard, error) {
	var board KanbanBoard
	err := c.doJSON("GET", fmt.Sprintf("/api/kanban/%s", id), nil, &board)
	return &board, err
}

func (c *Client) DeleteKanbanBoard(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/kanban/%s", id), nil, nil)
}

func (c *Client) CreateKanbanColumn(boardID, title, color string) (*KanbanColumn, error) {
	body := map[string]string{"title": title, "color": color}
	var col KanbanColumn
	err := c.doJSON("POST", fmt.Sprintf("/api/kanban/%s/columns", boardID), body, &col)
	return &col, err
}

func (c *Client) UpdateKanbanColumn(boardID, colID, title, color string) error {
	body := map[string]string{"title": title, "color": color}
	return c.doJSON("PATCH", fmt.Sprintf("/api/kanban/%s/columns/%s", boardID, colID), body, nil)
}

func (c *Client) ReorderKanbanColumns(boardID string, ids []string) error {
	body := map[string]interface{}{"column_ids": ids}
	return c.doJSON("POST", fmt.Sprintf("/api/kanban/%s/columns/reorder", boardID), body, nil)
}

func (c *Client) DeleteKanbanColumn(boardID, colID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/kanban/%s/columns/%s", boardID, colID), nil, nil)
}

func (c *Client) CreateKanbanCard(boardID, columnID, title string) (*KanbanCard, error) {
	body := map[string]string{"column_id": columnID, "title": title}
	var card KanbanCard
	err := c.doJSON("POST", fmt.Sprintf("/api/kanban/%s/cards", boardID), body, &card)
	return &card, err
}

func (c *Client) UpdateKanbanCard(cardID string, updates map[string]interface{}) (*KanbanCard, error) {
	var card KanbanCard
	err := c.doJSON("PATCH", fmt.Sprintf("/api/kanban/cards/%s", cardID), updates, &card)
	return &card, err
}

func (c *Client) MoveKanbanCard(cardID, columnID string, position int) error {
	body := map[string]interface{}{"column_id": columnID, "position": position}
	return c.doJSON("POST", fmt.Sprintf("/api/kanban/cards/%s/move", cardID), body, nil)
}

func (c *Client) DeleteKanbanCard(cardID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/kanban/cards/%s", cardID), nil, nil)
}

// Device bany

func (c *Client) GetDeviceBans() ([]DeviceBan, error) {
	var bans []DeviceBan
	err := c.doJSON("GET", "/api/bans/devices", nil, &bans)
	return bans, err
}

func (c *Client) DeleteDeviceBan(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/bans/devices/%s", id), nil, nil)
}

func (c *Client) GetUserDevices(userID string) ([]UserDevice, error) {
	var devices []UserDevice
	err := c.doJSON("GET", fmt.Sprintf("/api/devices/%s", userID), nil, &devices)
	return devices, err
}

// Invite chain

func (c *Client) GetInviteChain() ([]InviteChainNode, error) {
	var nodes []InviteChainNode
	err := c.doJSON("GET", "/api/invite-chain", nil, &nodes)
	return nodes, err
}

// Karanténa

func (c *Client) GetQuarantine() ([]QuarantineEntry, error) {
	var entries []QuarantineEntry
	err := c.doJSON("GET", "/api/quarantine", nil, &entries)
	return entries, err
}

func (c *Client) ApproveQuarantine(userID string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/quarantine/%s/approve", userID), nil, nil)
}

func (c *Client) RemoveQuarantine(userID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/quarantine/%s", userID), nil, nil)
}

// Approvals

func (c *Client) GetPendingApprovals() ([]PendingApproval, error) {
	var approvals []PendingApproval
	err := c.doJSON("GET", "/api/approvals", nil, &approvals)
	return approvals, err
}

func (c *Client) ApproveUser(userID string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/approvals/%s/approve", userID), nil, nil)
}

func (c *Client) RejectUser(userID string) error {
	return c.doJSON("POST", fmt.Sprintf("/api/approvals/%s/reject", userID), nil, nil)
}

// Calendar events

func (c *Client) ListEvents(from, to string) ([]Event, error) {
	var events []Event
	path := "/api/events"
	if from != "" || to != "" {
		path += "?"
		if from != "" {
			path += "from=" + from
		}
		if to != "" {
			if from != "" {
				path += "&"
			}
			path += "to=" + to
		}
	}
	err := c.doJSON("GET", path, nil, &events)
	return events, err
}

func (c *Client) CreateEvent(title, desc, location, color, startsAt string, endsAt *string, allDay bool, recurrenceRule string) (*Event, error) {
	body := map[string]interface{}{
		"title":           title,
		"description":     desc,
		"location":        location,
		"color":           color,
		"starts_at":       startsAt,
		"all_day":         allDay,
		"recurrence_rule": recurrenceRule,
	}
	if endsAt != nil {
		body["ends_at"] = *endsAt
	}
	var event Event
	err := c.doJSON("POST", "/api/events", body, &event)
	return &event, err
}

func (c *Client) GetEventDetail(id string) (*Event, error) {
	var event Event
	err := c.doJSON("GET", fmt.Sprintf("/api/events/%s", id), nil, &event)
	return &event, err
}

func (c *Client) UpdateEvent(id string, updates map[string]interface{}) (*Event, error) {
	var event Event
	err := c.doJSON("PATCH", fmt.Sprintf("/api/events/%s", id), updates, &event)
	return &event, err
}

func (c *Client) DeleteEvent(id string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/events/%s", id), nil, nil)
}

func (c *Client) SetEventReminder(eventID string, minutesBefore int) error {
	body := map[string]interface{}{"minutes_before": minutesBefore}
	return c.doJSON("POST", fmt.Sprintf("/api/events/%s/remind", eventID), body, nil)
}

func (c *Client) RemoveEventReminder(eventID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/events/%s/remind", eventID), nil, nil)
}

// Channel permission overrides

func (c *Client) GetChannelPermissions(channelID string) ([]ChannelPermOverride, error) {
	var overrides []ChannelPermOverride
	err := c.doJSON("GET", fmt.Sprintf("/api/channels/%s/permissions", channelID), nil, &overrides)
	return overrides, err
}

func (c *Client) SetChannelPermission(override ChannelPermOverride) error {
	return c.doJSON("PUT", fmt.Sprintf("/api/channels/%s/permissions", override.ChannelID), override, nil)
}

func (c *Client) DeleteChannelPermission(channelID, targetType, targetID string) error {
	return c.doJSON("DELETE", fmt.Sprintf("/api/channels/%s/permissions/%s/%s", channelID, targetType, targetID), nil, nil)
}

// BackupInfo vrátí informace o databázi serveru.
func (c *Client) BackupInfo() (*BackupInfo, error) {
	var info BackupInfo
	if err := c.doJSON("GET", "/api/admin/backup/info", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// BackupDownload stáhne zálohu databáze a uloží na disk.
func (c *Client) BackupDownload(savePath string) error {
	req, err := http.NewRequest("GET", c.BaseURL+"/api/admin/backup", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.GetAccessToken())

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("backup download failed: %d", resp.StatusCode)
	}

	f, err := os.Create(savePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// RestoreDatabase nahraje .db soubor na server jako obnovu.
func (c *Client) RestoreDatabase(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("database", filepath.Base(filePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	writer.Close()

	req, err := http.NewRequest("POST", c.BaseURL+"/api/admin/restore", body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.GetAccessToken())
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("restore failed: %d", resp.StatusCode)
	}
	return nil
}
