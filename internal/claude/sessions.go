package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Session struct {
	SessionID   string    `json:"sessionId"`
	FullPath    string    `json:"fullPath"`
	FirstPrompt string    `json:"firstPrompt"`
	Summary     string    `json:"summary"`
	MsgCount    int       `json:"messageCount"`
	Created     time.Time `json:"created"`
	Modified    time.Time `json:"modified"`
	GitBranch   string    `json:"gitBranch"`
	ProjectPath string    `json:"projectPath"`
	IsSidechain bool      `json:"isSidechain"`
	Source      string    `json:"source"`      // "claude" or "codex"
	Model       string    `json:"model,omitempty"`

	// Enriched
	Name    string `json:"name,omitempty"` // best name: stash name > agent name > empty
	Stashed bool   `json:"-"`             // true if session is in the stash index
}


// ClaudeDir returns the path to ~/.claude
func ClaudeDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// ProjectsDir returns the path to ~/.claude/projects
func ProjectsDir() string {
	return filepath.Join(ClaudeDir(), "projects")
}

// EncodeCwd converts a directory path to Claude's encoded format
func EncodeCwd(dir string) string {
	var b strings.Builder
	for _, c := range dir {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

// LoadSessionsForProject reads sessions for a given project directory using the cache.
func LoadSessionsForProject(projectDir string) ([]Session, error) {
	encoded := EncodeCwd(projectDir)
	claudeProjectDir := filepath.Join(ProjectsDir(), encoded)

	cache := loadCache()
	sessions, seen := loadProjectCached(cache, claudeProjectDir)
	pruneCache(cache, seen)
	saveCache(cache)

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})

	return sessions, nil
}

// LoadAllSessions reads sessions from all Claude project directories using the cache.
func LoadAllSessions() ([]Session, error) {
	projectsDir := ProjectsDir()
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("reading projects dir: %w", err)
	}

	cache := loadCache()
	allSeen := make(map[string]bool)

	var all []Session
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, entry.Name())
		sessions, seen := loadProjectCached(cache, dirPath)
		all = append(all, sessions...)
		for k := range seen {
			allSeen[k] = true
		}
	}

	pruneCache(cache, allSeen)
	saveCache(cache)

	sort.Slice(all, func(i, j int) bool {
		return all[i].Modified.After(all[j].Modified)
	})

	return all, nil
}


// loadProjectCached loads sessions for a single project dir, using the cache for
// JSONL files whose mtime+size haven't changed. Cache misses are scanned in parallel.
// Returns sessions and the set of JSONL paths that were found (for cache pruning).
func loadProjectCached(cache *SessionCache, claudeDir string) ([]Session, map[string]bool) {
	seen := make(map[string]bool)

	matches, err := filepath.Glob(filepath.Join(claudeDir, "*.jsonl"))
	if err != nil {
		return nil, seen
	}

	type fileEntry struct {
		path  string
		mtime int64
		size  int64
		modT  time.Time
	}

	// First pass: stat files and check cache hits
	var hits []Session
	var misses []fileEntry
	for _, path := range matches {
		seen[path] = true
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		mtime := info.ModTime().UnixNano()
		size := info.Size()
		if s, ok := cache.lookup(path, mtime, size); ok {
			hits = append(hits, s)
		} else {
			misses = append(misses, fileEntry{path: path, mtime: mtime, size: size, modT: info.ModTime()})
		}
	}

	// Scan cache misses in parallel
	scanned := make([]Session, len(misses))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i, fe := range misses {
		wg.Add(1)
		go func(i int, fe fileEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			sessionID := trimJSONLExt(filepath.Base(fe.path))
			meta := scanJSONLMeta(fe.path)
			s := Session{
				SessionID:   sessionID,
				FullPath:    fe.path,
				FirstPrompt: meta.firstPrompt,
				MsgCount:    meta.msgCount,
				Created:     meta.created,
				Modified:    fe.modT,
				GitBranch:   meta.gitBranch,
				ProjectPath: meta.cwd,
				Name:        meta.agentName,
			}
			scanned[i] = s
			cache.store(fe.path, fe.mtime, fe.size, s)
		}(i, fe)
	}
	wg.Wait()

	sessions := make([]Session, 0, len(hits)+len(scanned))
	sessions = append(sessions, hits...)
	sessions = append(sessions, scanned...)
	return sessions, seen
}

