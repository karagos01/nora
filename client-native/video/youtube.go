package video

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ytRegex = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)([a-zA-Z0-9_-]{11})`)

// YouTubeFormat represents a single quality option for a YouTube video.
type YouTubeFormat struct {
	Label  string // "360p", "480p", "720p"
	Width  int
	Height int
	ItagNo int
	Muxed  bool   // true = has audio+video, false = video-only (needs separate audio)
	URL    string // direct stream URL from yt-dlp
}

// YouTubeInfo holds metadata about a YouTube video.
type YouTubeInfo struct {
	Title        string
	Thumbnail    string
	Duration     time.Duration
	VideoID      string
	BestAudioTag int             // best audio-only itag (for adaptive formats)
	BestAudioURL string          // direct stream URL for best audio (from yt-dlp)
	Formats      []YouTubeFormat // available quality options, best first
}

// YouTubeVideoID extracts video ID from URL.
func YouTubeVideoID(url string) string {
	m := ytRegex.FindStringSubmatch(url)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// IsYouTubeURL returns true if URL is a YouTube video.
func IsYouTubeURL(url string) bool {
	return YouTubeVideoID(url) != ""
}

// YouTubeThumbnailURL returns thumbnail URL for a YouTube video.
func YouTubeThumbnailURL(videoID string) string {
	return fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)
}

func checkYtDlp() string {
	path, err := exec.LookPath("yt-dlp")
	if err != nil {
		return ""
	}
	return path
}

// YtDlpInstallHint returns a platform-specific installation hint for yt-dlp.
func YtDlpInstallHint() string {
	switch runtime.GOOS {
	case "windows":
		return "yt-dlp is required for YouTube playback.\nInstall: winget install yt-dlp"
	case "darwin":
		return "yt-dlp is required for YouTube playback.\nInstall: brew install yt-dlp"
	default:
		return "yt-dlp is required for YouTube playback.\nInstall: sudo apt install yt-dlp"
	}
}

// FetchYouTubeInfo fetches video metadata and available formats via yt-dlp.
func FetchYouTubeInfo(videoID string) (*YouTubeInfo, error) {
	ytdlp := checkYtDlp()
	if ytdlp == "" {
		return nil, fmt.Errorf("%s", YtDlpInstallHint())
	}
	return fetchInfoYtDlp(ytdlp, videoID)
}

// GetYouTubeStreamURLs uses yt-dlp to extract direct stream URLs (no download).
// ffmpeg can stream from these URLs directly for instant playback.
// Single yt-dlp invocation for both video+audio URLs.
func GetYouTubeStreamURLs(videoID string, videoItag, audioItag int) (videoURL, audioURL string, err error) {
	ytdlp := checkYtDlp()
	if ytdlp == "" {
		return "", "", fmt.Errorf("%s", YtDlpInstallHint())
	}

	ytURL := "https://www.youtube.com/watch?v=" + videoID
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Single call: -f "video+audio" outputs two lines, -f "video" outputs one
	formatSpec := strconv.Itoa(videoItag)
	if audioItag > 0 {
		formatSpec = fmt.Sprintf("%d+%d", videoItag, audioItag)
	}

	cmd := exec.CommandContext(ctx, ytdlp,
		"-f", formatSpec,
		"-g", "--no-warnings",
		ytURL,
	)
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("yt-dlp -g: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", "", fmt.Errorf("yt-dlp: no URL returned")
	}

	videoURL = lines[0]
	if audioItag > 0 && len(lines) >= 2 {
		audioURL = lines[1]
	}

	log.Printf("video: yt-dlp extracted stream URLs in single call")
	return videoURL, audioURL, nil
}

// DownloadYouTubeStreams downloads video (and optionally audio) to temp files.
// Returns file paths + cleanup function. Caller must call cleanup to remove temp files.
func DownloadYouTubeStreams(videoID string, videoItag, audioItag int) (videoPath, audioPath string, cleanup func(), err error) {
	ytdlp := checkYtDlp()
	if ytdlp == "" {
		return "", "", nil, fmt.Errorf("%s", YtDlpInstallHint())
	}
	return downloadYtDlp(ytdlp, videoID, videoItag, audioItag)
}

// --- yt-dlp backend ---

type ytdlpJSON struct {
	Title     string        `json:"title"`
	Duration  float64       `json:"duration"`
	Thumbnail string        `json:"thumbnail"`
	Formats   []ytdlpFmt    `json:"formats"`
}

type ytdlpFmt struct {
	FormatID   string  `json:"format_id"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Acodec     string  `json:"acodec"`
	Vcodec     string  `json:"vcodec"`
	ABR        float64 `json:"abr"`
	TBR        float64 `json:"tbr"`
	FormatNote string  `json:"format_note"`
	URL        string  `json:"url"`
}

