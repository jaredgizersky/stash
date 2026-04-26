package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type SessionCache struct {
	Version int                    `json:"version"`
	Entries map[string]*CacheEntry `json:"entries"` // key = JSONL full path
	mu      sync.Mutex             `json:"-"`
}

type CacheEntry struct {
	Mtime   int64   `json:"mtime"` // unix nano
	Size    int64   `json:"size"`
	Session Session `json:"session"`
}

const cacheVersion = 3

func cachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".stash", "session-cache.json")
}

func loadCache() *SessionCache {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return &SessionCache{Version: cacheVersion, Entries: make(map[string]*CacheEntry)}
	}
	var c SessionCache
	if err := json.Unmarshal(data, &c); err != nil || c.Version != cacheVersion {
		return &SessionCache{Version: cacheVersion, Entries: make(map[string]*CacheEntry)}
	}
	if c.Entries == nil {
		c.Entries = make(map[string]*CacheEntry)
	}
	return &c
}

func saveCache(c *SessionCache) {
	dir := filepath.Dir(cachePath())
	os.MkdirAll(dir, 0755)
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	os.WriteFile(cachePath(), data, 0644)
}

// lookup checks the cache for a hit. Returns the session and true if cached, zero and false otherwise.
func (c *SessionCache) lookup(path string, mtime int64, size int64) (Session, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.Entries[path]; ok {
		if entry.Mtime == mtime && entry.Size == size {
			return entry.Session, true
		}
	}
	return Session{}, false
}

// store writes a scanned session into the cache.
func (c *SessionCache) store(path string, mtime int64, size int64, s Session) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Entries[path] = &CacheEntry{
		Mtime:   mtime,
		Size:    size,
		Session: s,
	}
}

// pruneCache removes entries whose files no longer exist.
func pruneCache(cache *SessionCache, seen map[string]bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	for path := range cache.Entries {
		if !seen[path] {
			delete(cache.Entries, path)
		}
	}
}
