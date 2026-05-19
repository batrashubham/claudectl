package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/template"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage session templates (starter sessions)",
	Long: `Save warm sessions as templates and spawn new sessions from them.

Skip the warm-up phase — start new sessions with full project context
already loaded from a previous exploration.`,
}

// === SAVE ===

var (
	saveName        string
	saveDescription string
	saveTrim        bool
	saveForce       bool
)

var templateSaveCmd = &cobra.Command{
	Use:   "save <session-id>",
	Short: "Save a session as a named template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]

		if saveName == "" {
			return fmt.Errorf("--name is required")
		}

		builder := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir)
		sessions, err := builder.Build()
		if err != nil {
			return err
		}

		var target *index.SessionMeta
		for i := range sessions {
			if sessions[i].ID == sessionID {
				target = &sessions[i]
				break
			}
		}
		if target == nil {
			return fmt.Errorf("session %s not found", sessionID)
		}

		store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
		err = store.Save(template.SaveOptions{
			SessionID:   target.ID,
			ProjectDir:  target.ProjectDir,
			Project:     target.Project,
			Name:        saveName,
			Description: saveDescription,
			Trim:        saveTrim,
			Force:       saveForce,
		})
		if err != nil {
			return err
		}

		fmt.Printf("✓ Template '%s' saved from session %s\n", saveName, sessionID[:12])
		if saveTrim {
			fmt.Println("  (trimmed non-essential entries)")
		}
		fmt.Printf("  Project: %s\n", target.Project)
		fmt.Printf("  Stored at: %s\n", filepath.Join(cfg.TemplatesDir, target.ProjectDir, saveName))
		return nil
	},
}

// === SPAWN ===

var spawnResume bool

var templateSpawnCmd = &cobra.Command{
	Use:   "spawn <template-name>",
	Short: "Create a new session from a template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		projectDir := currentProjectDir()
		if projectDir == "" {
			return fmt.Errorf("could not determine current project — run from a project directory")
		}

		store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
		result, err := store.Spawn(projectDir, name)
		if err != nil {
			return err
		}

		fmt.Printf("✓ Spawned new session %s from template '%s'\n", result.SessionID[:12], name)

		if spawnResume {
			fmt.Println("  Resuming...")
			claudeBin, err := exec.LookPath("claude")
			if err != nil {
				return fmt.Errorf("claude CLI not found: %w", err)
			}
			if result.Project != "" {
				if _, err := os.Stat(result.Project); err == nil {
					os.Chdir(result.Project)
				}
			}
			return syscall.Exec(claudeBin, []string{"claude", "--resume", result.SessionID}, os.Environ())
		}

		fmt.Printf("  Resume with: claude --resume %s\n", result.SessionID)
		return nil
	},
}

// === LIST ===

var templateListJSON bool

var templateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)

		projectDir := currentProjectDir()
		var templates []template.Meta
		var err error

		if projectDir != "" {
			templates, err = store.List(projectDir)
		} else {
			templates, err = store.ListAll()
		}
		if err != nil {
			return err
		}

		if len(templates) == 0 {
			fmt.Println("No templates found.")
			if projectDir != "" {
				fmt.Printf("  (looking in project: %s)\n", projectDir)
			}
			fmt.Println("  Save one with: claudectl template save <session-id> --name <name>")
			return nil
		}

		if templateListJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(templates)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "NAME\tPROJECT\tENTRIES\tSIZE\tCREATED\tDESCRIPTION\n")
		for _, t := range templates {
			project := filepath.Base(t.SourceProject)
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
				t.Name, project, t.EntryCount,
				humanize.Bytes(uint64(t.SizeBytes)),
				humanize.Time(t.CreatedAt), t.Description)
		}
		return w.Flush()
	},
}

// === DELETE ===

var templateDeleteCmd = &cobra.Command{
	Use:   "delete <template-name>",
	Short: "Delete a template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		projectDir := currentProjectDir()
		if projectDir == "" {
			return fmt.Errorf("could not determine current project")
		}

		store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
		if err := store.Delete(projectDir, name); err != nil {
			return fmt.Errorf("template '%s' not found", name)
		}

		fmt.Printf("✓ Template '%s' deleted\n", name)
		return nil
	},
}

