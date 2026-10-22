package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.10.0", "0.9.0", true},
		{"v0.10.0", "0.10.0", false},
		{"0.10.1", "0.10.0+dirty", true},
		{"0.9.9", "0.10.0", false},
		{"1.0.0", "0.99.99", true},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestResolve_PrefersLdflag(t *testing.T) {
	if Resolve("v1.2.3") != "1.2.3" {
		t.Error("ldflag not used")
	}
}

func tarball(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	tw.Write(content)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func fakeGitHub(t *testing.T, archive []byte, sum string) {
	t.Helper()
	name := fmt.Sprintf("claudectl_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
		case strings.HasSuffix(r.URL.Path, "_checksums.txt"):
			fmt.Fprintf(w, "%s  %s\nabc  other.tar.gz\n", sum, name)
		case strings.HasSuffix(r.URL.Path, name):
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldDL := apiBase, downloadBase
	apiBase, downloadBase = srv.URL, srv.URL
	t.Cleanup(func() { apiBase, downloadBase = oldAPI, oldDL })
}

func TestInstall_ReplacesBinary(t *testing.T) {
	archive := tarball(t, "claudectl", []byte("new binary"))
	sum := sha256.Sum256(archive)
	fakeGitHub(t, archive, hex.EncodeToString(sum[:]))

	tag, err := Latest()
	if err != nil || tag != "v9.9.9" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}
	target := filepath.Join(t.TempDir(), "claudectl")
	os.WriteFile(target, []byte("old"), 0755)
	if err := Install(tag, target); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	info, _ := os.Stat(target)
	if string(got) != "new binary" || info.Mode().Perm()&0100 == 0 {
		t.Errorf("binary %q mode %v", got, info.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	if len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}

func TestInstall_ChecksumMismatchKeepsOldBinary(t *testing.T) {
	fakeGitHub(t, tarball(t, "claudectl", []byte("evil")), strings.Repeat("0", 64))
	target := filepath.Join(t.TempDir(), "claudectl")
	os.WriteFile(target, []byte("old"), 0755)
	err := Install("v9.9.9", target)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Error("binary replaced despite bad checksum")
	}
}

func TestInstall_ReadOnlyDirReportsPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	archive := tarball(t, "claudectl", []byte("new"))
	sum := sha256.Sum256(archive)
	fakeGitHub(t, archive, hex.EncodeToString(sum[:]))
	dir := t.TempDir()
	target := filepath.Join(dir, "claudectl")
	os.WriteFile(target, []byte("old"), 0755)
	os.Chmod(dir, 0555)
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	if err := Install("v9.9.9", target); err != ErrPermission {
		t.Errorf("err = %v, want ErrPermission", err)
	}
}
