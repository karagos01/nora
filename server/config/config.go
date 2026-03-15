package config

import (
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Server       ServerConfig       `toml:"server"`
	Database     DatabaseConfig     `toml:"database"`
	Auth         AuthConfig         `toml:"auth"`
	Uploads      UploadsConfig      `toml:"uploads"`
	RateLimit    RateLimitConfig    `toml:"ratelimit"`
	Registration RegistrationConfig `toml:"registration"`
	LAN          LANConfig          `toml:"lan"`
	GameServers  GameServersConfig  `toml:"gameservers"`
	Security     SecurityConfig     `toml:"security"`
}

type GameServersConfig struct {
	Enabled bool   `toml:"enabled"`
	DataDir string `toml:"data_dir"`
}

type LANConfig struct {
	Enabled   bool   `toml:"enabled"`
	Interface string `toml:"interface"`
	Subnet    string `toml:"subnet"`
	Port      int    `toml:"port"`
	Endpoint  string `toml:"endpoint"`
}

type ServerConfig struct {
	Host        string `toml:"host"`
	Port        int    `toml:"port"`
	Name        string `toml:"name"`
	Description string `toml:"description"`
	SourceURL   string `toml:"source_url"`
	StunURL     string `toml:"stun_url"`
}

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type AuthConfig struct {
	JWTSecret       string   `toml:"jwt_secret"`
	AccessTokenTTL  Duration `toml:"access_token_ttl"`
	RefreshTokenTTL Duration `toml:"refresh_token_ttl"`
	ChallengeTTL    Duration `toml:"challenge_ttl"`
}

type UploadsConfig struct {
	Dir                 string   `toml:"dir"`
	MaxSizeMB           int      `toml:"max_size_mb"`
	AllowedTypes        []string `toml:"allowed_types"`
	StorageMaxMB        int      `toml:"storage_max_mb"`
	ChannelHistoryLimit int      `toml:"channel_history_limit"`
}

type RateLimitConfig struct {
	RequestsPerSecond float64 `toml:"requests_per_second"`
	Burst             int     `toml:"burst"`
}

type RegistrationConfig struct {
	Open bool   `toml:"open"`
	Mode string `toml:"mode"` // "open", "approval", "closed" — má prioritu nad Open
}

type SecurityConfig struct {
	Device     DeviceConfig     `toml:"device"`
	Quarantine QuarantineConfig `toml:"quarantine"`
	Invites    InviteSecConfig  `toml:"invites"`
}

type DeviceConfig struct {
	TrackDevices       bool `toml:"track_devices"`
	BanDeviceOnUserBan bool `toml:"ban_device_on_user_ban"`
	AlertOnSuspicious  bool `toml:"alert_on_suspicious"`
}

type QuarantineConfig struct {
	Enabled              bool `toml:"enabled"`
	DurationDays         int  `toml:"duration_days"`
	RestrictSendMessages bool `toml:"restrict_send_messages"`
	RestrictUpload       bool `toml:"restrict_upload"`
	RestrictInvite       bool `toml:"restrict_invite"`
	RestrictDM           bool `toml:"restrict_dm"`
}

type InviteSecConfig struct {
	MaxInvitesPerUser int  `toml:"max_invites_per_user"`
	TrackInviteChain  bool `toml:"track_invite_chain"`
	RevokeOnBan       bool `toml:"revoke_on_ban"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	var err error
	d.Duration, err = time.ParseDuration(string(text))
	return err
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Host:      "0.0.0.0",
			Port:      8080,
			Name:      "NORA Server",
			SourceURL: "https://github.com/anthropics/nora",
		},
		Database: DatabaseConfig{
			Path: "data/nora.db",
		},
		Auth: AuthConfig{
			AccessTokenTTL:  Duration{15 * time.Minute},
			RefreshTokenTTL: Duration{7 * 24 * time.Hour},
			ChallengeTTL:    Duration{5 * time.Minute},
		},
		Uploads: UploadsConfig{
			Dir:       "data/uploads",
			MaxSizeMB: 10,
			AllowedTypes: []string{
				"image/jpeg", "image/png", "image/gif", "image/webp",
				"video/mp4", "video/webm",
				"audio/mpeg", "audio/ogg", "audio/wav", "audio/flac", "audio/webm", "audio/mp4",
				"application/pdf",
				"application/zip", "application/x-zip-compressed",
				"application/x-rar-compressed", "application/x-7z-compressed",
				"application/gzip", "application/x-tar",
				"application/octet-stream",
				"text/plain", "text/csv", "text/html", "text/css", "text/javascript",
				"application/json", "application/xml",
			},
		},
		RateLimit: RateLimitConfig{
			RequestsPerSecond: 10,
			Burst:             30,
		},
		LAN: LANConfig{
			Enabled:   false,
			Interface: "wg-nora",
			Subnet:    "10.42.0.0/24",
			Port:      51820,
		},
		GameServers: GameServersConfig{
			Enabled: true,
			DataDir: "/opt/nora/gameservers",
		},
		Security: SecurityConfig{
			Device: DeviceConfig{
				TrackDevices:       true,
				BanDeviceOnUserBan: true,
				AlertOnSuspicious:  true,
			},
			Quarantine: QuarantineConfig{
				DurationDays:         7,
				RestrictSendMessages: true,
				RestrictUpload:       true,
				RestrictInvite:       true,
				RestrictDM:           true,
			},
			Invites: InviteSecConfig{
				TrackInviteChain: true,
				RevokeOnBan:      true,
			},
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// ResolveRegMode vrací registration mode string.
// Pokud Mode je nastavený, použije se. Jinak se odvozuje z Open bool.
func ResolveRegMode(reg RegistrationConfig) string {
	if reg.Mode != "" {
		return reg.Mode
	}
	if reg.Open {
		return "open"
	}
	return "closed"
}
