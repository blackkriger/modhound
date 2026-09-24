package resolve

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blackkriger/modhound/internal/curseforge"
	"github.com/blackkriger/modhound/internal/gtnh"
	"github.com/blackkriger/modhound/internal/httpx"
	"github.com/blackkriger/modhound/internal/icons"
	"github.com/blackkriger/modhound/internal/jarinfo"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/modrinth"
)

type Status string

const (
	StatusUpdate  Status = "update"
	StatusCurrent Status = "current"
	StatusUnknown Status = "unknown"
	StatusManual  Status = "manual"
	StatusError   Status = "error"
)

const (
	SourceGTNH       = "gtnh"
	SourceCurseForge = "curseforge"
	SourceModrinth   = "modrinth"
)

type Target struct {
	Version     string `json:"version"`
	FileName    string `json:"fileName"`
	Date        string `json:"date"`
	PageURL     string `json:"pageUrl"`
	DownloadURL string `json:"-"`
	SHA1        string `json:"-"`
	SHA512      string `json:"-"`
}

type Mod struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	FileName    string    `json:"fileName"`
	RelPath     string    `json:"relPath"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Authors     []string  `json:"authors"`
	Description string    `json:"description"`
	Icon        string    `json:"icon"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	Status      Status    `json:"status"`
	Reason      string    `json:"reason"`
	Target      *Target   `json:"target"`
	Skipped     bool      `json:"skipped"`
	Size        int64     `json:"size"`
	Debug       *ModDebug `json:"debug,omitempty"`

	Path        string       `json:"-"`
	Jar         *jarinfo.Jar `json:"-"`
	cf          *cfState
	missingDeps []int
	notes       notes
	mr          *mrState
	gt          *gtnh.Hit
}

type ModDebug struct {
	Path        string `json:"path"`
	SHA1        string `json:"sha1"`
	Fingerprint uint32 `json:"fingerprint"`
	ClassMajor  int    `json:"classMajor"`
	GTNH        string `json:"gtnh,omitempty"`
	CurseForge  string `json:"curseforge,omitempty"`
	Modrinth    string `json:"modrinth,omitempty"`
}

func debugInfo(m *Mod) *ModDebug {
	d := &ModDebug{Path: m.Path}
	if m.Jar != nil {
		d.SHA1, d.Fingerprint, d.ClassMajor = m.Jar.SHA1, m.Jar.Murmur2, m.Jar.ClassMajor
	}
	if m.gt != nil {
		d.GTNH = m.gt.Mod.Name + " " + m.gt.Mod.Versions[m.gt.Index].Tag
	}
	if m.cf != nil {
		d.CurseForge = fmt.Sprintf("project %d, file %d", m.cf.modID, m.cf.file.ID)
	}
	if m.mr != nil {
		d.Modrinth = "project " + m.mr.version.ProjectID + ", version " + m.mr.version.ID
	}
	return d
}

type notes struct {
	text   string
	cfMod  int
	cfFile int
}

var (
	htmlBreaks = regexp.MustCompile(`(?i)<\s*(br|/p|/li|/h[1-6]|/div)\s*/?>`)
	htmlItems  = regexp.MustCompile(`(?i)<\s*li[^>]*>`)
	htmlTags   = regexp.MustCompile(`<[^>]*>`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

func plainText(s string) string {
	s = htmlBreaks.ReplaceAllString(s, "\n")
	s = htmlItems.ReplaceAllString(s, "- ")
	s = htmlTags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(blankLines.ReplaceAllString(s, "\n\n"))
}

const maxNotes = 6000

func cleanNotes(s string) string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "*"))
		if strings.EqualFold(t, "## What's Changed") || strings.EqualFold(t, "What's Changed") || strings.HasPrefix(strings.ToLower(t), "full changelog") {
			continue
		}
		out = append(out, strings.TrimRight(line, " "))
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
}

