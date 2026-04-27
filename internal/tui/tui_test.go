package tui

import (
	"testing"

	"github.com/jaredgizersky/stash/internal/config"
)

func TestShouldDangerouslySkipPermissionsFromEnv(t *testing.T) {
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
			t.Setenv(config.DangerouslySkipPermissionsEnv, tt.value)
			if got := shouldDangerouslySkipPermissions(); got != tt.want {
				t.Fatalf("shouldDangerouslySkipPermissions() = %v, want %v", got, tt.want)
			}
		})
	}
}
