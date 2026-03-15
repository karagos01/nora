package ui

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"nora-client/store"
)

var (
	soundMu       sync.Mutex
	lastSoundTime time.Time
	soundCooldown = 500 * time.Millisecond

	notifVolume    float64 = 1.0
	customNotifSnd string
	customDMSnd    string
)

// SetSoundSettings nastaví hlasitost a cesty ke custom zvukům.
func SetSoundSettings(volume float64, notifPath, dmPath string) {
	soundMu.Lock()
	notifVolume = volume
	customNotifSnd = notifPath
	customDMSnd = dmPath
	soundMu.Unlock()
}

// PlayNotificationSound plays a short notification beep.
// Cross-platform: uses paplay on Linux, PowerShell on Windows.
func PlayNotificationSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	vol := notifVolume
	snd := customNotifSnd
	soundMu.Unlock()

	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go playBeep(800, 120*time.Millisecond, vol)
	}
}

// PlayDMSound plays a two-tone DM notification sound.
func PlayDMSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	vol := notifVolume
	snd := customDMSnd
	soundMu.Unlock()

	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go func() {
			playBeep(600, 80*time.Millisecond, vol)
			time.Sleep(60 * time.Millisecond)
			playBeep(900, 100*time.Millisecond, vol)
		}()
	}
}

// PlayNotifPreview přehraje notification zvuk bez cooldownu (pro settings preview).
func PlayNotifPreview() {
	soundMu.Lock()
	vol := notifVolume
	snd := customNotifSnd
	soundMu.Unlock()

	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go playBeep(800, 120*time.Millisecond, vol)
	}
}

// PlayDMPreview přehraje DM zvuk bez cooldownu (pro settings preview).
func PlayDMPreview() {
	soundMu.Lock()
	vol := notifVolume
	snd := customDMSnd
	soundMu.Unlock()

	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go func() {
			playBeep(600, 80*time.Millisecond, vol)
			time.Sleep(60 * time.Millisecond)
			playBeep(900, 100*time.Millisecond, vol)
		}()
	}
}

func playBeep(freqHz float64, duration time.Duration, volume ...float64) {
	vol := 1.0
	if len(volume) > 0 {
		vol = volume[0]
	}
	wav := generateWAV(freqHz, duration, vol)

	switch runtime.GOOS {
	case "linux":
		// Try paplay (PulseAudio/PipeWire), fallback to aplay (ALSA)
		cmd := exec.Command("paplay", "--raw", "--rate=44100", "--channels=1", "--format=s16le")
		// Strip WAV header for raw mode
		if len(wav) > 44 {
			cmd.Stdin = bytes.NewReader(wav[44:])
		}
		if err := cmd.Run(); err != nil {
			cmd2 := exec.Command("aplay", "-q", "-f", "S16_LE", "-r", "44100", "-c", "1")
			if len(wav) > 44 {
				cmd2.Stdin = bytes.NewReader(wav[44:])
			}
			cmd2.Run()
		}
	case "windows":
		// Write WAV to temp and play via PowerShell
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			"[Console]::Beep("+itoa(int(freqHz))+","+itoa(int(duration.Milliseconds()))+")")
		cmd.Run()
	case "darwin":
		cmd := exec.Command("afplay", "/dev/stdin")
		cmd.Stdin = bytes.NewReader(wav)
		cmd.Run()
	}
}

func generateWAV(freqHz float64, duration time.Duration, volume float64) []byte {
	sampleRate := 44100
	numSamples := int(float64(sampleRate) * duration.Seconds())
	dataSize := numSamples * 2 // 16-bit samples

	buf := &bytes.Buffer{}

	// WAV header
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))     // chunk size
	binary.Write(buf, binary.LittleEndian, uint16(1))      // PCM
	binary.Write(buf, binary.LittleEndian, uint16(1))      // mono
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	binary.Write(buf, binary.LittleEndian, uint16(2))      // block align
	binary.Write(buf, binary.LittleEndian, uint16(16))     // bits per sample
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))

	// Generate sine wave with fade in/out
	fadeLen := numSamples / 8
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		sample := math.Sin(2 * math.Pi * freqHz * t)

		// Fade envelope
		env := 1.0
		if i < fadeLen {
			env = float64(i) / float64(fadeLen)
		} else if i > numSamples-fadeLen {
			env = float64(numSamples-i) / float64(fadeLen)
		}

		val := int16(sample * env * 16000 * volume)
		binary.Write(buf, binary.LittleEndian, val)
	}

	return buf.Bytes()
}

// playCustomSound přehraje custom zvukový soubor (MP3/WAV) s danou hlasitostí.
func playCustomSound(path string, volume float64) {
	volPct := int(volume * 100)
	switch runtime.GOOS {
	case "linux":
		// ffplay s volume, fallback na paplay
		cmd := exec.Command("ffplay", "-nodisp", "-autoexit", "-volume", itoa(volPct), path)
		if err := cmd.Run(); err != nil {
			exec.Command("paplay", path).Run()
		}
	case "windows":
		script := fmt.Sprintf(
			`$p = New-Object System.Windows.Media.MediaPlayer; $p.Open([Uri]'%s'); $p.Volume = %f; Start-Sleep -Milliseconds 200; $p.Play(); Start-Sleep -Seconds 3; $p.Close()`,
			strings.ReplaceAll(path, "'", "''"), volume)
		exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run()
	case "darwin":
		exec.Command("afplay", "-v", fmt.Sprintf("%.2f", volume), path).Run()
	}
}

