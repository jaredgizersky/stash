package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type msgCountCache struct {
	Version int                        `json:"version"`
	Entries map[string]*msgCountEntry  `json:"entries"`
	mu      sync.Mutex                 `json:"-"`
}

type msgCountEntry struct {
	Mtime    int64 `json:"mtime"`
	Size     int64 `json:"size"`
	MsgCount int   `json:"msgCount"`
}

const cacheVersion = 1

func cachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".stash", "codex-cache.json")
}

func loadMsgCache() *msgCountCache {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return &msgCountCache{Version: cacheVersion, Entries: make(map[string]*msgCountEntry)}
	}
	var c msgCountCache
	if err := json.Unmarshal(data, &c); err != nil || c.Version != cacheVersion {
		return &msgCountCache{Version: cacheVersion, Entries: make(map[string]*msgCountEntry)}
	}
	if c.Entries == nil {
		c.Entries = make(map[string]*msgCountEntry)
	}
	return &c
}

func saveMsgCache(c *msgCountCache) {
	dir := filepath.Dir(cachePath())
	os.MkdirAll(dir, 0755)
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	os.WriteFile(cachePath(), data, 0644)
}

func (c *msgCountCache) lookup(path string, mtime, size int64) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.Entries[path]; ok && e.Mtime == mtime && e.Size == size {
		return e.MsgCount, true
	}
	return 0, false
}

func (c *msgCountCache) store(path string, mtime, size int64, count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Entries[path] = &msgCountEntry{Mtime: mtime, Size: size, MsgCount: count}
}
