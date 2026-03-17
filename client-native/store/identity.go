package store

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// NotifyLevel defines the notification level.
type NotifyLevel int

const (
	NotifyAll      NotifyLevel = 0 // default — all sounds
	NotifyMentions NotifyLevel = 1 // only @mentions
	NotifyNothing  NotifyLevel = 2 // muted
)

// UnmarshalJSON handles both formats: old string ("displayName") and new object ({displayName, driveLetter}).
func (m *MountedShareInfo) UnmarshalJSON(data []byte) error {
	// Try old format: plain string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.DisplayName = s
		return nil
	}
	// New format: object
	type Alias MountedShareInfo
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*m = MountedShareInfo(a)
	return nil
}

type StoredIdentity struct {
	PublicKey       string         `json:"publicKey"`
	Username        string         `json:"username"`
	Encrypted       string         `json:"encrypted"`
	PasswordVerify  string         `json:"passwordVerify,omitempty"` // encrypted known string for wrong password detection
	Servers         []StoredServer `json:"servers,omitempty"`
	NotifyLevel    NotifyLevel    `json:"notifyLevel,omitempty"`
	NotifVolume    float64        `json:"notifVolume,omitempty"`    // DEPRECATED: use SoundVolumes
	CustomNotifSnd string         `json:"customNotifSnd,omitempty"` // DEPRECATED: use CustomSounds
	CustomDMSnd    string         `json:"customDmSnd,omitempty"`    // DEPRECATED: use CustomSounds
	SoundVolumes   map[string]float64 `json:"soundVolumes,omitempty"`   // soundKey → 0.0-1.0
	CustomSounds   map[string]string  `json:"customSounds,omitempty"`   // soundKey → file path
	VideoVolume    int            `json:"videoVolume,omitempty"`    // 0 → default 100 (range 0-200)
	YouTubeQuality int           `json:"youtubeQuality,omitempty"` // preferred height (360, 480, 720), 0 → auto (best ≤720p)
	FontScale      float64        `json:"fontScale,omitempty"`      // 0 → default 1.0 (range 0.7-1.6)
	MaxCacheBytes  int64          `json:"maxCacheBytes,omitempty"`  // 0 → default 2 GB
	MaxHistoryDays int            `json:"maxHistoryDays,omitempty"` // 0 → unlimited
	CompactMode    bool           `json:"compactMode,omitempty"`    // IRC-style compact messages
}

// MigrateSoundSettings migrates old flat sound fields to new maps.
// Called after loading an identity.
func (id *StoredIdentity) MigrateSoundSettings() bool {
	changed := false
	if id.SoundVolumes == nil {
		id.SoundVolumes = make(map[string]float64)
	}
	if id.CustomSounds == nil {
		id.CustomSounds = make(map[string]string)
	}
	// Migrate old NotifVolume → per-sound defaults
	if id.NotifVolume > 0 && len(id.SoundVolumes) == 0 {
		for _, key := range AllSoundKeys {
			id.SoundVolumes[key] = id.NotifVolume
		}
		id.NotifVolume = 0
		changed = true
	}
	if id.CustomNotifSnd != "" && id.CustomSounds["notification"] == "" {
		id.CustomSounds["notification"] = id.CustomNotifSnd
		id.CustomNotifSnd = ""
		changed = true
	}
	if id.CustomDMSnd != "" && id.CustomSounds["dm"] == "" {
		id.CustomSounds["dm"] = id.CustomDMSnd
		id.CustomDMSnd = ""
		changed = true
	}
	return changed
}

// AllSoundKeys lists all configurable sound types.
var AllSoundKeys = []string{
	"notification", "dm", "voiceJoin", "voiceLeave",
	"callRing", "callEnd", "friendRequest", "lfg", "calendar",
}

type MountedShareInfo struct {
	DisplayName string `json:"displayName"`
	DriveLetter string `json:"driveLetter,omitempty"` // Windows: mapped drive letter (e.g. "Z:")
	Port        int    `json:"port,omitempty"`         // WebDAV port for reuse on restart
	CanWrite    bool   `json:"canWrite,omitempty"`
}

// LobbyPrefs stores the last settings for a lobby channel
type LobbyPrefs struct {
	LastName     string `json:"last_name,omitempty"`
	LastPassword string `json:"last_password,omitempty"`
}

type StoredServer struct {
	URL           string                      `json:"url"`
	Name          string                      `json:"name"`
	RefreshToken  string                      `json:"refreshToken,omitempty"`
	SharePaths    map[string]string           `json:"sharePaths,omitempty"`    // shareID → local path
	MountedShares map[string]MountedShareInfo `json:"mountedShares,omitempty"` // shareID → mount info
	NotifyLevel   *NotifyLevel                `json:"notifyLevel,omitempty"`   // nil = inherit global
	ChannelNotify map[string]NotifyLevel      `json:"channelNotify,omitempty"` // channelID → level
	LobbyCache    map[string]LobbyPrefs       `json:"lobbyCache,omitempty"`   // lobbyID → prefs
	PinSeenIDs    map[string]string           `json:"pinSeenIds,omitempty"`   // channelID → messageID
}

func noraDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nora")
}

func identityPath() string {
	return filepath.Join(noraDir(), "identities.json")
}

func ensureDir() error {
	return os.MkdirAll(noraDir(), 0700)
}

func LoadIdentities() ([]StoredIdentity, error) {
	data, err := os.ReadFile(identityPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var ids []StoredIdentity
	if err := json.Unmarshal(data, &ids); err != nil {
		// Try single-object format (migration)
		var single StoredIdentity
		if err2 := json.Unmarshal(data, &single); err2 == nil && single.PublicKey != "" {
			return []StoredIdentity{single}, nil
		}
		return nil, err
	}
	return ids, nil
}

func SaveIdentities(ids []StoredIdentity) error {
	if err := ensureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(identityPath(), data, 0600)
}

func FindIdentity(publicKey string) (*StoredIdentity, error) {
	ids, err := LoadIdentities()
	if err != nil {
		return nil, err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			return &ids[i], nil
		}
	}
	return nil, nil
}

func SaveOrUpdateIdentity(id StoredIdentity) error {
	ids, err := LoadIdentities()
	if err != nil {
		ids = nil
	}

	found := false
	for i := range ids {
		if ids[i].PublicKey == id.PublicKey {
			ids[i] = id
			found = true
			break
		}
	}
	if !found {
		ids = append(ids, id)
	}
	return SaveIdentities(ids)
}

func UpdateServerToken(publicKey, serverURL, refreshToken string) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}

	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		found := false
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				ids[i].Servers[j].RefreshToken = refreshToken
				found = true
				break
			}
		}
		if !found {
			ids[i].Servers = append(ids[i].Servers, StoredServer{
				URL:          serverURL,
				RefreshToken: refreshToken,
			})
		}
		return SaveIdentities(ids)
	}
	return nil
}

func UpdateServerName(publicKey, serverURL, name string) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}

	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				ids[i].Servers[j].Name = name
				return SaveIdentities(ids)
			}
		}
	}
	return nil
}

func GetServerToken(publicKey, serverURL string) string {
	ids, _ := LoadIdentities()
	for _, id := range ids {
		if id.PublicKey != publicKey {
			continue
		}
		for _, s := range id.Servers {
			if s.URL == serverURL {
				return s.RefreshToken
			}
		}
	}
	return ""
}

func UpdateSharePaths(publicKey, serverURL string, sharePaths map[string]string) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}

	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				ids[i].Servers[j].SharePaths = sharePaths
				return SaveIdentities(ids)
			}
		}
	}
	return nil
}

func UpdateMountedShares(publicKey, serverURL string, mounted map[string]MountedShareInfo) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}

	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				ids[i].Servers[j].MountedShares = mounted
				return SaveIdentities(ids)
			}
		}
	}
	return nil
}

func GetMountedShares(publicKey, serverURL string) map[string]MountedShareInfo {
	ids, _ := LoadIdentities()
	for _, id := range ids {
		if id.PublicKey != publicKey {
			continue
		}
		for _, s := range id.Servers {
			if s.URL == serverURL {
				return s.MountedShares
			}
		}
	}
	return nil
}

func GetSharePaths(publicKey, serverURL string) map[string]string {
	ids, _ := LoadIdentities()
	for _, id := range ids {
		if id.PublicKey != publicKey {
			continue
		}
		for _, s := range id.Servers {
			if s.URL == serverURL {
				return s.SharePaths
			}
		}
	}
	return nil
}

func UpdateGlobalNotifyLevel(publicKey string, level NotifyLevel) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			ids[i].NotifyLevel = level
			return SaveIdentities(ids)
		}
	}
	return nil
}

func GetGlobalNotifyLevel(publicKey string) NotifyLevel {
	ids, _ := LoadIdentities()
	for _, id := range ids {
		if id.PublicKey == publicKey {
			return id.NotifyLevel
		}
	}
	return NotifyAll
}

func UpdateServerNotifyLevel(publicKey, serverURL string, level *NotifyLevel) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				ids[i].Servers[j].NotifyLevel = level
				return SaveIdentities(ids)
			}
		}
	}
	return nil
}

func UpdateChannelNotifyLevel(publicKey, serverURL, channelID string, level NotifyLevel) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				if ids[i].Servers[j].ChannelNotify == nil {
					ids[i].Servers[j].ChannelNotify = make(map[string]NotifyLevel)
				}
				ids[i].Servers[j].ChannelNotify[channelID] = level
				return SaveIdentities(ids)
			}
		}
	}
	return nil
}

