package harness

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func init() {
	register("opencode", "opencode", defaultOpencodeHome, func(home string) Harness { return &Opencode{home: home, bin: "opencode"} })
}

func defaultOpencodeHome() string {
	return filepath.Join(envOr("XDG_DATA_HOME", homeDir(), ".local", "share"), "opencode")
}

// Opencode reads opencode (>= 1.2), which keeps sessions in a SQLite
// database rather than files:
//
//	<data dir>/opencode.db    tables session, message, part (JSON in "data")
//
// A live database can't be copied safely or diffed usefully in git, so the
// backup holds one JSON snapshot per session, produced by `opencode export`
// and restored with `opencode import` (which preserves every id):
//
//	sessions/<sessionID>.json   {"info": {...}, "messages": [{"info", "parts"}]}
type Opencode struct {
	home string
	bin  string
}

func (o *Opencode) Name() string          { return "opencode" }
func (o *Opencode) DisplayName() string   { return "opencode" }
func (o *Opencode) Home() string          { return o.home }
func (o *Opencode) Binary() string        { return "opencode" }
func (o *Opencode) SetBinary(bin string)  { o.bin = bin }
func (o *Opencode) SyncRoots() []SyncRoot { return nil }

const opencodeLive = "opencode.db"

// dbPath honours OPENCODE_DB: absolute, or relative to the data dir.
func (o *Opencode) dbPath(root string) string {
	if root == o.home {
		if p := os.Getenv("OPENCODE_DB"); p != "" {
			if filepath.IsAbs(p) {
				return p
			}
			return filepath.Join(root, p)
		}
	}
	return filepath.Join(root, opencodeLive)
}

func openReadOnly(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
}

type opencodeExport struct {
	Info struct {
		ID        string `json:"id"`
		ParentID  string `json:"parentID"`
		ProjectID string `json:"projectID"`
		Directory string `json:"directory"`
		Title     string `json:"title"`
		Time      struct {
			Created int64 `json:"created"`
			Updated int64 `json:"updated"`
		} `json:"time"`
	} `json:"info"`
	Messages []opencodeMessage `json:"messages"`
}

type opencodeMessage struct {
	Info struct {
		Role string `json:"role"`
		Time struct {
			Created int64 `json:"created"`
		} `json:"time"`
	} `json:"info"`
	Parts []opencodePart `json:"parts"`
}

type opencodePart struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic"`
	Tool      string `json:"tool"`
}

func (m opencodeMessage) text() string {
	var b []string
	for _, p := range m.Parts {
		if p.Type == "text" && !p.Synthetic && p.Text != "" {
			b = append(b, p.Text)
		}
	}
	return strings.Join(b, "\n")
}

func (o *Opencode) Scan(root string) ([]Session, error) {
	if exists(o.dbPath(root)) {
		return o.scanDB(root)
	}
	return o.scanSnapshots(root)
}

