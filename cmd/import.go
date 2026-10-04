package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/harness"
	"github.com/batrashubham/claudectl/internal/session"
	"github.com/batrashubham/claudectl/internal/template"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	importProject string
	importResume  bool
)

var importCmd = &cobra.Command{
	Use:   "import [file]",
	Short: "Import a Claude Code session from a .jsonl file",
	Long: `Import a session from an exported .jsonl session file.
Enables team session sharing — one person exports, another imports.

The file is copied into your local Claude projects directory with a new
session UUID so it doesn't conflict with the original.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("file argument is required")
		}

		srcPath := args[0]
		if !strings.HasSuffix(srcPath, ".jsonl") {
			return fmt.Errorf("expected a .jsonl file, got: %s", srcPath)
		}

		if importProject == "" {
			return fmt.Errorf("--project is required")
		}
		project, err := filepath.Abs(config.ExpandHome(importProject))
		if err != nil {
			return fmt.Errorf("resolve project path: %w", err)
		}

		srcFile, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("open source file: %w", err)
		}
		defer srcFile.Close()

		// Extract old session ID from filename (UUID before .jsonl)
		base := filepath.Base(srcPath)
		oldID := strings.TrimSuffix(base, ".jsonl")

		// Generate new session ID
		newID := uuid.New().String()

		// Encode project path to directory name
		projectDir := harness.EncodeClaudeProject(project)

		// Create destination directory
		destDir := filepath.Join(cfg.ClaudeDir, "projects", projectDir)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("create project dir: %w", err)
		}

		destPath := filepath.Join(destDir, newID+".jsonl")
		destFile, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("create destination file: %w", err)
		}
		defer destFile.Close()

		lineCount, err := template.RewriteSessionID(srcFile, destFile, oldID, newID)
		if err != nil {
			os.Remove(destPath)
			return fmt.Errorf("rewrite session: %w", err)
		}

		fmt.Printf("Imported session %s (%d lines)\n", newID, lineCount)
		fmt.Printf("  Source: %s\n", srcPath)
		fmt.Printf("  Project: %s\n", project)
		fmt.Printf("  Stored at: %s\n", destPath)

		if importResume {
			fmt.Println("  Resuming...")
			claude, err := env.Harness("claude")
			if err != nil {
				return err
			}
			return session.Exec(env.Bin(claude), []string{"--resume", newID}, project)
		}

		fmt.Printf("  Resume with: claude --resume %s\n", newID)
		return nil
	},
}

func init() {
	importCmd.Flags().StringVarP(&importProject, "project", "p", "", "Project path (required)")
	importCmd.Flags().BoolVarP(&importResume, "resume", "r", false, "Immediately resume after import")

	rootCmd.AddCommand(importCmd)
}
