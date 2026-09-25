package resolve

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/blackkriger/modhound/internal/curseforge"
	"github.com/blackkriger/modhound/internal/gtnh"
	"github.com/blackkriger/modhound/internal/jarinfo"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/modrinth"
)

type Choice struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	FileName  string `json:"fileName"`
	Date      string `json:"date"`
	Installed bool   `json:"installed"`
	Pre       bool   `json:"pre"`
}

type choice struct {
	target *Target
	curse  *gtnh.CurseFile
	notes  string
	cfMod  int
	cfFile int
}

func (p *Pack) find(id string) *Mod {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.Mods {
		if m.ID == id {
			return m
		}
	}
	return nil
}

func (p *Pack) Snapshot() []Mod {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Mod, len(p.Mods))
	for i, m := range p.Mods {
		out[i] = *m
	}
	return out
}

func (p *Pack) MarkInstalled(m *Mod) {
	if m == nil || m.Target == nil {
		return
	}
	path := filepath.Join(filepath.Dir(m.Path), m.Target.FileName)
	j, err := jarinfo.Read(path)
	if err != nil {
		logx.Printf("%s: cannot read the installed jar: %v", m.Target.FileName, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	m.Path, m.FileName, m.Version = path, m.Target.FileName, m.Target.Version
	m.Status, m.Reason, m.Target = StatusCurrent, "", nil
	if j == nil {
		return
	}
	m.Jar, m.Size = j, j.Size
	for _, id := range j.ModIDs {
		p.ModIDs[strings.ToLower(id)] = true
	}
}

func (p *Pack) Versions(ctx context.Context, id, curseForgeKey string) ([]Choice, error) {
	found := p.find(id)
	if found == nil {
		return nil, errors.New("mod not found")
	}
	p.mu.Lock()
	mod := *found
	p.mu.Unlock()
	m := &mod
	var out []Choice
	choices := map[string]choice{}
	add := func(c Choice, ch choice) {
		c.Installed = strings.EqualFold(c.FileName, m.FileName)
		out = append(out, c)
		choices[c.ID] = ch
	}
	switch {
	case m.gt != nil:
		mod := m.gt.Mod
		for i := len(mod.Versions) - 1; i >= 0; i-- {
			v := mod.Versions[i]
			if v.Filename == "" {
				continue
			}
			t := &Target{Version: v.Tag, FileName: v.Filename, Date: dateOnly(v.TaggedAt), PageURL: v.BrowserDownloadURL}
			ch := choice{target: t, notes: v.Changelog}
			switch mod.Source {
			case gtnh.SourceCurse:
				ch.curse = v.CurseFile
			case gtnh.SourceOther:
			default:
				t.DownloadURL = v.BrowserDownloadURL
				if mod.RepoURL != "" {
					t.PageURL = mod.RepoURL + "/releases/tag/" + v.Tag
				}
			}
			add(Choice{ID: "gtnh:" + strconv.Itoa(i), Version: v.Tag, FileName: v.Filename, Date: t.Date, Pre: v.Prerelease}, ch)
		}
	case m.cf != nil:
		if curseForgeKey == "" {
			return nil, errors.New("needs a CurseForge API key")
		}
		files, err := (&curseforge.Client{Key: curseForgeKey}).Files(ctx, m.cf.modID, p.MCVersion)
		if err != nil {
			return nil, err
		}
		slices.SortFunc(files, func(a, b curseforge.File) int { return b.FileDate.Compare(a.FileDate) })
		stem := Stem(m.cf.file.FileName)
		for _, f := range files {
			if !f.IsAvailable || Stem(f.FileName) != stem || !slices.Contains(f.GameVersions, p.MCVersion) || slices.ContainsFunc(f.GameVersions, func(g string) bool { return slices.Contains(foreignLoaders, g) }) {
				continue
			}
			t := &Target{Version: f.DisplayName, FileName: f.FileName, Date: f.FileDate.Format(time.DateOnly), PageURL: strings.TrimRight(m.URL, "/") + "/files/" + strconv.Itoa(f.ID), DownloadURL: f.DownloadURL, SHA1: f.SHA1()}
			add(Choice{ID: "cf:" + strconv.Itoa(f.ID), Version: f.DisplayName, FileName: f.FileName, Date: t.Date, Pre: f.ReleaseType != curseforge.ReleaseTypeRelease}, choice{target: t, cfMod: m.cf.modID, cfFile: f.ID})
		}
	case m.mr != nil:
		versions, err := modrinth.Versions(ctx, m.mr.version.ProjectID, p.Loader, p.MCVersion)
		if err != nil {
			return nil, err
		}
		slices.SortFunc(versions, func(a, b modrinth.Version) int { return b.DatePublished.Compare(a.DatePublished) })
		var stem string
		if f := m.mr.version.PrimaryFile(); f != nil {
			stem = Stem(f.Filename)
		}
		for _, v := range versions {
			f := v.PrimaryFile()
			if f == nil || (stem != "" && Stem(f.Filename) != stem) {
				continue
			}
			t := &Target{Version: v.VersionNumber, FileName: f.Filename, Date: v.DatePublished.Format(time.DateOnly), PageURL: "https://modrinth.com/mod/" + v.ProjectID + "/version/" + v.ID, DownloadURL: f.URL, SHA1: f.Hashes.SHA1, SHA512: f.Hashes.SHA512}
			add(Choice{ID: "mr:" + v.ID, Version: v.VersionNumber, FileName: f.Filename, Date: t.Date, Pre: v.VersionType != "release"}, choice{target: t, notes: v.Changelog})
		}
	default:
		return nil, errors.New("the mod was not found on GTNH, CurseForge or Modrinth")
	}
	p.mu.Lock()
	found.choices = choices
	p.mu.Unlock()
	return out, nil
}

func (p *Pack) Choose(ctx context.Context, id, choiceID, curseForgeKey string) (*Mod, error) {
	m := p.find(id)
	if m == nil {
		return nil, errors.New("mod not found")
	}
	p.mu.Lock()
	ch, ok := m.choices[choiceID]
	p.mu.Unlock()
	if !ok {
		return nil, errors.New("unknown version")
	}
	t := *ch.target
	if ch.curse != nil {
		if curseForgeKey == "" {
			return nil, errors.New("needs a CurseForge API key to download")
		}
		modID, err1 := strconv.Atoi(ch.curse.ProjectNo)
		fileID, err2 := strconv.Atoi(ch.curse.FileNo)
		if err1 != nil || err2 != nil {
			return nil, errors.New("you can download it from the project page")
		}
		f, err := (&curseforge.Client{Key: curseForgeKey}).File(ctx, modID, fileID)
		if err != nil {
			return nil, err
		}
		t.DownloadURL, t.SHA1 = f.DownloadURL, f.SHA1()
	}
	if t.DownloadURL == "" {
		return nil, fmt.Errorf("%s can't be downloaded through apps, you can download it from the project page", t.FileName)
	}
	p.mu.Lock()
	m.Target, m.Status, m.Reason, m.chosen = &t, StatusUpdate, "", true
	p.mu.Unlock()
	return m, nil
}

func (p *Pack) VersionNotes(ctx context.Context, id, choiceID, curseForgeKey string) (string, error) {
	m := p.find(id)
	if m == nil {
		return "", errors.New("mod not found")
	}
	p.mu.Lock()
	ch, ok := m.choices[choiceID]
	p.mu.Unlock()
	if !ok {
		return "", errors.New("unknown version")
	}
	text := ch.notes
	if text == "" && ch.cfFile != 0 && curseForgeKey != "" {
		raw, err := (&curseforge.Client{Key: curseForgeKey}).Changelog(ctx, ch.cfMod, ch.cfFile)
		if err != nil {
			return "", err
		}
		text = plainText(raw)
	}
	text = cleanNotes(text)
	if len(text) > maxNotes {
		text = text[:maxNotes] + "…"
	}
	return text, nil
}

func (m *Mod) Chosen() bool {
	return m.chosen
}
