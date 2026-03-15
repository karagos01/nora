//go:build windows

package mount

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var webClientOnce sync.Once

// ensureWebClient ensures the WebClient service is running and allows HTTP WebDAV.
// Called only once per session (sync.Once) — restarting WebClient is slow and breaks existing mappings.
func ensureWebClient() {
	webClientOnce.Do(func() {
		// Set BasicAuthLevel=2 (allows WebDAV over HTTP, not just HTTPS)
		// Without this Windows rejects HTTP WebDAV with "The parameter is incorrect" error
		exec.Command("reg", "add",
			`HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters`,
			"/v", "BasicAuthLevel", "/t", "REG_DWORD", "/d", "2", "/f",
		).Run()

		// Increase max file size (default 50MB → 4GB)
		exec.Command("reg", "add",
			`HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters`,
			"/v", "FileSizeLimitInBytes", "/t", "REG_DWORD", "/d", "4294967295", "/f",
		).Run()

		// Restart WebClient for changes to take effect
		exec.Command("net", "stop", "WebClient").Run()
		exec.Command("net", "start", "WebClient").Run()
		time.Sleep(500 * time.Millisecond)
		log.Printf("drive_windows: WebClient service configured and started")
	})
}

// mapDrivePreferred tries the preferred letter, then falls back to a free one.
func mapDrivePreferred(webdavURL string, preferred string) (string, error) {
	ensureWebClient()

	if preferred != "" {
		// Try the preferred letter
		cmd := exec.Command("net", "use", preferred, webdavURL, "/persistent:no")
		out, err := cmd.CombinedOutput()
		if err == nil {
			log.Printf("drive_windows: mapped %s → %s (preferred)", preferred, webdavURL)
			return preferred, nil
		}
		// UNC fallback
		altURL := webdavToUNC(webdavURL)
		if altURL != "" {
			cmd2 := exec.Command("net", "use", preferred, altURL, "/persistent:no")
			_, err2 := cmd2.CombinedOutput()
			if err2 == nil {
				log.Printf("drive_windows: mapped %s → %s (preferred, UNC)", preferred, altURL)
				return preferred, nil
			}
		}
		log.Printf("drive_windows: preferred %s failed: %s, falling back", preferred, strings.TrimSpace(string(out)))
	}

	// Fallback to standard mapDrive
	return mapDrive(webdavURL)
}

// mapDrive finds a free drive letter and maps the WebDAV URL as a network drive.
func mapDrive(webdavURL string) (string, error) {
	ensureWebClient()

	letter, err := findFreeDriveLetter()
	if err != nil {
		return "", err
	}

	// net use Z: http://localhost:PORT/ /persistent:no
	cmd := exec.Command("net", "use", letter, webdavURL, "/persistent:no")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// If net use fails, try alternative URL format
		// Windows sometimes wants \\host@port\DavWWWRoot format
		altURL := webdavToUNC(webdavURL)
		if altURL != "" {
			cmd2 := exec.Command("net", "use", letter, altURL, "/persistent:no")
			out2, err2 := cmd2.CombinedOutput()
			if err2 == nil {
				log.Printf("drive_windows: mapped %s → %s (UNC)", letter, altURL)
				return letter, nil
			}
			log.Printf("drive_windows: UNC fallback also failed: %s", strings.TrimSpace(string(out2)))
		}

		return "", fmt.Errorf("net use %s %s: %s (%w)", letter, webdavURL, strings.TrimSpace(string(out)), err)
	}

	log.Printf("drive_windows: mapped %s → %s", letter, webdavURL)
	return letter, nil
}

// renameDrive sets the display name of a network drive in Explorer.
func renameDrive(letter, label string) {
	// Shell.Application COM object — sets the label in Explorer
	ps := fmt.Sprintf(`(New-Object -ComObject Shell.Application).NameSpace('%s\').Self.Name = '%s'`,
		letter, strings.ReplaceAll(label, "'", "''"))
	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("drive_windows: rename %s to %q: %s (%v)", letter, label, strings.TrimSpace(string(out)), err)
	} else {
		log.Printf("drive_windows: renamed %s → %q", letter, label)
	}
}

// unmapDrive disconnects a network drive.
func unmapDrive(letter string) {
	cmd := exec.Command("net", "use", letter, "/delete", "/yes")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("drive_windows: unmap %s: %s (%v)", letter, strings.TrimSpace(string(out)), err)
	} else {
		log.Printf("drive_windows: unmapped %s", letter)
	}
}

// findFreeDriveLetter finds the first free drive letter (Z → D).
func findFreeDriveLetter() (string, error) {
	cmd := exec.Command("powershell", "-Command", "(Get-PSDrive -PSProvider FileSystem).Name -join ','")
	out, err := cmd.Output()
	if err != nil {
		return "Z:", nil
	}

	used := make(map[string]bool)
	for _, name := range strings.Split(strings.TrimSpace(string(out)), ",") {
		used[strings.TrimSpace(name)] = true
	}

	for c := byte('Z'); c >= 'D'; c-- {
		letter := string(c)
		if !used[letter] {
			return letter + ":", nil
		}
	}
	return "", fmt.Errorf("no free drive letter found")
}

// webdavToUNC converts http://localhost:PORT/ to \\localhost@PORT\DavWWWRoot
func webdavToUNC(url string) string {
	// http://127.0.0.1:65498/ → \\127.0.0.1@65498\DavWWWRoot
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimSuffix(url, "/")
	parts := strings.SplitN(url, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	host := parts[0]
	port := parts[1]
	return fmt.Sprintf(`\\%s@%s\DavWWWRoot`, host, port)
}
