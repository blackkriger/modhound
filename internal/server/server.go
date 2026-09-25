package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
}

func Snapshot(pack *resolve.Pack) []Mod {
	mods := pack.Snapshot()
	out := make([]Mod, 0, len(mods))
	for _, m := range mods {
		if m.Jar != nil {
			mod := Mod{Name: m.Name, FileName: m.FileName, Path: m.Path, Key: m.Key}
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
	for _, p := range files {
		name := f.Base(p)
		m, ok := byStem[resolve.Stem(name)]
		if !ok || strings.EqualFold(name, m.FileName) || !resolve.OlderVersion(name, m.FileName, mcVersion) || !sameMod(f, p, m) {
			continue
		}
		diff.Items = append(diff.Items, Item{Name: m.Name, ClientFile: m.FileName, ServerFile: name, clientPath: m.Path, serverPath: p, key: m.Key})
	}
	logx.Printf("server %s: %d mods, %d behind the client", root, diff.Mods, len(diff.Items))
	return diff, nil
}

func sameMod(f fsx.FS, serverPath string, m Mod) bool {
	if f.Remote() != "" || m.ModID == "" {
		return true
	}
	j, err := jarinfo.Read(serverPath)
	if err != nil || len(j.ModIDs) == 0 {
		return true
	}
	if !strings.EqualFold(j.ModIDs[0], m.ModID) {
		logx.Printf("server %s: %s is %s, not %s", serverPath, f.Base(serverPath), j.ModIDs[0], m.ModID)
		return false
	}
	return true
}

var ErrInUse = errors.New("server mod files are in use")

func Sync(ctx context.Context, diff *Diff, session *backup.Session, mcVersion string) ([]Result, error) {
	f := diff.files
	if f.Remote() == "" {
		for _, it := range diff.Items {
			if install.InUse(it.serverPath) {
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
	if !strings.EqualFold(dest, it.serverPath) && f.Exists(dest) {
		return fmt.Errorf("%s already exists on the server", it.ClientFile)
	}
	part := f.Join(dir, ".modhound-sync.part")
	if err := f.Upload(it.clientPath, part); err != nil {
		f.Remove(part)
		return fmt.Errorf("cannot copy %s to the server: %w", it.ClientFile, err)
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
