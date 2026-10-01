package crash

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Report struct {
	Path   string
	Time   time.Time
	Causes []*Cause
}

type Cause struct {
	Error   string
	Message string
	Frames  []string
}

var (
	exceptionLine  = regexp.MustCompile(`^(?:Caused by: )?([\w$.]+(?:Exception|Error|Throwable))(?::\s*(.*))?$`)
	frameLine      = regexp.MustCompile(`^\s+at ([\w$.]+)\.[\w$<>]+\(`)
	resolvedMethod = regexp.MustCompile(`resolved method '(?:\S+ )*([\w$<>]+)\([^']*\)' of (?:interface|abstract class|class) ([\w$.]*[\w$])`)
	memberField    = regexp.MustCompile(`^Class (\S+) does not have member field '(?:\S+ )?(\S+)'$`)
)

const maxSize = 4 << 20

func Latest(root string) (*Report, error) {
	paths, err := filepath.Glob(filepath.Join(root, "crash-reports", "crash-*.txt"))
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	var newest string
	var newestTime time.Time
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.Size() <= maxSize && st.ModTime().After(newestTime) {
			newest, newestTime = p, st.ModTime()
		}
	}
	if newest == "" {
		return nil, nil
	}
	data, err := os.ReadFile(newest)
	if err != nil {
		return nil, err
	}
	r := Parse(string(data))
	if len(r.Causes) == 0 {
		return nil, nil
	}
	r.Path, r.Time = newest, newestTime
	return r, nil
}

func Parse(text string) *Report {
	r := &Report{}
	var c *Cause
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "A detailed walkthrough") || strings.HasPrefix(line, "-- ") {
			break
		}
		if m := exceptionLine.FindStringSubmatch(line); m != nil {
			c = &Cause{Error: m[1], Message: strings.TrimSpace(m[2])}
			r.Causes = append(r.Causes, c)
			continue
		}
		if m := frameLine.FindStringSubmatch(line); m != nil && c != nil {
			c.Frames = append(c.Frames, strings.ReplaceAll(m[1], ".", "/"))
		}
	}
	return r
}

func (c *Cause) Short() string {
	return c.Error[strings.LastIndex(c.Error, ".")+1:]
}

type Member struct {
	Owner string
	Name  string
}

func (c *Cause) Missing() (Member, bool) {
	msg := c.Message
	if strings.HasPrefix(msg, "Could not initialize class") {
		return Member{}, false
	}
	if m := resolvedMethod.FindStringSubmatch(msg); m != nil {
		return Member{Owner: strings.ReplaceAll(m[2], ".", "/"), Name: m[1]}, true
	}
	if m := memberField.FindStringSubmatch(msg); m != nil {
		return Member{Owner: strings.ReplaceAll(m[1], ".", "/"), Name: m[2]}, true
	}
	if len(msg) > 1 && strings.HasPrefix(msg, "'") && strings.HasSuffix(msg, "'") {
		msg = msg[1 : len(msg)-1]
		if i, j := strings.IndexByte(msg, ' '), strings.IndexByte(msg, '('); i >= 0 && (j < 0 || i < j) {
			msg = msg[i+1:]
		}
	}
	switch c.Short() {
	case "NoSuchMethodError", "AbstractMethodError":
		if i := strings.IndexByte(msg, '('); i >= 0 {
			msg = msg[:i]
		}
		if i := strings.LastIndexByte(msg, '.'); i > 0 {
			return Member{Owner: strings.ReplaceAll(msg[:i], ".", "/"), Name: msg[i+1:]}, true
		}
		return Member{Name: msg}, msg != ""
	case "NoSuchFieldError":
		if i := strings.LastIndexByte(msg, '.'); i > 0 {
			return Member{Owner: strings.ReplaceAll(msg[:i], ".", "/"), Name: msg[i+1:]}, true
		}
		return Member{Name: msg}, msg != ""
	case "NoClassDefFoundError", "ClassNotFoundException":
		if msg == "" {
			return Member{}, false
		}
		return Member{Owner: strings.ReplaceAll(msg, ".", "/")}, true
	}
	return Member{}, false
}
