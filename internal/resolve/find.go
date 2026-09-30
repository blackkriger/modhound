package resolve

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/blackkriger/modhound/internal/curseforge"
	"github.com/blackkriger/modhound/internal/gtnh"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/modrinth"
)

type Offer struct {
	ModID  string  `json:"modId"`
	Name   string  `json:"name"`
	Source string  `json:"source"`
	Target *Target `json:"target"`
}

func (p *Pack) Find(ctx context.Context, modID string, opts Options) (*Offer, error) {
	want := normName(modID)
	if want == "" {
		return nil, nil
	}
	var cf *curseforge.Client
	if opts.CurseForgeKey != "" {
		cf = &curseforge.Client{Key: opts.CurseForgeKey}
	}
	var errs []error
	if p.MCVersion == "1.7.10" {
		catalog, err := p.gtnhCatalog(ctx, opts.CacheDir)
		if err != nil {
			errs = append(errs, err)
		} else if o := findGTNH(ctx, catalog, cf, modID, want); o != nil {
			return o, nil
		}
	}
	if o, err := p.findModrinth(ctx, modID, want); o != nil {
		return o, nil
	} else if err != nil {
		errs = append(errs, err)
	}
	if cf != nil {
		if o, err := p.findCurseForge(ctx, cf, modID, want); o != nil {
			return o, nil
		} else if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	logx.Printf("find %s: not found", modID)
	return nil, nil
}

func (p *Pack) gtnhCatalog(ctx context.Context, cacheDir string) (*gtnh.Catalog, error) {
	p.mu.Lock()
	catalog := p.catalog
	p.mu.Unlock()
	if catalog != nil {
		return catalog, nil
	}
	catalog, err := gtnh.Load(ctx, cacheDir)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.catalog = catalog
	p.mu.Unlock()
	return catalog, nil
}

func findGTNH(ctx context.Context, catalog *gtnh.Catalog, cf *curseforge.Client, modID, want string) *Offer {
	mods := catalog.Mods()
	for i := range mods {
		mod := &mods[i]
		_, v := latestGTNH(mod, false)
		if v == nil {
			_, v = latestGTNH(mod, true)
		}
		if v == nil || v.Filename == "" || normName(mod.Name) != want && !fileNamed(v.Filename, want) {
			continue
		}
		t := &Target{Version: v.Tag, FileName: v.Filename, Date: dateOnly(v.TaggedAt), PageURL: first(mod.URL(), v.BrowserDownloadURL)}
		switch mod.Source {
		case gtnh.SourceCurse:
			if cf == nil || v.CurseFile == nil {
				continue
			}
			modNo, err1 := strconv.Atoi(v.CurseFile.ProjectNo)
			fileNo, err2 := strconv.Atoi(v.CurseFile.FileNo)
			if err1 != nil || err2 != nil {
				continue
			}
			f, err := cf.File(ctx, modNo, fileNo)
			if err != nil {
				logx.Printf("find %s: curseforge file %d: %v", modID, fileNo, err)
				continue
			}
			t.DownloadURL, t.SHA1 = f.DownloadURL, f.SHA1()
		case gtnh.SourceOther:
			continue
		default:
			t.DownloadURL = v.BrowserDownloadURL
		}
		if t.DownloadURL == "" {
			continue
		}
		logx.Printf("find %s: gtnh %s %s", modID, mod.Name, v.Tag)
		return &Offer{ModID: modID, Name: mod.Name, Source: SourceGTNH, Target: t}
	}
	return nil
}

func fileNamed(fileName, want string) bool {
	tokens := stemTokens(fileName)
	return len(tokens) > 0 && normName(tokens[0]) == want
}

func (p *Pack) findModrinth(ctx context.Context, modID, want string) (*Offer, error) {
	hits, err := modrinth.Search(ctx, modID, p.Loader, p.MCVersion)
	if err != nil {
		return nil, err
	}
	for _, h := range hits {
		if normName(h.Slug) != want && normName(h.Title) != want {
			continue
		}
		versions, err := modrinth.Versions(ctx, h.ProjectID, p.Loader, p.MCVersion)
		if err != nil {
			return nil, err
		}
		var best *modrinth.Version
		for i := range versions {
			v := &versions[i]
			if v.PrimaryFile() == nil {
				continue
			}
			if best == nil || releaseRank(v.VersionType) < releaseRank(best.VersionType) ||
				releaseRank(v.VersionType) == releaseRank(best.VersionType) && v.DatePublished.After(best.DatePublished) {
				best = v
			}
		}
		if best == nil {
			continue
		}
		f := best.PrimaryFile()
		logx.Printf("find %s: modrinth %s %s", modID, h.Slug, best.VersionNumber)
		return &Offer{ModID: modID, Name: h.Title, Source: SourceModrinth, Target: &Target{
			Version:     best.VersionNumber,
			FileName:    f.Filename,
			Date:        best.DatePublished.Format(time.DateOnly),
			PageURL:     modrinth.ProjectURL(h.Slug),
			DownloadURL: f.URL,
			SHA1:        f.Hashes.SHA1,
			SHA512:      f.Hashes.SHA512,
		}}, nil
	}
	return nil, nil
}

func (p *Pack) findCurseForge(ctx context.Context, cf *curseforge.Client, modID, want string) (*Offer, error) {
	mods, err := cf.Search(ctx, modID, p.MCVersion)
	if err != nil {
		return nil, err
	}
	for _, mod := range mods {
		if normName(mod.Slug) != want && normName(mod.Name) != want {
			continue
		}
		files, err := cf.Files(ctx, mod.ID, p.MCVersion)
		if err != nil {
			return nil, err
		}
		var best *curseforge.File
		for i := range files {
			f := &files[i]
			if !f.IsAvailable || f.DownloadURL == "" || !slices.Contains(f.GameVersions, p.MCVersion) ||
				slices.ContainsFunc(f.GameVersions, func(g string) bool { return slices.Contains(foreignLoaders, g) }) {
				continue
			}
			if best == nil || f.ReleaseType < best.ReleaseType || f.ReleaseType == best.ReleaseType && f.FileDate.After(best.FileDate) {
				best = f
			}
		}
		if best == nil {
			continue
		}
		logx.Printf("find %s: curseforge %s %s", modID, mod.Slug, best.FileName)
		return &Offer{ModID: modID, Name: mod.Name, Source: SourceCurseForge, Target: &Target{
			Version:     first(FileVersion(best.FileName, p.MCVersion), best.DisplayName),
			FileName:    best.FileName,
			Date:        best.FileDate.Format(time.DateOnly),
			PageURL:     curseforge.FilePageURL(mod.Slug, best.ID),
			DownloadURL: best.DownloadURL,
			SHA1:        best.SHA1(),
		}}, nil
	}
	return nil, nil
}
