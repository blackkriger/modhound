package config

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

type Pack struct {
	Skip      []string `json:"skip"`
	Server    *string  `json:"server,omitempty"`
	ServerKey string   `json:"serverKey,omitempty"`
}

type Config struct {
	CurseForgeKey string            `json:"curseforgeKey,omitempty"`
	Theme         string            `json:"theme,omitempty"`
	Debug         bool              `json:"debug,omitempty"`
	LastPack      string            `json:"lastPack,omitempty"`
	Sort          map[string]string `json:"sort,omitempty"`
	Packs         map[string]*Pack  `json:"packs"`
}

type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "modhound"), nil
}

func LocalDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "modhound"), nil
}

func localSub(name string) (string, error) {
	dir, err := LocalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func BackupDir() (string, error) { return localSub("backups") }

func LogDir() (string, error) { return localSub("logs") }

func ReportDir() (string, error) { return localSub("reports") }

func CacheDir() (string, error) { return localSub("cache") }

func Migrate() error {
	roaming, err := Dir()
	if err != nil {
		return err
	}
	local, err := LocalDir()
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range []string{"backups", "reports", "logs"} {
		errs = append(errs, moveTree(filepath.Join(roaming, name), filepath.Join(local, name)))
	}
	for _, name := range []string{"gtnh-assets.json", "gtnh-assets.json.etag"} {
		errs = append(errs, moveTree(filepath.Join(local, name), filepath.Join(local, "cache", name)))
	}
	return errors.Join(errs...)
}

func moveTree(src, dst string) error {
	info, err := os.Stat(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.Rename(src, dst)
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		errs = append(errs, moveTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())))
	}
	os.Remove(src)
	return errors.Join(errs...)
}

func Open() (*Store, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "config.json")}
	if data, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(data, &s.cfg); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.cfg.Packs == nil {
		s.cfg.Packs = map[string]*Pack{}
	}
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cfg
	c.Sort = maps.Clone(s.cfg.Sort)
	c.Packs = make(map[string]*Pack, len(s.cfg.Packs))
	for k, p := range s.cfg.Packs {
		cp := *p
		cp.Skip = slices.Clone(p.Skip)
		c.Packs[k] = &cp
	}
	return c
}

func (s *Store) CurseForgeKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.CurseForgeKey
}

func (s *Store) Update(fn func(c *Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	return s.save()
}

func (s *Store) Skipped(pack, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.cfg.Packs[pack]
	return p != nil && slices.Contains(p.Skip, key)
}

func (s *Store) SetSkipped(pack, key string, skip bool) error {
	return s.Update(func(c *Config) {
		p := c.Packs[pack]
		if p == nil {
			p = &Pack{}
			c.Packs[pack] = p
		}
		p.Skip = slices.DeleteFunc(p.Skip, func(k string) bool { return k == key })
		if skip {
			p.Skip = append(p.Skip, key)
			slices.Sort(p.Skip)
		}
	})
}

func (s *Store) Server(pack string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.cfg.Packs[pack]
	if p == nil || p.Server == nil {
		return "", false
	}
	return *p.Server, true
}

func (s *Store) SetServer(pack, dir string) error {
	return s.Update(func(c *Config) {
		p := c.Packs[pack]
		if p == nil {
			p = &Pack{}
			c.Packs[pack] = p
		}
		p.Server = &dir
	})
}

func (s *Store) ServerKey(pack string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.cfg.Packs[pack]; p != nil {
		return p.ServerKey
	}
	return ""
}

func (s *Store) SetServerKey(pack, key string) error {
	return s.Update(func(c *Config) {
		p := c.Packs[pack]
		if p == nil {
			p = &Pack{}
			c.Packs[pack] = p
		}
		p.ServerKey = key
	})
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
