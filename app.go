package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	goruntime "runtime"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/blackkriger/modhound/internal/backup"
	"github.com/blackkriger/modhound/internal/compat"
	"github.com/blackkriger/modhound/internal/config"
	"github.com/blackkriger/modhound/internal/fsx"
	"github.com/blackkriger/modhound/internal/install"
	"github.com/blackkriger/modhound/internal/jarinfo"
	"github.com/blackkriger/modhound/internal/launchers"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/opener"
	"github.com/blackkriger/modhound/internal/resolve"
	"github.com/blackkriger/modhound/internal/selfupdate"
	"github.com/blackkriger/modhound/internal/server"
)

type App struct {
	ctx   context.Context
	store *config.Store

	mu     sync.Mutex
	pack   *resolve.Pack
	cancel context.CancelFunc
	logs   chan string

	appUpdate *selfupdate.Release
	report    *Report
	session   *backup.Session

	post      sync.Mutex
	ops       int
	opsCtx    context.Context
	opsCancel context.CancelFunc

	compatMu     sync.Mutex
	compatKey    string
	compatResult []compat.Problem

	jvmdgMu sync.Mutex
	jvmdg   map[string]*compat.JvmdgSource

	offers map[string]*resolve.Offer
}

type Settings struct {
	CurseForgeKey string            `json:"curseforgeKey"`
	Theme         string            `json:"theme"`
	Debug         bool              `json:"debug"`
	LastPack      string            `json:"lastPack"`
	Server        string            `json:"server"`
	ServerKey     string            `json:"serverKey"`
	Sort          map[string]string `json:"sort"`
	Version       string            `json:"version"`
}

