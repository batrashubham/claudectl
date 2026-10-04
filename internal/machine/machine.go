// Package machine identifies this computer inside a shared backup and maps
// project paths recorded on one machine to their equivalent on another.
package machine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/batrashubham/claudectl/internal/config"
)

// Dir is the backup subdirectory holding one subtree per machine. Each
// machine only ever writes inside its own subtree, so pushes from several
// machines to one remote rebase cleanly.
const Dir = "machines"

const manifestFile = "machine.json"

// Legacy names the pseudo-machine that pre-multi-machine backups are
// attributed to once they have been shared through a remote, since they
// may hold any machine's sessions.
const Legacy = "legacy"

var nonSlug = regexp.MustCompile(`[^a-z0-9-]+`)

// Slug turns a hostname or user-supplied name into a stable directory name.
func Slug(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	// macOS hostnames change with the network ("mbp.local", "mbp.lan").
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	name = strings.Trim(nonSlug.ReplaceAllString(name, "-"), "-")
	if name == "" {
		return "machine"
	}
	return name
}

func ValidateName(name string) error {
	if name == "" || Slug(name) != name {
		return fmt.Errorf("invalid machine name %q: use lowercase letters, digits and '-' (e.g. %q)", name, Slug(name))
	}
	if name == Legacy {
		return fmt.Errorf("%q is reserved for backups made before multi-machine support", Legacy)
	}
	return nil
}

func namePath() string { return filepath.Join(config.Dir(), "machine") }

// LocalName returns this machine's name: the configured one if set,
// otherwise the one persisted on first use. It is persisted rather than
// re-derived because hostnames drift, and a new name would start a second
// subtree in the backup.
func LocalName(configured string) (string, error) {
	if configured != "" {
		if err := ValidateName(configured); err != nil {
			return "", err
		}
		return configured, nil
	}
	if data, err := os.ReadFile(namePath()); err == nil {
		if name := strings.TrimSpace(string(data)); name != "" {
			return name, nil
		}
	}
	host, _ := os.Hostname()
	name := Slug(host)
	if err := SetLocalName(name); err != nil {
		return "", err
	}
	return name, nil
}

func SetLocalName(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(namePath()), 0755); err != nil {
		return err
	}
	return os.WriteFile(namePath(), []byte(name+"\n"), 0644)
}

// Manifest describes a machine to the others sharing the backup. It holds
// no timestamps so it only changes, and gets committed, when facts change.
type Manifest struct {
	Name      string            `json:"name"`
	Hostname  string            `json:"hostname"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	Home      string            `json:"home"`
	Harnesses map[string]string `json:"harnesses"` // name -> data dir
}

func Local(name string, harnessHomes map[string]string) Manifest {
	host, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	return Manifest{
		Name:      name,
		Hostname:  host,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Home:      home,
		Harnesses: harnessHomes,
	}
}

// Root is where a machine's copy of one harness lives in the backup.
func Root(backupDir, machine, harness string) string {
	return filepath.Join(backupDir, Dir, machine, harness)
}

func WriteManifest(backupDir string, m Manifest) (bool, error) {
	path := filepath.Join(backupDir, Dir, m.Name, manifestFile)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, data, 0644)
}

// List returns every machine with a subtree in the backup. Machines whose
// manifest is missing (e.g. created by hand) are still listed by name.
func List(backupDir string) []Manifest {
	entries, _ := os.ReadDir(filepath.Join(backupDir, Dir))
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		m := Manifest{Name: e.Name()}
		if data, err := os.ReadFile(filepath.Join(backupDir, Dir, e.Name(), manifestFile)); err == nil {
			json.Unmarshal(data, &m)
			m.Name = e.Name()
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func Find(backupDir, name string) (Manifest, bool) {
	for _, m := range List(backupDir) {
		if m.Name == name {
			return m, true
		}
	}
	return Manifest{}, false
}

// MapPath translates a project path recorded on another machine. Explicit
// path_map rules win (longest prefix first); otherwise a path under the
// other machine's home is moved under this machine's home, which handles
// the common /Users/me -> /home/me case with no configuration.
func MapPath(path string, rules []config.PathMap, fromHome, toHome string) string {
	if path == "" {
		return path
	}
	sorted := append([]config.PathMap(nil), rules...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i].From) > len(sorted[j].From) })
	for _, r := range sorted {
		if rest, ok := underPrefix(path, r.From); ok {
			return filepath.Join(r.To, rest)
		}
	}
	if fromHome != "" && toHome != "" && fromHome != toHome {
		if rest, ok := underPrefix(path, fromHome); ok {
			return filepath.Join(toHome, rest)
		}
	}
	return path
}

func underPrefix(path, prefix string) (string, bool) {
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return "", false
	}
	if path == prefix {
		return "", true
	}
	if strings.HasPrefix(path, prefix+"/") {
		return path[len(prefix)+1:], true
	}
	return "", false
}