func trimJSONLExt(name string) string {
	return strings.TrimSuffix(name, ".jsonl")
}

type jsonlMeta struct {
	firstPrompt string
	msgCount    int
	created     time.Time
	gitBranch   string
	cwd         string
	agentName   string
}

// ScanSessionName extracts the native agent name from a JSONL transcript file.
// Returns empty string if no name is found.
func ScanSessionName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var name string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		var msg map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if t, _ := msg["type"].(string); t == "agent-name" {
			if n, ok := msg["agentName"].(string); ok && n != "" {
				name = n
			}
		}
	}
	return name
}

// scanJSONLMeta does a single pass over a JSONL file extracting key metadata
func scanJSONLMeta(path string) jsonlMeta {
	f, err := os.Open(path)
	if err != nil {
		return jsonlMeta{}
	}
	defer f.Close()

	var meta jsonlMeta
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	for scanner.Scan() {
		var msg map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		msgType, _ := msg["type"].(string)

		// Extract first timestamp
		if meta.created.IsZero() {
			if ts, ok := msg["timestamp"].(string); ok && ts != "" {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					meta.created = t
				}
			}
			// Also check nested snapshot timestamp
			if snap, ok := msg["snapshot"].(map[string]interface{}); ok {
				if ts, ok := snap["timestamp"].(string); ok && ts != "" {
					if t, err := time.Parse(time.RFC3339, ts); err == nil {
						meta.created = t
					}
				}
			}
		}

		// Extract git branch and cwd from entries that carry them
		if branch, ok := msg["gitBranch"].(string); ok && branch != "" && meta.gitBranch == "" {
			meta.gitBranch = branch
		}
		if cwd, ok := msg["cwd"].(string); ok && cwd != "" && meta.cwd == "" {
			meta.cwd = cwd
		}

		// Extract native session name
		if msgType == "agent-name" {
			if name, ok := msg["agentName"].(string); ok && name != "" {
				meta.agentName = name
			}
		}

		// Count user/assistant messages, excluding tool-only turns
		if msgType == "user" && !hasToolResult(msg) {
			meta.msgCount++
		}
		if msgType == "assistant" && hasText(msg) {
			meta.msgCount++
		}

		// Extract first user prompt
		if msgType == "user" && meta.firstPrompt == "" {
			meta.firstPrompt = extractTextFromMessage(msg)
		}
	}

	if meta.created.IsZero() {
		info, err := os.Stat(path)
		if err == nil {
			meta.created = info.ModTime()
		}
	}

	return meta
}

// extractTextFromMessage pulls the text content from a user/assistant message object
func extractTextFromMessage(msg map[string]interface{}) string {
	message, ok := msg["message"].(map[string]interface{})
	if !ok {
		return ""
	}
	content := message["content"]
	switch v := content.(type) {
	case string:
		return truncate(v, 200)
	case []interface{}:
		for _, block := range v {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if b["type"] == "text" {
				if text, ok := b["text"].(string); ok {
					return truncate(text, 200)
				}
			}
		}
	}
	return ""
}

// ReadTranscript extracts human-readable messages from a session JSONL file
func ReadTranscript(path string) ([]TranscriptEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []TranscriptEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var msg map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}

		msgType, _ := msg["type"].(string)
		switch msgType {
		case "user":
			if hasToolResult(msg) {
				continue // skip all tool-result turns
			}
			text, _ := extractMessageText(msg)
			if text != "" {
				entries = append(entries, TranscriptEntry{Role: "user", Text: text})
			}
		case "assistant":
			text, tools := extractMessageText(msg)
			if text != "" {
				entries = append(entries, TranscriptEntry{Role: "assistant", Text: text})
			}
			if tools != "" {
				entries = append(entries, TranscriptEntry{Role: "assistant", Text: tools, Tool: true})
			}
		}
	}

	return entries, scanner.Err()
}

type TranscriptEntry struct {
	Role string
	Text string
	Tool bool // true if this entry is a tool-use summary line
}

func hasToolResult(msg map[string]interface{}) bool {
	message, _ := msg["message"].(map[string]interface{})
	blocks, _ := message["content"].([]interface{})
	for _, block := range blocks {
		b, _ := block.(map[string]interface{})
		if b["type"] == "tool_result" {
			return true
		}
	}
	return false
}

