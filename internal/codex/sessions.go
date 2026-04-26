package codex

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jared/stash/internal/claude"
	_ "modernc.org/sqlite"
)

func codexDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func dbPath() string {
	return filepath.Join(codexDir(), "state_5.sqlite")
}

// LoadAllSessions reads Codex sessions from the SQLite state database.
// Returns nil, nil if Codex is not installed or the DB doesn't exist.
func LoadAllSessions() ([]claude.Session, error) {
	path := dbPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, rollout_path, title, first_user_message, cwd,
		       created_at, updated_at, git_branch, model,
		       COALESCE(tokens_used, 0)
		FROM threads
		WHERE source IN ('cli', 'exec', 'vscode')
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var sessions []claude.Session
	for rows.Next() {
		var (
			id, rolloutPath, title, firstMsg, cwd string
			createdAt, updatedAt                  int64
			gitBranch, model                      sql.NullString
			tokens                                int
		)
		if err := rows.Scan(&id, &rolloutPath, &title, &firstMsg, &cwd,
			&createdAt, &updatedAt, &gitBranch, &model, &tokens); err != nil {
			continue
		}

		name := ""
		if title != firstMsg && title != "" {
			name = title
		}

		sessions = append(sessions, claude.Session{
			SessionID:   id,
			FullPath:    rolloutPath,
			FirstPrompt: truncate(firstMsg, 200),
			MsgCount:    0,
			Created:     time.Unix(createdAt, 0),
			Modified:    time.Unix(updatedAt, 0),
			GitBranch:   gitBranch.String,
			ProjectPath: cwd,
			Source:      "codex",
			Model:       model.String,
			Name:        name,
		})
	}

	// Parallel scan for message counts, cached by mtime+size
	cache := loadMsgCache()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for i := range sessions {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			path := sessions[i].FullPath
			info, err := os.Stat(path)
			if err != nil {
				return
			}
			mtime := info.ModTime().UnixNano()
			size := info.Size()

			if count, ok := cache.lookup(path, mtime, size); ok {
				sessions[i].MsgCount = count
				return
			}

			count := countMessages(path)
			sessions[i].MsgCount = count
			cache.store(path, mtime, size, count)
		}(i)
	}
	wg.Wait()
	saveMsgCache(cache)

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})

	return sessions, nil
}

// LoadSessionsForProject returns Codex sessions matching a specific cwd.
func LoadSessionsForProject(projectDir string) ([]claude.Session, error) {
	all, err := LoadAllSessions()
	if err != nil {
		return nil, err
	}
	var filtered []claude.Session
	for _, s := range all {
		if s.ProjectPath == projectDir {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
