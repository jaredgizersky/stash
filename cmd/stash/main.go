package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jaredgizersky/stash/internal/claude"
	"github.com/jaredgizersky/stash/internal/codex"
	"github.com/jaredgizersky/stash/internal/store"
	"github.com/jaredgizersky/stash/internal/tui"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "stash",
		Short: "Manage and stash Claude sessions",
		Long:  "A TUI for browsing, naming, and resuming Claude Code sessions.",
		RunE:  runTUI,
	}

	rootCmd.Flags().BoolP("all", "a", false, "Show sessions from all projects")

	saveCmd := &cobra.Command{
		Use:   "save <name>",
		Short: "Stash the current Claude session with a name",
		Long:  "Run inside a Claude session via !stash save \"name\" to tag and save the current session.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSave,
	}

	hookCmd := &cobra.Command{
		Use:    "hook",
		Short:  "UserPromptSubmit hook handler (called by Claude, not directly)",
		Hidden: true,
		RunE:   runHook,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List stashed sessions (non-interactive)",
		RunE:  runList,
	}
	listCmd.Flags().BoolP("all", "a", false, "Show sessions from all projects")

	rootCmd.AddCommand(saveCmd, hookCmd, listCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	showAll, _ := cmd.Flags().GetBool("all")
	cwd, _ := os.Getwd()

	stashIdx, err := store.Load()
	if err != nil {
		return fmt.Errorf("loading stash index: %w", err)
	}

	sessions, err := claude.LoadAllSessions()
	if err != nil {
		return fmt.Errorf("loading sessions: %w", err)
	}
	for i := range sessions {
		if sessions[i].Source == "" {
			sessions[i].Source = "claude"
		}
	}

	codexSessions, _ := codex.LoadAllSessions()
	sessions = append(sessions, codexSessions...)

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})

	active, err := claude.LoadActiveSessions()
	if err != nil {
		return fmt.Errorf("loading active sessions: %w", err)
	}

	if len(sessions) == 0 && len(active) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	sessionID, resumeCwd, source, err := tui.Run(sessions, active, stashIdx, cwd, showAll)
	if err != nil {
		return err
	}

	if sessionID != "" {
		if source == "codex" {
			fmt.Printf("Resuming Codex session %s...\n", sessionID[:8])
		} else {
			fmt.Printf("Resuming session %s...\n", sessionID[:8])
		}
		return tui.Resume(sessionID, resumeCwd, source)
	}

	return nil
}

func runSave(cmd *cobra.Command, args []string) error {
	name := args[0]

	sessionID := os.Getenv("CLAUDE_SESSION_ID")
	if sessionID == "" {
		return fmt.Errorf("$CLAUDE_SESSION_ID not set — run this inside a Claude session via: !stash save \"%s\"", name)
	}

	cwd, _ := os.Getwd()

	branch := ""
	sessions, _ := claude.LoadSessionsForProject(cwd)
	for _, s := range sessions {
		if s.SessionID == sessionID {
			branch = s.GitBranch
			break
		}
	}

	stashIdx, err := store.Load()
	if err != nil {
		return fmt.Errorf("loading stash index: %w", err)
	}

	stashIdx.Add(store.StashEntry{
		SessionID:   sessionID,
		Name:        name,
		ProjectPath: cwd,
		GitBranch:   branch,
	})

	if err := store.Save(stashIdx); err != nil {
		return fmt.Errorf("saving stash index: %w", err)
	}

	fmt.Printf("Stashed session as \"%s\" (%s)\n", name, sessionID[:8])
	fmt.Println("Resume later with: stash (TUI) or claude --resume", sessionID[:8])

	return nil
}

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Prompt         string `json:"prompt"`
}

