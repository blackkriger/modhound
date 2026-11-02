package resolve

import (
	"regexp"
	"strings"
)

var (
	stemSplit   = regexp.MustCompile(`[-_+ .\[\]()]+`)
	versionish  = regexp.MustCompile(`^[a-z]{0,2}\d+[a-z]?\d*$`)
	stemDropped = map[string]bool{"jar": true, "zip": true, "mc": true, "v": true, "r": true, "b": true, "beta": true, "alpha": true, "release": true, "final": true, "hotfix": true, "pre": true}
	variantTags = map[string]bool{"dev": true, "deobf": true, "sources": true, "src": true, "api": true, "javadoc": true, "slim": true, "lib": true, "nodep": true, "client": true, "server": true}
	stageTag    = regexp.MustCompile(`^(?:(?:alpha|beta|pre|rc|snapshot)\d*|build\d+)$`)
)

func stemTokens(fileName string) []string {
	var out []string
	for _, t := range stemSplit.Split(strings.ToLower(fileName), -1) {
		if t == "" || stemDropped[t] || versionish.MatchString(t) || stageTag.MatchString(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func Stem(fileName string) string {
	return strings.Join(stemTokens(fileName), " ")
}

func isVariantOf(candidate, current string) bool {
	have := map[string]bool{}
	for _, t := range stemTokens(current) {
		have[t] = true
	}
	for _, t := range stemTokens(candidate) {
		if variantTags[t] && !have[t] {
			return true
		}
	}
	return false
}

func sharesStem(a, b string) bool {
	have := map[string]bool{}
	for _, t := range stemTokens(a) {
		have[t] = true
	}
	for _, t := range stemTokens(b) {
		if have[t] {
			return true
		}
	}
	return false
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func normName(s string) string {
	return nonAlnum.ReplaceAllString(strings.ToLower(s), "")
}

var versionSplit = regexp.MustCompile(`[-_+ \[\]()]+`)

func FileVersion(fileName, mcVersion string) string {
	name := strings.TrimSuffix(strings.TrimSuffix(fileName, ".jar"), ".zip")
	tokens := versionSplit.Split(name, -1)
	shortMC, numbered := "", 0
	if i := strings.LastIndex(mcVersion, "."); strings.Count(mcVersion, ".") == 2 {
		shortMC = mcVersion[:i]
	}
	for _, t := range tokens {
		if strings.ContainsAny(t, "0123456789") {
			numbered++
		}
	}
	var out []string
	for i, t := range tokens {
		lt := strings.ToLower(t)
		if len(out) > 0 && preRelease[lt] > 1 {
			out = append(out, t)
			continue
		}
		if t == "" || lt == mcVersion || lt == "mc"+mcVersion || shortMC != "" && numbered > 1 && (lt == shortMC || lt == "mc"+shortMC) || !strings.ContainsAny(t, "0123456789") {
			continue
		}
		if i == 0 && len(tokens) > 1 && !strings.ContainsAny(t[:1], "0123456789") {
			continue
		}
		out = append(out, t)
	}
	if len(tokens) == 1 && len(out) == 1 {
		if i := strings.IndexAny(out[0], "0123456789"); i > 0 {
			return out[0][i:]
		}
	}
	return strings.Join(out, "-")
}

func sameVersion(a, b, mcVersion string) bool {
	if c, ok := CompareVersions(a, b, mcVersion); ok {
		return c == 0
	}
	x := strings.TrimPrefix(strings.ToLower(FileVersion(a, mcVersion)), "v")
	y := strings.TrimPrefix(strings.ToLower(FileVersion(b, mcVersion)), "v")
	return x != "" && x == y
}

var versionPart = regexp.MustCompile(`\d+|[a-z]+`)

var preRelease = map[string]int{"dev": 1, "snapshot": 1, "alpha": 2, "beta": 3, "pre": 4, "rc": 5}

func versionParts(fileName, mcVersion string) []string {
	return versionPart.FindAllString(strings.TrimPrefix(strings.ToLower(FileVersion(fileName, mcVersion)), "v"), -1)
}

func numeric(s string) bool {
	return s[0] >= '0' && s[0] <= '9'
}

func CompareVersions(a, b, mcVersion string) (int, bool) {
	x, y := versionParts(a, mcVersion), versionParts(b, mcVersion)
	if len(x) == 0 || len(y) == 0 {
		return 0, false
	}
	for i := 0; i < len(x) && i < len(y); i++ {
		p, q := x[i], y[i]
		switch {
		case numeric(p) && numeric(q):
			p, q = strings.TrimLeft(p, "0"), strings.TrimLeft(q, "0")
			if len(p) != len(q) {
				return cmpInt(len(p), len(q)), true
			}
			if p != q {
				return strings.Compare(p, q), true
			}
		case p == q:
		case preRelease[p] > 0 && preRelease[q] > 0:
			return cmpInt(preRelease[p], preRelease[q]), true
		default:
			return 0, false
		}
	}
	switch {
	case len(x) > len(y):
		return tailOrder(x[len(y):]), true
	case len(y) > len(x):
		return -tailOrder(y[len(x):]), true
	}
	return 0, true
}

func tailOrder(rest []string) int {
	if preRelease[rest[0]] > 0 {
		return -1
	}
	for _, t := range rest {
		if strings.Trim(t, "0") != "" {
			return 1
		}
	}
	return 0
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func OlderVersion(candidate, current, mcVersion string) bool {
	c, ok := CompareVersions(candidate, current, mcVersion)
	return ok && c < 0
}
