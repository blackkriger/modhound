package backup

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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

func packDir(root, pack string) string {
	sum := sha1.Sum([]byte(strings.ToLower(filepath.Clean(pack))))
	return filepath.Join(root, hex.EncodeToString(sum[:6]))
}

func Begin(root, pack string) *Session {
	now := time.Now()
	id := now.Format("2006-01-02_15-04-05.000")
	return &Session{ID: id, Time: now.Format(time.DateTime), Pack: pack, dir: filepath.Join(packDir(root, pack), id)}
}

func (s *Session) Keep(oldPath string, item *Item) (func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	stored := filepath.Join(s.dir, item.OldFile)
	for n := 1; ; n++ {
		if _, err := os.Stat(stored); err != nil {
			break
		}
		stored = filepath.Join(s.dir, fmt.Sprintf("%d-%s", n, item.OldFile))
	}
	if err := move(oldPath, stored); err != nil {
		return nil, err
	}
	item.Stored = filepath.Base(stored)
	return func() error { return move(stored, oldPath) }, nil
}

func (s *Session) Add(item Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Items = append(s.Items, item)
	s.write()
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

func (s *Session) MarkRestored(key, newFile string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Items {
		if s.Items[i].Key == key && s.Items[i].NewFile == newFile {
			s.Items[i].Restored = true
		}
	}
}

func prune(dir, newest string) {
	sessions := load(dir)
	taken := map[string]bool{}
	for _, s := range sessions {
		if s.ID == newest {
			for _, it := range s.Items {
				taken[it.Key] = true
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
			if it.Restored || taken[it.Key] {
				os.Remove(filepath.Join(s.dir, it.Stored))
				changed = true
				continue
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

func move(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		in.Close()
		return err
	}
	_, err = io.Copy(out, in)
	in.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return err
	}
	if err := os.Remove(src); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

func (s *Session) Restore(keys map[string]bool) []Result {
	s.mu.Lock()
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
	if err := s.write(); err != nil {
		for i := range results {
			if results[i].OK {
				results[i].Error = "restored, but the backup record was not updated: " + err.Error()
			}
		}
	}
	s.mu.Unlock()
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
	stored := filepath.Join(s.dir, it.Stored)
	if _, err := os.Stat(stored); err != nil {
		return errors.New("the saved copy is missing")
	}
	current := filepath.Join(it.Dir, it.NewFile)
	original := filepath.Join(it.Dir, it.OldFile)
	if !strings.EqualFold(current, original) {
		if _, err := os.Stat(original); err == nil {
			return fmt.Errorf("%s already exists", filepath.Base(original))
		}
	}
	aside := current + ".modhound-undo"
	if _, err := os.Stat(current); err == nil {
		if err := os.Rename(current, aside); err != nil {
			return fmt.Errorf("cannot remove the new file (is the game running?): %w", err)
		}
	}
	if err := move(stored, original); err != nil {
		os.Rename(aside, current)
		return fmt.Errorf("cannot put the old file back: %w", err)
	}
	os.Remove(aside)
	return nil
}