type hookOutput struct {
	Decision string         `json:"decision,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	Output   hookSpecOutput `json:"hookSpecificOutput,omitempty"`
}

type hookSpecOutput struct {
	HookEventName     string `json:"hookEventName"`
	SessionTitle      string `json:"sessionTitle,omitempty"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

func runHook(cmd *cobra.Command, args []string) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}

	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return err
	}

	prompt := strings.TrimSpace(input.Prompt)
	promptLower := strings.ToLower(prompt)

	// Only intercept "stash" or "stash <name>"
	if promptLower != "stash" && !strings.HasPrefix(promptLower, "stash ") {
		return nil // pass through silently
	}

	isCodex := detectSource(input.TranscriptPath) == "codex"

	var name string
	if strings.HasPrefix(promptLower, "stash ") {
		name = strings.TrimSpace(prompt[6:]) // strip "stash "
	}

	// Bare "stash" with no name — use the session's existing name
	if name == "" {
		if isCodex {
			name = codex.LookupThreadName(input.SessionID)
		} else {
			name = claude.ScanSessionName(input.TranscriptPath)
		}
		if name == "" {
			out := hookOutput{
				Decision: "block",
				Reason:   "Session has no name. Use \"stash <name>\" to give it one.",
				Output: hookSpecOutput{
					HookEventName: "UserPromptSubmit",
				},
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}
	}

	// Write to stash index
	source := "claude"
	if isCodex {
		source = "codex"
	}
	stashIdx, _ := store.Load()
	stashIdx.Add(store.StashEntry{
		SessionID:   input.SessionID,
		Name:        name,
		ProjectPath: input.Cwd,
		Source:      source,
	})
	_ = store.Save(stashIdx)

	if isCodex {
		codex.AppendThreadName(input.SessionID, name)
		codex.UpdateThreadTitle(input.SessionID, name)
	}

	// Output the hook response
	reason := fmt.Sprintf("Session stashed as \"%s\". Exiting...", name)
	var out hookOutput
	if isCodex {
		out = hookOutput{
			Decision: "block",
			Reason:   reason,
			Output: hookSpecOutput{
				HookEventName: "UserPromptSubmit",
			},
		}
	} else {
		out = hookOutput{
			Decision: "block",
			Reason:   reason,
			Output: hookSpecOutput{
				HookEventName: "UserPromptSubmit",
				SessionTitle:  name,
			},
		}
	}

	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(out); err != nil {
		return err
	}

	// Schedule a delayed SIGINT to exit the session
	var pid int
	if isCodex {
		pid = os.Getppid()
	} else {
		active, _ := claude.LoadActiveSessions()
		for _, a := range active {
			if a.SessionID == input.SessionID {
				pid = a.PID
				break
			}
		}
	}
	if pid > 0 {
		exec.Command("sh", "-c", fmt.Sprintf("sleep 0.5 && kill -INT %d 2>/dev/null", pid)).Start()
	}

	return nil
}

func detectSource(transcriptPath string) string {
	home, _ := os.UserHomeDir()
	codexPrefix := filepath.Join(home, ".codex")
	if strings.HasPrefix(transcriptPath, codexPrefix) {
		return "codex"
	}
	return "claude"
}

func runList(cmd *cobra.Command, args []string) error {
	showAll, _ := cmd.Flags().GetBool("all")
	cwd, _ := os.Getwd()

	stashIdx, err := store.Load()
	if err != nil {
		return fmt.Errorf("loading stash index: %w", err)
	}
	var sessions []claude.Session
	if showAll {
		sessions, err = claude.LoadAllSessions()
	} else {
		sessions, err = claude.LoadSessionsForProject(cwd)
	}
	if err != nil {
		return fmt.Errorf("loading sessions: %w", err)
	}
	for i := range sessions {
		if sessions[i].Source == "" {
			sessions[i].Source = "claude"
		}
	}

	codexSessions, _ := codex.LoadAllSessions()
	if showAll {
		sessions = append(sessions, codexSessions...)
	} else {
		for _, s := range codexSessions {
			if s.ProjectPath == cwd {
				sessions = append(sessions, s)
			}
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Modified.After(sessions[j].Modified)
	})

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	claude.ApplyStashNames(sessions, stashIdx.NameMap(), stashIdx.SourceMap())

	fmt.Printf("%-12s  %-6s  %-50s  %5s  %s\n", "DATE", "SRC", "TITLE", "MSGS", "BRANCH")
	fmt.Println(repeatStr("─", 106))

	for _, s := range sessions {
		src := s.Source
		if src == "" {
			src = "claude"
		}
		title := s.Title()
		if len(title) > 50 {
			title = title[:49] + "…"
		}
		branch := s.GitBranch
		if len(branch) > 30 {
			branch = branch[:29] + "…"
		}
		date := s.Modified.Format("Jan 02 15:04")
		msgs := fmt.Sprintf("%d", s.MsgCount)
		fmt.Printf("%-12s  %-6s  %-50s  %5s  %s\n", date, src, title, msgs, branch)
	}

	return nil
}

func repeatStr(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
