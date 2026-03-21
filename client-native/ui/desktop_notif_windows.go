//go:build windows

package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// sendDesktopNotification odesle OS-level toast notifikaci na Windows (PowerShell).
func sendDesktopNotification(title, body, icon string) error {
	script := fmt.Sprintf(`
[void] [System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms')
$n = New-Object System.Windows.Forms.NotifyIcon
$n.Icon = [System.Drawing.SystemIcons]::Information
$n.BalloonTipTitle = '%s'
$n.BalloonTipText = '%s'
$n.BalloonTipIcon = [System.Windows.Forms.ToolTipIcon]::None
$n.Visible = $true
$n.ShowBalloonTip(5000)
Start-Sleep -Milliseconds 5100
$n.Dispose()`,
		strings.ReplaceAll(title, "'", "''"),
		strings.ReplaceAll(body, "'", "''"))

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.WaitDelay = 10 * time.Second
	return cmd.Run()
}
