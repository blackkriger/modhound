package install

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/blackkriger/modhound/internal/backup"
	"github.com/blackkriger/modhound/internal/fsx"
	"github.com/blackkriger/modhound/internal/httpx"
	"github.com/blackkriger/modhound/internal/jarinfo"
	"github.com/blackkriger/modhound/internal/logx"
	"github.com/blackkriger/modhound/internal/resolve"
)

const (
	StateQueued      = "queued"
	StateDownloading = "downloading"
	StateDone        = "done"
	StateFailed      = "failed"
)

type Event struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Percent int    `json:"percent"`
	Error   string `json:"error"`
}

type Result struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	From     string `json:"from"`
	To       string `json:"to"`
	FileFrom string `json:"fileFrom"`
	FileTo   string `json:"fileTo"`
	OK       bool   `json:"ok"`
	Error    string `json:"error"`
	Warning  string `json:"warning"`
}

var builtin = map[string]bool{"forge": true, "fml": true, "minecraft": true, "mcp": true, "minecraftforge": true}

func requiredIDs(requires []string) []string {
	var out []string
	for _, r := range requires {
		for _, part := range splitOutsideBrackets(r) {
			id := strings.ToLower(strings.TrimSpace(part))
			if i := strings.IndexAny(id, "@["); i >= 0 {
				id = strings.TrimSpace(id[:i])
			}
			if id != "" && !builtin[id] {
				out = append(out, id)
			}
		}
	}
	return out
}

func splitOutsideBrackets(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case ',':
			if depth <= 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func missingRequired(pack *resolve.Pack, old, requires []string) []string {
	had := map[string]bool{}
	for _, id := range requiredIDs(old) {
		had[id] = true
	}
	var out []string
	for _, id := range requiredIDs(requires) {
		if pack.ModIDs[id] || had[id] {
			continue
		}
		had[id] = true
		out = append(out, id)
	}
	return out
}

var javaVersions = map[int]string{50: "6", 51: "7", 52: "8", 53: "9", 54: "10", 55: "11", 56: "12", 57: "13", 58: "14", 59: "15", 60: "16", 61: "17", 62: "18", 63: "19", 64: "20", 65: "21", 66: "22", 67: "23", 68: "24", 69: "25"}

func javaName(major int) string {
	if v, ok := javaVersions[major]; ok {
		return "Java " + v
	}
	return fmt.Sprintf("class version %d", major)
}

var slots = make(chan struct{}, 2)

func Run(ctx context.Context, pack *resolve.Pack, mods []*resolve.Mod, session *backup.Session, onEvent func(Event)) []Result {
	emit := func(e Event) {
		if onEvent != nil {
			onEvent(e)
		}
	}
	logx.Printf("install %d mods into %s", len(mods), pack.ModsDir)
	for _, m := range mods {
		emit(Event{ID: m.ID, State: StateQueued})
		if m.Target != nil {
			logx.Printf("%s: queued %s from %s", m.FileName, m.Target.FileName, m.Target.DownloadURL)
		}
	}
	results := make([]Result, len(mods))
	var wg sync.WaitGroup
	for i, m := range mods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := Result{ID: m.ID, Name: m.Name, From: m.Version, FileFrom: m.FileName}
			if m.Target != nil {
				res.To, res.FileTo = m.Target.Version, m.Target.FileName
			}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
			}
			var err error
			if err = ctx.Err(); err == nil {
				err = one(ctx, pack, m, session, &res, func(pct int) {
					emit(Event{ID: m.ID, State: StateDownloading, Percent: pct})
				})
			}
			if err != nil {
				if errors.Is(err, context.Canceled) {
					err = errors.New("stopped")
				}
				logx.Printf("%s: install failed: %v", m.FileName, err)
				res.Error = err.Error()
				emit(Event{ID: m.ID, State: StateFailed, Error: res.Error})
			} else {
				res.OK = true
				emit(Event{ID: m.ID, State: StateDone, Percent: 100})
			}
			results[i] = res
		}()
	}
	wg.Wait()
	return results
}

var ErrInUse = errors.New("mod files are in use")

func CheckNotInUse(mods []*resolve.Mod) error {
	for _, m := range mods {
		if InUse(m.Path) {
			logx.Printf("%s is in use by another program", m.Path)
			return ErrInUse
		}
	}
	return nil
}