func (p *Pack) Changelog(ctx context.Context, id, curseForgeKey string) (string, error) {
	var m *Mod
	for _, x := range p.Mods {
		if x.ID == id {
			m = x
		}
	}
	if m == nil {
		return "", errors.New("mod not found")
	}
	text := m.notes.text
	if text == "" && m.notes.cfFile != 0 && curseForgeKey != "" {
		raw, err := (&curseforge.Client{Key: curseForgeKey}).Changelog(ctx, m.notes.cfMod, m.notes.cfFile)
		if err != nil {
			return "", err
		}
		text = plainText(raw)
		m.notes.text = text
	}
	text = cleanNotes(text)
	if len(text) > maxNotes {
		text = text[:maxNotes] + "…"
	}
	return text, nil
}

type cfState struct {
	modID int
	file  curseforge.File
}

type mrState struct {
	version modrinth.Version
}

type Pack struct {
	Root          string          `json:"root"`
	ModsDir       string          `json:"modsDir"`
	MCVersion     string          `json:"mcVersion"`
	Loader        string          `json:"loader"`
	Mods          []*Mod          `json:"mods"`
	Warnings      []string        `json:"warnings"`
	CurseForgeOK  bool            `json:"curseforgeOk"`
	ModIDs        map[string]bool `json:"-"`
	MaxClassMajor int             `json:"-"`

	mu sync.Mutex
}

type Options struct {
	CurseForgeKey string
	CacheDir      string
	Skipped       func(key string) bool
	Progress      func(stage string, done, total int)
	OnMod         func(m *Mod)
	OnMatched     func(gtnh, curseforge, modrinth int)
}

func (o *Options) progress(stage string, done, total int) {
	if o.Progress != nil {
		o.Progress(stage, done, total)
	}
}

var mcVersionRe = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

func FindModsDir(root string) (string, error) {
	if strings.EqualFold(filepath.Base(root), "mods") {
		return root, nil
	}
	dir := filepath.Join(root, "mods")
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("no mods folder in %s", root)
}

func listJars(modsDir string) ([]string, error) {
	var out []string
	add := func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if !e.IsDir() && (strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".zip")) {
				out = append(out, filepath.Join(dir, e.Name()))
			}
		}
		return nil
	}
	if err := add(modsDir); err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(modsDir)
	for _, e := range entries {
		if e.IsDir() && mcVersionRe.MatchString(e.Name()) {
			add(filepath.Join(modsDir, e.Name()))
		}
	}
	return out, nil
}

func Check(ctx context.Context, root string, opts Options) (*Pack, error) {
	started := time.Now()
	modsDir, err := FindModsDir(root)
	if err != nil {
		return nil, err
	}
	paths, err := listJars(modsDir)
	if err != nil {
		return nil, err
	}
	pack := &Pack{Root: root, ModsDir: modsDir, Loader: "forge"}
	pack.Mods = readJars(ctx, paths, modsDir, &opts)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pack.MCVersion = detectMCVersion(pack.Mods, modsDir)
	pack.ModIDs = map[string]bool{}
	for _, m := range pack.Mods {
		if m.Jar != nil {
			for _, id := range m.Jar.ModIDs {
				pack.ModIDs[strings.ToLower(id)] = true
			}
		}
	}
	logx.Printf("check %s: mods folder %s, %d files, Minecraft %s, loader %s, curseforge key set %v", root, modsDir, len(pack.Mods), pack.MCVersion, pack.Loader, opts.CurseForgeKey != "")
	if logx.Enabled() {
		for _, m := range pack.Mods {
			logJar(m)
		}
	}
	if pack.MCVersion == "" {
		return nil, errors.New("could not detect the Minecraft version of this pack")
	}
	for _, m := range pack.Mods {
		if m.Jar != nil && m.Jar.ClassMajor > pack.MaxClassMajor {
			pack.MaxClassMajor = m.Jar.ClassMajor
		}
	}

	r := identify(ctx, pack, pack.Mods, &opts)
	r.resolveAll(ctx, pack.Mods)
	r.describeMissingDeps(ctx, pack.Mods, pack.Mods)
	if logx.Enabled() {
		counts := map[Status]int{}
		for _, m := range pack.Mods {
			counts[m.Status]++
			if len(m.missingDeps) > 0 {
				logx.Printf("%s: %s", m.FileName, m.Reason)
			}
		}
		logx.Printf("check done in %v: %d update, %d manual, %d error, %d unknown, %d current", time.Since(started).Round(time.Millisecond), counts[StatusUpdate], counts[StatusManual], counts[StatusError], counts[StatusUnknown], counts[StatusCurrent])
	}
	sort.Slice(pack.Mods, func(i, j int) bool {
		return strings.ToLower(pack.Mods[i].Name) < strings.ToLower(pack.Mods[j].Name)
	})
	return pack, ctx.Err()
}

