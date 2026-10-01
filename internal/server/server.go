package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/blackkriger/modhound/internal/backup"
	"github.com/blackkriger/modhound/internal/fsx"
	"github.com/blackkriger/modhound/internal/install"
	"github.com/blackkriger/modhound/internal/jarinfo"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/resolve"
)

const KeyPrefix = "server:"

type Mod struct {
	Name     string
	FileName string
	Path     string
	Key      string
	ModID    string
	Side     string
}

const (
	SideClient = "client"
	SideServer = "server"
	SideBoth   = "both"
)

func Snapshot(pack *resolve.Pack) []Mod {
	mods := pack.Snapshot()
	out := make([]Mod, 0, len(mods))
	for _, m := range mods {
		if m.Jar != nil {
			mod := Mod{Name: m.Name, FileName: m.FileName, Path: m.Path, Key: m.Key, Side: m.Side}
			if len(m.Jar.ModIDs) > 0 {
				mod.ModID = m.Jar.ModIDs[0]
			}
			out = append(out, mod)
		}
	}
	return out
}

type Item struct {
	Name       string `json:"name"`
	ClientFile string `json:"clientFile"`
	ServerFile string `json:"serverFile"`

	clientPath string
	serverPath string
	key        string
	add        bool
}

type Diff struct {
	Root  string `json:"root"`
	Mods  int    `json:"mods"`
	Items []Item `json:"items"`

	files   fsx.FS
	modsDir string
}

func (d *Diff) Only(keys map[string]bool) {
	kept := d.Items[:0]
	for _, it := range d.Items {
		if keys[it.key] {
			kept = append(kept, it)
		}
	}
	d.Items = kept
}

type Result struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func Compare(client []Mod, mcVersion, root string) (*Diff, error) {
	f, base, err := fsx.Open(root)
	if err != nil {
		return nil, err
	}
	modsDir, err := fsx.ModsDir(f, base)
	if err != nil {
		return nil, err
	}
	files, err := fsx.ListJars(f, modsDir)
	if err != nil {
		return nil, err
	}
	byStem := map[string]Mod{}
	for _, m := range client {
		if stem := resolve.Stem(m.FileName); stem != "" {
			byStem[stem] = m
		}
	}
	diff := &Diff{Root: root, Mods: len(files), files: f, modsDir: modsDir}
	onServer := map[string]bool{}
	for _, p := range files {
		name := f.Base(p)
		onServer[resolve.Stem(name)] = true
		onServer["word:"+firstWord(name)] = true
		m, ok := byStem[resolve.Stem(name)]
		if !ok || m.Side == SideClient || strings.EqualFold(name, m.FileName) || !resolve.OlderVersion(name, m.FileName, mcVersion) || !sameMod(f, p, m) {
			continue
		}
		diff.Items = append(diff.Items, Item{Name: m.Name, ClientFile: m.FileName, ServerFile: name, clientPath: m.Path, serverPath: p, key: m.Key})
	}
	var adds []Mod
	for _, m := range client {
		if (m.Side == SideBoth || m.Side == SideServer) && !onServer[resolve.Stem(m.FileName)] {
			adds = append(adds, m)
		}
	}
	if len(adds) > 0 && f.Remote() == "" {
		for _, p := range files {
			for _, id := range localModIDs(p) {
				onServer["modid:"+strings.ToLower(id)] = true
			}
		}
	}
	for _, m := range adds {
		if m.ModID != "" && onServer["modid:"+strings.ToLower(m.ModID)] || f.Remote() != "" && onServer["word:"+firstWord(m.FileName)] {
			continue
		}
		diff.Items = append(diff.Items, Item{Name: m.Name, ClientFile: m.FileName, clientPath: m.Path, serverPath: f.Join(modsDir, m.FileName), key: m.Key, add: true})
	}
	logx.Printf("server %s: %d mods, %d behind the client", root, diff.Mods, len(diff.Items))
	return diff, nil
}

type cachedIDs struct {
	size  int64
	mtime time.Time
	ids   []string
}

var modIDCache sync.Map

func localModIDs(p string) []string {
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	if c, ok := modIDCache.Load(p); ok && c.(cachedIDs).size == st.Size() && c.(cachedIDs).mtime.Equal(st.ModTime()) {
		return c.(cachedIDs).ids
	}
	ids, _ := jarinfo.ModIDs(p)
	modIDCache.Store(p, cachedIDs{size: st.Size(), mtime: st.ModTime(), ids: ids})
	return ids
}