// PlayVoiceJoinSound plays an ascending two-tone when a user joins voice.
func PlayVoiceJoinSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	vol := notifVolume
	soundMu.Unlock()

	go func() {
		playBeep(440, 60*time.Millisecond, vol)
		time.Sleep(30 * time.Millisecond)
		playBeep(660, 80*time.Millisecond, vol)
	}()
}

// PlayVoiceLeaveSound plays a descending two-tone when a user leaves voice.
func PlayVoiceLeaveSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	vol := notifVolume
	soundMu.Unlock()

	go func() {
		playBeep(660, 60*time.Millisecond, vol)
		time.Sleep(30 * time.Millisecond)
		playBeep(440, 80*time.Millisecond, vol)
	}()
}

// callRingStop zastaví aktuální ring loop.
var (
	callRingMu   sync.Mutex
	callRingStop chan struct{}
)

// StartCallRingLoop spustí opakující se zvonění (příchozí hovor).
// Zvuk se opakuje dokud se nezavolá StopCallRingLoop.
func StartCallRingLoop() {
	callRingMu.Lock()
	// Zastavit předchozí loop
	if callRingStop != nil {
		select {
		case <-callRingStop:
		default:
			close(callRingStop)
		}
	}
	stop := make(chan struct{})
	callRingStop = stop
	callRingMu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			soundMu.Lock()
			vol := notifVolume
			soundMu.Unlock()
			playBeep(523, 150*time.Millisecond, vol)
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
			}
			playBeep(659, 150*time.Millisecond, vol)
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}

// StartOutgoingRingLoop spustí zvuk vyzvánění pro volajícího.
func StartOutgoingRingLoop() {
	callRingMu.Lock()
	if callRingStop != nil {
		select {
		case <-callRingStop:
		default:
			close(callRingStop)
		}
	}
	stop := make(chan struct{})
	callRingStop = stop
	callRingMu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			soundMu.Lock()
			vol := notifVolume
			soundMu.Unlock()
			playBeep(440, 200*time.Millisecond, vol)
			select {
			case <-stop:
				return
			case <-time.After(150 * time.Millisecond):
			}
			playBeep(440, 200*time.Millisecond, vol)
			select {
			case <-stop:
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
}

// StopCallRingLoop zastaví ring loop (incoming i outgoing).
func StopCallRingLoop() {
	callRingMu.Lock()
	if callRingStop != nil {
		select {
		case <-callRingStop:
		default:
			close(callRingStop)
		}
		callRingStop = nil
	}
	callRingMu.Unlock()
}

// PlayCallEndSound přehraje zvuk ukončení hovoru.
func PlayCallEndSound() {
	soundMu.Lock()
	vol := notifVolume
	soundMu.Unlock()
	go func() {
		playBeep(440, 100*time.Millisecond, vol)
		time.Sleep(30 * time.Millisecond)
		playBeep(330, 150*time.Millisecond, vol)
	}()
}

// ShouldNotify rozhoduje jestli přehrát notifikační zvuk.
// Hierarchie: channel override → server override → global default.
// channelID == "" → DM/group (jen server + global).
func (a *App) ShouldNotify(conn *ServerConnection, channelID, content string) bool {
	if conn == nil {
		return true
	}

	// Resolve level: channel > server > global
	level := a.GlobalNotifyLevel
	if conn.NotifyLevel != nil {
		level = *conn.NotifyLevel
	}
	if channelID != "" {
		if chLevel, ok := conn.ChannelNotify[channelID]; ok {
			level = chLevel
		}
	}

	switch level {
	case store.NotifyNothing:
		return false
	case store.NotifyMentions:
		a.mu.RLock()
		username := a.Username
		a.mu.RUnlock()
		return strings.Contains(content, "@"+username)
	default:
		return true
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

// SendOSNotification posílá systémovou notifikaci (toast/banner).
// Linux: notify-send, Windows: PowerShell, macOS: osascript.
func SendOSNotification(title, body string) {
	go sendOSNotification(title, body)
}

func sendOSNotification(title, body string) {
	switch runtime.GOOS {
	case "linux":
		exec.Command("notify-send", "-a", "NORA", title, body).Run()
	case "windows":
		script := fmt.Sprintf(`
[void] [System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms')
$n = New-Object System.Windows.Forms.NotifyIcon
$n.Icon = [System.Drawing.SystemIcons]::Information
$n.BalloonTipTitle = '%s'
$n.BalloonTipText = '%s'
$n.Visible = $true
$n.ShowBalloonTip(5000)
Start-Sleep -Milliseconds 5100
$n.Dispose()`, escapePS(title), escapePS(body))
		exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run()
	case "darwin":
		script := fmt.Sprintf(`display notification "%s" with title "%s"`, escapeAS(body), escapeAS(title))
		exec.Command("osascript", "-e", script).Run()
	}
}

// escapePS escapuje single quotes pro PowerShell stringy.
func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// escapeAS escapuje double quotes a backslashe pro AppleScript.
func escapeAS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// copyFile zkopíruje soubor ze src do dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// truncateMsg zkrátí zprávu na max znaků s "..." na konci.
func truncateMsg(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
