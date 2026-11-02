package crash

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type LogReport struct {
	Path   string
	Time   time.Time
	Start  time.Time
	Blocks [][]*Cause
	first  string
}

var (
	logPrefix   = regexp.MustCompile(`^\[([\d:.]+)\] \[[^\]]*\]: (?:\[[^\]]*:-?\d+\]: )?`)
	eventFailed = regexp.MustCompile(`Exception caught during firing event|Failed to Load |NEI plugin failed to load`)
	linkage     = map[string]bool{
		"NoClassDefFoundError": true, "NoSuchMethodError": true, "NoSuchFieldError": true, "AbstractMethodError": true,
		"IncompatibleClassChangeError": true, "VerifyError": true, "IllegalAccessError": true, "InstantiationError": true,
		"BootstrapMethodError": true,
	}
)

const maxLogBlocks = 1000

func LatestLog(root string) (*LogReport, error) {
	p := filepath.Join(root, "logs", "latest.log")
	st, err := os.Stat(p)
	if err != nil {
		return nil, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := ParseLog(f)
	r.Path, r.Time = p, st.ModTime()
	r.Start = startTime(r.first, r.Time)
	return r, nil
}

func startTime(clock string, end time.Time) time.Time {
	t, err := time.ParseInLocation("15:04:05", strings.SplitN(clock, ".", 2)[0], end.Location())
	if err != nil {
		return end
	}
	start := time.Date(end.Year(), end.Month(), end.Day(), t.Hour(), t.Minute(), t.Second(), 0, end.Location())
	if start.After(end) {
		start = start.AddDate(0, 0, -1)
	}
	return start
}

func ParseLog(f io.Reader) *LogReport {
	r := &LogReport{}
	br := bufio.NewReaderSize(f, 1<<16)
	var block []*Cause
	var c *Cause
	wanted, pending := false, false
	flush := func() {
		if len(block) > 0 && len(r.Blocks) < maxLogBlocks && (wanted || hasLinkage(block)) {
			r.Blocks = append(r.Blocks, block)
		}
		block, c, wanted = nil, nil, false
	}
	for {
		raw, err := br.ReadString('\n')
		if raw == "" && err != nil {
			break
		}
		raw = strings.TrimRight(raw, "\r\n")
		prefix := logPrefix.FindStringSubmatch(raw)
		if prefix != nil && r.first == "" {
			r.first = prefix[1]
		}
		line := raw
		if prefix != nil {
			line = raw[len(prefix[0]):]
		}
		if m := exceptionLine.FindStringSubmatch(strings.TrimRight(line, " ")); m != nil {
			if !strings.HasPrefix(line, "Caused by: ") {
				flush()
				wanted = pending
				pending = false
			}
			c = &Cause{Error: m[1], Message: strings.TrimSpace(m[2])}
			block = append(block, c)
			continue
		}
		if m := frameLine.FindStringSubmatch(line); m != nil && c != nil {
			c.Frames = append(c.Frames, strings.ReplaceAll(m[1], ".", "/"))
			continue
		}
		if c != nil && (prefix == nil || strings.HasPrefix(strings.TrimSpace(line), "... ")) {
			continue
		}
		flush()
		if eventFailed.MatchString(line) {
			pending = true
		} else if prefix != nil {
			pending = false
		}
	}
	flush()
	return r
}

func hasLinkage(block []*Cause) bool {
	for _, c := range block {
		if linkage[c.Short()] {
			return true
		}
	}
	return false
}
