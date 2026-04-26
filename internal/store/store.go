package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type StashEntry struct {
	SessionID   string `json:"sessionId"`
	Name        string `json:"name"`
	ProjectPath string `json:"projectPath"`
	GitBranch   string `json:"gitBranch"`
	StashedAt   string `json:"stashedAt"`
	Source      string `json:"source,omitempty"` // "claude" (default) or "codex"
}

type StashIndex struct {
	Entries []StashEntry `json:"entries"`
}

var (
	mu sync.Mutex
)

func stashDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".stash")
}

func indexPath() string {
	return filepath.Join(stashDir(), "index.json")
}

func Load() (*StashIndex, error) {
	data, err := os.ReadFile(indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &StashIndex{}, nil
		}
		return nil, err
	}

	var idx StashIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func Save(idx *StashIndex) error {
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(stashDir(), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath(), data, 0644)
}

func (idx *StashIndex) Add(entry StashEntry) {
	// Update existing entry for same session ID
	for i, e := range idx.Entries {
		if e.SessionID == entry.SessionID {
			idx.Entries[i] = entry
			return
		}
	}
	idx.Entries = append(idx.Entries, entry)
}

func (idx *StashIndex) Remove(sessionID string) {
	for i, e := range idx.Entries {
		if e.SessionID == sessionID {
			idx.Entries = append(idx.Entries[:i], idx.Entries[i+1:]...)
			return
		}
	}
}

func (idx *StashIndex) FindByName(name string) *StashEntry {
	for _, e := range idx.Entries {
		if e.Name == name {
			return &e
		}
	}
	return nil
}

func (idx *StashIndex) FindBySessionID(sessionID string) *StashEntry {
	for _, e := range idx.Entries {
		if e.SessionID == sessionID {
			return &e
		}
	}
	return nil
}

// NameMap returns a map of sessionID -> stash name for quick lookup
func (idx *StashIndex) NameMap() map[string]string {
	m := make(map[string]string, len(idx.Entries))
	for _, e := range idx.Entries {
		m[e.SessionID] = e.Name
	}
	return m
}

// SourceMap returns a map of sessionID -> source for quick lookup
func (idx *StashIndex) SourceMap() map[string]string {
	m := make(map[string]string, len(idx.Entries))
	for _, e := range idx.Entries {
		if e.Source != "" {
			m[e.SessionID] = e.Source
		}
	}
	return m
}
