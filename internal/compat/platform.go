package compat

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type Platform struct {
	Name     string
	Prefix   string
	Foreign  string
	Classes  map[string]*Class
	packages map[string]bool
}

type PlatformJars struct {
	Name    string
	Prefix  string
	Foreign string
	Paths   []string
}

func FindLWJGL(root, mcVersion string) *PlatformJars {
	files, _ := filepath.Glob(filepath.Join(root, "versions", "*", "*.json"))
	var versions []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var v struct {
			ID           string `json:"id"`
			InheritsFrom string `json:"inheritsFrom"`
			Libraries    []struct {
				Name  string        `json:"name"`
				Rules []libraryRule `json:"rules"`
			} `json:"libraries"`
		}
		if json.Unmarshal(data, &v) != nil || !strings.Contains(v.ID+" "+v.InheritsFrom, mcVersion) {
			continue
		}
		for _, l := range v.Libraries {
			if !allowed(l.Rules) {
				continue
			}
			if ver, ok := strings.CutPrefix(l.Name, "org.lwjgl.lwjgl:lwjgl:"); ok && !slices.Contains(versions, ver) {
				versions = append(versions, ver)
			}
		}
	}
	if len(versions) != 1 {
		return nil
	}
	ver := versions[0]
	var paths []string
	for _, artifact := range []string{"lwjgl", "lwjgl_util"} {
		p := filepath.Join(root, "libraries", "org", "lwjgl", "lwjgl", artifact, ver, artifact+"-"+ver+".jar")
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return &PlatformJars{Name: "LWJGL " + ver, Prefix: "org/lwjgl/", Foreign: "lwjgl3", Paths: paths}
}

type libraryRule struct {
	Action string `json:"action"`
	OS     struct {
		Name string `json:"name"`
	} `json:"os"`
}

func allowed(rules []libraryRule) bool {
	if len(rules) == 0 {
		return true
	}
	system := map[string]string{"windows": "windows", "darwin": "osx", "linux": "linux"}[runtime.GOOS]
	ok := false
	for _, r := range rules {
		if r.OS.Name == "" || r.OS.Name == system {
			ok = r.Action == "allow"
		}
	}
	return ok
}

func LoadPlatform(jars *PlatformJars, cacheDir string) (*Platform, error) {
	p := &Platform{Name: jars.Name, Prefix: jars.Prefix, Foreign: jars.Foreign, Classes: map[string]*Class{}}
	for _, jar := range jars.Paths {
		sum, err := fileSHA1(jar)
		if err != nil {
			return nil, err
		}
		x, err := Load(jar, sum, cacheDir)
		if err != nil {
			return nil, err
		}
		for name, c := range x.Classes {
			if p.Classes[name] == nil {
				p.Classes[name] = c
			}
		}
	}
	return p, nil
}

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (l *Platform) missing(r ref) bool {
	want := member(r.Kind, r.Name, r.Desc)
	seen := map[string]bool{}
	queue := []string{r.Owner}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		c := l.Classes[name]
		if c == nil {
			if name == "java/lang/Object" {
				continue
			}
			return false
		}
		if slices.Contains(c.Members, want) {
			return false
		}
		if r.Name == "<init>" {
			return true
		}
		queue = append(queue, c.Super)
		queue = append(queue, c.Ifaces...)
	}
	return !(r.Kind == refMethod && slices.Contains(objectMethods, r.Name))
}

func (l *Platform) foreign(c *class) bool {
	if l.Foreign != "" && slices.Contains(strings.Split(strings.ToLower(path.Dir(c.Name)), "/"), l.Foreign) {
		return true
	}
	if l.packages == nil {
		l.packages = map[string]bool{}
		for name := range l.Classes {
			l.packages[path.Dir(name)] = true
		}
	}
	for _, name := range c.Classes {
		if strings.HasPrefix(name, l.Prefix) && !l.packages[path.Dir(name)] {
			return true
		}
	}
	for _, r := range c.Refs {
		if strings.HasPrefix(r.Owner, l.Prefix) && !l.packages[path.Dir(r.Owner)] {
			return true
		}
	}
	return false
}

func (l *Platform) reachable(jarPath string, candidates map[ref]bool) ([]string, error) {
	z, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	seen := map[string]bool{}
	var out []string
	for _, f := range z.File {
		if !strings.HasSuffix(f.Name, ".class") || strings.HasPrefix(f.Name, "META-INF/versions/") {
			continue
		}
		data, err := read(f)
		if err != nil {
			continue
		}
		c, err := parseClass(data)
		if err != nil || l.foreign(c) {
			continue
		}
		for _, r := range c.Refs {
			if d := describe(r); candidates[r] && !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	slices.Sort(out)
	return out, nil
}

func (p *pack) platformProblems(env Env, add func(Problem)) {
	lib := env.Platform
	if lib == nil {
		return
	}
	for _, j := range p.jars {
		if slices.Contains(j.ModIDs, "lwjgl3ify") || slices.Contains(j.Index.ModIDs, "lwjgl3ify") {
			return
		}
	}
	for i, j := range p.jars {
		candidates := map[ref]bool{}
		for _, r := range j.Index.Refs {
			if strings.HasPrefix(r.Owner, lib.Prefix) && lib.missing(r) && !p.patched(i, r) {
				candidates[r] = true
			}
		}
		if len(candidates) == 0 || j.Path == "" {
			continue
		}
		details, err := lib.reachable(j.Path, candidates)
		if err != nil || len(details) == 0 {
			continue
		}
		add(Problem{Jar: j.Name, Kind: KindAPI, Cause: lib.Name, Details: details})
	}
}
