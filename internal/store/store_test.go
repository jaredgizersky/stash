package store

import "testing"

func TestStashIndexAddReplaceRemove(t *testing.T) {
	idx := &StashIndex{}

	idx.Add(StashEntry{
		SessionID:   "session-1",
		Name:        "first name",
		ProjectPath: "/tmp/one",
		GitBranch:   "main",
		Source:      "claude",
	})
	idx.Add(StashEntry{
		SessionID:   "session-1",
		Name:        "renamed",
		ProjectPath: "/tmp/two",
		GitBranch:   "feature",
		Source:      "codex",
	})

	if len(idx.Entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(idx.Entries))
	}
	got := idx.FindBySessionID("session-1")
	if got == nil {
		t.Fatal("FindBySessionID returned nil")
	}
	if got.Name != "renamed" || got.ProjectPath != "/tmp/two" || got.GitBranch != "feature" || got.Source != "codex" {
		t.Fatalf("entry = %#v, want replaced entry", got)
	}
	if idx.NameMap()["session-1"] != "renamed" {
		t.Fatalf("NameMap = %#v, want renamed entry", idx.NameMap())
	}
	if idx.SourceMap()["session-1"] != "codex" {
		t.Fatalf("SourceMap = %#v, want codex source", idx.SourceMap())
	}

	idx.Remove("session-1")
	if len(idx.Entries) != 0 {
		t.Fatalf("entry count after remove = %d, want 0", len(idx.Entries))
	}
}
