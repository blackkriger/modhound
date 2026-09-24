package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/blackkriger/modhound/internal/backup"
	"github.com/blackkriger/modhound/internal/install"
	"github.com/blackkriger/modhound/internal/jarinfo"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/resolve"
)

const KeyPrefix = "server:"

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

	remote bool
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

type serverJar struct {
	path string
	jar  *jarinfo.Jar
}

func Compare(client *resolve.Pack, root string) (*Diff, error) {
	if IsRemote(root) {
		return compareRemote(client, root)
	}
	modsDir, err := resolve.FindModsDir(root)
	if err != nil {
		return nil, err
	}
	jars := readJars(modsDir)
	byStem := map[string]*resolve.Mod{}
	for _, m := range client.Mods {
		if m.Jar == nil {
			continue
		}
		if stem := resolve.Stem(m.FileName); stem != "" {
			byStem[stem] = m
		}
	}
	diff := &Diff{Root: root, Mods: len(jars)}
	for _, s := range jars {
		m := byStem[resolve.Stem(filepath.Base(s.path))]
		if m == nil || m.Jar.SHA1 == s.jar.SHA1 || !sameMod(m.Jar, s.jar) || !resolve.OlderVersion(filepath.Base(s.path), m.FileName, client.MCVersion) {
			continue
		}
		diff.Items = append(diff.Items, Item{Name: m.Name, ClientFile: m.FileName, ServerFile: filepath.Base(s.path), clientPath: m.Path, serverPath: s.path, key: m.Key})
	}
	logx.Printf("server %s: %d mods, %d differ from the client", root, diff.Mods, len(diff.Items))
	return diff, nil
}

func sameMod(a, b *jarinfo.Jar) bool {
	if len(a.ModIDs) == 0 || len(b.ModIDs) == 0 {
		return true
	}
	return strings.EqualFold(a.ModIDs[0], b.ModIDs[0])
}

func readJars(modsDir string) []serverJar {
	var paths []string
	add := func(dir string) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if !e.IsDir() && (strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".zip")) {
				paths = append(paths, filepath.Join(dir, e.Name()))
			}
		}
	}
	add(modsDir)
	entries, _ := os.ReadDir(modsDir)
	for _, e := range entries {
		if e.IsDir() && isVersionDir(e.Name()) {
			add(filepath.Join(modsDir, e.Name()))
		}
	}
	out := make([]serverJar, len(paths))
	sem := make(chan struct{}, min(runtime.NumCPU(), 8))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			j, err := jarinfo.Read(p)
			if err == nil {
				out[i] = serverJar{path: p, jar: j}
			}
		}()
	}
	wg.Wait()
	kept := out[:0]
	for _, s := range out {
		if s.jar != nil {
			kept = append(kept, s)
		}
	}
	return kept
}

func isVersionDir(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}

var ErrInUse = errors.New("server mod files are in use")

func Sync(diff *Diff, session *backup.Session, mcVersion string) ([]Result, error) {
	if diff.remote {
		return syncRemote(diff, session, mcVersion)
	}
	for _, it := range diff.Items {
		if install.InUse(it.serverPath) {
			logx.Printf("%s is in use by another program", it.serverPath)
			return nil, ErrInUse
		}
	}
	var results []Result
	for _, it := range diff.Items {
		res := Result{Name: it.Name, File: it.ClientFile}
		if err := syncOne(session, it, mcVersion); err != nil {
			res.Error = err.Error()
		} else {
			res.OK = true
		}
		logx.Printf("server sync %s: %s -> %s, ok=%v %s", it.Name, it.ServerFile, it.ClientFile, res.OK, res.Error)
		results = append(results, res)
	}
	return results, nil
}

func syncOne(session *backup.Session, it Item, mcVersion string) error {
	dir := filepath.Dir(it.serverPath)
	dest := filepath.Join(dir, it.ClientFile)
	if !strings.EqualFold(dest, it.serverPath) {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("%s already exists on the server", it.ClientFile)
		}
	}
	part := filepath.Join(dir, ".modhound-sync.part")
	defer os.Remove(part)
	if err := copyFile(it.clientPath, part); err != nil {
		return err
	}
	item := backup.Item{Key: KeyPrefix + it.key, Name: it.Name, Dir: dir, OldFile: it.ServerFile, NewFile: it.ClientFile, From: resolve.FileVersion(it.ServerFile, mcVersion), To: resolve.FileVersion(it.ClientFile, mcVersion)}
	undo, err := session.Keep(it.serverPath, &item)
	if err != nil {
		return fmt.Errorf("cannot move the old server file: %w", err)
	}
	if err := os.Rename(part, dest); err != nil {
		if undoErr := undo(); undoErr != nil {
			session.Add(item)
			return fmt.Errorf("cannot place the new file, the old one is kept in the backup: %w", err)
		}
		return fmt.Errorf("cannot place the new file: %w", err)
	}
	session.Add(item)
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