func fetchInfoYtDlp(ytdlpPath, videoID string) (*YouTubeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	url := "https://www.youtube.com/watch?v=" + videoID
	cmd := exec.CommandContext(ctx, ytdlpPath, "-j", "--no-warnings", url)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp: %w", err)
	}

	var data ytdlpJSON
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, fmt.Errorf("yt-dlp json: %w", err)
	}

	info := &YouTubeInfo{
		Title:     data.Title,
		Thumbnail: YouTubeThumbnailURL(videoID),
		Duration:  time.Duration(data.Duration * float64(time.Second)),
		VideoID:   videoID,
	}

	type entry struct {
		label  string
		width  int
		height int
		itag   int
		muxed  bool
		tbr    float64
		url    string
	}

	bestMuxed := make(map[int]entry)
	bestAdaptive := make(map[int]entry)
	bestAudioBR := float64(0)

	for _, f := range data.Formats {
		itag, err := strconv.Atoi(f.FormatID)
		if err != nil {
			continue // skip non-numeric format IDs (storyboards etc.)
		}

		hasVideo := f.Vcodec != "" && f.Vcodec != "none" && f.Width > 0
		hasAudio := f.Acodec != "" && f.Acodec != "none"

		label := fmt.Sprintf("%dp", f.Height)
		if f.FormatNote != "" {
			label = f.FormatNote
		}

		if hasVideo && hasAudio {
			if existing, ok := bestMuxed[f.Height]; !ok || f.TBR > existing.tbr {
				bestMuxed[f.Height] = entry{label, f.Width, f.Height, itag, true, f.TBR, f.URL}
			}
		} else if hasVideo {
			if _, hasMuxed := bestMuxed[f.Height]; !hasMuxed {
				if existing, ok := bestAdaptive[f.Height]; !ok || f.TBR > existing.tbr {
					bestAdaptive[f.Height] = entry{label, f.Width, f.Height, itag, false, f.TBR, f.URL}
				}
			}
		} else if hasAudio && f.ABR > bestAudioBR {
			bestAudioBR = f.ABR
			info.BestAudioTag = itag
			info.BestAudioURL = f.URL
		}
	}

	var all []entry
	for _, e := range bestMuxed {
		all = append(all, e)
	}
	for _, e := range bestAdaptive {
		all = append(all, e)
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no video formats available")
	}

	// Sort: highest quality ≤720p first, then >720p ascending
	sort.Slice(all, func(i, j int) bool {
		wi, wj := all[i].width, all[j].width
		if wi <= 1280 && wj <= 1280 {
			return wi > wj
		}
		if wi <= 1280 {
			return true
		}
		if wj <= 1280 {
			return false
		}
		return wi < wj
	})

	for _, e := range all {
		info.Formats = append(info.Formats, YouTubeFormat{
			Label:  e.label,
			Width:  e.width,
			Height: e.height,
			ItagNo: e.itag,
			Muxed:  e.muxed,
			URL:    e.url,
		})
	}

	return info, nil
}

func downloadYtDlp(ytdlpPath, videoID string, videoItag, audioItag int) (string, string, func(), error) {
	url := "https://www.youtube.com/watch?v=" + videoID

	tmpDir, err := os.MkdirTemp("", "nora-yt-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanupFn := func() { os.RemoveAll(tmpDir) }

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Download video
	videoOut := filepath.Join(tmpDir, "video.%(ext)s")
	cmd := exec.CommandContext(ctx, ytdlpPath,
		"-f", strconv.Itoa(videoItag),
		"--no-part", "--no-warnings",
		"-o", videoOut,
		url,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanupFn()
		return "", "", nil, fmt.Errorf("yt-dlp video: %w: %s", err, out)
	}

	matches, _ := filepath.Glob(filepath.Join(tmpDir, "video.*"))
	if len(matches) == 0 {
		cleanupFn()
		return "", "", nil, fmt.Errorf("yt-dlp: no video file created")
	}
	videoPath := matches[0]

	fi, _ := os.Stat(videoPath)
	if fi == nil || fi.Size() == 0 {
		cleanupFn()
		return "", "", nil, fmt.Errorf("yt-dlp: video file empty")
	}
	log.Printf("video: yt-dlp downloaded video %d bytes to %s", fi.Size(), videoPath)

	// Download audio if needed (adaptive format)
	audioPath := ""
	if audioItag > 0 {
		audioOut := filepath.Join(tmpDir, "audio.%(ext)s")
		cmd = exec.CommandContext(ctx, ytdlpPath,
			"-f", strconv.Itoa(audioItag),
			"--no-part", "--no-warnings",
			"-o", audioOut,
			url,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			cleanupFn()
			return "", "", nil, fmt.Errorf("yt-dlp audio: %w: %s", err, out)
		}

		matches, _ = filepath.Glob(filepath.Join(tmpDir, "audio.*"))
		if len(matches) > 0 {
			audioPath = matches[0]
			fi, _ = os.Stat(audioPath)
			if fi != nil {
				log.Printf("video: yt-dlp downloaded audio %d bytes to %s", fi.Size(), audioPath)
			}
		}
	}

	return videoPath, audioPath, cleanupFn, nil
}