func (o *Opencode) scanDB(root string) ([]Session, error) {
	db, err := openReadOnly(o.dbPath(root))
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, project_id, directory, title, time_created, time_updated,
		(SELECT COALESCE(SUM(LENGTH(data)), 0) FROM part WHERE part.session_id = session.id)
		FROM session WHERE parent_id IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("read opencode sessions: %w", err)
	}
	byID := map[string]*Session{}
	var order []string
	for rows.Next() {
		var s Session
		var created, updated int64
		if err := rows.Scan(&s.ID, &s.ProjectKey, &s.Project, &s.Title, &created, &updated, &s.SizeBytes); err != nil {
			rows.Close()
			return nil, err
		}
		s.Harness = o.Name()
		s.Files = []string{opencodeLive}
		if s.SizeBytes == 0 {
			s.SizeBytes = 1 // a brand-new session is live, not a ghost
		}
		updateSpan(&s, time.UnixMilli(created))
		updateSpan(&s, time.UnixMilli(updated))
		byID[s.ID] = &s
		order = append(order, s.ID)
	}
	rows.Close()

	prompts, err := db.Query(`SELECT m.session_id, m.time_created, p.data FROM message m
		JOIN part p ON p.message_id = m.id
		WHERE json_extract(m.data, '$.role') = 'user' AND json_extract(p.data, '$.type') = 'text'
		ORDER BY m.time_created, p.id`)
	if err == nil {
		for prompts.Next() {
			var sid string
			var ts int64
			var data []byte
			if prompts.Scan(&sid, &ts, &data) != nil {
				continue
			}
			s := byID[sid]
			var p opencodePart
			if s == nil || json.Unmarshal(data, &p) != nil || p.Synthetic || isBoilerplate(p.Text) {
				continue
			}
			s.Prompts = append(s.Prompts, Prompt{Text: p.Text, Time: time.UnixMilli(ts)})
		}
		prompts.Close()
	}

	out := make([]Session, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func (o *Opencode) scanSnapshots(root string) ([]Session, error) {
	files, _ := os.ReadDir(filepath.Join(root, "sessions"))
	var out []Session
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		var exp opencodeExport
		if readJSON(filepath.Join(root, "sessions", f.Name()), &exp) != nil || exp.Info.ID == "" {
			continue
		}
		info, _ := f.Info()
		s := Session{
			Harness:    o.Name(),
			ID:         exp.Info.ID,
			Project:    exp.Info.Directory,
			ProjectKey: exp.Info.ProjectID,
			Title:      exp.Info.Title,
			Files:      []string{filepath.Join("sessions", f.Name())},
		}
		if info != nil {
			s.SizeBytes = info.Size()
		}
		updateSpan(&s, time.UnixMilli(exp.Info.Time.Created))
		updateSpan(&s, time.UnixMilli(exp.Info.Time.Updated))
		for _, m := range exp.Messages {
			if m.Info.Role == "user" {
				if text := m.text(); !isBoilerplate(text) {
					s.Prompts = append(s.Prompts, Prompt{Text: text, Time: time.UnixMilli(m.Info.Time.Created)})
				}
			}
		}
		out = append(out, s)
	}
	return out, nil
}

func (o *Opencode) Messages(root string, s Session) ([]Message, error) {
	if len(s.Files) == 0 {
		return nil, os.ErrNotExist
	}
	var msgs []opencodeMessage
	if s.Files[0] == opencodeLive {
		var err error
		if msgs, err = o.dbMessages(root, s.ID); err != nil {
			return nil, err
		}
	} else {
		var exp opencodeExport
		if err := readJSON(filepath.Join(root, s.Files[0]), &exp); err != nil {
			return nil, err
		}
		msgs = exp.Messages
	}

	var out []Message
	for _, m := range msgs {
		ts := time.UnixMilli(m.Info.Time.Created)
		text := m.text()
		switch m.Info.Role {
		case "user":
			if !isBoilerplate(text) {
				out = append(out, Message{Role: "user", Text: text, Time: ts})
			}
		case "assistant":
			if text != "" {
				out = append(out, Message{Role: "assistant", Text: text, Time: ts})
			}
		}
		for _, p := range m.Parts {
			if p.Type == "tool" && p.Tool != "" {
				out = append(out, Message{Role: "tool", Text: p.Tool, Time: ts})
			}
		}
	}
	return out, nil
}

func (o *Opencode) dbMessages(root, sessionID string) ([]opencodeMessage, error) {
	db, err := openReadOnly(o.dbPath(root))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT m.id, m.data, p.data FROM message m
		LEFT JOIN part p ON p.message_id = m.id
		WHERE m.session_id = ? ORDER BY m.time_created, m.id, p.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []opencodeMessage
	last := ""
	for rows.Next() {
		var id string
		var mdata []byte
		var pdata sql.NullString
		if err := rows.Scan(&id, &mdata, &pdata); err != nil {
			return nil, err
		}
		if id != last {
			var m opencodeMessage
			json.Unmarshal(mdata, &m.Info)
			out = append(out, m)
			last = id
		}
		if pdata.Valid {
			var p opencodePart
			if json.Unmarshal([]byte(pdata.String), &p) == nil {
				out[len(out)-1].Parts = append(out[len(out)-1].Parts, p)
			}
		}
	}
	return out, rows.Err()
}

