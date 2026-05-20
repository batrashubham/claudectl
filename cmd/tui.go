package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/session"
	"github.com/batrashubham/claudectl/internal/template"
	"github.com/batrashubham/claudectl/internal/tui"
)

func runTUI() error {
	builder := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir)
	sessions, err := builder.Build()
	if err != nil {
		return fmt.Errorf("build index: %w", err)
	}

	model := tui.NewModel(cfg, sessions)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	// Check post-TUI actions
	if m, ok := finalModel.(tui.Model); ok {
		// Resume a session
		if resumeID := m.ResumeID(); resumeID != "" {
			var target *index.SessionMeta
			for i := range sessions {
				if sessions[i].ID == resumeID {
					target = &sessions[i]
					break
				}
			}
			if target == nil {
				fmt.Fprintf(os.Stderr, "session %s not found\n", resumeID)
				os.Exit(1)
			}

			locator := session.NewLocator(cfg.ClaudeDir, cfg.BackupDir)
			loc := locator.Locate(target.ID, target.ProjectDir)
			if loc.ActivePath == "" && loc.ArchivedPath == "" {
				fmt.Fprintf(os.Stderr, "Cannot resume: session file was deleted before backup. Only history metadata remains.\n")
				os.Exit(1)
			}
			return locator.Resume(target.ID, target.ProjectDir, target.Project)
		}

		// Spawn from template
		if tmplName := m.SpawnTemplate(); tmplName != "" {
			cwd, _ := os.Getwd()
			projectDir := strings.ReplaceAll(cwd, "/", "-")

			store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
			result, err := store.Spawn(projectDir, tmplName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "spawn failed: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Spawned session %s from template '%s'\n", result.SessionID[:12], tmplName)
			claudeBin, err := exec.LookPath("claude")
			if err != nil {
				fmt.Fprintf(os.Stderr, "claude not found in PATH\n")
				os.Exit(1)
			}
			if result.Project != "" {
				if _, err := os.Stat(result.Project); err == nil {
					os.Chdir(result.Project)
				}
			}
			return syscall.Exec(claudeBin, []string{"claude", "--resume", result.SessionID}, os.Environ())
		}

		// Rewarm template (spawn + rewarm prompt)
		if tmplName := m.RewarmTemplate(); tmplName != "" {
			cwd, _ := os.Getwd()
			projectDir := strings.ReplaceAll(cwd, "/", "-")

			store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
			result, err := store.Spawn(projectDir, tmplName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "rewarm failed: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Rewarming template '%s' → session %s\n", tmplName, result.SessionID[:12])
			fmt.Printf("When done, save back: claudectl template save %s --name %s --force --trim\n", result.SessionID, tmplName)
			claudeBin, err := exec.LookPath("claude")
			if err != nil {
				fmt.Fprintf(os.Stderr, "claude not found in PATH\n")
				os.Exit(1)
			}
			if result.Project != "" {
				if _, err := os.Stat(result.Project); err == nil {
					os.Chdir(result.Project)
				}
			}
			rewarmPrompt := "The codebase has evolved since your last exploration. Please re-read the project structure, check for new/changed files, and update your understanding of the architecture, patterns, and key decisions. Focus on what has changed rather than re-reading everything."
			return syscall.Exec(claudeBin, []string{"claude", "--resume", result.SessionID, "-p", rewarmPrompt}, os.Environ())
		}
	}

	return nil
}
