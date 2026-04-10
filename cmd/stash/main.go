package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jared/stash/internal/claude"
	"github.com/jared/stash/internal/store"
	"github.com/jared/stash/internal/tui"
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

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install the stash hook into Claude settings",
		RunE:  runInstall,
	}

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the stash hook from Claude settings",
		RunE:  runUninstall,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List stashed sessions (non-interactive)",
		RunE:  runList,
	}
	listCmd.Flags().BoolP("all", "a", false, "Show sessions from all projects")

	rootCmd.AddCommand(saveCmd, hookCmd, installCmd, uninstallCmd, listCmd)

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

	active, err := claude.LoadActiveSessions()
	if err != nil {
		return fmt.Errorf("loading active sessions: %w", err)
	}

	if len(sessions) == 0 && len(active) == 0 {
		fmt.Println("No Claude sessions found.")
		return nil
	}

	sessionID, resumeCwd, err := tui.Run(sessions, active, stashIdx, cwd, showAll)
	if err != nil {
		return err
	}

	if sessionID != "" {
		fmt.Printf("Resuming session %s...\n", sessionID[:8])
		return tui.Resume(sessionID, resumeCwd)
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

	var name string
	if strings.HasPrefix(promptLower, "stash ") {
		name = strings.TrimSpace(prompt[6:]) // strip "stash "
	}

	// Bare "stash" with no name — use the session's existing name
	if name == "" {
		meta := claude.ScanSessionName(input.TranscriptPath)
		if meta == "" {
			out := hookOutput{
				Decision: "block",
				Reason:   "Session has no name. Use \"stash <name>\" to give it one.",
				Output: hookSpecOutput{
					HookEventName: "UserPromptSubmit",
				},
			}
			return json.NewEncoder(os.Stdout).Encode(out)
		}
		name = meta
	}

	// Write to stash index
	stashIdx, _ := store.Load()
	stashIdx.Add(store.StashEntry{
		SessionID:   input.SessionID,
		Name:        name,
		ProjectPath: input.Cwd,
	})
	_ = store.Save(stashIdx)

	// Find the Claude PID for this session so we can exit it
	active, _ := claude.LoadActiveSessions()
	var claudePID int
	for _, a := range active {
		if a.SessionID == input.SessionID {
			claudePID = a.PID
			break
		}
	}

	// Output the hook response — rename the session
	out := hookOutput{
		Decision: "block",
		Reason:   fmt.Sprintf("Session stashed as \"%s\". Exiting...", name),
		Output: hookSpecOutput{
			HookEventName: "UserPromptSubmit",
			SessionTitle:  name,
		},
	}

	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(out); err != nil {
		return err
	}

	// Schedule a delayed SIGINT to exit the Claude session
	// The delay gives Claude time to process the title rename
	if claudePID > 0 {
		exec.Command("sh", "-c", fmt.Sprintf("sleep 0.5 && kill -INT %d 2>/dev/null", claudePID)).Start()
	}

	return nil
}


func runInstall(cmd *cobra.Command, args []string) error {
	// Find the stash binary path
	stashBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding stash binary: %w", err)
	}
	stashBin, _ = filepath.EvalSymlinks(stashBin)

	hookCommand := stashBin + " hook"

	// Add hook to Claude settings
	settingsPath := filepath.Join(claude.ClaudeDir(), "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("reading Claude settings: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parsing Claude settings: %w", err)
	}

	// Build hook entry in Claude's expected format:
	// {"matcher": "", "hooks": [{"type": "command", "command": "..."}]}
	hookEntry := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": hookCommand,
			},
		},
	}

	// Get or create hooks map
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	// Get or create UserPromptSubmit array
	var submitEntries []interface{}
	if existing, ok := hooks["UserPromptSubmit"].([]interface{}); ok {
		// Remove any existing stash hook entries
		for _, entry := range existing {
			em, ok := entry.(map[string]interface{})
			if !ok {
				submitEntries = append(submitEntries, entry)
				continue
			}
			// Check if any hook in this entry is a stash hook
			isStash := false
			if innerHooks, ok := em["hooks"].([]interface{}); ok {
				for _, ih := range innerHooks {
					ihm, ok := ih.(map[string]interface{})
					if !ok {
						continue
					}
					cmd, _ := ihm["command"].(string)
					if strings.Contains(cmd, "stash hook") || strings.Contains(cmd, "stash-hook") {
						isStash = true
						break
					}
				}
			}
			if !isStash {
				submitEntries = append(submitEntries, entry)
			}
		}
	}
	submitEntries = append(submitEntries, hookEntry)
	hooks["UserPromptSubmit"] = submitEntries
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("writing Claude settings: %w", err)
	}

	fmt.Println("Installed stash hook into Claude settings.")
	fmt.Println("Now type \"stash <name>\" in any Claude session to name and exit it.")
	fmt.Println("Hook command:", hookCommand)

	return nil
}

func runUninstall(cmd *cobra.Command, args []string) error {
	settingsPath := filepath.Join(claude.ClaudeDir(), "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("reading Claude settings: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parsing Claude settings: %w", err)
	}

	if hooks, ok := settings["hooks"].(map[string]interface{}); ok {
		if existing, ok := hooks["UserPromptSubmit"].([]interface{}); ok {
			var kept []interface{}
			for _, entry := range existing {
				em, ok := entry.(map[string]interface{})
				if !ok {
					kept = append(kept, entry)
					continue
				}
				isStash := false
				if innerHooks, ok := em["hooks"].([]interface{}); ok {
					for _, ih := range innerHooks {
						ihm, ok := ih.(map[string]interface{})
						if !ok {
							continue
						}
						c, _ := ihm["command"].(string)
						if strings.Contains(c, "stash hook") || strings.Contains(c, "stash-hook") {
							isStash = true
							break
						}
					}
				}
				if !isStash {
					kept = append(kept, entry)
				}
			}
			if len(kept) == 0 {
				delete(hooks, "UserPromptSubmit")
			} else {
				hooks["UserPromptSubmit"] = kept
			}
			if len(hooks) == 0 {
				delete(settings, "hooks")
			}
		}
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("writing Claude settings: %w", err)
	}

	fmt.Println("Uninstalled stash hook from Claude settings.")
	return nil
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

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	claude.ApplyStashNames(sessions, stashIdx.NameMap())

	fmt.Printf("%-12s  %-50s  %5s  %s\n", "DATE", "TITLE", "MSGS", "BRANCH")
	fmt.Println(repeatStr("─", 100))

	for _, s := range sessions {
		title := s.Title()
		if len(title) > 50 {
			title = title[:49] + "…"
		}
		branch := s.GitBranch
		if len(branch) > 30 {
			branch = branch[:29] + "…"
		}
		date := s.Modified.Format("Jan 02 15:04")
		fmt.Printf("%-12s  %-50s  %5d  %s\n", date, title, s.MsgCount, branch)
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