// Export snapshots every session that changed since its last snapshot via
// `opencode export`. Each call costs a couple of seconds of CLI startup, so
// work stops at the deadline and resumes on the next sync.
func (o *Opencode) Export(dstRoot string, deadline time.Time) (int, int, error) {
	db, err := openReadOnly(o.dbPath(o.home))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	rows, err := db.Query(`SELECT id, time_updated FROM session WHERE parent_id IS NULL ORDER BY time_updated DESC`)
	if err != nil {
		db.Close()
		return 0, 0, fmt.Errorf("read opencode sessions: %w", err)
	}
	type pending struct {
		id      string
		updated int64
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.id, &p.updated) == nil {
			todo = append(todo, p)
		}
	}
	rows.Close()
	db.Close()

	var stale []pending
	for _, p := range todo {
		var exp opencodeExport
		if readJSON(filepath.Join(dstRoot, "sessions", p.id+".json"), &exp) == nil && exp.Info.Time.Updated >= p.updated {
			continue
		}
		stale = append(stale, p)
	}
	if len(stale) == 0 {
		return 0, 0, nil
	}
	if _, err := exec.LookPath(o.bin); err != nil {
		return 0, len(stale), fmt.Errorf("%s not in PATH; %d opencode sessions not backed up", o.bin, len(stale))
	}

	done := 0
	for i, p := range stale {
		if i > 0 && time.Now().After(deadline) {
			return done, len(stale) - done, nil
		}
		if err := o.exportOne(p.id, filepath.Join(dstRoot, "sessions", p.id+".json"), deadline); err != nil {
			return done, len(stale) - done, err
		}
		done++
	}
	return done, 0, nil
}

func (o *Opencode) exportOne(id, dst string, deadline time.Time) error {
	timeout := time.Until(deadline)
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, o.bin, "export", id)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("opencode export %s: %w: %s", id, err, strings.TrimSpace(stderr.String()))
	}
	var exp opencodeExport
	if err := json.Unmarshal(stdout.Bytes(), &exp); err != nil || exp.Info.ID != id {
		return fmt.Errorf("opencode export %s: unexpected output", id)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, stdout.Bytes(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// RestoreFiles is unused: opencode sessions are restored by Import.
func (o *Opencode) RestoreFiles(s Session, project string) []FileCopy { return nil }

// Import loads a snapshot with `opencode import`, pointing it at the local
// project dir first when the session was recorded under another path
// (opencode won't resume a session from outside its directory).
func (o *Opencode) Import(root string, s Session, project string) error {
	if len(s.Files) == 0 {
		return os.ErrNotExist
	}
	src := filepath.Join(root, s.Files[0])
	if project != "" && project != s.Project {
		var doc map[string]json.RawMessage
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return err
		}
		var info map[string]any
		if err := json.Unmarshal(doc["info"], &info); err != nil {
			return err
		}
		info["directory"] = project
		doc["info"], _ = json.Marshal(info)
		rewritten, _ := json.Marshal(doc)
		tmp, err := os.CreateTemp("", "claudectl-opencode-*.json")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.Write(rewritten); err != nil {
			tmp.Close()
			return err
		}
		tmp.Close()
		src = tmp.Name()
	}
	cmd := exec.Command(o.bin, "import", src)
	if project != "" && exists(project) {
		cmd.Dir = project
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("opencode import: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (o *Opencode) ResumeArgs(s Session) []string {
	return []string{"--session", s.ID}
}
