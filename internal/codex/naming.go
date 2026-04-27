package codex

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

type sessionIndexEntry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
	UpdatedAt  string `json:"updated_at"`
}

func sessionIndexPath() string {
	return codexDir() + "/session_index.jsonl"
}

// LookupThreadName finds a session's name from ~/.codex/session_index.jsonl.
// Scans from the end so the latest rename wins.
func LookupThreadName(sessionID string) string {
	f, err := os.Open(sessionIndexPath())
	if err != nil {
		return ""
	}
	defer f.Close()

	var name string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry sessionIndexEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.ID == sessionID {
			name = entry.ThreadName
		}
	}
	return name
}

// AppendThreadName writes a name entry to ~/.codex/session_index.jsonl.
func AppendThreadName(sessionID, name string) error {
	entry := sessionIndexEntry{
		ID:         sessionID,
		ThreadName: name,
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(sessionIndexPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%s\n", data)
	return err
}

// UpdateThreadTitle updates the title column in the SQLite threads table.
func UpdateThreadTitle(sessionID, title string) error {
	path := dbPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec("UPDATE threads SET title = ? WHERE id = ?", title, sessionID)
	return err
}
