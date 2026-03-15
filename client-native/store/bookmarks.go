package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// StoredBookmark je lokální záložka zprávy.
type StoredBookmark struct {
	ID           string    `json:"id"`
	ServerURL    string    `json:"server_url"`
	ChannelID    string    `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	Content      string    `json:"content"`
	AuthorID     string    `json:"author_id"`
	AuthorName   string    `json:"author_name"`
	CreatedAt    time.Time `json:"created_at"`
	BookmarkedAt time.Time `json:"bookmarked_at"`
	Note         string    `json:"note,omitempty"`
}

// BookmarkStore spravuje lokální záložky zpráv per identita.
type BookmarkStore struct {
	mu        sync.Mutex
	publicKey string
	bookmarks []StoredBookmark
	dirty     bool
}

func NewBookmarkStore(publicKey string) *BookmarkStore {
	s := &BookmarkStore{
		publicKey: publicKey,
	}
	s.load()
	return s
}

func (s *BookmarkStore) bookmarkPath() string {
	short := s.publicKey
	if len(short) > 16 {
		short = short[:16]
	}
	return filepath.Join(noraDir(), "bookmarks_"+short+".json")
}

func (s *BookmarkStore) load() {
	data, err := os.ReadFile(s.bookmarkPath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &s.bookmarks)
}

func (s *BookmarkStore) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	ensureDir()
	data, err := json.Marshal(s.bookmarks)
	if err != nil {
		return
	}
	os.WriteFile(s.bookmarkPath(), data, 0600)
	s.dirty = false
}

// Add přidá záložku (dedup by ID + ServerURL).
func (s *BookmarkStore) Add(b StoredBookmark) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.bookmarks {
		if existing.ID == b.ID && existing.ServerURL == b.ServerURL {
			return
		}
	}
	b.BookmarkedAt = time.Now()
	s.bookmarks = append(s.bookmarks, b)
	s.dirty = true
}

// Remove odebere záložku.
func (s *BookmarkStore) Remove(msgID, serverURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, b := range s.bookmarks {
		if b.ID == msgID && b.ServerURL == serverURL {
			s.bookmarks = append(s.bookmarks[:i], s.bookmarks[i+1:]...)
			s.dirty = true
			return
		}
	}
}

// IsBookmarked kontroluje zda je zpráva v záložkách.
func (s *BookmarkStore) IsBookmarked(msgID, serverURL string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range s.bookmarks {
		if b.ID == msgID && b.ServerURL == serverURL {
			return true
		}
	}
	return false
}

// GetAll vrátí všechny záložky pro server (sorted by BookmarkedAt desc).
func (s *BookmarkStore) GetAll(serverURL string) []StoredBookmark {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []StoredBookmark
	for _, b := range s.bookmarks {
		if b.ServerURL == serverURL {
			result = append(result, b)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].BookmarkedAt.After(result[j].BookmarkedAt)
	})
	return result
}

// GetByChannel vrátí záložky pro server + kanál.
func (s *BookmarkStore) GetByChannel(serverURL, channelID string) []StoredBookmark {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []StoredBookmark
	for _, b := range s.bookmarks {
		if b.ServerURL == serverURL && b.ChannelID == channelID {
			result = append(result, b)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].BookmarkedAt.After(result[j].BookmarkedAt)
	})
	return result
}

// UpdateNote edituje poznámku záložky.
func (s *BookmarkStore) UpdateNote(msgID, serverURL, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, b := range s.bookmarks {
		if b.ID == msgID && b.ServerURL == serverURL {
			s.bookmarks[i].Note = note
			s.dirty = true
			return
		}
	}
}

// Count vrátí počet záložek pro server.
func (s *BookmarkStore) Count(serverURL string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, b := range s.bookmarks {
		if b.ServerURL == serverURL {
			count++
		}
	}
	return count
}
