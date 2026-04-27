package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDangerouslySkipPermissionsFromConfig(t *testing.T) {
	unsetDangerouslySkipPermissionsEnv(t)
	t.Setenv("HOME", t.TempDir())

	configPath := Path()
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("dangerously_skip_permissions = true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := DangerouslySkipPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("DangerouslySkipPermissions() = false, want true")
	}
}

func TestDangerouslySkipPermissionsEnvOverridesConfig(t *testing.T) {
	t.Setenv(DangerouslySkipPermissionsEnv, "false")
	t.Setenv("HOME", t.TempDir())

	configPath := Path()
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("dangerously_skip_permissions = true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := DangerouslySkipPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("DangerouslySkipPermissions() = true, want false")
	}
}

func unsetDangerouslySkipPermissionsEnv(t *testing.T) {
	t.Helper()

	oldValue, hadValue := os.LookupEnv(DangerouslySkipPermissionsEnv)
	if err := os.Unsetenv(DangerouslySkipPermissionsEnv); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv(DangerouslySkipPermissionsEnv, oldValue)
		} else {
			_ = os.Unsetenv(DangerouslySkipPermissionsEnv)
		}
	})
}