type Progress struct {
	Stage string `json:"stage"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

type Manual struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Reason  string `json:"reason"`
	PageURL string `json:"pageUrl"`
}

type Report struct {
	Time      string             `json:"time"`
	Pack      string             `json:"pack"`
	Installed []install.Result   `json:"installed"`
	Failed    []install.Result   `json:"failed"`
	UpToDate  int                `json:"upToDate"`
	Skipped   []string           `json:"skipped"`
	Remaining []string           `json:"remaining"`
	Manual    []Manual           `json:"manual"`
	Unknown   []string           `json:"unknown"`
	Server    *ServerSync        `json:"server"`
	Path      string             `json:"path"`
	Rechecked []resolve.Replaced `json:"rechecked"`
}

type ServerSync struct {
	Root    string          `json:"root"`
	Remote  bool            `json:"remote"`
	Results []server.Result `json:"results"`
	Error   string          `json:"error"`
}

func NewApp(store *config.Store) *App {
	return &App{store: store, logs: make(chan string, 4096)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	selfupdate.CleanOld()
	migrateErr := config.Migrate()
	fsx.KeyFor = func(string) string { return a.serverKey() }
	go a.forwardLogs()
	a.configureLog()
	if migrateErr != nil {
		logx.Printf("moving data to the local app data folder: %v", migrateErr)
	}
}

func (a *App) configureLog() {
	dir, err := config.LogDir()
	if err != nil {
		return
	}
	logx.Configure(a.store.Get().Debug, dir, func(line string) {
		select {
		case a.logs <- line:
		default:
		}
	})
	cfg := a.store.Get()
	configDir, _ := config.Dir()
	cacheDir, _ := config.CacheDir()
	logx.Printf("modhound %s, %s/%s, %s, config %s, cache %s, logs %s", Version, goruntime.GOOS, goruntime.GOARCH, goruntime.Version(), configDir, cacheDir, dir)
	logx.Printf("settings: curseforge key set %v, theme %q, last pack %q", cfg.CurseForgeKey != "", cfg.Theme, cfg.LastPack)
}

func (a *App) forwardLogs() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var batch []string
	for {
		select {
		case <-a.ctx.Done():
			return
		case line := <-a.logs:
			batch = append(batch, line)
		case <-ticker.C:
			if len(batch) > 0 {
				runtime.EventsEmit(a.ctx, "log", batch)
				batch = nil
			}
		}
	}
}

func (a *App) SetConsoleOpen(open bool) {
	height := windowHeight
	if open {
		height += consoleHeight
	}
	runtime.WindowSetSize(a.ctx, windowWidth, height)
}

func (a *App) KeepWindowSize() {
	if runtime.WindowIsMaximised(a.ctx) || runtime.WindowIsFullscreen(a.ctx) {
		runtime.WindowUnfullscreen(a.ctx)
		runtime.WindowUnmaximise(a.ctx)
	}
}

func (a *App) OpenLogFolder() {
	dir, err := config.LogDir()
	if err != nil || os.MkdirAll(dir, 0o755) != nil {
		return
	}
	opener.Open(dir)
}

func (a *App) Settings() Settings {
	c := a.store.Get()
	return Settings{
		CurseForgeKey: c.CurseForgeKey,
		Theme:         c.Theme,
		Debug:         c.Debug,
		LastPack:      c.LastPack,
		Server:        a.serverRoot(),
		ServerKey:     a.serverKey(),
		Sort:          c.Sort,
		Version:       Version,
	}
}

func (a *App) serverRoot() string {
	pack := a.packRoot()
	if pack == "" {
		return ""
	}
	dir, _ := a.store.Server(pack)
	return dir
}

func (a *App) serverKey() string {
	if key := a.store.ServerKey(a.packRoot()); key != "" {
		return key
	}
	return fsx.DefaultKey()
}

func (a *App) ChooseKey() (string, error) {
	start := filepath.Dir(a.serverKey())
	if a.serverKey() == "" {
		start, _ = os.UserHomeDir()
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select SSH key", DefaultDirectory: start})
}

func (a *App) ChooseServer() (string, error) {
	start := a.serverRoot()
	if start == "" {
		start = filepath.Dir(filepath.Clean(a.packRoot()))
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select folder", DefaultDirectory: start})
	if err != nil || dir == "" {
		return "", err
	}
	if _, err := resolve.FindModsDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func (a *App) SaveSettings(key, theme string, debug bool, serverDir, serverKey string) error {
	serverDir = strings.TrimSpace(serverDir)
	serverKey = strings.TrimSpace(serverKey)
	pack := a.packRoot()
	keyChanged := serverKey != a.serverKey()
	switch {
	case serverDir == "":
	case fsx.IsRemote(serverDir):
		if serverDir != a.serverRoot() || keyChanged {
			check := serverKey
			if check == "" {
				check = fsx.DefaultKey()
			}
			if err := fsx.Check(serverDir, check); err != nil {
				return err
			}
		}
	default:
		if _, err := resolve.FindModsDir(serverDir); err != nil {
			return err
		}
		if strings.EqualFold(filepath.Clean(serverDir), filepath.Clean(pack)) {
			return errors.New("the server folder is the minecraft folder itself")
		}
	}
	if pack != "" && serverDir != a.serverRoot() {
		if err := a.store.SetServer(pack, serverDir); err != nil {
			return err
		}
	}
	if pack != "" && keyChanged {
		if serverKey == fsx.DefaultKey() {
			serverKey = ""
		}
		if err := a.store.SetServerKey(pack, serverKey); err != nil {
			return err
		}
	}
	err := a.store.Update(func(c *config.Config) {
		c.CurseForgeKey = strings.TrimSpace(key)
		c.Debug = debug
		switch theme {
		case "light", "dark":
			c.Theme = theme
		default:
			c.Theme = ""
		}
	})
	a.configureLog()
	return err
}

func (a *App) SetSort(section, by string) error {
	if by != "name" && by != "date" {
		return fmt.Errorf("unknown sort %q", by)
	}
	return a.store.Update(func(c *config.Config) {
		if c.Sort == nil {
			c.Sort = map[string]string{}
		}
		c.Sort[section] = by
	})
}

func (a *App) ChoosePack() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Select folder",
		DefaultDirectory: a.store.Get().LastPack,
	})
	if err != nil || dir == "" {
		return "", err
	}
	if _, err := resolve.FindModsDir(dir); err != nil {
		return "", err
	}
	return dir, a.store.Update(func(c *config.Config) { c.LastPack = dir })
}

func (a *App) DefaultPack() string {
	return launchers.Default()
}

func (a *App) SelectPack(dir string) error {
	if _, err := resolve.FindModsDir(dir); err != nil {
		return err
	}
	return a.store.Update(func(c *config.Config) { c.LastPack = dir })
}

func (a *App) begin() (context.Context, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil || a.ops > 0 {
		return nil, errors.New("another task is running")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	return ctx, nil
}

func (a *App) end() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

func (a *App) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	logx.Printf("stop requested")
	if a.cancel != nil {
		a.cancel()
	}
	if a.opsCancel != nil {
		a.opsCancel()
	}
}

func (a *App) emitProgress(stage string, done, total int) {
	runtime.EventsEmit(a.ctx, "progress", Progress{Stage: stage, Done: done, Total: total})
}

func (a *App) Check(root string) (*resolve.Pack, error) {
	if root == "" {
		return nil, errors.New("select a pack folder first")
	}
	cacheDir, err := config.CacheDir()
	if err != nil {
		return nil, err
	}
	ctx, err := a.begin()
	if err != nil {
		return nil, err
	}
	defer a.end()
	pack, err := resolve.Check(ctx, root, resolve.Options{
		CurseForgeKey: a.store.CurseForgeKey(),
		CacheDir:      cacheDir,
		Skipped:       func(key string) bool { return a.store.Skipped(root, key) },
		Progress:      a.emitProgress,
		OnMod:         func(m *resolve.Mod) { runtime.EventsEmit(a.ctx, "mod", m) },
		OnMatched: func(gtnh, curseforge, modrinth int) {
			runtime.EventsEmit(a.ctx, "matched", map[string]int{"gtnh": gtnh, "curseforge": curseforge, "modrinth": modrinth})
		},
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("stopped")
		}
		return nil, err
	}
	a.mu.Lock()
	a.pack = pack
	a.mu.Unlock()
	return pack, nil
}

func (a *App) SetSkipped(key string, skip bool) error {
	a.mu.Lock()
	if a.cancel != nil || a.ops > 0 {
		a.mu.Unlock()
		return errors.New("wait until the current task finishes")
	}
	pack := a.pack
	if pack != nil {
		pack.SetSkipped(key, skip)
	}
	a.mu.Unlock()
	if pack == nil {
		return errors.New("no pack loaded")
	}
	logx.Printf("skip %s = %v", key, skip)
	return a.store.SetSkipped(pack.Root, key, skip)
}

func (a *App) beforeClose(context.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ops > 0
}

func (a *App) joinOp() (context.Context, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		return nil, errors.New("another task is running")
	}
	if a.ops == 0 || a.opsCtx.Err() != nil {
		a.opsCtx, a.opsCancel = context.WithCancel(a.ctx)
	}
	a.ops++
	return a.opsCtx, nil
}

func (a *App) leaveOp() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ops--
	if a.ops == 0 {
		a.opsCancel()
		a.opsCtx, a.opsCancel = nil, nil
	}
}

func (a *App) Update(ids []string, continued bool) (*Report, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil {
		return nil, errors.New("no pack loaded")
	}
	ctx, err := a.joinOp()
	if err != nil {
		return nil, err
	}
	defer a.leaveOp()
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	a.post.Lock()
	var todo []*resolve.Mod
	for _, m := range pack.List() {
		if m.Status == resolve.StatusUpdate && !m.Skipped && want[m.ID] {
			todo = append(todo, m)
		}
	}
	if err := install.CheckNotInUse(todo); err != nil {
		a.post.Unlock()
		return nil, err
	}
	if !continued || a.session == nil || a.report == nil || a.report.Pack != pack.Root {
		backupDir, err := config.BackupDir()
		if err != nil {
			a.post.Unlock()
			return nil, err
		}
		a.session = backup.Begin(backupDir, pack.Root)
		a.report = &Report{Time: time.Now().Format("2006-01-02 15:04:05"), Pack: pack.Root}
	}
	session, report := a.session, a.report
	a.post.Unlock()

	results := install.Run(ctx, pack, todo, session, func(e install.Event) {
		runtime.EventsEmit(a.ctx, "install", e)
	})

	byID := map[string]*resolve.Mod{}
	for _, m := range todo {
		byID[m.ID] = m
	}
	a.post.Lock()
	failed := report.Failed[:0:0]
	for _, f := range report.Failed {
		if byID[f.ID] == nil {
			failed = append(failed, f)
		}
	}
	report.Failed = failed
	keys := map[string]bool{}
	moved := map[string]string{}
	for _, r := range results {
		m := byID[r.ID]
		if !r.OK || m == nil {
			report.Failed = append(report.Failed, r)
			continue
		}
		report.Installed = append(report.Installed, r)
		chosen := m.Chosen()
		pack.MarkInstalled(m)
		keys[m.Key] = true
		if chosen {
			moved[m.Path] = m.Path
		}
	}
	var client []server.Mod
	if len(keys) > 0 {
		client = server.Snapshot(pack)
	}
	a.post.Unlock()

	var sync *ServerSync
	if len(keys) > 0 {
		sync = a.syncServer(ctx, client, pack.MCVersion, session, keys)
	}
	var rechecked []resolve.Replaced
	if len(moved) > 0 {
		var err error
		if rechecked, err = pack.Recheck(context.WithoutCancel(ctx), moved, a.recheckOptions(pack.Root)); err != nil {
			logx.Printf("recheck after choosing a version: %v", err)
		}
	}
	var done []install.Result
	for _, r := range results {
		if r.OK {
			done = append(done, r)
		}
	}
	warnings := a.compatWarnings(ctx, pack, done)

	a.post.Lock()
	defer a.post.Unlock()
	for i, x := range report.Installed {
		if w, ok := warnings[x.ID]; ok {
			report.Installed[i].Warning = w
		}
	}
	report.Server = mergeServer(report.Server, sync)
	renamed := map[string]string{}
	for _, r := range rechecked {
		renamed[r.OldID] = r.Mod.ID
	}
	for i, x := range report.Installed {
		if id, ok := renamed[x.ID]; ok {
			report.Installed[i].ID = id
		}
	}
	report.Skipped, report.Remaining, report.Manual, report.Unknown, report.UpToDate = nil, nil, nil, nil, 0
	fillReport(report, pack)
	if err := session.Save(); err != nil {
		logx.Printf("backup session not saved: %v", err)
	}
	if path, err := saveReport(report); err == nil {
		report.Path = path
	}
	logx.Printf("install finished: %d updated, %d failed, report %s", len(report.Installed), len(report.Failed), report.Path)
	out := *report
	out.Installed = append([]install.Result(nil), report.Installed...)
	out.Failed = append([]install.Result(nil), report.Failed...)
	out.Rechecked = rechecked
	return &out, nil
}

func (a *App) compatEnv(ctx context.Context, root string, failed *atomic.Bool) compat.Env {
	a.jvmdgMu.Lock()
	defer a.jvmdgMu.Unlock()
	if a.jvmdg == nil {
		a.jvmdg = map[string]*compat.JvmdgSource{}
	}
	src := a.jvmdg[root]
	if src == nil {
		cacheDir, _ := config.CacheDir()
		src = &compat.JvmdgSource{PackRoot: root, CacheDir: cacheDir}
		a.jvmdg[root] = src
	}
	return compat.Env{JvmdgClasses: func(version string) (map[string]bool, error) {
		classes, err := src.Classes(ctx, version)
		if err != nil {
			failed.Store(true)
			logx.Printf("compat: JvmDowngrader %s: %v", version, err)
		}
		return classes, err
	}}
}

type compatInput struct {
	name, path, key string
	jar             *jarinfo.Jar
}

func (a *App) Compat() ([]compat.Problem, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil {
		return nil, errors.New("no pack loaded")
	}
	return a.compatProblems(a.ctx, pack), nil
}

func compatInputs(pack *resolve.Pack) []compatInput {
	var out []compatInput
	for _, m := range pack.Snapshot() {
		if m.Jar != nil {
			out = append(out, compatInput{name: m.FileName, path: m.Path, key: m.Jar.SHA1, jar: m.Jar})
		}
	}
	return out
}

func (a *App) compatProblems(ctx context.Context, pack *resolve.Pack) []compat.Problem {
	current := compatInputs(pack)
	previous := a.previousVersions(pack, current)
	h := sha1.New()
	fmt.Fprintln(h, pack.Root)
	for _, in := range append(slices.Clone(current), previous...) {
		fmt.Fprintln(h, in.name, in.key)
	}
	key := hex.EncodeToString(h.Sum(nil))

	a.compatMu.Lock()
	defer a.compatMu.Unlock()
	if key == a.compatKey {
		return slices.Clone(a.compatResult)
	}
	began := time.Now()
	cacheDir, _ := config.CacheDir()
	jars := compatLoad(current, cacheDir)
	old := map[string]*compat.Index{}
	for _, j := range compatLoad(previous, cacheDir) {
		old[j.Name] = j.Index
	}
	var list []compat.Jar
	for _, j := range jars {
		if j.Index != nil {
			list = append(list, j)
		}
	}
	var failed atomic.Bool
	env := a.compatEnv(ctx, pack.Root, &failed)
	problems := compat.Merge(compat.Check(list, env), compat.Replaced(list, old, env))
	if problems == nil {
		problems = []compat.Problem{}
	}
	logx.Printf("compat: %d problems in %v", len(problems), time.Since(began).Round(time.Millisecond))
	if ctx.Err() == nil && !failed.Load() {
		a.compatKey, a.compatResult = key, problems
	}
	if cacheDir != "" {
		compat.PruneCache(cacheDir, 30*24*time.Hour)
	}
	return slices.Clone(problems)
}

func compatLoad(inputs []compatInput, cacheDir string) []compat.Jar {
	jars := make([]compat.Jar, len(inputs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, in := range inputs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			x, err := compat.Load(in.path, in.key, cacheDir)
			if err != nil {
				logx.Printf("compat: %s not indexed: %v", in.path, err)
				return
			}
			jars[i] = compat.Jar{Name: in.name, Path: in.path, Index: x}
			if in.jar != nil {
				jars[i].ModIDs, jars[i].Requires, jars[i].DeclaresDeps = in.jar.ModIDs, in.jar.Requires, in.jar.DeclaresDeps
			}
		})
	}
	wg.Wait()
	return jars
}

func (a *App) previousVersions(pack *resolve.Pack, current []compatInput) []compatInput {
	dir, err := config.BackupDir()
	if err != nil {
		return nil
	}
	present := map[string]bool{}
	for _, in := range current {
		present[in.name] = true
	}
	seen := map[string]bool{}
	var out []compatInput
	for _, s := range backup.All(dir, pack.Root) {
		for _, it := range s.Pending() {
			if it.Added || it.Remote != "" || strings.HasPrefix(it.Key, server.KeyPrefix) || !present[it.NewFile] || seen[it.NewFile] {
				continue
			}
			path := s.Stored(it)
			st, err := os.Stat(path)
			if err != nil || st.IsDir() {
				continue
			}
			seen[it.NewFile] = true
			key := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().UnixNano())))
			out = append(out, compatInput{name: it.NewFile, path: path, key: hex.EncodeToString(key[:])})
		}
	}
	return out
}

func (a *App) compatWarnings(ctx context.Context, pack *resolve.Pack, done []install.Result) map[string]string {
	if len(done) == 0 {
		return nil
	}
	problems := slices.DeleteFunc(a.compatProblems(ctx, pack), func(p compat.Problem) bool { return !p.Severe })
	previous := map[string]compatInput{}
	for _, in := range a.previousVersions(pack, compatInputs(pack)) {
		previous[in.name] = in
	}
	cacheDir, _ := config.CacheDir()
	out := map[string]string{}
	for _, r := range done {
		own := problems
		if in, ok := previous[r.FileTo]; ok {
			x, err := compat.Load(in.path, in.key, cacheDir)
			j, jerr := jarinfo.Read(in.path)
			if err == nil && jerr == nil {
				own = compat.Introduced(problems, r.FileTo, compat.Jar{Name: r.FileTo, Index: x, ModIDs: j.ModIDs, Requires: j.Requires, DeclaresDeps: j.DeclaresDeps})
			}
		}
		if text := compat.Summary(r.FileTo, own); text != "" {
			out[r.ID] = text
			logx.Printf("compat: %s: %s", r.FileTo, text)
		}
	}
	return out
}

func (a *App) recheckOptions(root string) resolve.Options {
	cacheDir, _ := config.CacheDir()
	return resolve.Options{
		CurseForgeKey: a.store.CurseForgeKey(),
		CacheDir:      cacheDir,
		Skipped:       func(key string) bool { return a.store.Skipped(root, key) },
	}
}

func (a *App) FindMissing(modID string) (*resolve.Offer, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil {
		return nil, errors.New("no pack loaded")
	}
	offer, err := pack.Find(a.ctx, modID, a.recheckOptions(pack.Root))
	if err != nil {
		logx.Printf("find %s: %v", modID, err)
		return nil, err
	}
	a.mu.Lock()
	if a.offers == nil {
		a.offers = map[string]*resolve.Offer{}
	}
	a.offers[pack.Root+"|"+modID] = offer
	a.mu.Unlock()
	return offer, nil
}

func (a *App) InstallMissing(modID string) (*resolve.Mod, error) {
	a.mu.Lock()
	pack := a.pack
	var offer *resolve.Offer
	if pack != nil {
		offer = a.offers[pack.Root+"|"+modID]
	}
	a.mu.Unlock()
	if pack == nil {
		return nil, errors.New("no pack loaded")
	}
	if offer == nil {
		return nil, fmt.Errorf("%s was not found", modID)
	}
	dir, err := config.BackupDir()
	if err != nil {
		return nil, err
	}
	ctx, err := a.joinOp()
	if err != nil {
		return nil, err
	}
	defer a.leaveOp()
	path, err := install.Add(ctx, pack, modID, offer.Target, func(int) {})
	if err != nil {
		logx.Printf("%s: install failed: %v", modID, err)
		return nil, err
	}
	m := pack.Add(context.WithoutCancel(ctx), path, a.recheckOptions(pack.Root))
	a.post.Lock()
	defer a.post.Unlock()
	session := backup.Begin(dir, pack.Root)
	session.Add(backup.Item{Key: m.Key, Name: m.Name, Dir: pack.ModsDir, NewFile: m.FileName, To: m.Version, Added: true})
	if err := session.Save(); err != nil {
		logx.Printf("backup session not saved: %v", err)
	}
	return m, nil
}

func (a *App) Versions(id string) ([]resolve.Choice, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil {
		return nil, errors.New("no pack loaded")
	}
	return pack.Versions(a.ctx, id, a.store.CurseForgeKey())
}

func (a *App) VersionNotes(id, choice string) (string, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil {
		return "", errors.New("no pack loaded")
	}
	return pack.VersionNotes(a.ctx, id, choice, a.store.CurseForgeKey())
}

func (a *App) ChooseVersion(id, choice string) (*resolve.Mod, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil {
		return nil, errors.New("no pack loaded")
	}
	return pack.Choose(a.ctx, id, choice, a.store.CurseForgeKey())
}

func fillReport(report *Report, pack *resolve.Pack) {
	installed := map[string]bool{}
	for _, r := range report.Installed {
		installed[r.ID] = true
	}
	for _, r := range report.Failed {
		installed[r.ID] = true
	}
	for _, m := range pack.List() {
		switch {
		case installed[m.ID]:
		case m.Skipped:
			report.Skipped = append(report.Skipped, m.Name)
		case m.Status == resolve.StatusUpdate:
			report.Remaining = append(report.Remaining, m.Name)
		case m.Status == resolve.StatusCurrent:
			report.UpToDate++
		case m.Status == resolve.StatusManual || m.Status == resolve.StatusError:
			page := ""
			if m.Target != nil {
				page = m.Target.PageURL
			}
			report.Manual = append(report.Manual, Manual{ID: m.ID, Name: m.Name, Reason: m.Reason, PageURL: page})
		default:
			report.Unknown = append(report.Unknown, m.FileName)
		}
	}
}

func mergeServer(prev, next *ServerSync) *ServerSync {
	if prev == nil {
		return next
	}
	if next == nil {
		return prev
	}
	merged := &ServerSync{Root: next.Root, Remote: next.Remote, Results: append(append([]server.Result{}, prev.Results...), next.Results...), Error: next.Error}
	if merged.Error == "" && prev.Root == next.Root {
		merged.Error = prev.Error
	}
	return merged
}

type LastUpdate struct {
	Restorable []backup.Item `json:"restorable"`
}

func (a *App) packRoot() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pack != nil {
		return a.pack.Root
	}
	return a.store.Get().LastPack
}

func (a *App) LastUpdate() (*LastUpdate, error) {
	dir, err := config.BackupDir()
	if err != nil {
		return nil, err
	}
	all := backup.All(dir, a.packRoot())
	if len(all) == 0 {
		return nil, nil
	}
	last := &LastUpdate{}
	for _, s := range all {
		for _, it := range s.Pending() {
			it.Time = s.Time
			last.Restorable = append(last.Restorable, it)
		}
	}
	return last, nil
}

type UndoResult struct {
	Results []backup.Result    `json:"results"`
	Mods    []resolve.Replaced `json:"mods"`
}

func (a *App) Undo(ids []string) (*UndoResult, error) {
	dir, err := config.BackupDir()
	if err != nil {
		return nil, err
	}
	ctx, err := a.joinOp()
	if err != nil {
		return nil, err
	}
	defer a.leaveOp()
	if len(ids) == 0 {
		return nil, errors.New("nothing to undo")
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
		want[server.KeyPrefix+id] = true
	}
	a.post.Lock()
	var results []backup.Result
	for _, s := range backup.All(dir, a.packRoot()) {
		if a.session != nil && s.ID == a.session.ID {
			s = a.session
		}
		results = append(results, s.Restore(want)...)
	}
	a.post.Unlock()
	moved := map[string]string{}
	var removed []string
	for _, r := range results {
		logx.Printf("undo %s: %s -> %s, ok=%v %s", r.Name, r.From, r.To, r.OK, r.Error)
		switch {
		case !r.OK || strings.HasPrefix(r.Key, server.KeyPrefix):
		case r.Added:
			removed = append(removed, filepath.Join(r.Dir, r.NewFile))
		default:
			moved[filepath.Join(r.Dir, r.NewFile)] = filepath.Join(r.Dir, r.File)
		}
	}
	out := &UndoResult{Results: results}
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil || len(moved) == 0 && len(removed) == 0 {
		return out, nil
	}
	for _, path := range removed {
		if id := pack.Remove(path); id != "" {
			out.Mods = append(out.Mods, resolve.Replaced{OldID: id})
		}
	}
	if len(moved) > 0 {
		rechecked, err := pack.Recheck(context.WithoutCancel(ctx), moved, a.recheckOptions(pack.Root))
		if err != nil {
			logx.Printf("recheck after undo: %v", err)
		}
		out.Mods = append(out.Mods, rechecked...)
	}
	undone := map[string]bool{}
	for _, r := range out.Mods {
		undone[r.OldID] = true
	}
	a.post.Lock()
	defer a.post.Unlock()
	if a.report != nil {
		kept := a.report.Installed[:0:0]
		for _, x := range a.report.Installed {
			if !undone[x.ID] {
				kept = append(kept, x)
			}
		}
		a.report.Installed = kept
	}
	return out, nil
}

func (a *App) syncServer(ctx context.Context, client []server.Mod, mcVersion string, session *backup.Session, keys map[string]bool) *ServerSync {
	root := a.serverRoot()
	if root == "" {
		return nil
	}
	out := &ServerSync{Root: root, Remote: fsx.IsRemote(root)}
	diff, err := server.Compare(client, mcVersion, root)
	if err == nil {
		if keys != nil {
			diff.Only(keys)
		}
		if len(diff.Items) == 0 {
			return nil
		}
		out.Results, err = server.Sync(ctx, diff, session, mcVersion)
	}
	if err != nil {
		out.Error = err.Error()
		logx.Printf("server sync: %v", err)
	}
	return out
}

func (a *App) ServerBehind() (int, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	root := a.serverRoot()
	if pack == nil || root == "" {
		return 0, nil
	}
	diff, err := server.Compare(server.Snapshot(pack), pack.MCVersion, root)
	if err != nil {
		return 0, err
	}
	return len(diff.Items), nil
}

func (a *App) SyncServer() (*ServerSync, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil {
		return nil, errors.New("no pack loaded")
	}
	ctx, err := a.begin()
	if err != nil {
		return nil, err
	}
	defer a.end()
	dir, err := config.BackupDir()
	if err != nil {
		return nil, err
	}
	session := backup.Begin(dir, pack.Root)
	out := a.syncServer(ctx, server.Snapshot(pack), pack.MCVersion, session, nil)
	if err := session.Save(); err != nil {
		logx.Printf("backup session not saved: %v", err)
	}
	if out == nil {
		return nil, errors.New("no server folder")
	}
	return out, nil
}

func (a *App) Changelog(id string) (string, error) {
	a.mu.Lock()
	pack := a.pack
	a.mu.Unlock()
	if pack == nil {
		return "", errors.New("no pack loaded")
	}
	return pack.Changelog(a.ctx, id, a.store.CurseForgeKey())
}

func (a *App) CheckAppUpdate() (string, error) {
	rel, err := selfupdate.Latest(a.ctx, Version)
	if err != nil {
		logx.Printf("selfupdate: %v", err)
		return "", err
	}
	a.mu.Lock()
	a.appUpdate = rel
	a.mu.Unlock()
	if rel == nil {
		return "", nil
	}
	return rel.Version, nil
}

func (a *App) ApplyAppUpdate() error {
	a.mu.Lock()
	rel := a.appUpdate
	busy := a.cancel != nil || a.ops > 0
	a.mu.Unlock()
	if rel == nil {
		return errors.New("no update available")
	}
	if busy {
		return errors.New("wait until the current task finishes")
	}
	if err := selfupdate.Install(a.ctx, rel); err != nil {
		logx.Printf("selfupdate: %v", err)
		return err
	}
	if err := selfupdate.Relaunch(); err != nil {
		return fmt.Errorf("modhound %s is installed, restart it manually: %w", rel.Version, err)
	}
	runtime.Quit(a.ctx)
	return nil
}

func (a *App) LogFrontend(message string) {
	logx.Printf("ui error: %s", message)
}

func (a *App) OpenURL(link string) {
	if strings.HasPrefix(link, "https://") || strings.HasPrefix(link, "http://") {
		runtime.BrowserOpenURL(a.ctx, link)
	}
}

func (a *App) OpenReport(path string) {
	dir, err := config.ReportDir()
	if err != nil {
		return
	}
	rel, err := filepath.Rel(dir, filepath.Clean(path))
	if err != nil || !filepath.IsLocal(rel) {
		return
	}
	opener.Open(filepath.Clean(path))
}

func saveReport(r *Report) (string, error) {
	dir, err := config.ReportDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "modhound %s report, %s\n%s\n", Version, r.Time, r.Pack)
	fmt.Fprintf(&b, "\nUpdated (%d)\n", len(r.Installed))
	for _, x := range r.Installed {
		fmt.Fprintf(&b, "  %s: %s -> %s\n", x.Name, x.FileFrom, x.FileTo)
		if x.Warning != "" {
			fmt.Fprintf(&b, "    warning: %s\n", x.Warning)
		}
	}
	fmt.Fprintf(&b, "\nFailed (%d)\n", len(r.Failed))
	for _, x := range r.Failed {
		fmt.Fprintf(&b, "  %s: %s -> %s: %s\n", x.Name, x.FileFrom, x.FileTo, x.Error)
	}
	fmt.Fprintf(&b, "\nNeeds attention (%d)\n", len(r.Manual))
	for _, x := range r.Manual {
		fmt.Fprintf(&b, "  %s: %s %s\n", x.Name, x.Reason, x.PageURL)
	}
	fmt.Fprintf(&b, "\nNot selected (%d)\n", len(r.Remaining))
	for _, x := range r.Remaining {
		fmt.Fprintf(&b, "  %s\n", x)
	}
	fmt.Fprintf(&b, "\nSkipped (%d)\n", len(r.Skipped))
	for _, x := range r.Skipped {
		fmt.Fprintf(&b, "  %s\n", x)
	}
	fmt.Fprintf(&b, "\nNot found (%d)\n", len(r.Unknown))
	for _, x := range r.Unknown {
		fmt.Fprintf(&b, "  %s\n", x)
	}
	fmt.Fprintf(&b, "\nUp to date: %d\n", r.UpToDate)
	if r.Server != nil {
		fmt.Fprintf(&b, "\nServer %s\n", r.Server.Root)
		if r.Server.Error != "" {
			fmt.Fprintf(&b, "  %s\n", r.Server.Error)
		}
		for _, x := range r.Server.Results {
			if x.OK {
				fmt.Fprintf(&b, "  %s: %s\n", x.Name, x.File)
			} else {
				fmt.Fprintf(&b, "  %s: %s: %s\n", x.Name, x.File, x.Error)
			}
		}
	}
	path := r.Path
	if path == "" {
		path = filepath.Join(dir, "report-"+time.Now().Format("2006-01-02_15-04-05")+".txt")
	}
	return path, os.WriteFile(path, []byte(b.String()), 0o644)
}