// === INSPECT ===

var templateInspectCmd = &cobra.Command{
	Use:   "inspect <template-name>",
	Short: "Show template details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		projectDir := currentProjectDir()
		if projectDir == "" {
			return fmt.Errorf("could not determine current project")
		}

		store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
		meta, err := store.ReadMeta(projectDir, name)
		if err != nil {
			return fmt.Errorf("template '%s' not found", name)
		}

		fmt.Printf("Name:         %s\n", meta.Name)
		fmt.Printf("Description:  %s\n", meta.Description)
		fmt.Printf("Project:      %s\n", meta.SourceProject)
		fmt.Printf("Source ID:    %s\n", meta.SourceSessionID)
		fmt.Printf("Created:      %s (%s)\n", meta.CreatedAt.Format("2006-01-02 15:04"), humanize.Time(meta.CreatedAt))
		fmt.Printf("Entries:      %d\n", meta.EntryCount)
		fmt.Printf("Size:         %s\n", humanize.Bytes(uint64(meta.SizeBytes)))
		fmt.Printf("Trimmed:      %v\n", meta.Trimmed)
		fmt.Printf("Subagents:    %v\n", meta.HasSubagents)
		return nil
	},
}

// === UPDATE ===

var templateUpdateCmd = &cobra.Command{
	Use:   "update <template-name> <session-id>",
	Short: "Update a template with a newer session (re-warm)",
	Long: `Replace an existing template with a new session's content.

Use this when your codebase has evolved and the template's context
is stale. The template keeps its name and description but gets
fresh content from the specified session.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		sessionID := args[1]

		projectDir := currentProjectDir()
		if projectDir == "" {
			return fmt.Errorf("could not determine current project")
		}

		store := template.NewStore(cfg.TemplatesDir, cfg.ClaudeDir)
		meta, err := store.ReadMeta(projectDir, name)
		if err != nil {
			return fmt.Errorf("template '%s' not found", name)
		}

		// Resolve session
		builder := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir)
		sessions, err := builder.Build()
		if err != nil {
			return err
		}

		var target *index.SessionMeta
		for i := range sessions {
			if sessions[i].ID == sessionID {
				target = &sessions[i]
				break
			}
		}
		if target == nil {
			return fmt.Errorf("session %s not found", sessionID)
		}

		// Re-save with existing name and description, force overwrite
		err = store.Save(template.SaveOptions{
			SessionID:   target.ID,
			ProjectDir:  target.ProjectDir,
			Project:     target.Project,
			Name:        name,
			Description: meta.Description,
			Trim:        true,
			Force:       true,
		})
		if err != nil {
			return err
		}

		fmt.Printf("✓ Template '%s' updated from session %s\n", name, sessionID[:12])
		fmt.Printf("  Previous source: %s\n", meta.SourceSessionID[:12])
		fmt.Printf("  New source:      %s\n", target.ID[:12])
		return nil
	},
}

func init() {
	templateSaveCmd.Flags().StringVar(&saveName, "name", "", "Template name (required)")
	templateSaveCmd.Flags().StringVar(&saveDescription, "description", "", "Template description")
	templateSaveCmd.Flags().BoolVar(&saveTrim, "trim", false, "Strip non-essential entries")
	templateSaveCmd.Flags().BoolVar(&saveForce, "force", false, "Overwrite existing template")

	templateSpawnCmd.Flags().BoolVar(&spawnResume, "resume", false, "Immediately resume the spawned session")

	templateListCmd.Flags().BoolVar(&templateListJSON, "json", false, "Output as JSON")

	templateCmd.AddCommand(templateSaveCmd)
	templateCmd.AddCommand(templateSpawnCmd)
	templateCmd.AddCommand(templateListCmd)
	templateCmd.AddCommand(templateDeleteCmd)
	templateCmd.AddCommand(templateInspectCmd)
	templateCmd.AddCommand(templateUpdateCmd)

	rootCmd.AddCommand(templateCmd)
}

func currentProjectDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(cwd, "/", "-")
}
