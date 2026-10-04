package cmd

import (
	"fmt"

	"github.com/batrashubham/claudectl/internal/template"
	"github.com/batrashubham/claudectl/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func runTUI() error {
	sessions, err := env.Index()
	if err != nil {
		return fmt.Errorf("build index: %w", err)
	}

	model := tui.NewModel(env, sessions)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	m, ok := finalModel.(tui.Model)
	if !ok {
		return nil
	}

	if key := m.ResumeID(); key != "" {
		target, err := findSession(key)
		if err != nil {
			return err
		}
		return env.Resume(*target)
	}

	if tmplName := m.SpawnTemplate(); tmplName != "" {
		store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
		projectDir := findTemplateProjectDir(store, tmplName)
		if projectDir == "" {
			return fmt.Errorf("template '%s' not found", tmplName)
		}
		result, err := store.Spawn(projectDir, tmplName)
		if err != nil {
			return fmt.Errorf("spawn failed: %w", err)
		}
		fmt.Printf("Spawned session %s from template '%s'\n", shortID(result.SessionID), tmplName)
		return execClaude(result, "--resume", result.SessionID)
	}

	if tmplName := m.RewarmTemplate(); tmplName != "" {
		store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
		projectDir := findTemplateProjectDir(store, tmplName)
		if projectDir == "" {
			return fmt.Errorf("template '%s' not found", tmplName)
		}
		result, err := store.Spawn(projectDir, tmplName)
		if err != nil {
			return fmt.Errorf("rewarm failed: %w", err)
		}
		fmt.Printf("Rewarming template '%s' → session %s\n", tmplName, shortID(result.SessionID))
		fmt.Printf("When done, save back: claudectl template save %s --name %s --force --trim\n", result.SessionID, tmplName)
		meta, _ := store.ReadMeta(projectDir, tmplName)
		rewarmPrompt := template.DefaultRewarmPrompt
		if meta != nil && meta.RewarmPrompt != "" {
			rewarmPrompt = meta.RewarmPrompt
		}
		return execClaude(result, "--resume", result.SessionID, "-p", rewarmPrompt)
	}

	return nil
}
