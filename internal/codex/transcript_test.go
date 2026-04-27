package codex

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jaredgizersky/stash/internal/claude"
)

func TestCodexCLIVersionedTranscriptSupport(t *testing.T) {
	tests := []struct {
		name           string
		fixture        string
		wantTranscript []claude.TranscriptEntry
	}{
		{
			name:    "Codex CLI 0.107.0 response_item JSONL",
			fixture: "codex-cli-0.107.0-response-item.jsonl",
			wantTranscript: []claude.TranscriptEntry{
				{Role: "user", Text: "Please inspect the parser fixtures."},
				{Role: "assistant", Text: "I will check the fixtures."},
				{Role: "assistant", Text: "$ go test ./internal/codex", Tool: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadTranscript(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.wantTranscript) {
				t.Fatalf("transcript = %#v, want %#v", got, tt.wantTranscript)
			}
		})
	}
}
