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

// ensureWebClient zajistí, že WebClient service běží a povoluje HTTP WebDAV.
// Volá se jen jednou per session (sync.Once) — restart WebClient je pomalý a rozbíjí existující mapování.
func ensureWebClient() {
	webClientOnce.Do(func() {
		// Nastavit BasicAuthLevel=2 (povolí WebDAV přes HTTP, nejen HTTPS)
		// Bez toho Windows odmítá HTTP WebDAV s chybou "Parametr není správný"
		exec.Command("reg", "add",
			`HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters`,
			"/v", "BasicAuthLevel", "/t", "REG_DWORD", "/d", "2", "/f",
		).Run()

		// Zvýšit max velikost souboru (výchozí 50MB → 4GB)
		exec.Command("reg", "add",
			`HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters`,
			"/v", "FileSizeLimitInBytes", "/t", "REG_DWORD", "/d", "4294967295", "/f",
		).Run()

		// Restartovat WebClient aby se změny projevily
		exec.Command("net", "stop", "WebClient").Run()
		exec.Command("net", "start", "WebClient").Run()
		time.Sleep(500 * time.Millisecond)
		log.Printf("drive_windows: WebClient service configured and started")
	})
}

// mapDrivePreferred zkusí preferované písmeno, pak fallback na volné.
func mapDrivePreferred(webdavURL string, preferred string) (string, error) {
	ensureWebClient()

	if preferred != "" {
		// Zkusit preferované písmeno
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

	// Fallback na standardní mapDrive
	return mapDrive(webdavURL)
}

// mapDrive najde volné písmeno a namapuje WebDAV URL jako síťový disk.
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
		// Pokud net use selže, zkusit alternativní formát URL
		// Windows někdy chce \\host@port\DavWWWRoot formát
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

// renameDrive nastaví zobrazovaný název síťového disku v Průzkumníku.
func renameDrive(letter, label string) {
	// Shell.Application COM objekt — nastaví popisek v Exploreru
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

// unmapDrive odpojí síťový disk.
func unmapDrive(letter string) {
	cmd := exec.Command("net", "use", letter, "/delete", "/yes")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("drive_windows: unmap %s: %s (%v)", letter, strings.TrimSpace(string(out)), err)
	} else {
		log.Printf("drive_windows: unmapped %s", letter)
	}
}

// findFreeDriveLetter najde první volné písmeno disku (Z → D).
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

// webdavToUNC konvertuje http://localhost:PORT/ na \\localhost@PORT\DavWWWRoot
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
