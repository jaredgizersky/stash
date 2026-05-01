package claude

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestClaudeCodeVersionedTranscriptSupport(t *testing.T) {
	tests := []struct {
		name            string
		fixture         string
		wantFirstPrompt string
		wantMsgCount    int
		wantCreated     string
		wantGitBranch   string
		wantCWD         string
		wantAgentName   string
		wantTranscript  []TranscriptEntry
	}{
		{
			name:            "Claude Code 2.1.108 JSONL",
			fixture:         "claude-code-2.1.108.jsonl",
			wantFirstPrompt: "Please inspect the parser fixtures.",
			wantMsgCount:    2,
			wantCreated:     "2026-04-14T20:30:46Z",
			wantGitBranch:   "parser-fixtures",
			wantCWD:         "/tmp/stash-fixture",
			wantAgentName:   "Parser Fixture",
			wantTranscript: []TranscriptEntry{
				{Role: "user", Text: "Please inspect the parser fixtures."},
				{Role: "assistant", Text: "I will check the fixtures."},
				{Role: "assistant", Text: "$ go test ./internal/claude", Tool: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("testdata", tt.fixture)

			meta := scanJSONLMeta(path)
			if meta.firstPrompt != tt.wantFirstPrompt {
				t.Fatalf("first prompt = %q, want %q", meta.firstPrompt, tt.wantFirstPrompt)
			}
			if meta.msgCount != tt.wantMsgCount {
				t.Fatalf("message count = %d, want %d", meta.msgCount, tt.wantMsgCount)
			}
			wantCreated, err := time.Parse(time.RFC3339, tt.wantCreated)
			if err != nil {
				t.Fatal(err)
			}
			if !meta.created.Equal(wantCreated) {
				t.Fatalf("created = %s, want %s", meta.created, wantCreated)
			}
			if meta.gitBranch != tt.wantGitBranch {
				t.Fatalf("git branch = %q, want %q", meta.gitBranch, tt.wantGitBranch)
			}
			if meta.cwd != tt.wantCWD {
				t.Fatalf("cwd = %q, want %q", meta.cwd, tt.wantCWD)
			}
			if meta.agentName != tt.wantAgentName {
				t.Fatalf("agent name = %q, want %q", meta.agentName, tt.wantAgentName)
			}

			gotTranscript, err := ReadTranscript(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotTranscript, tt.wantTranscript) {
				t.Fatalf("transcript = %#v, want %#v", gotTranscript, tt.wantTranscript)
			}
		})
	}
}

func TestSessionTitleIsSingleLine(t *testing.T) {
	s := Session{
		Name:        "UserPromptSubmit hook (failed)\nerror: hook returned invalid user prompt",
		Summary:     "ignored\nsummary",
		FirstPrompt: "ignored\nprompt",
	}

	got := s.Title()
	want := "UserPromptSubmit hook (failed) error: hook returned invalid user prompt"
	if got != want {
		t.Fatalf("Title() = %q, want %q", got, want)
	}
}

func TestActiveSessionDisplayNameIsSingleLine(t *testing.T) {
	a := ActiveSession{Name: "working title\nwith\tspacing"}

	got := a.DisplayName()
	want := "working title with spacing"
	if got != want {
		t.Fatalf("DisplayName() = %q, want %q", got, want)
	}
}
