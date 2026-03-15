package ui

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"
)

const lanHelperURL = "http://127.0.0.1:9023"

// LANHelper poskytuje WG logiku pro LAN kanály (bez UI).
type LANHelper struct {
	app         *App
	helperOK    bool
	helperToken string
	lastCheck   time.Time
	checkClient *http.Client
}

func NewLANHelper(a *App) *LANHelper {
	return &LANHelper{
		app:         a,
		checkClient: &http.Client{Timeout: 2 * time.Second},
	}
}

func (h *LANHelper) IsOK() bool {
	return h.helperOK
}

// CheckHelper periodicky kontroluje dostupnost WG helperu (rate-limited 10s).
func (h *LANHelper) CheckHelper() {
	if time.Since(h.lastCheck) < 10*time.Second {
		return
	}
	h.lastCheck = time.Now()
	h.readHelperToken()
	go func() {
		resp, err := h.helperGet(lanHelperURL + "/status")
		if err != nil {
			h.helperOK = false
			h.app.Window.Invalidate()
			return
		}
		resp.Body.Close()
		h.helperOK = resp.StatusCode == 200
		h.app.Window.Invalidate()
	}()
}

// IsMember zkontroluje zda je aktuální uživatel členem LAN party daného kanálu.
func (h *LANHelper) IsMember(conn *ServerConnection, channelID string) bool {
	if conn == nil {
		return false
	}
	members := conn.LANMembers[channelID]
	for _, m := range members {
		if m.UserID == conn.UserID {
			return true
		}
	}
	return false
}

// ToggleLAN přepne membership — join pokud nejsem člen, leave pokud jsem.
func (h *LANHelper) ToggleLAN(conn *ServerConnection, channelID string) {
	if conn == nil {
		return
	}
	h.app.mu.RLock()
	isMember := h.IsMember(conn, channelID)
	h.app.mu.RUnlock()

	if isMember {
		h.LeaveLAN(conn, channelID)
	} else {
		h.JoinLAN(conn, channelID)
	}
}

// JoinLAN provede plný join flow: check helper → WG keypair → API join → helper /up.
func (h *LANHelper) JoinLAN(conn *ServerConnection, channelID string) {
	resp, err := h.helperGet(lanHelperURL + "/status")
	if err != nil {
		log.Printf("LAN helper not available: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Printf("LAN helper returned %d", resp.StatusCode)
		return
	}

	wgPriv, wgPub, err := generateWGKeypair()
	if err != nil {
		log.Printf("WG keypair: %v", err)
		return
	}

	joinResp, err := conn.Client.JoinLANParty(channelID, wgPub)
	if err != nil {
		log.Printf("JoinLANParty: %v", err)
		return
	}

	if joinResp.WGConfig != nil {
		cfg := joinResp.WGConfig
		helperReq := map[string]string{
			"private_key": wgPriv,
			"peer_pub":    cfg.ServerPublicKey,
			"endpoint":    cfg.ServerEndpoint,
			"ip":          cfg.AssignedIP,
			"allowed_ips": cfg.AllowedIPs,
		}
		body, _ := json.Marshal(helperReq)
		upResp, err := h.helperPost(lanHelperURL+"/up", "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("Helper /up: %v", err)
			return
		}
		upResp.Body.Close()
	}
	h.app.Window.Invalidate()
}

// LeaveLAN provede leave flow: API leave → helper /down.
func (h *LANHelper) LeaveLAN(conn *ServerConnection, channelID string) {
	if err := conn.Client.LeaveLANParty(channelID); err != nil {
		log.Printf("LeaveLANParty: %v", err)
		return
	}
	// Zavolat helper /down pro odpojení WG tunelu
	downResp, err := h.helperPost(lanHelperURL+"/down", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		log.Printf("Helper /down: %v", err)
		return
	}
	downResp.Body.Close()
	h.app.Window.Invalidate()
}

func (h *LANHelper) readHelperToken() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(home, ".nora", "wg-helper-token"))
	if err != nil {
		return
	}
	h.helperToken = strings.TrimSpace(string(data))
}

func (h *LANHelper) helperGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if h.helperToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.helperToken)
	}
	return h.checkClient.Do(req)
}

func (h *LANHelper) helperPost(url, contentType string, body *bytes.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if h.helperToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.helperToken)
	}
	return h.checkClient.Do(req)
}

// generateWGKeypair generates a WireGuard X25519 keypair.
func generateWGKeypair() (privateKeyB64, publicKeyB64 string, err error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", err
	}
	// Clamp private key (WireGuard convention)
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}

	return base64.StdEncoding.EncodeToString(priv[:]), base64.StdEncoding.EncodeToString(pub), nil
}

