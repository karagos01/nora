package video

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/kkdai/youtube/v2"
)

var ytRegex = regexp.MustCompile(`(?:youtube\.com/watch\?v=|youtu\.be/|youtube\.com/shorts/)([a-zA-Z0-9_-]{11})`)

// YouTubeInfo obsahuje metadata o YouTube videu.
type YouTubeInfo struct {
	Title     string
	Thumbnail string
	StreamURL string
	Duration  time.Duration
}

// YouTubeVideoID extrahuje video ID z URL.
func YouTubeVideoID(url string) string {
	m := ytRegex.FindStringSubmatch(url)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// IsYouTubeURL zjistí, zda URL je YouTube video.
func IsYouTubeURL(url string) bool {
	return YouTubeVideoID(url) != ""
}

// YouTubeThumbnailURL vrátí URL thumbnailiu pro YouTube video.
func YouTubeThumbnailURL(videoID string) string {
	return fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID)
}

// FetchYouTubeInfo získá metadata a stream URL pro YouTube video.
func FetchYouTubeInfo(videoID string) (*YouTubeInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := youtube.Client{}
	video, err := client.GetVideoContext(ctx, videoID)
	if err != nil {
		return nil, fmt.Errorf("youtube get video: %w", err)
	}

	info := &YouTubeInfo{
		Title:     video.Title,
		Thumbnail: YouTubeThumbnailURL(videoID),
		Duration:  video.Duration,
	}

	// Najít muxed formát (audio+video) — max 720p
	formats := video.Formats
	// Filtrovat na muxed (mají AudioChannels > 0 a nenulovou šířku)
	var muxed youtube.FormatList
	for _, f := range formats {
		if f.AudioChannels > 0 && f.Width > 0 {
			muxed = append(muxed, f)
		}
	}

	if len(muxed) == 0 {
		return nil, fmt.Errorf("no muxed formats available")
	}

	// Seřadit podle kvality — preferovat max 720p
	sort.Slice(muxed, func(i, j int) bool {
		wi, wj := muxed[i].Width, muxed[j].Width
		// Preferovat nižší rozlišení pokud je pod 720p, jinak seřadit sestupně
		if wi <= 1280 && wj <= 1280 {
			return wi > wj // Vyšší kvalita první (ale max 720p)
		}
		if wi <= 1280 {
			return true
		}
		if wj <= 1280 {
			return false
		}
		return wi < wj // Oba nad 720p → preferovat nižší
	})

	streamURL, err := client.GetStreamURLContext(ctx, video, &muxed[0])
	if err != nil {
		return nil, fmt.Errorf("youtube stream url: %w", err)
	}

	info.StreamURL = streamURL
	return info, nil
}
