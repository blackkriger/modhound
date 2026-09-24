package resolve

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	stemSplit   = regexp.MustCompile(`[-_+ .\[\]()]+`)
	versionish  = regexp.MustCompile(`^[a-z]{0,2}\d+[a-z]?\d*$`)
	stemDropped = map[string]bool{"jar": true, "zip": true, "mc": true, "v": true, "r": true, "b": true, "beta": true, "alpha": true, "release": true, "final": true, "hotfix": true, "pre": true}
	variantTags = map[string]bool{"dev": true, "deobf": true, "sources": true, "src": true, "api": true, "javadoc": true, "slim": true, "lib": true, "nodep": true, "client": true, "server": true}
)

func stemTokens(fileName string) []string {
	var out []string
	for _, t := range stemSplit.Split(strings.ToLower(fileName), -1) {
		if t == "" || stemDropped[t] || versionish.MatchString(t) {
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
	var out []string
	for i, t := range tokens {
		lt := strings.ToLower(t)
		if t == "" || lt == mcVersion || lt == "mc"+mcVersion || !strings.ContainsAny(t, "0123456789") {
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
	x := strings.TrimPrefix(strings.ToLower(FileVersion(a, mcVersion)), "v")
	y := strings.TrimPrefix(strings.ToLower(FileVersion(b, mcVersion)), "v")
	return x != "" && x == y
}

var dotted = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)$`)

func OlderVersion(candidate, current, mcVersion string) bool {
	a := dotted.FindStringSubmatch(strings.ToLower(FileVersion(candidate, mcVersion)))
	b := dotted.FindStringSubmatch(strings.ToLower(FileVersion(current, mcVersion)))
	if a == nil || b == nil {
		return false
	}
	x, y := strings.Split(a[1], "."), strings.Split(b[1], ".")
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p, _ = strconv.Atoi(x[i])
		}
		if i < len(y) {
			q, _ = strconv.Atoi(y[i])
		}
		if p != q {
			return p < q
		}
	}
	return false
}
