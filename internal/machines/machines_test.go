package machines

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/batrashubham/claudectl/internal/config"
	"github.com/batrashubham/claudectl/internal/index"
)

const sid = "11111111-1111-1111-1111-111111111111"

func fixture(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg := &config.Config{ClaudeDir: filepath.Join(tmp, "claude"), BackupDir: filepath.Join(tmp, "backup"), MachineName: "home"}
	p := filepath.Join(cfg.BackupDir, "machines", "home", "projects", "-p", sid+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte(`{"cwd":"/p"}`+"\n"), 0644)
	os.MkdirAll(filepath.Join(cfg.BackupDir, "machines", "work"), 0755)
	return cfg
}

func sessions(t *testing.T, cfg *config.Config) []index.SessionMeta {
	t.Helper()
	s, err := index.NewBuilder(cfg.ClaudeDir, cfg.BackupDir, cfg.MachineName).Build()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNamesIncludesUnsyncedThisMachine(t *testing.T) {
	cfg := fixture(t)
	cfg.MachineName = "new-box"
	if got := strings.Join(Names(cfg), ","); got != "home,new-box,work" {
		t.Errorf("got %s", got)
	}
}

func TestSummarize(t *testing.T) {
	cfg := fixture(t)
	sum := Summarize(cfg, sessions(t, cfg))
	if len(sum) != 2 || sum[0].Name != "home" || !sum[0].This || sum[0].Sessions != 1 || sum[0].Bytes == 0 || sum[1].Sessions != 0 {
		t.Errorf("got %+v", sum)
	}
}

func TestCopy(t *testing.T) {
	cfg := fixture(t)
	s := sessions(t, cfg)[0]

	if _, err := Copy(cfg, s, "home", ""); err == nil {
		t.Error("copy to own machine without new path should fail")
	}
	out, err := Copy(cfg, s, "laptop", "")
	if err != nil || !out.NewMachine || out.Skipped || out.Pushed {
		t.Fatalf("out %+v err %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.BackupDir, "machines", "laptop", "projects", "-p", sid+".jsonl")); err != nil {
		t.Error("not copied")
	}
	if out, _ := Copy(cfg, s, "laptop", ""); !out.Skipped || out.NewMachine {
		t.Errorf("second copy: %+v", out)
	}
	if _, err := Copy(cfg, s, "Bad Name", ""); err == nil {
		t.Error("invalid machine name accepted")
	}
}

func TestRename(t *testing.T) {
	cfg := fixture(t)
	if err := Rename(cfg, "work"); err == nil {
		t.Error("rename onto existing machine should fail")
	}
	if err := Rename(cfg, "laptop"); err != nil {
		t.Fatal(err)
	}
	if cfg.MachineName != "laptop" {
		t.Error("config not updated")
	}
	if _, err := os.Stat(filepath.Join(cfg.BackupDir, "machines", "laptop", "projects", "-p", sid+".jsonl")); err != nil {
		t.Error("folder not moved")
	}
	if saved, _ := config.Load(); saved.MachineName != "laptop" {
		t.Errorf("saved %q", saved.MachineName)
	}
}
