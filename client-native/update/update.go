package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

const (
	// UpdateURL is the URL of the version manifest on the VPS.
	UpdateURL = "https://noraproject.eu/version.json"

	// CheckInterval is how often to check for updates after startup.
	CheckInterval = 1 * time.Hour
)

// VersionInfo is the JSON structure served by the VPS.
type VersionInfo struct {
	Build         int    `json:"build"`
	URLWindows    string `json:"url_windows"`
	URLLinux      string `json:"url_linux"`
	SHA256Windows string `json:"sha256_windows,omitempty"`
	SHA256Linux   string `json:"sha256_linux,omitempty"`
}

// Result of an update check.
type Result struct {
	Available   bool
	NewVersion  string
	CurrentVer  string
	DownloadURL string
	SHA256      string
}

// Check checks for a newer version. Returns nil result if up to date.
func Check(currentVersion string) (*Result, error) {
	if currentVersion == "" || currentVersion == "dev" {
		return nil, nil
	}

	currentBuild, err := strconv.Atoi(currentVersion)
	if err != nil {
		return nil, nil // non-numeric version, skip check
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(UpdateURL)
	if err != nil {
		return nil, fmt.Errorf("update check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("update check HTTP %d", resp.StatusCode)
	}

	var info VersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("update parse failed: %w", err)
	}

	if info.Build <= currentBuild {
		return nil, nil
	}

	dlURL := info.URLLinux
	expectedHash := info.SHA256Linux
	if runtime.GOOS == "windows" {
		dlURL = info.URLWindows
		expectedHash = info.SHA256Windows
	}
	if dlURL == "" {
		return nil, nil
	}

	return &Result{
		Available:   true,
		NewVersion:  strconv.Itoa(info.Build),
		CurrentVer:  currentVersion,
		DownloadURL: dlURL,
		SHA256:      expectedHash,
	}, nil
}

// Download downloads the new binary to a temp file and returns its path.
// If expectedSHA256 is non-empty, the file is verified after download.
func Download(url, expectedSHA256 string, progress func(downloaded, total int64)) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)

	tmpFile, err := os.CreateTemp(dir, "nora-update-*")
	if err != nil {
		return "", fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	total := resp.ContentLength
	var downloaded int64
	hasher := sha256.New()

	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := tmpFile.Write(buf[:n]); err != nil {
				tmpFile.Close()
				os.Remove(tmpPath)
				return "", err
			}
			hasher.Write(buf[:n])
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return "", readErr
		}
	}

	tmpFile.Close()

	// SHA-256 checksum verification (mandatory — refuse update without valid hash)
	if expectedSHA256 == "" {
		os.Remove(tmpPath)
		return "", fmt.Errorf("refusing update: no SHA-256 checksum provided")
	}
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedSHA256 {
		os.Remove(tmpPath)
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, actualHash)
	}

	return tmpPath, nil
}

// Apply replaces the current binary with the downloaded one.
// On Windows: rename old → .old, rename new → current, then restart.
// On Linux: rename new → current directly.
func Apply(tmpPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		oldPath := exe + ".old"
		os.Remove(oldPath) // remove previous .old if exists
		if err := os.Rename(exe, oldPath); err != nil {
			return fmt.Errorf("rename old: %w", err)
		}
		if err := os.Rename(tmpPath, exe); err != nil {
			// rollback
			os.Rename(oldPath, exe)
			return fmt.Errorf("rename new: %w", err)
		}
	} else {
		// Linux: overwrite directly (atomic via rename within same dir)
		if err := os.Chmod(tmpPath, 0755); err != nil {
			return err
		}
		if err := os.Rename(tmpPath, exe); err != nil {
			return fmt.Errorf("rename: %w", err)
		}
	}

	return nil
}

// Restart re-launches the current executable and exits.
func Restart() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("restart: cannot find executable: %v", err)
		return
	}

	// Start new process
	attr := &os.ProcAttr{
		Dir:   filepath.Dir(exe),
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
	proc, err := os.StartProcess(exe, os.Args, attr)
	if err != nil {
		log.Printf("restart: cannot start process: %v", err)
		return
	}
	proc.Release()

	// Exit current process
	os.Exit(0)
}

// CheckAndPrompt runs a background check and calls onResult with the result.
// Safe to call from a goroutine.
func CheckAndPrompt(currentVersion string, onResult func(Result)) {
	res, err := Check(currentVersion)
	if err != nil {
		log.Printf("update check: %v", err)
		return
	}
	if res != nil && res.Available {
		onResult(*res)
	}
}
