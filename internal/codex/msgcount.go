package codex

import (
	"bufio"
	"encoding/json"
	"os"
)

func countMessages(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	for scanner.Scan() {
		var raw struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Role string `json:"role"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &raw) != nil {
			continue
		}
		if raw.Type != "response_item" {
			continue
		}
		if raw.Payload.Type == "message" && (raw.Payload.Role == "user" || raw.Payload.Role == "assistant") {
			count++
		}
	}
	return count
}
