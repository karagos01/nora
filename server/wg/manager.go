package wg

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"nora/config"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/crypto/curve25519"
)

type Manager struct {
	iface    string
	port     int
	endpoint string
	subnet   string
	privKey  string
	pubKey   string
}

func NewManager(cfg config.LANConfig) (*Manager, error) {
	m := &Manager{
		iface:    cfg.Interface,
		port:     cfg.Port,
		endpoint: cfg.Endpoint,
		subnet:   cfg.Subnet,
	}
	return m, nil
}

func (m *Manager) Close() {
	m.DestroyInterface()
}

// GenerateKeypair generates a WireGuard Curve25519 keypair
func GenerateKeypair() (privateKey, publicKey string, err error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", fmt.Errorf("random: %w", err)
	}
	// Clamp per Curve25519 spec
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("curve25519: %w", err)
	}

	privateKey = base64.StdEncoding.EncodeToString(priv[:])
	publicKey = base64.StdEncoding.EncodeToString(pub)
	return privateKey, publicKey, nil
}

// CreateInterface creates a WireGuard interface and sets IP + port
func (m *Manager) CreateInterface(privateKey string) error {
	// Delete existing interface if it exists
	exec.Command("ip", "link", "del", m.iface).Run()

	// Create WG interface
	if err := run("ip", "link", "add", m.iface, "type", "wireguard"); err != nil {
		return fmt.Errorf("create interface: %w", err)
	}

	// Write private key to temp file
	tmpFile, err := os.CreateTemp("", "wg-key-*")
	if err != nil {
		return fmt.Errorf("create temp key file: %w", err)
	}
	tmpFile.WriteString(privateKey)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Set WG config
	if err := run("wg", "set", m.iface, "listen-port", fmt.Sprintf("%d", m.port), "private-key", tmpFile.Name()); err != nil {
		m.DestroyInterface()
		return fmt.Errorf("configure wireguard: %w", err)
	}

	// Set IP address (server is always .1)
	serverIP := subnetToServerIP(m.subnet)
	if err := run("ip", "addr", "add", serverIP, "dev", m.iface); err != nil {
		m.DestroyInterface()
		return fmt.Errorf("add address: %w", err)
	}

	// Bring up interface
	if err := run("ip", "link", "set", m.iface, "up"); err != nil {
		m.DestroyInterface()
		return fmt.Errorf("bring up interface: %w", err)
	}

	// Enable IP forwarding for LAN mesh
	os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)

	slog.Info("WireGuard interface created", "interface", m.iface, "port", m.port)
	return nil
}

// DestroyInterface deletes the WireGuard interface
func (m *Manager) DestroyInterface() {
	if err := run("ip", "link", "del", m.iface); err != nil {
		slog.Error("failed to delete WG interface", "interface", m.iface, "error", err)
	} else {
		slog.Info("WireGuard interface deleted", "interface", m.iface)
	}
}

// AddPeer adds a WG peer
func (m *Manager) AddPeer(pubKey string, assignedIP string) error {
	allowedIPs := assignedIP + "/32"
	if err := run("wg", "set", m.iface, "peer", pubKey, "allowed-ips", allowedIPs); err != nil {
		return fmt.Errorf("add peer: %w", err)
	}
	slog.Info("WG peer added", "pub_key", pubKey[:8]+"...", "assigned_ip", assignedIP)
	return nil
}

// RemovePeer removes a WG peer
func (m *Manager) RemovePeer(pubKey string) error {
	if err := run("wg", "set", m.iface, "peer", pubKey, "remove"); err != nil {
		return fmt.Errorf("remove peer: %w", err)
	}
	slog.Info("WG peer removed", "pub_key", pubKey[:8]+"...")
	return nil
}

// PeerInfo holds the information needed to add a peer
type PeerInfo struct {
	PublicKey  string
	AssignedIP string
}

// RecoverInterface restores the WireGuard interface after a server restart
func (m *Manager) RecoverInterface(privateKey string, peers []PeerInfo) error {
	if err := m.CreateInterface(privateKey); err != nil {
		return fmt.Errorf("recover create interface: %w", err)
	}

	for _, p := range peers {
		if err := m.AddPeer(p.PublicKey, p.AssignedIP); err != nil {
			slog.Warn("peer recovery failed", "pub_key", p.PublicKey[:8]+"...", "assigned_ip", p.AssignedIP, "error", err)
			continue
		}
	}

	slog.Info("WireGuard interface recovered", "peers", len(peers))
	return nil
}

// Endpoint returns the public endpoint for clients
func (m *Manager) Endpoint() string {
	return m.endpoint
}

// Subnet returns the subnet
func (m *Manager) Subnet() string {
	return m.subnet
}

// SetKeys sets the server-level WG keypair
func (m *Manager) SetKeys(privKey, pubKey string) {
	m.privKey = privKey
	m.pubKey = pubKey
}

// PublicKey returns the server's public key
func (m *Manager) PublicKey() string {
	return m.pubKey
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %s (%w)", name, strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return nil
}

// subnetToServerIP converts "10.42.0.0/24" to "10.42.0.1/24"
func subnetToServerIP(subnet string) string {
	parts := strings.SplitN(subnet, "/", 2)
	if len(parts) != 2 {
		return "10.42.0.1/24"
	}
	ip := parts[0]
	mask := parts[1]
	// Replace the last octet (zero) with one
	octets := strings.Split(ip, ".")
	if len(octets) == 4 {
		octets[3] = "1"
	}
	return strings.Join(octets, ".") + "/" + mask
}
