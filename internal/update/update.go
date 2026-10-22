package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repo = "batrashubham/claudectl"

var (
	apiBase      = "https://api.github.com"
	downloadBase = "https://github.com"
	client       = &http.Client{Timeout: 60 * time.Second}
)

// Latest returns the newest release tag, e.g. "v0.10.0".
func Latest() (string, error) {
	resp, err := client.Get(apiBase + "/repos/" + repo + "/releases/latest")
	if err != nil {
		return "", fmt.Errorf("check latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("check latest release: github returned %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil || rel.TagName == "" {
		return "", fmt.Errorf("check latest release: unexpected response")
	}
	return rel.TagName, nil
}

// ErrPermission means the binary's directory isn't writable by this user.
var ErrPermission = errors.New("permission denied")

// Install downloads the release archive for this OS/arch, verifies it against
// the release checksums, and atomically replaces the binary at target.
func Install(tag, target string) error {
	archive := fmt.Sprintf("claudectl_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("%s/%s/releases/download/%s/", downloadBase, repo, tag)

	sums, err := fetch(base + fmt.Sprintf("claudectl_%s_checksums.txt", strings.TrimPrefix(tag, "v")))
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, archive)
	if err != nil {
		return err
	}
	data, err := fetch(base + archive)
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s", archive)
	}

	bin, err := extractBinary(data)
	if err != nil {
		return err
	}
	return replace(target, bin)
}

func fetch(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[1] == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in release", name)
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("claudectl binary not found in archive")
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "claudectl" {
			return io.ReadAll(tr)
		}
	}
}

// replace writes bin next to target and renames it over, so a failed update
// never leaves a half-written binary.
func replace(target string, bin []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".claudectl-update-*")
	if err != nil {
		if os.IsPermission(err) {
			return ErrPermission
		}
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		if os.IsPermission(err) {
			return ErrPermission
		}
		return err
	}
	return nil
}
