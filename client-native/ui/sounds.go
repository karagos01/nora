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

	soundVolumes map[string]float64 // per-sound volumes
	customSounds map[string]string  // per-sound custom paths
)

// SetAllSoundSettings sets per-sound volumes and custom paths.
func SetAllSoundSettings(volumes map[string]float64, customs map[string]string) {
	soundMu.Lock()
	soundVolumes = volumes
	customSounds = customs
	soundMu.Unlock()
}

// getSoundConfig returns the volume and custom path for a sound key.
func getSoundConfig(key string) (volume float64, customPath string) {
	soundMu.Lock()
	defer soundMu.Unlock()
	vol, ok := soundVolumes[key]
	if !ok {
		vol = 1.0
	}
	return vol, customSounds[key]
}

// PlayNotificationSound plays a short notification beep.
func PlayNotificationSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	soundMu.Unlock()

	vol, snd := getSoundConfig("notification")
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
	soundMu.Unlock()

	vol, snd := getSoundConfig("dm")
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

// PlayFriendRequestSound plays an ascending 3-tone (friend request notification).
func PlayFriendRequestSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	soundMu.Unlock()

	vol, snd := getSoundConfig("friendRequest")
	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go func() {
			playBeep(523, 60*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(659, 60*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(784, 80*time.Millisecond, vol)
		}()
	}
}

// PlayLFGSound plays a distinctive game-ready staccato sound.
func PlayLFGSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	soundMu.Unlock()

	vol, snd := getSoundConfig("lfg")
	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go func() {
			playBeep(392, 50*time.Millisecond, vol)
			time.Sleep(25 * time.Millisecond)
			playBeep(523, 50*time.Millisecond, vol)
			time.Sleep(25 * time.Millisecond)
			playBeep(659, 50*time.Millisecond, vol)
			time.Sleep(25 * time.Millisecond)
			playBeep(784, 70*time.Millisecond, vol)
		}()
	}
}

// PlayCalendarSound plays a bell-like tone for calendar reminders.
func PlayCalendarSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	soundMu.Unlock()

	vol, snd := getSoundConfig("calendar")
	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go playBeep(1047, 200*time.Millisecond, vol)
	}
}