// hasText reports whether a user/assistant message has any non-empty text block.
// Plain string content also counts as text.
func hasText(msg map[string]interface{}) bool {
	message, _ := msg["message"].(map[string]interface{})
	switch content := message["content"].(type) {
	case string:
		return strings.TrimSpace(content) != ""
	case []interface{}:
		for _, block := range content {
			b, _ := block.(map[string]interface{})
			if b["type"] != "text" {
				continue
			}
			if text, _ := b["text"].(string); strings.TrimSpace(text) != "" {
				return true
			}
		}
	}
	return false
}

// extractMessageText returns (prose, toolSummary).
// Tool results are dropped entirely.
func extractMessageText(msg map[string]interface{}) (string, string) {
	message, ok := msg["message"].(map[string]interface{})
	if !ok {
		return "", ""
	}
	content := message["content"]
	switch v := content.(type) {
	case string:
		return v, ""
	case []interface{}:
		var textParts []string
		var toolParts []string
		for _, block := range v {
			b, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			switch b["type"] {
			case "text":
				if text, ok := b["text"].(string); ok {
					textParts = append(textParts, text)
				}
			case "tool_use":
				toolParts = append(toolParts, formatToolUse(b))
			// tool_result: intentionally dropped
			}
		}
		return strings.Join(textParts, "\n"), strings.Join(toolParts, "\n")
	}
	return "", ""
}

func formatToolUse(b map[string]interface{}) string {
	name, _ := b["name"].(string)
	input, _ := b["input"].(map[string]interface{})

	switch name {
	case "Read":
		fp, _ := input["file_path"].(string)
		fp = shortenPath(fp)
		if offset, ok := input["offset"].(float64); ok {
			if limit, ok := input["limit"].(float64); ok {
				return fmt.Sprintf("Read %s:%d-%d", fp, int(offset), int(offset+limit))
			}
			return fmt.Sprintf("Read %s:%d", fp, int(offset))
		}
		return fmt.Sprintf("Read %s", fp)

	case "Edit":
		fp, _ := input["file_path"].(string)
		return fmt.Sprintf("Edit %s", shortenPath(fp))

	case "Write":
		fp, _ := input["file_path"].(string)
		return fmt.Sprintf("Write %s", shortenPath(fp))

	case "Grep":
		pattern, _ := input["pattern"].(string)
		if len(pattern) > 40 {
			pattern = pattern[:40] + "..."
		}
		return fmt.Sprintf("Grep %q", pattern)

	case "Glob":
		pattern, _ := input["pattern"].(string)
		return fmt.Sprintf("Glob %s", pattern)

	case "Bash":
		cmd, _ := input["command"].(string)
		if len(cmd) > 60 {
			cmd = cmd[:60] + "..."
		}
		return fmt.Sprintf("$ %s", cmd)

	case "Agent":
		desc, _ := input["description"].(string)
		if desc == "" {
			desc, _ = input["prompt"].(string)
			if len(desc) > 50 {
				desc = desc[:50] + "..."
			}
		}
		return fmt.Sprintf("Agent: %s", desc)

	default:
		return name
	}
}

func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ApplyStashNames stamps stash names and the Stashed flag onto sessions.
func ApplyStashNames(sessions []Session, nameMap map[string]string, sourceMap map[string]string) {
	for i := range sessions {
		if name, ok := nameMap[sessions[i].SessionID]; ok {
			sessions[i].Name = name
			sessions[i].Stashed = true
		}
		if src, ok := sourceMap[sessions[i].SessionID]; ok && sessions[i].Source == "" {
			sessions[i].Source = src
		}
	}
}

// Title returns the best display title for a session
func (s *Session) Title() string {
	if s.Name != "" {
		return s.Name
	}
	if s.Summary != "" {
		return s.Summary
	}
	if s.FirstPrompt != "" {
		return truncate(s.FirstPrompt, 80)
	}
	return s.SessionID[:8]
}

// HasName returns true if the session has a name
func (s *Session) HasName() bool {
	return s.Name != ""
}

// ShortProject returns a shortened project path for display
func (s *Session) ShortProject() string {
	home, _ := os.UserHomeDir()
	p := s.ProjectPath
	if strings.HasPrefix(p, home) {
		p = "~" + p[len(home):]
	}
	return p
}
