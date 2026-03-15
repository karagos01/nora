package store

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// NotifyLevel určuje úroveň notifikací.
type NotifyLevel int

const (
	NotifyAll      NotifyLevel = 0 // výchozí — všechny zvuky
	NotifyMentions NotifyLevel = 1 // jen @mentions
	NotifyNothing  NotifyLevel = 2 // muted
)

// UnmarshalJSON zpracuje oba formáty: starý string ("displayName") i nový objekt ({displayName, driveLetter}).
func (m *MountedShareInfo) UnmarshalJSON(data []byte) error {
	// Zkusit starý formát: prostý string
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.DisplayName = s
		return nil
	}
	// Nový formát: objekt
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
	PasswordVerify  string         `json:"passwordVerify,omitempty"` // šifrovaný známý string pro detekci špatného hesla
	Servers         []StoredServer `json:"servers,omitempty"`
	NotifyLevel    NotifyLevel    `json:"notifyLevel,omitempty"`
	NotifVolume    float64        `json:"notifVolume,omitempty"`    // 0.0-1.0, 0 → default 1.0
	CustomNotifSnd string         `json:"customNotifSnd,omitempty"` // cesta ke custom notification zvuku
	CustomDMSnd    string         `json:"customDmSnd,omitempty"`    // cesta ke custom DM zvuku
	VideoVolume    int            `json:"videoVolume,omitempty"`    // 0 → default 100 (rozsah 0-200)
	FontScale      float64        `json:"fontScale,omitempty"`      // 0 → default 1.0 (rozsah 0.7-1.6)
	MaxCacheBytes  int64          `json:"maxCacheBytes,omitempty"`  // 0 → default 2 GB
	MaxHistoryDays int            `json:"maxHistoryDays,omitempty"` // 0 → neomezeno
	CompactMode    bool           `json:"compactMode,omitempty"`    // IRC-style compact zprávy
}

type MountedShareInfo struct {
	DisplayName string `json:"displayName"`
	DriveLetter string `json:"driveLetter,omitempty"` // Windows: mapované písmeno (např. "Z:")
	Port        int    `json:"port,omitempty"`         // WebDAV port pro reuse při restartu
	CanWrite    bool   `json:"canWrite,omitempty"`
}

// LobbyPrefs uchovává poslední nastavení pro lobby kanál
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
		// Pokus o single-object formát (migrace)
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

// SoundsDir vrátí cestu k ~/.nora/sounds/ a vytvoří ji pokud neexistuje.
func SoundsDir() string {
	dir := filepath.Join(noraDir(), "sounds")
	os.MkdirAll(dir, 0700)
	return dir
}

// NoraDir vrátí cestu k ~/.nora/ (exportovaná pro cleanup).
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
