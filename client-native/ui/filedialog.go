package ui

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// openFileDialog opens a native file dialog and returns the selected file path.
// Returns empty string if the user cancels.
func openFileDialog() string {
	switch runtime.GOOS {
	case "linux":
		// Try zenity first, then kdialog
		if path := zenityFileDialog(); path != "" {
			return path
		}
		return kdialogFileDialog()
	case "windows":
		return powershellFileDialog()
	default:
		return ""
	}
}

func zenityFileDialog() string {
	cmd := exec.Command("zenity", "--file-selection", "--title=Select file to upload")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func kdialogFileDialog() string {
	cmd := exec.Command("kdialog", "--getopenfilename", ".", "All Files (*)")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func powershellFileDialog() string {
	script := `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.OpenFileDialog; $f.Title = 'Select file to upload'; if ($f.ShowDialog() -eq 'OK') { $f.FileName }`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// openMultiFileDialog opens a native file dialog allowing multiple file selection.
// Returns a slice of file paths, or nil if the user cancels.
func openMultiFileDialog() []string {
	switch runtime.GOOS {
	case "linux":
		if paths := zenityMultiFileDialog(); len(paths) > 0 {
			return paths
		}
		return kdialogMultiFileDialog()
	case "windows":
		return powershellMultiFileDialog()
	default:
		return nil
	}
}

func zenityMultiFileDialog() []string {
	cmd := exec.Command("zenity", "--file-selection", "--multiple", "--separator=|", "--title=Select files to upload")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "|")
}

func kdialogMultiFileDialog() []string {
	cmd := exec.Command("kdialog", "--getopenfilename", ".", "All Files (*)", "--multiple")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func powershellMultiFileDialog() []string {
	script := `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.OpenFileDialog; $f.Title = 'Select files to upload'; $f.Multiselect = $true; if ($f.ShowDialog() -eq 'OK') { $f.FileNames -join [char]10 }`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

// saveFileDialog otevře "Save As" dialog s předvyplněným jménem souboru.
func saveFileDialog(defaultName string) string {
	switch runtime.GOOS {
	case "linux":
		if path := zenitySaveDialog(defaultName); path != "" {
			return path
		}
		return kdialogSaveDialog(defaultName)
	case "windows":
		return powershellSaveDialog(defaultName)
	case "darwin":
		return osascriptSaveDialog(defaultName)
	}
	return ""
}

func defaultSavePath(filename string) string {
	home, _ := os.UserHomeDir()
	// Zkusit Downloads složku
	dl := filepath.Join(home, "Downloads")
	if info, err := os.Stat(dl); err == nil && info.IsDir() {
		return filepath.Join(dl, filename)
	}
	return filepath.Join(home, filename)
}

func zenitySaveDialog(defaultName string) string {
	savePath := defaultSavePath(defaultName)
	cmd := exec.Command("zenity", "--file-selection", "--save",
		"--title=Save file", "--filename="+savePath)
	// Vynutit nativní GTK dialog místo xdg-desktop-portal (portal ignoruje --filename)
	cmd.Env = append(os.Environ(), "GTK_USE_PORTAL=0")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func kdialogSaveDialog(defaultName string) string {
	cmd := exec.Command("kdialog", "--getsavefilename", defaultSavePath(defaultName), "All Files (*)")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func powershellSaveDialog(defaultName string) string {
	ext := filepath.Ext(defaultName)
	filter := "All Files (*.*)|*.*"
	if ext != "" {
		filter = "*" + ext + "|*" + ext + "|All Files (*.*)|*.*"
	}
	// Escape single quotes for PowerShell single-quoted string
	safeName := strings.ReplaceAll(defaultName, "'", "''")
	safeFilter := strings.ReplaceAll(filter, "'", "''")
	script := `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.SaveFileDialog; $f.Title = 'Save file'; $f.FileName = '` + safeName + `'; $f.InitialDirectory = [Environment]::GetFolderPath('UserProfile') + '\Downloads'; $f.Filter = '` + safeFilter + `'; if ($f.ShowDialog() -eq 'OK') { $f.FileName }`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func osascriptSaveDialog(defaultName string) string {
	// Escape backslashes and double quotes for AppleScript string
	safeName := strings.ReplaceAll(defaultName, `\`, `\\`)
	safeName = strings.ReplaceAll(safeName, `"`, `\"`)
	script := `choose file name with prompt "Save file" default name "` + safeName + `"`
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	// osascript vrací "alias Macintosh HD:Users:..." — převod na POSIX path
	raw := strings.TrimSpace(string(out))
	if strings.HasPrefix(raw, "file ") || strings.HasPrefix(raw, "alias ") {
		// Use separate -e argument to avoid injection from raw output
		safeRaw := strings.ReplaceAll(raw, `\`, `\\`)
		safeRaw = strings.ReplaceAll(safeRaw, `"`, `\"`)
		cmd2 := exec.Command("osascript", "-e", `POSIX path of "`+safeRaw+`"`)
		out2, err := cmd2.Output()
		if err == nil {
			return strings.TrimSpace(string(out2))
		}
	}
	return raw
}

// pickDirectory opens a native directory picker and returns the selected path.
func pickDirectory() (string, error) {
	switch runtime.GOOS {
	case "linux":
		if path := zenityDirDialog(); path != "" {
			return path, nil
		}
		if path := kdialogDirDialog(); path != "" {
			return path, nil
		}
		return "", nil
	case "windows":
		return powershellDirDialog(), nil
	default:
		return "", nil
	}
}

func zenityDirDialog() string {
	cmd := exec.Command("zenity", "--file-selection", "--directory", "--title=Select folder to share")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func kdialogDirDialog() string {
	cmd := exec.Command("kdialog", "--getexistingdirectory", ".", "Select folder to share")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func powershellDirDialog() string {
	script := `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.FolderBrowserDialog; $f.Description = 'Select folder to share'; if ($f.ShowDialog() -eq 'OK') { $f.SelectedPath }`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// downloadFile stáhne soubor z URL a uloží na disk přes save dialog.
func downloadFile(fileURL, filename, token string) error {
	savePath := saveFileDialog(filename)
	if savePath == "" {
		return nil
	}
	return downloadToPath(fileURL, savePath, token)
}

// downloadToPath stáhne soubor z URL a uloží na zadanou cestu.
func downloadToPath(fileURL, savePath, token string) error {
	req, err := http.NewRequest("GET", fileURL, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.Create(savePath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}
