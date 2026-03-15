package device

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// GetDeviceID vraci stabilni ID zarizeni, ulozeny na skrytem miste.
// Pokud neexistuje, vygeneruje novy UUID.
func GetDeviceID() string {
	p := deviceIDPath()
	if p == "" {
		return ""
	}

	data, err := os.ReadFile(p)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	}

	// Vytvorit adresar pokud neexistuje
	dir := p[:strings.LastIndex(p, string(os.PathSeparator))]
	os.MkdirAll(dir, 0700)

	id := uuid.New().String()
	os.WriteFile(p, []byte(id), 0600)
	return id
}

// GetHardwareHash vraci SHA-256 hash kombinace HW identifikatoru.
func GetHardwareHash() string {
	var parts []string

	// MAC adresy (ne-loopback)
	ifaces, err := net.Interfaces()
	if err == nil {
		var macs []string
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			mac := iface.HardwareAddr.String()
			if mac != "" {
				macs = append(macs, mac)
			}
		}
		sort.Strings(macs)
		if len(macs) > 0 {
			parts = append(parts, macs[0]) // jen prvni stabilni MAC
		}
	}

	// Hostname
	if hostname, err := os.Hostname(); err == nil {
		parts = append(parts, hostname)
	}

	// Platform-specific identifikatory
	parts = append(parts, platformHWParts()...)

	if len(parts) == 0 {
		return ""
	}

	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("%x", h)
}