const lookupTimeout = 45 * time.Second

func identify(ctx context.Context, pack *Pack, mods []*Mod, opts *Options) *resolver {
	var (
		err       error
		catalog   *gtnh.Catalog
		cf        *curseforge.Client
		cfMatches map[uint32]curseforge.Match
		mrMatches map[string]modrinth.Version
		lookups   sync.WaitGroup
	)
	if pack.MCVersion == "1.7.10" {
		lookups.Go(func() {
			opts.progress("gtnh", 0, 1)
			c, err := gtnh.Load(ctx, opts.CacheDir)
			if err != nil {
				pack.warn("The GTNH catalog is unavailable right now (%s), GTNH mods were not checked", httpx.Short(err))
			}
			catalog = c
			opts.progress("gtnh", 1, 1)
		})
	}
	if opts.CurseForgeKey != "" {
		lookups.Go(func() {
			opts.progress("curseforge", 0, 1)
			client := &curseforge.Client{Key: opts.CurseForgeKey}
			fps := make([]uint32, 0, len(mods))
			for _, m := range mods {
				if m.Jar != nil {
					fps = append(fps, m.Jar.Murmur2)
				}
			}
			lctx, cancel := context.WithTimeout(ctx, lookupTimeout)
			matches, err := client.MatchFingerprints(lctx, fps)
			cancel()
			if err != nil {
				pack.warn("CurseForge is unavailable right now (%s), CurseForge mods were not checked", httpx.Short(err))
			} else {
				cf, cfMatches = client, matches
			}
			opts.progress("curseforge", 1, 1)
		})
	}
	lookups.Go(func() {
		opts.progress("modrinth", 0, 1)
		sha1s := make([]string, 0, len(mods))
		for _, m := range mods {
			if m.Jar != nil {
				sha1s = append(sha1s, m.Jar.SHA1)
			}
		}
		lctx, cancel := context.WithTimeout(ctx, lookupTimeout)
		matches, err := modrinth.VersionsByHash(lctx, sha1s)
		cancel()
		if err != nil {
			pack.warn("Modrinth is unavailable right now (%s), Modrinth mods were not checked", httpx.Short(err))
		}
		mrMatches = matches
		opts.progress("modrinth", 1, 1)
	})
	lookups.Wait()

	cfIDs := map[int]bool{}
	var mrIDs []string
	for _, m := range mods {
		if m.Jar == nil {
			continue
		}
		if catalog != nil {
			if hit, ok := catalog.Lookup(m.FileName); ok {
				m.gt = &hit
			}
		}
		if match, ok := cfMatches[m.Jar.Murmur2]; ok {
			m.cf = &cfState{modID: match.ModID, file: match.File}
			cfIDs[match.ModID] = true
		}
		if v, ok := mrMatches[m.Jar.SHA1]; ok {
			m.mr = &mrState{version: v}
			mrIDs = append(mrIDs, v.ProjectID)
		}
		if logx.Enabled() && (m.gt != nil || m.cf != nil || m.mr != nil) {
			var found []string
			if m.gt != nil {
				found = append(found, "gtnh "+m.gt.Mod.Name)
			}
			if m.cf != nil {
				found = append(found, fmt.Sprintf("curseforge %d/%d", m.cf.modID, m.cf.file.ID))
			}
			if m.mr != nil {
				found = append(found, "modrinth "+m.mr.version.ProjectID+"/"+m.mr.version.ID)
			}
			logx.Printf("%s: found in %s", m.FileName, strings.Join(found, ", "))
		}
		switch {
		case m.gt != nil:
			m.Source, m.Key = SourceGTNH, "gtnh:"+m.gt.Mod.Name
		case m.cf != nil:
			m.Source, m.Key = SourceCurseForge, "cf:"+strconv.Itoa(m.cf.modID)
		case m.mr != nil:
			m.Source, m.Key = SourceModrinth, "mr:"+m.mr.version.ProjectID
		}
	}

	if opts.OnMatched != nil {
		var nGT, nCF, nMR int
		for _, m := range mods {
			if m.gt != nil {
				nGT++
			}
			if m.cf != nil {
				nCF++
			}
			if m.mr != nil {
				nMR++
			}
		}
		opts.OnMatched(nGT, nCF, nMR)
	}

	cfMods := map[int]curseforge.Mod{}
	if cf != nil && len(cfIDs) > 0 {
		ids := keys(cfIDs)
		if cfMods, err = cf.Mods(ctx, ids); err != nil {
			pack.warn("CurseForge project details are unavailable (%s)", httpx.Short(err))
			cfMods = map[int]curseforge.Mod{}
		}
	}
	mrProjects := map[string]modrinth.Project{}
	if len(mrIDs) > 0 {
		if mrProjects, err = modrinth.Projects(ctx, uniq(mrIDs)); err != nil {
			pack.warn("Modrinth project details are unavailable (%s)", httpx.Short(err))
			mrProjects = map[string]modrinth.Project{}
		}
	}

	pack.CurseForgeOK = cf != nil
	logx.Printf("identified: gtnh %d, curseforge %d, modrinth %d, warnings %v", countIf(mods, func(m *Mod) bool { return m.gt != nil }), len(cfMatches), len(mrMatches), pack.Warnings)
	return &resolver{pack: pack, opts: opts, cf: cf, catalog: catalog, cfMods: cfMods, cfIDs: cfIDs, mrProjects: mrProjects, total: len(mods)}
}

