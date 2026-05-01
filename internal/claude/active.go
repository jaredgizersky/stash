package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

type ActiveSession struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	StartedAt  int64  `json:"startedAt"`
	Kind       string `json:"kind"`
	Entrypoint string `json:"entrypoint"`
	Name       string `json:"name"`

	// Enriched
	Alive   bool
	Linked  *Session // matched transcript from projects/
	Started time.Time
}

func LoadActiveSessions() ([]ActiveSession, error) {
	dir := filepath.Join(ClaudeDir(), "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []ActiveSession
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var s ActiveSession
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}

		s.Started = time.UnixMilli(s.StartedAt)
		s.Alive = isProcessAlive(s.PID)
		sessions = append(sessions, s)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Started.After(sessions[j].Started)
	})

	return sessions, nil
}

// LinkTranscripts matches active sessions to their JSONL transcripts
// LinkTranscripts matches active sessions to their JSONL transcripts.
func LinkTranscripts(active []ActiveSession, all []Session) {
	byID := make(map[string]*Session, len(all))
	for i := range all {
		byID[all[i].SessionID] = &all[i]
	}
	for i := range active {
		if s, ok := byID[active[i].SessionID]; ok {
			active[i].Linked = s
			if active[i].Name != "" && s.Name == "" {
				s.Name = active[i].Name
			}
		}
	}
}

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// ShortCwd returns a shortened cwd for display
func (a *ActiveSession) ShortCwd() string {
	home, _ := os.UserHomeDir()
	p := a.Cwd
	if len(home) > 0 && len(p) > len(home) && p[:len(home)] == home {
		p = "~" + p[len(home):]
	}
	return p
}

// DisplayName returns the best name for this active session
func (a *ActiveSession) DisplayName() string {
	if a.Name != "" {
		return singleLine(a.Name)
	}
	if a.Linked != nil {
		t := a.Linked.Title()
		if t != "" {
			return t
		}
	}
	// Fall back: try to read first prompt from the JSONL
	encoded := EncodeCwd(a.Cwd)
	jsonlPath := filepath.Join(ProjectsDir(), encoded, a.SessionID+".jsonl")
	if prompt := extractTextFromFirstUserMsg(jsonlPath); prompt != "" {
		return prompt
	}
	return a.SessionID[:8]
}

// extractTextFromFirstUserMsg reads the first user text from a JSONL (lightweight)
func extractTextFromFirstUserMsg(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var msg map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg["type"] != "user" {
			continue
		}
		message, ok := msg["message"].(map[string]interface{})
		if !ok {
			continue
		}
		content := message["content"]
		switch v := content.(type) {
		case string:
			return truncateStr(v, 120)
		case []interface{}:
			for _, block := range v {
				b, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				if b["type"] == "text" {
					if text, ok := b["text"].(string); ok {
						return truncateStr(text, 120)
					}
				}
			}
		}
	}
	return ""
}

func truncateStr(s string, max int) string {
	s = singleLine(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
