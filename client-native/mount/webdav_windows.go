//go:build windows

package mount

import (
	"log"
	"os/exec"
)

// FixWindowsWebDAVRegistry adjusts Windows WebClient registry settings
// to allow larger files and longer timeouts for WebDAV connections.
// Requires admin privileges; silently fails if not admin.
func FixWindowsWebDAVRegistry() {
	fixes := []struct {
		name  string
		value string
	}{
		// 4GB file size limit (default is 50MB)
		{"FileSizeLimitInBytes", "4294967295"},
		// 10 minute send timeout (default 30s)
		{"SendTimeout", "600000"},
		// 10 minute receive timeout (default 30s)
		{"ReceiveTimeout", "600000"},
	}

	for _, fix := range fixes {
		cmd := exec.Command("reg", "add",
			`HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters`,
			"/v", fix.name, "/t", "REG_DWORD", "/d", fix.value, "/f")
		if err := cmd.Run(); err != nil {
			log.Printf("WebDAV registry fix %s: %v (may need admin)", fix.name, err)
		}
	}

	// Restart WebClient service to apply changes
	exec.Command("net", "stop", "WebClient").Run()
	exec.Command("net", "start", "WebClient").Run()
}