func firstWord(fileName string) string {
	words := strings.Fields(resolve.Stem(fileName))
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

func sameMod(f fsx.FS, serverPath string, m Mod) bool {
	if f.Remote() != "" || m.ModID == "" {
		return true
	}
	ids := localModIDs(serverPath)
	if len(ids) == 0 {
		return true
	}
	if !strings.EqualFold(ids[0], m.ModID) {
		logx.Printf("server %s: %s is %s, not %s", serverPath, f.Base(serverPath), ids[0], m.ModID)
		return false
	}
	return true
}

var ErrInUse = errors.New("server mod files are in use")

func Sync(ctx context.Context, diff *Diff, session *backup.Session, mcVersion string) ([]Result, error) {
	f := diff.files
	if f.Remote() == "" {
		for _, it := range diff.Items {
			if !it.add && install.InUse(it.serverPath) {
				logx.Printf("%s is in use by another program", it.serverPath)
				return nil, ErrInUse
			}
		}
	}
	store := ""
	if f.Remote() != "" {
		store = f.Join(f.Dir(diff.modsDir), "modhound-backups", session.ID)
	}
	var results []Result
	for _, it := range diff.Items {
		res := Result{Name: it.Name, File: it.ClientFile}
		err := ctx.Err()
		if err == nil {
			err = syncOne(f, store, session, it, mcVersion)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				err = errors.New("stopped")
			}
			res.Error = err.Error()
		} else {
			res.OK = true
		}
		logx.Printf("server sync %s: %s -> %s, ok=%v %s", it.Name, it.ServerFile, it.ClientFile, res.OK, res.Error)
		results = append(results, res)
	}
	return results, nil
}

func syncOne(f fsx.FS, store string, session *backup.Session, it Item, mcVersion string) error {
	dir := f.Dir(it.serverPath)
	dest := f.Join(dir, it.ClientFile)
	if (it.add || !strings.EqualFold(dest, it.serverPath)) && f.Exists(dest) {
		return fmt.Errorf("%s already exists on the server", it.ClientFile)
	}
	part := f.Join(dir, ".modhound-sync.part")
	if err := f.Upload(it.clientPath, part); err != nil {
		f.Remove(part)
		return fmt.Errorf("cannot copy %s to the server: %w", it.ClientFile, err)
	}
	if it.add {
		if err := f.Rename(part, dest); err != nil {
			f.Remove(part)
			return fmt.Errorf("cannot place %s on the server: %w", it.ClientFile, err)
		}
		session.Add(backup.Item{Key: KeyPrefix + it.key, Name: it.Name, Dir: dir, NewFile: it.ClientFile, To: resolve.FileVersion(it.ClientFile, mcVersion), Added: true, Remote: f.Remote()})
		return nil
	}
	item := backup.Item{Key: KeyPrefix + it.key, Name: it.Name, Dir: dir, OldFile: it.ServerFile, NewFile: it.ClientFile, From: resolve.FileVersion(it.ServerFile, mcVersion), To: resolve.FileVersion(it.ClientFile, mcVersion)}
	undo, err := session.Keep(f, it.serverPath, store, &item)
	if err != nil {
		f.Remove(part)
		return fmt.Errorf("cannot move the old server file: %w", err)
	}
	if err := f.Rename(part, dest); err != nil {
		if undoErr := undo(); undoErr != nil {
			session.Add(item)
			return fmt.Errorf("cannot place the new file, the old one is kept in the backup: %w", err)
		}
		f.Remove(part)
		return fmt.Errorf("cannot place the new file: %w", err)
	}
	session.Add(item)
	return nil
}

func Remove(ctx context.Context, root string, mods, remaining []Mod, session *backup.Session, mcVersion string) ([]Result, error) {
	f, base, err := fsx.Open(root)
	if err != nil {
		return nil, err
	}
	modsDir, err := fsx.ModsDir(f, base)
	if err != nil {
		return nil, err
	}
	files, err := fsx.ListJars(f, modsDir)
	if err != nil {
		return nil, err
	}
	store := ""
	if f.Remote() != "" {
		store = f.Join(f.Dir(modsDir), "modhound-backups", session.ID)
	}
	kept := map[string]bool{}
	for _, m := range remaining {
		kept[resolve.Stem(m.FileName)] = true
		if m.ModID != "" {
			kept["modid:"+strings.ToLower(m.ModID)] = true
		}
	}
	var results []Result
	for _, m := range mods {
		var targets []string
		for _, p := range files {
			if strings.EqualFold(f.Base(p), m.FileName) {
				targets = []string{p}
				break
			}
		}
		stem := resolve.Stem(m.FileName)
		if targets == nil && stem != "" && !kept[stem] && (m.ModID == "" || !kept["modid:"+strings.ToLower(m.ModID)]) {
			for _, p := range files {
				if resolve.Stem(f.Base(p)) == stem && sameMod(f, p, m) {
					targets = append(targets, p)
				}
			}
		}
		for _, p := range targets {
			name := f.Base(p)
			res := Result{Name: m.Name, File: name}
			err := ctx.Err()
			if err == nil && f.Remote() == "" && install.InUse(p) {
				err = ErrInUse
			}
			if err == nil {
				item := backup.Item{Key: KeyPrefix + m.Key, Name: m.Name, Dir: f.Dir(p), OldFile: name, From: resolve.FileVersion(name, mcVersion), Removed: true, Pair: backup.RemovedKey(m.Key, m.FileName)}
				if _, err = session.Keep(f, p, store, &item); err == nil {
					session.Add(item)
				}
			}
			if err != nil {
				res.Error = err.Error()
			} else {
				res.OK = true
			}
			logx.Printf("server remove %s: ok=%v %s", name, res.OK, res.Error)
			results = append(results, res)
		}
	}
	return results, nil
}
