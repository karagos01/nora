package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

//go:embed wintun.dll
var wintunDLL []byte

const ifaceName = "nora-lan"

type TunnelRequest struct {
	PrivateKey string `json:"private_key"` // base64
	PeerPub    string `json:"peer_pub"`    // base64
	Endpoint   string `json:"endpoint"`    // host:port
	IP         string `json:"ip"`          // "10.42.0.2/24"
	AllowedIPs string `json:"allowed_ips"` // "10.42.0.0/24"
}

var (
	mu        sync.Mutex
	activeDev *device.Device
	activeTun tun.Device
	activeIF  string
)

func b64ToHex(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func handleUp(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	log.Printf("POST /up received")

	teardown()

	var req TunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("ERROR: decode: %v", err)
		http.Error(w, err.Error(), 400)
		return
	}
	log.Printf("Request: endpoint=%s ip=%s", req.Endpoint, req.IP)

	// Vytvoř TUN interface
	tunDev, err := tun.CreateTUN(ifaceName, device.DefaultMTU)
	if err != nil {
		log.Printf("ERROR: create TUN: %v", err)
		http.Error(w, fmt.Sprintf("create TUN: %v", err), 500)
		return
	}

	realName, err := tunDev.Name()
	if err != nil {
		realName = ifaceName
	}

	// WireGuard device
	logger := device.NewLogger(device.LogLevelError, "wg: ")
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)

	privHex, err := b64ToHex(req.PrivateKey)
	if err != nil {
		dev.Close()
		http.Error(w, fmt.Sprintf("invalid private_key: %v", err), 400)
		return
	}

	peerHex, err := b64ToHex(req.PeerPub)
	if err != nil {
		dev.Close()
		http.Error(w, fmt.Sprintf("invalid peer_pub: %v", err), 400)
		return
	}

	uapi := fmt.Sprintf("private_key=%s\nlisten_port=0\nreplace_peers=true\npublic_key=%s\nendpoint=%s\nallowed_ip=%s\npersistent_keepalive_interval=25\n",
		privHex, peerHex, req.Endpoint, req.AllowedIPs)

	if err := dev.IpcSet(uapi); err != nil {
		log.Printf("ERROR: IpcSet: %v", err)
		dev.Close()
		http.Error(w, fmt.Sprintf("configure WG: %v", err), 500)
		return
	}

	if err := dev.Up(); err != nil {
		log.Printf("ERROR: device up: %v", err)
		dev.Close()
		http.Error(w, fmt.Sprintf("device up: %v", err), 500)
		return
	}

	// Nastav IP adresu na interface
	if err := setIP(realName, req.IP); err != nil {
		log.Printf("ERROR: setIP: %v", err)
		dev.Close()
		http.Error(w, fmt.Sprintf("set IP: %v", err), 500)
		return
	}

	activeDev = dev
	activeTun = tunDev
	activeIF = realName

	log.Printf("Tunnel UP: %s (%s)", realName, req.IP)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "interface": realName})
}

func handleDown(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	teardown()
	log.Printf("Tunnel DOWN")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"active":    activeDev != nil,
		"interface": activeIF,
	})
}

func teardown() {
	if activeDev != nil {
		activeDev.Close()
		activeDev = nil
	}
	if activeTun != nil {
		activeTun.Close()
		activeTun = nil
	}
	activeIF = ""
}

func setIP(ifname, cidr string) error {
	switch runtime.GOOS {
	case "linux":
		if err := exec.Command("ip", "addr", "add", cidr, "dev", ifname).Run(); err != nil {
			return fmt.Errorf("ip addr add: %w", err)
		}
		return exec.Command("ip", "link", "set", ifname, "up").Run()

	case "windows":
		parts := strings.SplitN(cidr, "/", 2)
		addr := parts[0]
		return exec.Command("netsh", "interface", "ip", "set", "address", ifname, "static", addr, "255.255.255.0").Run()

	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// authToken je bearer token generovaný při startu, uložený v ~/.nora/wg-helper-token
var authToken string

func generateAuthToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeTokenFile(token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".nora")
	os.MkdirAll(dir, 0700)
	return os.WriteFile(filepath.Join(dir, "wg-helper-token"), []byte(token), 0600)
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		ah := r.Header.Get("Authorization")
		if !strings.HasPrefix(ah, "Bearer ") || ah[7:] != authToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractWintun() {
	if runtime.GOOS != "windows" {
		return
	}

	// Extrahuj wintun.dll vedle exe (LoadLibrary hledá primárně tam)
	exePath, err := os.Executable()
	if err != nil {
		exePath = "."
	}
	exeDir := filepath.Dir(exePath)
	dllPath := filepath.Join(exeDir, "wintun.dll")

	if _, err := os.Stat(dllPath); err == nil {
		log.Printf("wintun.dll already exists at %s", dllPath)
		return
	}
	if err := os.WriteFile(dllPath, wintunDLL, 0644); err != nil {
		// Fallback na temp pokud vedle exe nejde zapsat (read-only adresář)
		dllPath = filepath.Join(os.TempDir(), "wintun.dll")
		if err2 := os.WriteFile(dllPath, wintunDLL, 0644); err2 != nil {
			log.Printf("WARNING: failed to extract wintun.dll: %v", err2)
			return
		}
		os.Setenv("PATH", os.TempDir()+";"+os.Getenv("PATH"))
		log.Printf("Extracted wintun.dll to %s (fallback)", dllPath)
	} else {
		log.Printf("Extracted wintun.dll to %s", dllPath)
	}
}

func main() {
	extractWintun()
	addr := "127.0.0.1:9023"

	// Generovat auth token a uložit do ~/.nora/wg-helper-token
	token, err := generateAuthToken()
	if err != nil {
		log.Fatalf("Failed to generate auth token: %v", err)
	}
	authToken = token
	if err := writeTokenFile(token); err != nil {
		log.Printf("WARNING: failed to write token file: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /up", handleUp)
	mux.HandleFunc("POST /down", handleDown)
	mux.HandleFunc("GET /status", handleStatus)

	log.Printf("nora-lan helper v1.1 listening on %s", addr)
	if err := http.ListenAndServe(addr, authMiddleware(mux)); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}
}
