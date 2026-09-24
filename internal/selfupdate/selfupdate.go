package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/blackkriger/modhound/internal/httpx"
	"github.com/blackkriger/modhound/internal/logx"
)

const (
	releaseAPI = "https://api.github.com/repos/blackkriger/modhound/releases/latest"
	assetName  = "modhound.exe"
)

type Release struct {
	Version string `json:"version"`
	URL     string `json:"-"`
	Sum     string `json:"-"`
}

var sumPattern = regexp.MustCompile(`\b[0-9a-fA-F]{64}\b`)

func Latest(ctx context.Context, current string) (*Release, error) {
	if current == "" || current == "dev" {
		return nil, nil
	}
	var body struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := httpx.Do(ctx, http.MethodGet, releaseAPI, map[string]string{"Accept": "application/vnd.github+json"}, nil, &body); err != nil {
		return nil, err
	}
	rel := &Release{Version: strings.TrimPrefix(strings.TrimSpace(body.TagName), "v")}
	logx.Printf("selfupdate: running %s, latest release %s", current, rel.Version)
	if !Newer(rel.Version, current) {
		return nil, nil
	}
	rel.Sum = strings.ToLower(sumPattern.FindString(body.Body))
	for _, a := range body.Assets {
		if a.Name == assetName {
			rel.URL = a.URL
		}
	}
	if rel.URL == "" {
		return nil, fmt.Errorf("release %q has no %s", body.TagName, assetName)
	}
	if rel.Sum == "" {
		return nil, fmt.Errorf("release %q publishes no sha256 for %s", body.TagName, assetName)
	}
	return rel, nil
}

func Install(ctx context.Context, rel *Release) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	tmp := exe + ".new"
	sum, n, err := download(ctx, rel.URL, tmp)
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("download: %w", err)
	}
	if sum != rel.Sum {
		os.Remove(tmp)
		return fmt.Errorf("checksum mismatch after %d bytes: got %s, want %s", n, sum, rel.Sum)
	}
	logx.Printf("selfupdate: downloaded %d bytes, checksum matches the release notes", n)
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("move running binary aside: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Rename(old, exe)
		os.Remove(tmp)
		return fmt.Errorf("install new binary: %w", err)
	}
	return nil
}

func Relaunch() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func CleanOld() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	old := exe + ".old"
	if _, err := os.Stat(old); err != nil {
		return
	}
	if err := os.Remove(old); err != nil {
		logx.Printf("selfupdate: leftover %s could not be removed: %v", filepath.Base(old), err)
	}
}

func download(ctx context.Context, url, dest string) (string, int64, error) {
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	counter := &countingWriter{}
	if err := httpx.Download(ctx, url, io.MultiWriter(f, h, counter), nil); err != nil {
		f.Close()
		return "", counter.n, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", counter.n, err
	}
	if err := f.Close(); err != nil {
		return "", counter.n, err
	}
	return hex.EncodeToString(h.Sum(nil)), counter.n, nil
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(b []byte) (int, error) {
	c.n += int64(len(b))
	return len(b), nil
}

func Newer(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		x, y := field(as, i), field(bs, i)
		if x != y {
			return x > y
		}
	}
	return false
}

func field(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimFunc(parts[i], func(r rune) bool { return r < '0' || r > '9' }))
	if err != nil {
		return 0
	}
	return n
}
