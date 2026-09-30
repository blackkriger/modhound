package compat

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/blackkriger/modhound/internal/httpx"
)

const (
	jvmdgMaven   = "https://maven.wagyourtail.xyz/releases/xyz/wagyourtail/jvmdowngrader/jvmdowngrader-java-api/"
	jvmdgTimeout = time.Minute
	jvmdgRetry   = 10 * time.Minute
)

var jvmdgVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)

type JvmdgSource struct {
	PackRoot string
	CacheDir string

	mu    sync.Mutex
	known map[string]*jvmdgEntry
}

type jvmdgEntry struct {
	done    chan struct{}
	classes map[string]bool
	err     error
	at      time.Time
}

func (s *JvmdgSource) Classes(ctx context.Context, version string) (map[string]bool, error) {
	if !jvmdgVersion.MatchString(version) || strings.Contains(version, "..") {
		return nil, fmt.Errorf("unexpected JvmDowngrader version %q", version)
	}
	s.mu.Lock()
	if s.known == nil {
		s.known = map[string]*jvmdgEntry{}
	}
	e := s.known[version]
	if e != nil && e.failed() && time.Since(e.at) > jvmdgRetry {
		e = nil
	}
	if e == nil {
		e = &jvmdgEntry{done: make(chan struct{})}
		s.known[version] = e
		s.mu.Unlock()
		loadCtx, cancel := context.WithTimeout(ctx, jvmdgTimeout)
		e.classes, e.err = s.load(loadCtx, version)
		cancel()
		e.at = time.Now()
		if errors.Is(e.err, context.Canceled) {
			s.mu.Lock()
			delete(s.known, version)
			s.mu.Unlock()
		}
		close(e.done)
	} else {
		s.mu.Unlock()
	}
	select {
	case <-e.done:
		return e.classes, e.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *jvmdgEntry) failed() bool {
	select {
	case <-e.done:
		return e.err != nil
	default:
		return false
	}
}

func (s *JvmdgSource) load(ctx context.Context, version string) (map[string]bool, error) {
	path, err := s.jar(ctx, version)
	if err != nil {
		return nil, err
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	classes := map[string]bool{}
	for _, f := range z.File {
		if name, ok := strings.CutSuffix(f.Name, ".class"); ok {
			classes[name] = true
		}
	}
	return classes, nil
}

func (s *JvmdgSource) jar(ctx context.Context, version string) (string, error) {
	name := fmt.Sprintf("jvmdowngrader-java-api-%s-downgraded-8.jar", version)
	if s.PackRoot != "" {
		loaded := filepath.Join(s.PackRoot, "falsepattern", "xyz.wagyourtail.jvmdowngrader-"+name)
		if _, err := os.Stat(loaded); err == nil {
			return loaded, nil
		}
	}
	if s.CacheDir == "" {
		return "", errors.New("no cache folder for the JvmDowngrader runtime")
	}
	cached := filepath.Join(s.CacheDir, "compat", name)
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	}
	url := jvmdgMaven + version + "/" + name
	var sum bytes.Buffer
	if err := httpx.Download(ctx, url+".sha1", &sum, nil); err != nil {
		return "", err
	}
	var data bytes.Buffer
	if err := httpx.Download(ctx, url, &data, nil); err != nil {
		return "", err
	}
	got := sha1.Sum(data.Bytes())
	want := strings.Fields(sum.String())
	if len(want) == 0 || !strings.EqualFold(want[0], hex.EncodeToString(got[:])) {
		return "", fmt.Errorf("%s: checksum mismatch", name)
	}
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		return "", err
	}
	tmp := cached + ".tmp"
	if err := os.WriteFile(tmp, data.Bytes(), 0o644); err != nil {
		return "", err
	}
	return cached, os.Rename(tmp, cached)
}
