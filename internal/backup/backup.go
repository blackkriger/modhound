package backup

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blackkriger/modhound/internal/fsx"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/resolve"
)

type Item struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	OldFile  string `json:"oldFile"`
	Stored   string `json:"stored"`
	NewFile  string `json:"newFile"`
	From     string `json:"from"`
	To       string `json:"to"`
	Restored bool   `json:"restored"`
	Time     string `json:"time,omitempty"`
	Remote   string `json:"remote,omitempty"`
}

func (it Item) files() (fsx.FS, error) {
	if it.Remote == "" {
		return fsx.Local, nil
	}
	f, _, err := fsx.Open(it.Remote)
	return f, err
}

func (it Item) chain(file string) string {
	return it.Remote + "|" + strings.ToLower(it.Dir) + "|" + strings.ToLower(file)
}

type Session struct {
	ID    string `json:"id"`
	Time  string `json:"time"`
	Pack  string `json:"pack"`
	Items []Item `json:"items"`

	dir string
	mu  sync.Mutex
}

func (s *Session) Pending() []Item {
	var out []Item
	for _, it := range s.Items {
		if !it.Restored {
			out = append(out, it)
		}
	}
	return out
}

func (s *Session) storedPath(it Item) string {
	if it.Remote != "" {
		return it.Stored
	}
	return filepath.Join(s.dir, it.Stored)
}

func packDir(root, pack string) string {
	sum := sha1.Sum([]byte(strings.ToLower(filepath.Clean(pack))))
	return filepath.Join(root, hex.EncodeToString(sum[:6]))
}

func Begin(root, pack string) *Session {
	now := time.Now()
	id := now.Format("2006-01-02_15-04-05.000")
	return &Session{ID: id, Time: now.Format(time.DateTime), Pack: pack, dir: filepath.Join(packDir(root, pack), id)}
}

func (s *Session) Keep(f fsx.FS, oldPath, store string, item *Item) (func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if store == "" {
		store = s.dir
	}
	if err := f.MkdirAll(store); err != nil {
		return nil, err
	}
	stored := f.Join(store, item.OldFile)
	for n := 1; f.Exists(stored); n++ {
		stored = f.Join(store, fmt.Sprintf("%d-%s", n, item.OldFile))
	}
	if err := f.Rename(oldPath, stored); err != nil {
		return nil, err
	}
	item.Remote = f.Remote()
	item.Stored = stored
	if item.Remote == "" {
		item.Stored = filepath.Base(stored)
	}
	return func() error { return f.Rename(stored, oldPath) }, nil
}

func (s *Session) Add(item Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Items = append(s.Items, item)
	if err := s.write(); err != nil {
		logx.Printf("backup record of %s not written: %v", item.OldFile, err)
	}
}

func (s *Session) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Items) == 0 {
		os.RemoveAll(s.dir)
		return nil
	}
	if err := s.write(); err != nil {
		return err
	}
	prune(filepath.Dir(s.dir), s.ID)
	return nil
}

func (s *Session) write() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, "session.json")
	if err := os.WriteFile(path+".tmp", data, 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func prune(dir, newest string) {
	sessions := load(dir)
	replaced := map[string]bool{}
	for _, s := range sessions {
		if s.ID == newest {
			for _, it := range s.Items {
				replaced[it.chain(it.OldFile)] = true
			}
		}
	}
	for _, s := range sessions {
		if s.ID == newest {
			continue
		}
		changed := false
		kept := s.Items[:0]
		for _, it := range s.Items {
			if it.Restored {
				changed = true
				continue
			}
			if replaced[it.chain(it.NewFile)] {
				if err := s.discard(it); err != nil {
					logx.Printf("old backup %s not removed, kept for later: %v", it.Stored, err)
				} else {
					changed = true
					continue
				}
			}
			kept = append(kept, it)
		}
		s.Items = kept
		if len(s.Items) == 0 {
			os.RemoveAll(s.dir)
		} else if changed {
			s.write()
		}
	}
}

func (s *Session) discard(it Item) error {
	f, err := it.files()
	if err != nil {
		return err
	}
	stored := s.storedPath(it)
	if !f.Exists(stored) {
		return nil
	}
	if err := f.Remove(stored); err != nil {
		return err
	}
	if it.Remote != "" {
		f.RemoveEmptyDir(f.Dir(stored))
	}
	return nil
}

func load(dir string) []*Session {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []*Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name(), "session.json"))
		if err != nil {
			continue
		}
		s := &Session{dir: filepath.Join(dir, e.Name())}
		if json.Unmarshal(data, s) == nil {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func All(root, pack string) []*Session {
	var out []*Session
	for _, s := range load(packDir(root, pack)) {
		if len(s.Pending()) > 0 {
			out = append(out, s)
		}
	}
	return out
}

func (s *Session) Restore(keys map[string]bool) []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	var results []Result
	for i := range s.Items {
		it := &s.Items[i]
		if it.Restored || (keys != nil && !keys[it.Key+"|"+it.NewFile]) {
			continue
		}
		res := Result{Key: it.Key, Name: it.Name, From: it.To, To: it.From, File: it.OldFile, Dir: it.Dir, NewFile: it.NewFile}
		if err := s.restore(*it); err != nil {
			res.Error = err.Error()
		} else {
			it.Restored = true
			res.OK = true
		}
		results = append(results, res)
	}
	if len(results) == 0 {
		return nil
	}
	if err := s.write(); err != nil {
		for i := range results {
			if results[i].OK {
				results[i].Error = "restored, but the backup record was not updated: " + err.Error()
			}
		}
	}
	return results
}

type Result struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	From  string `json:"from"`
	To    string `json:"to"`
	File  string `json:"file"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`

	Dir     string `json:"-"`
	NewFile string `json:"-"`
}

func (s *Session) restore(it Item) error {
	f, err := it.files()
	if err != nil {
		return err
	}
	stored := s.storedPath(it)
	if !f.Exists(stored) {
		return errors.New("the saved copy is missing")
	}
	current := f.Join(it.Dir, it.NewFile)
	original := f.Join(it.Dir, it.OldFile)
	if !strings.EqualFold(current, original) && f.Exists(original) {
		return fmt.Errorf("%s already exists", it.OldFile)
	}
	if !f.Exists(current) {
		if other := otherVersion(f, it); other != "" {
			return fmt.Errorf("%s is in the folder now, restoring %s would leave two versions of the mod", other, it.OldFile)
		}
	}
	aside := current + ".modhound-undo"
	if f.Exists(current) {
		if err := f.Rename(current, aside); err != nil {
			return fmt.Errorf("cannot remove the new file (is the game running?): %w", err)
		}
	}
	if err := f.Rename(stored, original); err != nil {
		f.Rename(aside, current)
		return fmt.Errorf("cannot put the old file back: %w", err)
	}
	f.Remove(aside)
	if it.Remote != "" {
		f.RemoveEmptyDir(f.Dir(stored))
	}
	return nil
}

func otherVersion(f fsx.FS, it Item) string {
	entries, err := f.ReadDir(it.Dir)
	if err != nil {
		return ""
	}
	stem := resolve.Stem(it.OldFile)
	for _, e := range entries {
		if !e.Dir && fsx.IsJar(e.Name) && !strings.EqualFold(e.Name, it.OldFile) && resolve.Stem(e.Name) == stem {
			return e.Name
		}
	}
	return ""
}