// PlaySoundPreview plays the sound for a given key without cooldown (for settings).
func PlaySoundPreview(key string) {
	vol, snd := getSoundConfig(key)
	if snd != "" {
		go playCustomSound(snd, vol)
		return
	}
	switch key {
	case "notification":
		go playBeep(800, 120*time.Millisecond, vol)
	case "dm":
		go func() {
			playBeep(600, 80*time.Millisecond, vol)
			time.Sleep(60 * time.Millisecond)
			playBeep(900, 100*time.Millisecond, vol)
		}()
	case "voiceJoin":
		go func() {
			playBeep(440, 60*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(660, 80*time.Millisecond, vol)
		}()
	case "voiceLeave":
		go func() {
			playBeep(660, 60*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(440, 80*time.Millisecond, vol)
		}()
	case "callRing":
		go func() {
			playBeep(523, 150*time.Millisecond, vol)
			time.Sleep(100 * time.Millisecond)
			playBeep(659, 150*time.Millisecond, vol)
		}()
	case "callEnd":
		go func() {
			playBeep(440, 100*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(330, 150*time.Millisecond, vol)
		}()
	case "friendRequest":
		go func() {
			playBeep(523, 60*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(659, 60*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(784, 80*time.Millisecond, vol)
		}()
	case "lfg":
		go func() {
			playBeep(392, 50*time.Millisecond, vol)
			time.Sleep(25 * time.Millisecond)
			playBeep(523, 50*time.Millisecond, vol)
			time.Sleep(25 * time.Millisecond)
			playBeep(659, 50*time.Millisecond, vol)
			time.Sleep(25 * time.Millisecond)
			playBeep(784, 70*time.Millisecond, vol)
		}()
	case "calendar":
		go playBeep(1047, 200*time.Millisecond, vol)
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
		// Try pw-play (PipeWire), paplay (PulseAudio), aplay (ALSA)
		played := false
		for _, player := range []string{"pw-play", "paplay"} {
			if _, err := exec.LookPath(player); err != nil {
				continue
			}
			cmd := exec.Command(player, "-")
			cmd.Stdin = bytes.NewReader(wav)
			if cmd.Run() == nil {
				played = true
				break
			}
		}
		if !played {
			cmd := exec.Command("aplay", "-q", "-f", "S16_LE", "-r", "44100", "-c", "1")
			if len(wav) > 44 {
				cmd.Stdin = bytes.NewReader(wav[44:])
			}
			cmd.Run()
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

// playCustomSound plays a custom sound file (MP3/WAV) at the given volume.
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
	soundMu.Unlock()

	vol, snd := getSoundConfig("voiceJoin")
	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go func() {
			playBeep(440, 60*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(660, 80*time.Millisecond, vol)
		}()
	}
}

// PlayVoiceLeaveSound plays a descending two-tone when a user leaves voice.
func PlayVoiceLeaveSound() {
	soundMu.Lock()
	if time.Since(lastSoundTime) < soundCooldown {
		soundMu.Unlock()
		return
	}
	lastSoundTime = time.Now()
	soundMu.Unlock()

	vol, snd := getSoundConfig("voiceLeave")
	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go func() {
			playBeep(660, 60*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(440, 80*time.Millisecond, vol)
		}()
	}
}

// callRingStop stops the current ring loop.
var (
	callRingMu   sync.Mutex
	callRingStop chan struct{}
)

// StartCallRingLoop starts a repeating ring sound (incoming call).
// The sound repeats until StopCallRingLoop is called.
func StartCallRingLoop() {
	callRingMu.Lock()
	// Stop previous loop
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
			vol, snd := getSoundConfig("callRing")
			if snd != "" {
				playCustomSound(snd, vol)
			} else {
				playBeep(523, 150*time.Millisecond, vol)
				select {
				case <-stop:
					return
				case <-time.After(100 * time.Millisecond):
				}
				playBeep(659, 150*time.Millisecond, vol)
			}
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}

// StartOutgoingRingLoop starts a ring sound for the caller.
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
			vol, snd := getSoundConfig("callRing")
			if snd != "" {
				playCustomSound(snd, vol)
			} else {
				playBeep(440, 200*time.Millisecond, vol)
				select {
				case <-stop:
					return
				case <-time.After(150 * time.Millisecond):
				}
				playBeep(440, 200*time.Millisecond, vol)
			}
			select {
			case <-stop:
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
}

// StopCallRingLoop stops the ring loop (both incoming and outgoing).
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

// PlayCallEndSound plays the call end sound.
func PlayCallEndSound() {
	vol, snd := getSoundConfig("callEnd")
	if snd != "" {
		go playCustomSound(snd, vol)
	} else {
		go func() {
			playBeep(440, 100*time.Millisecond, vol)
			time.Sleep(30 * time.Millisecond)
			playBeep(330, 150*time.Millisecond, vol)
		}()
	}
}

// ShouldNotify decides whether to play a notification sound.
// Channel messages: channel override → server override → global default.
// DMs/groups (channelID == ""): always notify (personal messages ignore server muting).
func (a *App) ShouldNotify(conn *ServerConnection, channelID, content string) bool {
	if conn == nil {
		return true
	}

	// DMs and groups always notify — they are personal messages
	if channelID == "" {
		return true
	}

	// Channel messages: resolve level: channel > server > global
	level := a.GlobalNotifyLevel
	if conn.NotifyLevel != nil {
		level = *conn.NotifyLevel
	}
	if chLevel, ok := conn.ChannelNotify[channelID]; ok {
		level = chLevel
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

// SendOSNotification sends a system notification (toast/banner).
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
$n.BalloonTipIcon = [System.Windows.Forms.ToolTipIcon]::None
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

// escapePS escapes single quotes for PowerShell strings.
func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// escapeAS escapes double quotes and backslashes for AppleScript.
func escapeAS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// copyFile copies a file from src to dst.
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

// truncateMsg shortens a message to max characters with "..." at the end.
func truncateMsg(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
