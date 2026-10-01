package compat

import (
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type Kind string

const (
	KindAPI       Kind = "api"
	KindJvmdg     Kind = "jvmdg"
	KindMissing   Kind = "missing"
	KindDuplicate Kind = "duplicate"
)

type Problem struct {
	Jar     string   `json:"jar"`
	Kind    Kind     `json:"kind"`
	Cause   string   `json:"cause"`
	Details []string `json:"details"`
	Severe  bool     `json:"severe"`
}

type Jar struct {
	Name     string
	Path     string
	Index    *Index
	ModIDs   []string
	Requires []string
}

type Env struct {
	JvmdgClasses func(version string) (map[string]bool, error)
}

const jvmdgArtifact = "xyz.wagyourtail.jvmdowngrader:jvmdowngrader-java-api"

var builtinMods = []string{"forge", "fml", "mcp", "minecraft", "minecraftforge"}

var objectMethods = []string{"<init>", "equals", "hashCode", "toString", "getClass", "clone", "finalize", "notify", "notifyAll", "wait"}

var platform = []string{
	"net/minecraft/", "net/minecraftforge/", "cpw/mods/fml/", "java/", "javax/", "sun/", "jdk/",
	"com/google/", "com/mojang/", "org/apache/", "org/lwjgl/", "io/netty/", "org/objectweb/", "org/spongepowered/",
	"org/slf4j/", "org/json/", "org/yaml/", "gnu/", "kotlin/", "scala/", "it/unimi/", "org/joml/", "paulscode/", "joptsimple/",
}

func isPlatform(name string) bool {
	for _, prefix := range platform {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

type pack struct {
	jars   []Jar
	owners map[string][]int
	pkgs   map[string]map[int]int
	fixes  []Fix
	fixed  map[int]map[string]bool
}

func newPack(jars []Jar) *pack {
	p := &pack{jars: jars, owners: map[string][]int{}, pkgs: map[string]map[int]int{}, fixed: map[int]map[string]bool{}}
	for i, j := range jars {
		p.fixes = append(p.fixes, j.Index.Fixes...)
		for name := range j.Index.Classes {
			p.owners[name] = append(p.owners[name], i)
			pkg := path.Dir(name)
			if p.pkgs[pkg] == nil {
				p.pkgs[pkg] = map[int]int{}
			}
			p.pkgs[pkg][i]++
		}
	}
	return p
}

func (p *pack) home(name string) int {
	counts := p.pkgs[path.Dir(name)]
	best, most := -1, 0
	for i, n := range counts {
		if n > most || (n == most && i < best) {
			best, most = i, n
		}
	}
	return best
}

type outcome int

const (
	found outcome = iota
	unknown
	missing
)

func (p *pack) resolve(r ref) (outcome, int) {
	want := member(r.Kind, r.Name, r.Desc)
	seen := map[string]bool{}
	queue := []string{r.Owner}
	result := missing
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		owners, ok := p.owners[name]
		if !ok {
			switch {
			case name == "java/lang/Object":
				if r.Kind == refMethod && slices.Contains(objectMethods, r.Name) {
					return found, -1
				}
			case strings.HasPrefix(name, jvmdgPrefix):
			default:
				result = unknown
			}
			continue
		}
		for _, i := range owners {
			c := p.jars[i].Index.Classes[name]
			if slices.Contains(c.Members, want) {
				return found, i
			}
			queue = append(queue, c.Super)
			queue = append(queue, c.Ifaces...)
		}
	}
	return result, -1
}

func (p *pack) patched(i int, r ref) bool {
	set, ok := p.fixed[i]
	if !ok {
		set = p.fixedRefs(p.jars[i])
		p.fixed[i] = set
	}
	return set[r.Owner+"."+r.Name]
}

func (p *pack) fixedRefs(j Jar) map[string]bool {
	classes := map[string]bool{}
	declared := map[string]bool{}
	for _, f := range p.fixes {
		inJar := false
		for _, c := range f.Classes {
			if j.Index.Classes[c] != nil {
				classes[c], inJar = true, true
			}
		}
		if inJar {
			for _, r := range f.Refs {
				declared[r] = true
			}
		}
	}
	if len(declared) == 0 || j.Path == "" {
		return nil
	}
	elsewhere, err := classRefs(j.Path, classes)
	if err != nil {
		return nil
	}
	for r := range elsewhere {
		delete(declared, r)
	}
	return declared
}

func (p *pack) apiProblems(add func(Problem)) {
	for i, j := range p.jars {
		byCause := map[int][]string{}
		for _, r := range j.Index.Refs {
			if isPlatform(r.Owner) {
				continue
			}
			if _, ok := p.owners[r.Owner]; !ok {
				continue
			}
			o := p.home(r.Owner)
			if o == i || o < 0 {
				continue
			}
			if res, _ := p.resolve(r); res == missing && !p.patched(i, r) {
				byCause[o] = append(byCause[o], describe(r))
			}
		}
		for _, name := range j.Index.Uses {
			if isPlatform(name) {
				continue
			}
			if _, ok := p.owners[name]; ok {
				continue
			}
			if o := p.home(name); o >= 0 && o != i {
				byCause[o] = append(byCause[o], dotted(name))
			}
		}
		for o, details := range byCause {
			add(Problem{Jar: j.Name, Kind: KindAPI, Cause: p.jars[o].Name, Details: details})
		}
	}
}

func (p *pack) jvmdgProblems(env Env, add func(Problem)) {
	for _, j := range p.jars {
		if slices.Contains(j.ModIDs, "lwjgl3ify") || slices.Contains(j.Index.ModIDs, "lwjgl3ify") {
			return
		}
	}
	version, by := "", ""
	for _, j := range p.jars {
		for _, d := range j.Index.Decls {
			if d.Artifact != jvmdgArtifact || d.MinJava > 8 || (d.MaxJava != 0 && d.MaxJava < 8) {
				continue
			}
			if version == "" || compareVersions(d.Version, version) > 0 {
				version, by = d.Version, j.Name
			}
		}
	}
	var classes map[string]bool
	if version != "" && env.JvmdgClasses != nil {
		classes, _ = env.JvmdgClasses(version)
	}
	for _, j := range p.jars {
		var lost []string
		for _, name := range j.Index.Jvmdg {
			if !classes[name] && len(p.owners[name]) == 0 {
				lost = append(lost, dotted(name))
			}
		}
		if len(lost) == 0 {
			continue
		}
		if version == "" {
			add(Problem{Jar: j.Name, Kind: KindJvmdg, Details: []string{"no mod in the modpack provides the JvmDowngrader runtime"}})
			continue
		}
		if classes != nil {
			add(Problem{Jar: j.Name, Kind: KindJvmdg, Cause: "JvmDowngrader " + version + " (" + by + ")", Details: lost})
		}
	}
}

func (p *pack) duplicateProblems(add func(Problem)) {
	byID := map[string][]int{}
	for i, j := range p.jars {
		seen := map[string]bool{}
		for _, id := range j.Index.Mods {
			id = strings.ToLower(id)
			if !seen[id] {
				seen[id] = true
				byID[id] = append(byID[id], i)
			}
		}
	}
	for id, list := range byID {
		for _, i := range list {
			for _, o := range list {
				if o != i {
					add(Problem{Jar: p.jars[i].Name, Kind: KindDuplicate, Cause: p.jars[o].Name, Details: []string{id}})
				}
			}
		}
	}
}

func (p *pack) missingProblems(add func(Problem)) {
	have := map[string]bool{}
	for _, id := range builtinMods {
		have[id] = true
	}
	for _, j := range p.jars {
		for _, id := range append(slices.Clone(j.ModIDs), j.Index.ModIDs...) {
			have[strings.ToLower(id)] = true
		}
	}
	for _, j := range p.jars {
		var lost []string
		for _, id := range required(j) {
			if !have[id] && !slices.Contains(lost, id) {
				lost = append(lost, id)
			}
		}
		for _, id := range lost {
			add(Problem{Jar: j.Name, Kind: KindMissing, Cause: id})
		}
	}
}

func required(j Jar) []string {
	var out []string
	for _, spec := range j.Requires {
		for _, id := range SplitOutsideBrackets(spec) {
			out = append(out, modID(id))
		}
	}
	for _, specs := range j.Index.Deps {
		for _, s := range specs {
			for _, part := range SplitOutsideBrackets(strings.ReplaceAll(s, ";", ",")) {
				part = strings.TrimSpace(part)
				if !strings.HasPrefix(part, "required-") {
					continue
				}
				if i := strings.Index(part, ":"); i >= 0 {
					out = append(out, modID(part[i+1:]))
				}
			}
		}
	}
	return slices.DeleteFunc(out, func(s string) bool { return s == "" || s == "*" })
}

func modID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "@[("); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func Check(jars []Jar, env Env) []Problem {
	p := newPack(jars)
	var out []Problem
	add := func(pr Problem) { out = append(out, pr) }
	p.apiProblems(add)
	p.jvmdgProblems(env, add)
	p.missingProblems(add)
	p.duplicateProblems(add)
	return sorted(out)
}

func Replaced(jars []Jar, old map[string]*Index, env Env) []Problem {
	after := newPack(jars)
	beforeJars := slices.Clone(jars)
	for i, j := range beforeJars {
		if x, ok := old[j.Name]; ok {
			beforeJars[i].Index = x
		}
	}
	before := newPack(beforeJars)
	var out []Problem
	for i, j := range jars {
		if _, ok := old[j.Name]; ok {
			continue
		}
		byCause := map[int][]string{}
		for _, r := range j.Index.Refs {
			res, at := before.resolve(r)
			if res != found || at < 0 {
				continue
			}
			if _, replaced := old[jars[at].Name]; !replaced {
				continue
			}
			if now, _ := after.resolve(r); (now == missing || now == unknown && !srgName(r.Name)) && at != i && !after.patched(i, r) {
				byCause[at] = append(byCause[at], describe(r))
			}
		}
		for o, details := range byCause {
			out = append(out, Problem{Jar: j.Name, Kind: KindAPI, Cause: jars[o].Name, Details: details})
		}
	}
	return sorted(out)
}

func srgName(name string) bool {
	return strings.HasPrefix(name, "func_") || strings.HasPrefix(name, "field_")
}

func sorted(ps []Problem) []Problem {
	for i := range ps {
		sort.Strings(ps[i].Details)
		ps[i].Details = slices.Compact(ps[i].Details)
	}
	sort.Slice(ps, func(a, b int) bool {
		if ps[a].Jar != ps[b].Jar {
			return ps[a].Jar < ps[b].Jar
		}
		return ps[a].Cause < ps[b].Cause
	})
	return ps
}

func describe(r ref) string {
	name := dotted(r.Owner)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	if r.Kind == refMethod {
		return name + "." + r.Name + "()"
	}
	return name + "." + r.Name
}

func dotted(name string) string {
	return strings.ReplaceAll(name, "/", ".")
}

func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func SplitOutsideBrackets(s string) []string {
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

func Merge(found, introduced []Problem) []Problem {
	key := func(p Problem) string { return string(p.Kind) + "|" + p.Jar + "|" + p.Cause }
	out := slices.Clone(found)
	at := map[string]int{}
	for i := range out {
		out[i].Severe = out[i].Kind != KindAPI
		at[key(out[i])] = i
	}
	for _, p := range introduced {
		p.Severe = true
		if i, ok := at[key(p)]; ok {
			out[i].Severe = true
			out[i].Details = append(out[i].Details, p.Details...)
			continue
		}
		at[key(p)] = len(out)
		out = append(out, p)
	}
	return sorted(out)
}

func Summary(file string, problems []Problem) string {
	var broken, missingIn, requires, duplicates []string
	jvmdg := false
	for _, p := range problems {
		switch {
		case p.Kind == KindAPI && p.Cause == file:
			broken = append(broken, p.Jar)
		case p.Jar != file:
		case p.Kind == KindAPI:
			missingIn = append(missingIn, p.Cause)
		case p.Kind == KindJvmdg:
			jvmdg = true
		case p.Kind == KindMissing:
			requires = append(requires, p.Cause)
		case p.Kind == KindDuplicate:
			duplicates = append(duplicates, p.Cause)
		}
	}
	var parts []string
	if len(broken) > 0 {
		parts = append(parts, "may break "+strings.Join(uniqueSorted(broken), ", "))
	}
	if len(missingIn) > 0 {
		parts = append(parts, "uses things missing in "+strings.Join(uniqueSorted(missingIn), ", "))
	}
	if jvmdg {
		parts = append(parts, "needs JvmDowngrader classes the modpack does not have")
	}
	if len(duplicates) > 0 {
		parts = append(parts, "is the same mod as "+strings.Join(uniqueSorted(duplicates), ", "))
	}
	if len(requires) > 0 {
		parts = append(parts, "requires "+strings.Join(uniqueSorted(requires), ", ")+", not in the modpack")
	}
	return strings.Join(parts, "; ")
}

func uniqueSorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return slices.Compact(s)
}

func Introduced(problems []Problem, file string, old Jar) []Problem {
	had := map[string]bool{}
	for _, id := range required(old) {
		had[id] = true
	}
	for _, name := range old.Index.Jvmdg {
		had[dotted(name)] = true
	}
	var out []Problem
	for _, p := range problems {
		if p.Jar == file {
			switch p.Kind {
			case KindMissing:
				if had[p.Cause] {
					continue
				}
			case KindJvmdg:
				if p.Cause == "" && len(old.Index.Jvmdg) > 0 {
					continue
				}
				p.Details = slices.DeleteFunc(slices.Clone(p.Details), func(d string) bool { return had[d] })
				if len(p.Details) == 0 {
					continue
				}
			}
		}
		out = append(out, p)
	}
	return out
}

func ClassJars(jars []Jar) map[string]string {
	p := newPack(jars)
	out := make(map[string]string, len(p.owners))
	for name, owners := range p.owners {
		if isPlatform(name) {
			continue
		}
		counts := p.pkgs[path.Dir(name)]
		best := owners[0]
		for _, i := range owners[1:] {
			if counts[i] > counts[best] {
				best = i
			}
		}
		out[name] = p.jars[best].Name
	}
	return out
}

func IsPlatform(name string) bool {
	return isPlatform(name) || strings.HasPrefix(name, jvmdgPrefix)
}

func IsJvmdg(name string) bool {
	return strings.HasPrefix(name, jvmdgPrefix)
}

func Needs(j Jar) []string {
	return uniqueSorted(required(j))
}

func Provides(j Jar) []string {
	var out []string
	for _, id := range append(slices.Clone(j.ModIDs), j.Index.ModIDs...) {
		out = append(out, strings.ToLower(id))
	}
	return uniqueSorted(out)
}

func Declares(j Jar) []string {
	var out []string
	for _, id := range j.Index.Mods {
		out = append(out, strings.ToLower(id))
	}
	return uniqueSorted(out)
}

func JarUses(jars []Jar, classes map[string]string) map[string][]string {
	out := map[string][]string{}
	for _, j := range jars {
		seen := map[string]bool{}
		note := func(class string) {
			if jar := classes[class]; jar != "" && jar != j.Name && !seen[jar] {
				seen[jar] = true
				out[j.Name] = append(out[j.Name], jar)
			}
		}
		for _, r := range j.Index.Refs {
			note(r.Owner)
		}
		for _, name := range j.Index.Uses {
			note(name)
		}
	}
	return out
}
