package search

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
	"github.com/batrashubham/claudectl/internal/session"
)

func metaText(s index.SessionMeta) string {
	parts := []string{s.Info.CustomTitle, s.Info.AITitle, s.Info.Branch, s.Info.Worktree, s.Info.Agent, s.Machine}
	for _, pr := range s.Info.PRs {
		parts = append(parts, pr.URL, fmt.Sprintf("%s#%d", pr.Repo, pr.Number))
	}
	return strings.Join(parts, " ")
}

func CacheDir() string {
	return filepath.Join(filepath.Dir(config.ConfigPath()), "cache", "search")
}

// BuildForSessions indexes each session's transcript, preferring the live
// copy over the backup. Ghost sessions are searchable by their prompts only.
func BuildForSessions(cfg *config.Config, sessions []index.SessionMeta) (*Index, error) {
	locator := session.NewLocator(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName)
	docs := make([]Doc, 0, len(sessions))
	for _, s := range sessions {
		loc := locator.Locate(s.ID, s.ProjectDir)
		path := loc.ActivePath
		if path == "" {
			path = loc.ArchivedPath
		}
		docs = append(docs, Doc{ID: s.ID, Project: s.Project, Meta: metaText(s), LastSeen: s.LastSeen, Path: path, Fallback: s.SearchText})
	}
	return Build(docs, CacheDir())
}