var unsafeName = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

var mcVersionIn = regexp.MustCompile(`\d+\.\d+(\.\d+)?`)

func safeFileName(name string) bool {
	lower := strings.ToLower(name)
	return name != "" && !unsafeName.MatchString(name) && !strings.HasPrefix(name, ".") &&
		!strings.HasSuffix(name, " ") && !strings.HasSuffix(name, ".") &&
		(strings.HasSuffix(lower, ".jar") || strings.HasSuffix(lower, ".zip")) &&
		filepath.IsLocal(name) && filepath.Base(name) == name
}

func mcVersionMatches(declared, pack string) bool {
	v := mcVersionIn.FindString(declared)
	return v == "" || v == pack || strings.HasPrefix(pack, v+".")
}

func one(ctx context.Context, pack *resolve.Pack, m *resolve.Mod, session *backup.Session, res *Result, progress func(pct int)) error {
	t := m.Target
	if t == nil || t.DownloadURL == "" {
		return errors.New("no download available")
	}
	if !strings.HasPrefix(t.DownloadURL, "https://") {
		return errors.New("download is not served over https")
	}
	if !safeFileName(t.FileName) {
		return fmt.Errorf("unsafe file name %q", t.FileName)
	}
	dir := filepath.Dir(m.Path)
	dest := filepath.Join(dir, t.FileName)
	if !strings.EqualFold(dest, m.Path) {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("%s already exists", t.FileName)
		}
	}

	part := filepath.Join(dir, ".modhound-"+randomHex()+".part")
	defer os.Remove(part)
	progress(0)
	last := -1
	err := download(ctx, t.DownloadURL, part, func(done, size int64) {
		if size > 0 {
			if pct := int(done * 100 / size); pct != last {
				last = pct
				progress(pct)
			}
		}
	})
	if err != nil {
		return err
	}
	j, err := jarinfo.ReadFull(part)
	if err != nil {
		return err
	}
	mcv := ""
	if j.Info != nil {
		mcv = j.Info.MCVersion
	}
	logx.Printf("%s: downloaded %s, %d bytes, sha1 %s, valid jar %v, mcversion %q, %s", m.FileName, t.FileName, j.Size, j.SHA1, j.Valid, mcv, javaName(j.ClassMajor))
	if t.SHA512 != "" && !strings.EqualFold(j.SHA512, t.SHA512) {
		return errors.New("downloaded file hash does not match")
	}
	if t.SHA1 != "" && !strings.EqualFold(j.SHA1, t.SHA1) {
		return errors.New("downloaded file hash does not match")
	}
	if !j.Valid {
		return errors.New("downloaded file is not a valid jar")
	}
	if j.Info != nil && !mcVersionMatches(j.Info.MCVersion, pack.MCVersion) {
		return fmt.Errorf("new version is built for Minecraft %s", j.Info.MCVersion)
	}
	var oldRequires []string
	if m.Jar != nil {
		oldRequires = m.Jar.Requires
	}
	if missing := missingRequired(pack, oldRequires, j.Requires); len(missing) > 0 {
		res.Warning = "requires " + strings.Join(missing, ", ") + ", not found in the pack"
		logx.Printf("%s: %s", m.FileName, res.Warning)
	}
	if j.ClassMajor > pack.MaxClassMajor && pack.MaxClassMajor > 0 {
		return fmt.Errorf("new version requires %s, the pack targets %s", javaName(j.ClassMajor), javaName(pack.MaxClassMajor))
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	err = replaceKeeping(session, m, part, dest)
	logx.Printf("%s: replaced with %s, err=%v", m.FileName, t.FileName, err)
	return err
}

func replaceKeeping(session *backup.Session, m *resolve.Mod, part, dest string) error {
	item := backup.Item{Key: m.Key, Name: m.Name, Dir: filepath.Dir(m.Path), OldFile: m.FileName, NewFile: m.Target.FileName, From: m.Version, To: m.Target.Version}
	undo, err := session.Keep(fsx.Local, m.Path, "", &item)
	if err != nil {
		return fmt.Errorf("cannot move the old file (is the game running?): %w", err)
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

func download(ctx context.Context, url, path string, progress func(done, size int64)) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := httpx.Download(ctx, url, f, progress); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func randomHex() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}
