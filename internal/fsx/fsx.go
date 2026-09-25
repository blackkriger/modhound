package fsx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Entry struct {
	Name string
	Dir  bool
}

type FS interface {
	ReadDir(dir string) ([]Entry, error)
	IsDir(p string) bool
	Exists(p string) bool
	Rename(from, to string) error
	Remove(p string) error
	RemoveEmptyDir(p string)
	MkdirAll(p string) error
	Upload(local, p string) error
	Join(elem ...string) string
	Dir(p string) string
	Base(p string) string
	Remote() string
}

func Open(location string) (FS, string, error) {
	if !IsRemote(location) {
		return Local, location, nil
	}
	t, err := ParseTarget(location)
	if err != nil {
		return nil, "", err
	}
	return &sftpFS{t: t}, t.Root(), nil
}

func ModsDir(f FS, root string) (string, error) {
	if strings.EqualFold(f.Base(root), "mods") {
		return root, nil
	}
	if dir := f.Join(root, "mods"); f.IsDir(dir) {
		return dir, nil
	}
	return "", fmt.Errorf("no mods folder in %s", root)
}

var versionDir = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

func IsVersionDir(name string) bool {
	return versionDir.MatchString(name)
}

func IsJar(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".zip")
}

func ListJars(f FS, modsDir string) ([]string, error) {
	entries, err := f.ReadDir(modsDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", modsDir, err)
	}
	var out []string
	for _, e := range entries {
		switch {
		case !e.Dir && IsJar(e.Name):
			out = append(out, f.Join(modsDir, e.Name))
		case e.Dir && IsVersionDir(e.Name):
			sub, err := f.ReadDir(f.Join(modsDir, e.Name))
			if err != nil {
				return nil, err
			}
			for _, s := range sub {
				if !s.Dir && IsJar(s.Name) {
					out = append(out, f.Join(modsDir, e.Name, s.Name))
				}
			}
		}
	}
	return out, nil
}

type localFS struct{}

var Local FS = localFS{}

func (localFS) ReadDir(dir string) ([]Entry, error) {
	entries, err := os.ReadDir(dir)
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, Entry{Name: e.Name(), Dir: e.IsDir()})
	}
	return out, err
}

func (localFS) IsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func (localFS) Exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (localFS) Rename(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := copyFile(from, to); err != nil {
		os.Remove(to)
		return err
	}
	if err := os.Remove(from); err != nil {
		os.Remove(to)
		return err
	}
	return nil
}

func (localFS) Remove(p string) error { return os.Remove(p) }

func (localFS) RemoveEmptyDir(p string) { os.Remove(p) }

func (localFS) MkdirAll(p string) error { return os.MkdirAll(p, 0o755) }

func (localFS) Upload(local, p string) error { return copyFile(local, p) }

func (localFS) Join(elem ...string) string { return filepath.Join(elem...) }

func (localFS) Dir(p string) string { return filepath.Dir(p) }

func (localFS) Base(p string) string { return filepath.Base(p) }

func (localFS) Remote() string { return "" }

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
