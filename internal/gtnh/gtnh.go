package gtnh

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/blackkriger/modhound/internal/httpx"
	"github.com/blackkriger/modhound/internal/logx"
)

const (
	assetsURL = "https://raw.githubusercontent.com/GTNewHorizons/DreamAssemblerXXL/master/gtnh-assets.json"

	SourceCurse = "curse"
	SourceOther = "other"
)

type CurseFile struct {
	FileNo    string `json:"file_no"`
	ProjectNo string `json:"project_no"`
}

type Version struct {
	Tag                string     `json:"version_tag"`
	Filename           string     `json:"filename"`
	BrowserDownloadURL string     `json:"browser_download_url"`
	Prerelease         bool       `json:"prerelease"`
	TaggedAt           string     `json:"tagged_at"`
	Changelog          string     `json:"changelog"`
	CurseFile          *CurseFile `json:"curse_file"`
}

type Mod struct {
	Name          string    `json:"name"`
	LatestVersion string    `json:"latest_version"`
	RepoURL       string    `json:"repo_url"`
	ExternalURL   string    `json:"external_url"`
	Source        string    `json:"source"`
	Versions      []Version `json:"versions"`
}

func (m *Mod) URL() string {
	if m.RepoURL != "" {
		return m.RepoURL
	}
	return m.ExternalURL
}

func (m *Mod) Version(tag string) (int, *Version) {
	for i := range m.Versions {
		if m.Versions[i].Tag == tag {
			return i, &m.Versions[i]
		}
	}
	return -1, nil
}

func (m *Mod) latestTaggedAt() string {
	if _, v := m.Version(m.LatestVersion); v != nil {
		return v.TaggedAt
	}
	return ""
}

type Hit struct {
	Mod   *Mod
	Index int
}

type Catalog struct {
	mods       []Mod
	byFilename map[string]Hit
}

func (c *Catalog) Lookup(filename string) (Hit, bool) {
	h, ok := c.byFilename[filename]
	return h, ok
}

func Load(ctx context.Context, cacheDir string) (*Catalog, error) {
	cachePath := filepath.Join(cacheDir, "gtnh-assets.json")
	raw, err := cachedGet(ctx, assetsURL, cachePath)
	if err != nil {
		return nil, err
	}
	var assets struct {
		Mods []Mod `json:"mods"`
	}
	if err := json.Unmarshal(raw, &assets); err != nil {
		os.Remove(cachePath)
		os.Remove(cachePath + ".etag")
		return nil, err
	}
	c := &Catalog{mods: assets.Mods, byFilename: map[string]Hit{}}
	for i := range c.mods {
		m := &c.mods[i]
		for j, v := range m.Versions {
			if v.Filename == "" {
				continue
			}
			if prev, ok := c.byFilename[v.Filename]; ok && dateKey(prev.Mod.latestTaggedAt()) >= dateKey(m.latestTaggedAt()) {
				continue
			}
			c.byFilename[v.Filename] = Hit{Mod: m, Index: j}
		}
	}
	logx.Printf("gtnh catalog: %d mods, %d file names", len(c.mods), len(c.byFilename))
	return c, nil
}

func dateKey(s string) string {
	if len(s) > 19 {
		return s[:19]
	}
	return s
}

func cachedGet(ctx context.Context, url, cachePath string) ([]byte, error) {
	etagPath := cachePath + ".etag"
	cached, cacheErr := os.ReadFile(cachePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	if cacheErr == nil {
		if etag, err := os.ReadFile(etagPath); err == nil {
			req.Header.Set("If-None-Match", strings.TrimSpace(string(etag)))
		}
	}
	fallback := func(err error) ([]byte, error) {
		logx.Printf("gtnh manifest download failed: %v, cache available %v", err, cacheErr == nil)
		if cacheErr == nil {
			return cached, nil
		}
		return nil, err
	}
	resp, err := httpx.Client.Do(req)
	if err != nil {
		return fallback(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && cacheErr == nil {
		logx.Printf("gtnh manifest not modified, using cache (%d bytes)", len(cached))
		return cached, nil
	}
	if resp.StatusCode != http.StatusOK {
		return fallback(&httpx.StatusError{URL: url, Status: resp.StatusCode})
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fallback(err)
	}
	if !json.Valid(data) {
		return fallback(errors.New("GTNH manifest is not valid JSON"))
	}
	logx.Printf("gtnh manifest downloaded, %d bytes", len(data))
	if err := writeAtomic(cachePath, data); err == nil {
		if etag := resp.Header.Get("ETag"); etag != "" {
			writeAtomic(etagPath, []byte(etag))
		} else {
			os.Remove(etagPath)
		}
	} else {
		os.Remove(etagPath)
	}
	return data, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
