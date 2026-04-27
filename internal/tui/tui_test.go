package tui

import "testing"

func TestShouldSkipClaudeResumePermissions(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "unset", value: "", want: false},
		{name: "one", value: "1", want: true},
		{name: "true", value: "true", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "on", value: "on", want: true},
		{name: "false", value: "false", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(claudeResumeSkipPermissionsEnv, tt.value)
			if got := shouldSkipClaudeResumePermissions(); got != tt.want {
				t.Fatalf("shouldSkipClaudeResumePermissions() = %v, want %v", got, tt.want)
			}
		})
	}
}
