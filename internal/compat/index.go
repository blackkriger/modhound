package compat

import (
	"archive/zip"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const indexVersion = 4

type Class struct {
	Super   string
	Ifaces  []string
	Members []string
}

type DepDecl struct {
	Artifact string
	Version  string
	MinJava  int
	MaxJava  int
}

type Index struct {
	Version int
	Classes map[string]*Class
	Refs    []ref
	Uses    []string
	ModIDs  []string
	Deps    map[string][]string
	Jvmdg   []string
	Decls   []DepDecl
	Fixes   []Fix
}

type Fix struct {
	Classes []string `json:"classes"`
	Refs    []string `json:"refs"`
}

const fixesFile = "META-INF/modhound-fixes.json"

const jvmdgPrefix = "xyz/wagyourtail/jvmdg/"

func (x *Index) has(className, m string) bool {
	c := x.Classes[className]
	return c != nil && slices.Contains(c.Members, m)
}

func Load(path, sha1, cacheDir string) (*Index, error) {
	cachePath := ""
	if cacheDir != "" && sha1 != "" {
		cachePath = filepath.Join(cacheDir, "compat", sha1+".gob")
		if f, err := os.Open(cachePath); err == nil {
			var x Index
			err := gob.NewDecoder(f).Decode(&x)
			f.Close()
			if err == nil && x.Version == indexVersion {
				if st, err := os.Stat(cachePath); err == nil && time.Since(st.ModTime()) > 24*time.Hour {
					now := time.Now()
					os.Chtimes(cachePath, now, now)
				}
				return &x, nil
			}
		}
	}
	x, err := build(path)
	if err != nil {
		return nil, err
	}
	if cachePath != "" {
		var buf bytes.Buffer
		if gob.NewEncoder(&buf).Encode(x) == nil && os.MkdirAll(filepath.Dir(cachePath), 0o755) == nil {
			tmp := cachePath + ".tmp"
			if os.WriteFile(tmp, buf.Bytes(), 0o644) == nil {
				os.Rename(tmp, cachePath)
			}
		}
	}
	return x, nil
}

var depsJSON = regexp.MustCompile(`^META-INF/\w+\.json$`)

func build(path string) (*Index, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	x := &Index{Version: indexVersion, Classes: map[string]*Class{}, Deps: map[string][]string{}}
	var parsed []*class
	for _, f := range z.File {
		switch {
		case strings.HasSuffix(f.Name, ".class") && !strings.HasPrefix(f.Name, "META-INF/versions/"):
			data, err := read(f)
			if err != nil {
				continue
			}
			c, err := parseClass(data)
			if err != nil {
				continue
			}
			x.Classes[c.Name] = &Class{Super: c.Super, Ifaces: c.Ifaces, Members: c.Members}
			parsed = append(parsed, c)
		case f.Name == fixesFile:
			if data, err := read(f); err == nil {
				var root struct {
					Fixes []Fix `json:"fixes"`
				}
				if json.Unmarshal(data, &root) == nil {
					x.Fixes = append(x.Fixes, root.Fixes...)
				}
			}
		case depsJSON.MatchString(f.Name):
			if data, err := read(f); err == nil {
				x.Decls = append(x.Decls, parseDecls(data)...)
			}
		}
	}
	refs := map[ref]bool{}
	uses := map[string]bool{}
	jvmdg := map[string]bool{}
	for _, c := range parsed {
		x.ModIDs = append(x.ModIDs, c.ModIDs...)
		if c.Mod != "" && len(c.Deps) > 0 {
			id := strings.ToLower(c.Mod)
			x.Deps[id] = append(x.Deps[id], c.Deps...)
		}
		for _, name := range c.Classes {
			switch {
			case strings.HasPrefix(name, jvmdgPrefix):
				if x.Classes[name] == nil {
					jvmdg[name] = true
				}
			case x.Classes[name] == nil:
				uses[name] = true
			}
		}
		for _, r := range c.Refs {
			if strings.HasPrefix(r.Owner, jvmdgPrefix) || x.has(r.Owner, member(r.Kind, r.Name, r.Desc)) {
				continue
			}
			refs[r] = true
		}
	}
	for r := range refs {
		x.Refs = append(x.Refs, r)
	}
	for name := range uses {
		x.Uses = append(x.Uses, name)
	}
	for name := range jvmdg {
		x.Jvmdg = append(x.Jvmdg, name)
	}
	return x, nil
}

func read(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 16<<20))
}

func parseDecls(data []byte) []DepDecl {
	var root struct {
		Identifier   string                         `json:"identifier"`
		MinJava      int                            `json:"minJava"`
		MaxJava      int                            `json:"maxJava"`
		Dependencies map[string]map[string][]string `json:"dependencies"`
	}
	if json.Unmarshal(data, &root) != nil || root.Identifier != "falsepatternlib_dependencies" {
		return nil
	}
	var out []DepDecl
	for _, sides := range root.Dependencies {
		for _, list := range sides {
			for _, spec := range list {
				parts := strings.Split(spec, ":")
				if len(parts) < 3 {
					continue
				}
				out = append(out, DepDecl{Artifact: parts[0] + ":" + parts[1], Version: parts[2], MinJava: root.MinJava, MaxJava: root.MaxJava})
			}
		}
	}
	return out
}

func PruneCache(cacheDir string, unused time.Duration) {
	dir := filepath.Join(cacheDir, "compat")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gob") {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > unused {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func classRefs(path string, skip map[string]bool) (map[string]bool, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	out := map[string]bool{}
	for _, f := range z.File {
		if !strings.HasSuffix(f.Name, ".class") || strings.HasPrefix(f.Name, "META-INF/versions/") {
			continue
		}
		data, err := read(f)
		if err != nil {
			continue
		}
		c, err := parseClass(data)
		if err != nil || skip[c.Name] {
			continue
		}
		for _, r := range c.Refs {
			out[r.Owner+"."+r.Name] = true
		}
	}
	return out, nil
}
