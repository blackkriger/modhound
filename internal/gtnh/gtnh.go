package gtnh

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

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

func (c *Catalog) Mods() []Mod {
	return c.mods
}

func Load(ctx context.Context, cacheDir string) (*Catalog, error) {
	cachePath := filepath.Join(cacheDir, "gtnh-assets.json")
	res, err := httpx.CachedGet(ctx, assetsURL, cachePath, 0, nil, json.Valid)
	if err != nil {
		return nil, err
	}
	raw := res.Data
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