func DeleteChannelNotifyLevel(publicKey, serverURL, channelID string) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				delete(ids[i].Servers[j].ChannelNotify, channelID)
				return SaveIdentities(ids)
			}
		}
	}
	return nil
}

func GetServerNotifySettings(publicKey, serverURL string) (*NotifyLevel, map[string]NotifyLevel) {
	ids, _ := LoadIdentities()
	for _, id := range ids {
		if id.PublicKey != publicKey {
			continue
		}
		for _, s := range id.Servers {
			if s.URL == serverURL {
				return s.NotifyLevel, s.ChannelNotify
			}
		}
	}
	return nil, nil
}

func UpdateSoundSettings(publicKey string, volume float64, notifSnd, dmSnd string) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			ids[i].NotifVolume = volume
			ids[i].CustomNotifSnd = notifSnd
			ids[i].CustomDMSnd = dmSnd
			return SaveIdentities(ids)
		}
	}
	return nil
}

func UpdateAllSoundSettings(publicKey string, volumes map[string]float64, customs map[string]string) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			ids[i].SoundVolumes = volumes
			ids[i].CustomSounds = customs
			return SaveIdentities(ids)
		}
	}
	return nil
}

// SoundsDir returns the path to ~/.nora/sounds/ and creates it if it doesn't exist.
func SoundsDir() string {
	dir := filepath.Join(noraDir(), "sounds")
	os.MkdirAll(dir, 0700)
	return dir
}

// NoraDir returns the path to ~/.nora/ (exported for cleanup).
func NoraDir() string {
	return noraDir()
}

func UpdateVideoVolume(publicKey string, volume int) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			ids[i].VideoVolume = volume
			return SaveIdentities(ids)
		}
	}
	return nil
}

func UpdateYouTubeQuality(publicKey string, height int) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			ids[i].YouTubeQuality = height
			return SaveIdentities(ids)
		}
	}
	return nil
}

func UpdateStorageSettings(publicKey string, maxCache int64, maxDays int) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			ids[i].MaxCacheBytes = maxCache
			ids[i].MaxHistoryDays = maxDays
			return SaveIdentities(ids)
		}
	}
	return nil
}

func GetStorageSettings(publicKey string) (maxCache int64, maxDays int) {
	ids, _ := LoadIdentities()
	for _, id := range ids {
		if id.PublicKey == publicKey {
			return id.MaxCacheBytes, id.MaxHistoryDays
		}
	}
	return 0, 0
}

func GetLobbyPrefs(publicKey, serverURL, lobbyID string) LobbyPrefs {
	ids, _ := LoadIdentities()
	for _, id := range ids {
		if id.PublicKey != publicKey {
			continue
		}
		for _, s := range id.Servers {
			if s.URL == serverURL && s.LobbyCache != nil {
				return s.LobbyCache[lobbyID]
			}
		}
	}
	return LobbyPrefs{}
}

func UpdateLobbyPrefs(publicKey, serverURL, lobbyID string, prefs LobbyPrefs) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				if ids[i].Servers[j].LobbyCache == nil {
					ids[i].Servers[j].LobbyCache = make(map[string]LobbyPrefs)
				}
				ids[i].Servers[j].LobbyCache[lobbyID] = prefs
				return SaveIdentities(ids)
			}
		}
	}
	return nil
}

func UpdateFontScale(publicKey string, scale float64) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			ids[i].FontScale = scale
			return SaveIdentities(ids)
		}
	}
	return nil
}

func UpdateCompactMode(publicKey string, compact bool) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey == publicKey {
			ids[i].CompactMode = compact
			return SaveIdentities(ids)
		}
	}
	return nil
}

func GetPinSeenID(publicKey, serverURL, channelID string) string {
	ids, _ := LoadIdentities()
	for _, id := range ids {
		if id.PublicKey != publicKey {
			continue
		}
		for _, s := range id.Servers {
			if s.URL == serverURL && s.PinSeenIDs != nil {
				return s.PinSeenIDs[channelID]
			}
		}
	}
	return ""
}

func SetPinSeenID(publicKey, serverURL, channelID, messageID string) error {
	ids, err := LoadIdentities()
	if err != nil {
		return err
	}
	for i := range ids {
		if ids[i].PublicKey != publicKey {
			continue
		}
		for j := range ids[i].Servers {
			if ids[i].Servers[j].URL == serverURL {
				if ids[i].Servers[j].PinSeenIDs == nil {
					ids[i].Servers[j].PinSeenIDs = make(map[string]string)
				}
				ids[i].Servers[j].PinSeenIDs[channelID] = messageID
				return SaveIdentities(ids)
			}
		}
	}
	return nil
}