type Replaced struct {
	OldID string `json:"oldId"`
	Mod   *Mod   `json:"mod"`
}

func (p *Pack) Recheck(ctx context.Context, moved map[string]string, opts Options) ([]Replaced, error) {
	byPath := map[string]*Mod{}
	for _, m := range p.Mods {
		byPath[strings.ToLower(m.Path)] = m
	}
	var paths []string
	var olds []*Mod
	for oldPath, newPath := range moved {
		if m := byPath[strings.ToLower(oldPath)]; m != nil {
			paths = append(paths, newPath)
			olds = append(olds, m)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	mods := readJars(ctx, paths, p.ModsDir, &opts)
	scope := &Pack{Root: p.Root, ModsDir: p.ModsDir, Loader: p.Loader, MCVersion: p.MCVersion, ModIDs: p.ModIDs, MaxClassMajor: p.MaxClassMajor, Mods: mods}
	r := identify(ctx, scope, mods, &opts)
	r.resolveAll(ctx, mods)
	out := make([]Replaced, len(mods))
	for i, m := range mods {
		out[i] = Replaced{OldID: olds[i].ID, Mod: m}
	}
	p.mu.Lock()
	for i, m := range p.Mods {
		for j, old := range olds {
			if m == old {
				p.Mods[i] = mods[j]
			}
		}
	}
	p.mu.Unlock()
	r.describeMissingDeps(ctx, mods, p.Mods)
	for _, w := range scope.Warnings {
		logx.Printf("recheck: %s", w)
	}
	return out, ctx.Err()
}

func (p *Pack) warn(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, args...))
}

func readJars(ctx context.Context, paths []string, modsDir string, opts *Options) []*Mod {
	mods := make([]*Mod, len(paths))
	var done int
	var mu sync.Mutex
	sem := make(chan struct{}, min(runtime.NumCPU(), 8))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			rel, _ := filepath.Rel(modsDir, p)
			m := &Mod{FileName: filepath.Base(p), RelPath: filepath.ToSlash(rel), Path: p}
			if ctx.Err() != nil {
				mods[i] = m
				return
			}
			j, err := jarinfo.Read(p)
			if err != nil {
				m.Status, m.Reason = StatusError, err.Error()
			} else {
				m.Jar, m.Size = j, j.Size
			}
			mods[i] = m
			mu.Lock()
			done++
			opts.progress("scan", done, len(paths))
			mu.Unlock()
		}()
	}
	wg.Wait()
	return mods
}

var mcVersionIn = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

