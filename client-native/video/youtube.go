package video

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kkdai/youtube/v2"
)

var ytRegex = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)([a-zA-Z0-9_-]{11})`)

// YouTubeFormat represents a single quality option for a YouTube video.
type YouTubeFormat struct {
	Label  string // "360p", "480p", "720p"
	Width  int
	Height int
	ItagNo int
	Muxed  bool   // true = has audio+video, false = video-only (needs separate audio)
	URL    string // direct stream URL (from yt-dlp, empty if kkdai fallback)
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

// FetchYouTubeInfo fetches video metadata and available formats.
// Tries yt-dlp first (more reliable), falls back to kkdai/youtube library.
func FetchYouTubeInfo(videoID string) (*YouTubeInfo, error) {
	if ytdlp := checkYtDlp(); ytdlp != "" {
		info, err := fetchInfoYtDlp(ytdlp, videoID)
		if err == nil {
			return info, nil
		}
		log.Printf("video: yt-dlp info failed: %v, trying kkdai", err)
	}
	return fetchInfoKkdai(videoID)
}

// GetYouTubeStreamURLs uses yt-dlp to extract direct stream URLs (no download).
// ffmpeg can stream from these URLs directly for instant playback.
// Single yt-dlp invocation for both video+audio URLs.
func GetYouTubeStreamURLs(videoID string, videoItag, audioItag int) (videoURL, audioURL string, err error) {
	ytdlp := checkYtDlp()
	if ytdlp == "" {
		return "", "", fmt.Errorf("yt-dlp not found")
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
// Fallback for when stream URL extraction fails.
// Returns file paths + cleanup function. Caller must call cleanup to remove temp files.
func DownloadYouTubeStreams(videoID string, videoItag, audioItag int) (videoPath, audioPath string, cleanup func(), err error) {
	if ytdlp := checkYtDlp(); ytdlp != "" {
		vp, ap, cl, err := downloadYtDlp(ytdlp, videoID, videoItag, audioItag)
		if err == nil {
			return vp, ap, cl, nil
		}
		log.Printf("video: yt-dlp download failed: %v, trying kkdai", err)
	}
	return downloadKkdai(videoID, videoItag, audioItag)
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

// --- kkdai/youtube fallback backend ---

func fetchInfoKkdai(videoID string) (*YouTubeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := youtube.Client{}
	vid, err := client.GetVideoContext(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("youtube get video: %w", err)
	}

	info := &YouTubeInfo{
		Title:     vid.Title,
		Thumbnail: YouTubeThumbnailURL(videoID),
		Duration:  vid.Duration,
		VideoID:   videoID,
	}

	var muxed, adaptive youtube.FormatList
	var bestAudio youtube.Format
	bestAudioBitrate := 0
	for _, f := range vid.Formats {
		if f.AudioChannels > 0 && f.Width > 0 {
			muxed = append(muxed, f)
		} else if f.Width > 0 && f.AudioChannels == 0 {
			adaptive = append(adaptive, f)
		} else if f.AudioChannels > 0 && f.Width == 0 {
			if f.Bitrate > bestAudioBitrate {
				bestAudio = f
				bestAudioBitrate = f.Bitrate
			}
		}
	}

	if len(muxed) == 0 && len(adaptive) == 0 {
		return nil, fmt.Errorf("no video formats available")
	}

	bestByHeight := make(map[int]youtube.Format)
	for _, f := range muxed {
		if existing, ok := bestByHeight[f.Height]; !ok || f.Bitrate > existing.Bitrate {
			bestByHeight[f.Height] = f
		}
	}

	adaptiveByHeight := make(map[int]youtube.Format)
	for _, f := range adaptive {
		if _, hasMuxed := bestByHeight[f.Height]; hasMuxed {
			continue
		}
		if existing, ok := adaptiveByHeight[f.Height]; !ok || f.Bitrate > existing.Bitrate {
			adaptiveByHeight[f.Height] = f
		}
	}

	type qualityEntry struct {
		format youtube.Format
		muxed  bool
	}
	var all []qualityEntry
	for _, f := range bestByHeight {
		all = append(all, qualityEntry{f, true})
	}
	for _, f := range adaptiveByHeight {
		all = append(all, qualityEntry{f, false})
	}

	sort.Slice(all, func(i, j int) bool {
		wi, wj := all[i].format.Width, all[j].format.Width
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
		label := fmt.Sprintf("%dp", e.format.Height)
		if e.format.QualityLabel != "" {
			label = e.format.QualityLabel
		}
		info.Formats = append(info.Formats, YouTubeFormat{
			Label:  label,
			Width:  e.format.Width,
			Height: e.format.Height,
			ItagNo: e.format.ItagNo,
			Muxed:  e.muxed,
		})
	}

	if len(all) == 0 {
		return nil, fmt.Errorf("no video formats available")
	}

	if bestAudioBitrate > 0 {
		info.BestAudioTag = bestAudio.ItagNo
	}

	return info, nil
}

func downloadKkdai(videoID string, videoItag, audioItag int) (string, string, func(), error) {
	client := youtube.Client{}
	vid, err := client.GetVideo(videoID)
	if err != nil {
		return "", "", nil, fmt.Errorf("youtube get video: %w", err)
	}

	var videoFormat, audioFormat *youtube.Format
	for i := range vid.Formats {
		if vid.Formats[i].ItagNo == videoItag {
			videoFormat = &vid.Formats[i]
		}
		if audioItag > 0 && vid.Formats[i].ItagNo == audioItag {
			audioFormat = &vid.Formats[i]
		}
	}
	if videoFormat == nil {
		return "", "", nil, fmt.Errorf("video format itag %d not found", videoItag)
	}

	var files []string
	cleanupFn := func() {
		for _, f := range files {
			os.Remove(f)
		}
	}

	type result struct {
		path string
		err  error
	}
	videoCh := make(chan result, 1)
	audioCh := make(chan result, 1)

	download := func(f *youtube.Format, prefix string) (string, error) {
		stream, _, err := client.GetStream(vid, f)
		if err != nil {
			return "", fmt.Errorf("get stream: %w", err)
		}
		defer stream.Close()

		tmp, err := os.CreateTemp("", "nora-yt-"+prefix+"-*.mp4")
		if err != nil {
			return "", err
		}
		n, err := io.Copy(tmp, stream)
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
			return "", fmt.Errorf("download: %w", err)
		}
		log.Printf("video: kkdai downloaded %s %d bytes to %s", prefix, n, tmp.Name())
		return tmp.Name(), nil
	}

	go func() {
		path, err := download(videoFormat, "video")
		videoCh <- result{path, err}
	}()

	if audioFormat != nil {
		go func() {
			path, err := download(audioFormat, "audio")
			audioCh <- result{path, err}
		}()
	} else {
		audioCh <- result{}
	}

	vr := <-videoCh
	ar := <-audioCh

	if vr.err != nil {
		if ar.path != "" {
			os.Remove(ar.path)
		}
		return "", "", nil, fmt.Errorf("video download: %w", vr.err)
	}

	files = append(files, vr.path)
	if ar.path != "" {
		files = append(files, ar.path)
	}

	return vr.path, ar.path, cleanupFn, nil
}
