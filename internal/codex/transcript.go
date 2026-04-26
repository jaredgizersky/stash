package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jared/stash/internal/claude"
)

// ReadTranscript extracts human-readable messages from a Codex session JSONL file.
// Maps to the same TranscriptEntry type used by Claude transcripts.
func ReadTranscript(path string) ([]claude.TranscriptEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []claude.TranscriptEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var raw map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			continue
		}

		typ, _ := raw["type"].(string)
		if typ != "response_item" {
			continue
		}

		payload, ok := raw["payload"].(map[string]interface{})
		if !ok {
			continue
		}

		itemType, _ := payload["type"].(string)

		switch itemType {
		case "message":
			role, _ := payload["role"].(string)
			text := extractMessageContent(payload)
			if text == "" {
				continue
			}
			switch role {
			case "user":
				if looksLikeSystemMessage(text) {
					continue
				}
				entries = append(entries, claude.TranscriptEntry{Role: "user", Text: text})
			case "assistant":
				entries = append(entries, claude.TranscriptEntry{Role: "assistant", Text: text})
			}

		case "function_call":
			tool := formatFunctionCall(payload)
			if tool != "" {
				entries = append(entries, claude.TranscriptEntry{Role: "assistant", Text: tool, Tool: true})
			}
		}
	}

	return entries, scanner.Err()
}

func extractMessageContent(payload map[string]interface{}) string {
	content, ok := payload["content"].([]interface{})
	if !ok {
		return ""
	}

	var parts []string
	for _, block := range content {
		b, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		blockType, _ := b["type"].(string)
		if blockType == "input_text" || blockType == "output_text" {
			if text, ok := b["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func looksLikeSystemMessage(text string) bool {
	return strings.HasPrefix(text, "# AGENTS.md") ||
		strings.HasPrefix(text, "# System") ||
		strings.HasPrefix(text, "<system")
}

func formatFunctionCall(payload map[string]interface{}) string {
	name, _ := payload["name"].(string)
	argsStr, _ := payload["arguments"].(string)

	switch name {
	case "exec_command":
		var args struct {
			Cmd     string `json:"cmd"`
			Workdir string `json:"workdir"`
		}
		if json.Unmarshal([]byte(argsStr), &args) == nil && args.Cmd != "" {
			cmd := args.Cmd
			if len(cmd) > 80 {
				cmd = cmd[:80] + "..."
			}
			return fmt.Sprintf("$ %s", cmd)
		}
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsStr), &args) == nil {
			return fmt.Sprintf("Read %s", shortenPath(args.Path))
		}
	case "write_file":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsStr), &args) == nil {
			return fmt.Sprintf("Write %s", shortenPath(args.Path))
		}
	case "patch_file", "apply_patch":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsStr), &args) == nil {
			return fmt.Sprintf("Patch %s", shortenPath(args.Path))
		}
	default:
		if name != "" {
			return name
		}
	}
	return ""
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