func detectMCVersion(mods []*Mod, modsDir string) string {
	counts := map[string]int{}
	for _, m := range mods {
		if m.Jar != nil && m.Jar.Info != nil {
			if v := mcVersionIn.FindString(m.Jar.Info.MCVersion); v != "" {
				counts[v]++
			}
		}
	}
	entries, _ := os.ReadDir(modsDir)
	for _, e := range entries {
		if e.IsDir() && mcVersionRe.MatchString(e.Name()) {
			counts[e.Name()] += 10
		}
	}
	best, bestN := "", 0
	for v, n := range counts {
		if n > bestN || (n == bestN && v > best) {
			best, bestN = v, n
		}
	}
	return best
}

type resolver struct {
	pack    *Pack
	opts    *Options
	cf      *curseforge.Client
	catalog *gtnh.Catalog
	cfMods  map[int]curseforge.Mod
	cfIDs   map[int]bool

	mrProjects map[string]modrinth.Project
	mu         sync.Mutex
	done       int
	total      int
}

func (r *resolver) finish(m *Mod) {
	fillMeta(m, r.pack.MCVersion, r.cfMods, r.mrProjects)
	if logx.Enabled() {
		m.Debug = debugInfo(m)
		target := ""
		if m.Target != nil {
			target = " -> " + m.Target.FileName
		}
		via := ""
		if m.Source != "" {
			via = " via " + m.Source
		}
		logx.Printf("%s: %s%s%s %s", m.FileName, m.Status, via, target, m.Reason)
	}
	if m.Key == "" {
		m.Key = "file:" + Stem(m.FileName)
	}
	m.ID = m.Key + "|" + m.RelPath
	if r.opts.Skipped != nil {
		m.Skipped = r.opts.Skipped(m.Key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done++
	r.opts.progress("resolve", r.done, r.total)
	if r.opts.OnMod != nil {
		r.opts.OnMod(m)
	}
}

func (r *resolver) resolveAll(ctx context.Context, mods []*Mod) {
	var todo []*Mod
	for _, m := range mods {
		switch {
		case m.Jar == nil:
			r.finish(m)
		case m.Source == "":
			m.Status = StatusUnknown
			m.Reason = "Not found on GTNH, CurseForge or Modrinth"
			r.finish(m)
		default:
			todo = append(todo, m)
		}
	}
	sem := make(chan struct{}, 12)
	var wg sync.WaitGroup
	for _, m := range todo {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var err error
			switch m.Source {
			case SourceGTNH:
				err = r.resolveGTNH(ctx, m)
			case SourceCurseForge:
				err = r.resolveCurseForge(ctx, m)
			case SourceModrinth:
				err = r.resolveModrinth(ctx, m)
			}
			if err != nil {
				m.Status, m.Reason, m.Target = StatusError, err.Error(), nil
			}
			r.finish(m)
		}()
	}
	wg.Wait()
}

func (r *resolver) describeMissingDeps(ctx context.Context, mods, all []*Mod) {
	ids := map[int]bool{}
	for _, m := range mods {
		for _, id := range m.missingDeps {
			ids[id] = true
		}
	}
	if len(ids) == 0 {
		return
	}
	names, err := r.cf.Mods(ctx, keys(ids))
	if err != nil {
		names = map[int]curseforge.Mod{}
	}
	present := map[string]bool{}
	for _, m := range all {
		present[normName(m.Name)] = true
		if m.gt != nil {
			present[normName(m.gt.Mod.Name)] = true
		}
		if m.Jar != nil && m.Jar.Info != nil {
			present[normName(m.Jar.Info.ModID)] = true
		}
	}
	for _, m := range mods {
		var list []string
		for _, id := range m.missingDeps {
			dep, ok := names[id]
			if ok && (present[normName(dep.Name)] || present[normName(dep.Slug)]) {
				continue
			}
			list = append(list, first(dep.Name, strconv.Itoa(id)))
		}
		if len(list) > 0 {
			m.Reason = "Requires mods not detected in the pack: " + strings.Join(list, ", ")
		}
	}
}

func (r *resolver) resolveGTNH(ctx context.Context, m *Mod) error {
	mod, cur := m.gt.Mod, m.gt.Index
	idx, v := latestGTNH(mod, mod.Versions[cur].Prerelease)
	if v != nil {
		logx.Printf("%s: gtnh %s (hosted on %s), installed %s, latest %s", m.FileName, mod.Name, first(mod.Source, "github"), mod.Versions[cur].Tag, v.Tag)
	}
	if v == nil || idx <= cur {
		m.Status = StatusCurrent
		return nil
	}
	m.Status = StatusUpdate
	m.Target = &Target{Version: v.Tag, FileName: v.Filename, Date: dateOnly(v.TaggedAt), PageURL: v.BrowserDownloadURL}
	for i := idx; i > cur; i-- {
		if text := strings.TrimSpace(mod.Versions[i].Changelog); text != "" {
			m.notes.text += "## " + mod.Versions[i].Tag + "\n" + text + "\n\n"
		}
	}
	switch mod.Source {
	case gtnh.SourceCurse:
		if v.CurseFile != nil {
			return r.fillCurseDownload(ctx, m, v.CurseFile)
		}
		m.Status, m.Reason = StatusManual, "You can download it from the project page"
	case gtnh.SourceOther:
		m.Status, m.Reason = StatusManual, "You can download it from the project page"
	default:
		if v.BrowserDownloadURL == "" {
			m.Status, m.Reason = StatusManual, "You can download it from the project page"
			break
		}
		m.Target.DownloadURL = v.BrowserDownloadURL
		if repo := mod.RepoURL; repo != "" {
			m.Target.PageURL = repo + "/releases/tag/" + v.Tag
		}
	}
	return nil
}

func latestGTNH(mod *gtnh.Mod, allowPre bool) (int, *gtnh.Version) {
	if i, v := mod.Version(mod.LatestVersion); v != nil && (!v.Prerelease || allowPre) {
		return i, v
	}
	for i := len(mod.Versions) - 1; i >= 0; i-- {
		if !mod.Versions[i].Prerelease || allowPre {
			return i, &mod.Versions[i]
		}
	}
	return -1, nil
}

func (r *resolver) fillCurseDownload(ctx context.Context, m *Mod, cfFile *gtnh.CurseFile) error {
	if r.cf == nil {
		m.Status, m.Reason = StatusManual, "Needs a CurseForge API key to download"
		return nil
	}
	modID, err1 := strconv.Atoi(cfFile.ProjectNo)
	fileID, err2 := strconv.Atoi(cfFile.FileNo)
	if err1 != nil || err2 != nil || modID <= 0 || fileID <= 0 {
		m.Status, m.Reason = StatusManual, "You can download it from the project page"
		return nil
	}
	f, err := r.cf.File(ctx, modID, fileID)
	if err != nil {
		return err
	}
	if f.DownloadURL == "" {
		m.Status, m.Reason = StatusManual, "The author does not allow downloads through apps"
		return nil
	}
	m.Target.DownloadURL, m.Target.SHA1 = f.DownloadURL, f.SHA1()
	return nil
}

var foreignLoaders = []string{"Fabric", "Quilt", "NeoForge", "LiteLoader"}

func (r *resolver) resolveCurseForge(ctx context.Context, m *Mod) error {
	cur := m.cf.file
	files, err := r.cf.Files(ctx, m.cf.modID, r.pack.MCVersion)
	if err != nil {
		return err
	}
	allowed := max(cur.ReleaseType, curseforge.ReleaseTypeRelease)
	curStem := Stem(cur.FileName)
	logx.Printf("%s: curseforge project %d, installed file %d %s (%s), %d files for %s", m.FileName, m.cf.modID, cur.ID, cur.FileName, cur.FileDate.Format(time.DateOnly), len(files), r.pack.MCVersion)
	var best, renamed *curseforge.File
	for i := range files {
		f := &files[i]
		reason := ""
		switch {
		case f.ID == cur.ID:
			continue
		case !f.IsAvailable:
			reason = "not available"
		case f.ReleaseType > allowed:
			reason = fmt.Sprintf("%s, installed is %s", releaseName(f.ReleaseType), releaseName(cur.ReleaseType))
		case !f.FileDate.After(cur.FileDate):
			reason = "not newer"
		case !slices.Contains(f.GameVersions, r.pack.MCVersion):
			reason = "not tagged for " + r.pack.MCVersion
		case slices.ContainsFunc(f.GameVersions, func(g string) bool { return slices.Contains(foreignLoaders, g) }):
			reason = "other loader"
		case Stem(f.FileName) != curStem:
			reason = fmt.Sprintf("different name %q vs %q", Stem(f.FileName), curStem)
			if sharesStem(f.FileName, cur.FileName) && !isVariantOf(f.FileName, cur.FileName) && (renamed == nil || f.FileDate.After(renamed.FileDate)) {
				renamed = f
			}
		case sameVersion(f.FileName, cur.FileName, r.pack.MCVersion):
			reason = "same version"
		case OlderVersion(f.FileName, cur.FileName, r.pack.MCVersion):
			reason = "older version"
		}
		if reason == "not newer" {
			continue
		}
		if reason != "" {
			logx.Printf("%s:   skip %d %s: %s", m.FileName, f.ID, f.FileName, reason)
			continue
		}
		logx.Printf("%s:   candidate %d %s (%s)", m.FileName, f.ID, f.FileName, f.FileDate.Format(time.DateOnly))
		if best == nil || f.FileDate.After(best.FileDate) {
			best = f
		}
	}
	slug := r.cfMods[m.cf.modID].Slug
	if best == nil {
		m.Status = StatusCurrent
		if renamed != nil {
			m.Status, m.Reason = StatusManual, "A newer file has a different name: "+renamed.FileName
			m.Target = &Target{Version: renamed.DisplayName, FileName: renamed.FileName, Date: renamed.FileDate.Format(time.DateOnly), PageURL: curseforge.FilePageURL(slug, renamed.ID)}
		}
		return nil
	}
	m.Status = StatusUpdate
	m.notes = notes{cfMod: m.cf.modID, cfFile: best.ID}
	m.Target = &Target{
		Version:     best.DisplayName,
		FileName:    best.FileName,
		Date:        best.FileDate.Format(time.DateOnly),
		PageURL:     curseforge.FilePageURL(slug, best.ID),
		DownloadURL: best.DownloadURL,
		SHA1:        best.SHA1(),
	}
	if best.DownloadURL == "" {
		m.Status, m.Reason = StatusManual, "The author does not allow downloads through apps"
		return nil
	}
	for _, d := range best.Dependencies {
		if d.RelationType == curseforge.RelationRequired && !r.cfIDs[d.ModID] {
			m.missingDeps = append(m.missingDeps, d.ModID)
		}
	}
	return nil
}

func releaseRank(t string) int {
	switch t {
	case "beta":
		return 2
	case "alpha":
		return 3
	}
	return 1
}

func (r *resolver) resolveModrinth(ctx context.Context, m *Mod) error {
	cur := m.mr.version
	versions, err := modrinth.Versions(ctx, cur.ProjectID, r.pack.Loader, r.pack.MCVersion)
	if err != nil {
		return err
	}
	allowed := releaseRank(cur.VersionType)
	curFile := cur.PrimaryFile()
	logx.Printf("%s: modrinth project %s, installed version %s %s, %d versions for %s %s", m.FileName, cur.ProjectID, cur.ID, cur.VersionNumber, len(versions), r.pack.Loader, r.pack.MCVersion)
	var best *modrinth.Version
	for i := range versions {
		v := &versions[i]
		f := v.PrimaryFile()
		if v.ID == cur.ID || f == nil || !v.DatePublished.After(cur.DatePublished) {
			continue
		}
		reason := ""
		switch {
		case releaseRank(v.VersionType) > allowed:
			reason = v.VersionType + " release"
		case curFile != nil && Stem(f.Filename) != Stem(curFile.Filename):
			reason = "different name"
		case curFile != nil && sameVersion(f.Filename, curFile.Filename, r.pack.MCVersion):
			reason = "same version"
		case curFile != nil && OlderVersion(f.Filename, curFile.Filename, r.pack.MCVersion):
			reason = "older version"
		}
		if reason != "" {
			logx.Printf("%s:   skip %s %s: %s", m.FileName, v.ID, f.Filename, reason)
			continue
		}
		logx.Printf("%s:   candidate %s %s", m.FileName, v.ID, f.Filename)
		if best == nil || v.DatePublished.After(best.DatePublished) {
			best = v
		}
	}
	if best == nil {
		m.Status = StatusCurrent
		return nil
	}
	f := best.PrimaryFile()
	m.Status = StatusUpdate
	m.notes.text = best.Changelog
	m.Target = &Target{
		Version:     best.VersionNumber,
		FileName:    f.Filename,
		Date:        best.DatePublished.Format(time.DateOnly),
		PageURL:     "https://modrinth.com/mod/" + best.ProjectID + "/version/" + best.ID,
		DownloadURL: f.URL,
		SHA1:        f.Hashes.SHA1,
		SHA512:      f.Hashes.SHA512,
	}
	return nil
}

func fillMeta(m *Mod, mcVersion string, cfMods map[int]curseforge.Mod, mrProjects map[string]modrinth.Project) {
	var cfm *curseforge.Mod
	if m.cf != nil {
		if x, ok := cfMods[m.cf.modID]; ok {
			cfm = &x
		}
	}
	var mrp *modrinth.Project
	if m.mr != nil {
		if x, ok := mrProjects[m.mr.version.ProjectID]; ok {
			mrp = &x
		}
	}
	var info *jarinfo.ModInfo
	if m.Jar != nil {
		info = m.Jar.Info
		if m.Jar.Logo != nil {
			m.Icon = icons.Put(m.Jar.SHA1, m.Jar.LogoType, m.Jar.Logo)
		}
		m.Authors = m.Jar.Authors()
	}
	if info != nil {
		m.Description, m.URL = info.Description, info.URL
	}
	if cfm != nil {
		m.Name = cfm.Name
		m.Description = first(cfm.Summary, m.Description)
		if cfm.Logo != nil {
			m.Icon = first(cfm.Logo.ThumbnailURL, m.Icon)
		}
		if len(m.Authors) == 0 {
			for _, a := range cfm.Authors {
				m.Authors = append(m.Authors, a.Name)
			}
		}
		if m.Source == SourceCurseForge {
			m.URL = first(cfm.Links.WebsiteURL, m.URL)
		}
	}
	if mrp != nil {
		m.Name = first(m.Name, mrp.Title)
		m.Description = first(m.Description, mrp.Description)
		m.Icon = first(mrp.IconURL, m.Icon)
		if m.Source == SourceModrinth {
			m.URL = modrinth.ProjectURL(mrp.Slug)
		}
	}
	if info != nil {
		m.Name = first(m.Name, info.Name)
	}
	switch {
	case m.gt != nil:
		m.Name = first(m.Name, m.gt.Mod.Name)
		m.Version = m.gt.Mod.Versions[m.gt.Index].Tag
		m.URL = first(m.gt.Mod.URL(), m.URL)
	case m.mr != nil && m.Source == SourceModrinth:
		m.Version = m.mr.version.VersionNumber
	default:
		m.Version = FileVersion(m.FileName, mcVersion)
		if m.Target != nil {
			m.Target.Version = first(FileVersion(m.Target.FileName, mcVersion), m.Target.Version)
		}
	}
	if m.Version == "" && info != nil {
		m.Version = info.Version
	}
	m.Name = first(m.Name, strings.TrimSuffix(strings.TrimSuffix(m.FileName, ".jar"), ".zip"))
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func dateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func releaseName(t int) string {
	switch t {
	case 1:
		return "release"
	case 2:
		return "beta"
	case 3:
		return "alpha"
	}
	return fmt.Sprintf("release type %d", t)
}

func logJar(m *Mod) {
	j := m.Jar
	if j == nil {
		logx.Printf("jar %s: unreadable: %s", m.RelPath, m.Reason)
		return
	}
	info := "no mcmod.info"
	if j.Info != nil {
		info = fmt.Sprintf("modid %q, version %q, mcversion %q", j.Info.ModID, j.Info.Version, j.Info.MCVersion)
	}
	logx.Printf("jar %s: %d bytes, sha1 %s, fingerprint %d, class version %d, valid zip %v, %s", m.RelPath, j.Size, j.SHA1, j.Murmur2, j.ClassMajor, j.Valid, info)
}

func countIf(mods []*Mod, fn func(*Mod) bool) int {
	n := 0
	for _, m := range mods {
		if fn(m) {
			n++
		}
	}
	return n
}

func keys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func uniq(s []string) []string {
	slices.Sort(s)
	return slices.Compact(s)
}
