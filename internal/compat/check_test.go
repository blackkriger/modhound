package compat

import (
	"slices"
	"strings"
	"testing"
)

func jar(name string, ids, requires []string, annotated ...string) Jar {
	x := &Index{Classes: map[string]*Class{}, ModIDs: ids, Deps: map[string][]string{}}
	if len(ids) > 0 && len(annotated) > 0 {
		x.Deps[ids[0]] = annotated
	}
	return Jar{Name: name, Index: x, Requires: requires}
}

func TestMissingRequiredMods(t *testing.T) {
	jars := []Jar{
		jar("a.jar", []string{"a"}, []string{"b", "Forge@[10.13,)"}, "required-after:gtnhlib@[0.5,);after:optional;required-before:c"),
		jar("b.jar", []string{"B"}, nil),
	}
	var got []string
	for _, p := range Check(jars, Env{}) {
		if p.Kind == KindMissing {
			got = append(got, p.Jar+":"+p.Cause)
		}
	}
	slices.Sort(got)
	if want := []string{"a.jar:c", "a.jar:gtnhlib"}; !slices.Equal(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}

func TestRequirementsFromInfoAndAnnotation(t *testing.T) {
	a := jar("a.jar", []string{"a"}, []string{"b"}, "required-after:gtnhlib")
	var got []string
	for _, p := range Check([]Jar{a}, Env{}) {
		if p.Kind == KindMissing {
			got = append(got, p.Cause)
		}
	}
	slices.Sort(got)
	if want := []string{"b", "gtnhlib"}; !slices.Equal(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}

func TestSplitOutsideBrackets(t *testing.T) {
	got := SplitOutsideBrackets("required-after:x@[1.0,2.0);required-after:y,z@(,3]")
	want := []string{"required-after:x@[1.0,2.0);required-after:y", "z@(,3]"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestMergeSeverity(t *testing.T) {
	found := []Problem{
		{Jar: "x.jar", Kind: KindAPI, Cause: "lib.jar", Details: []string{"A.b()"}},
		{Jar: "y.jar", Kind: KindAPI, Cause: "lib.jar", Details: []string{"C.d"}},
		{Jar: "z.jar", Kind: KindMissing, Cause: "q"},
	}
	introduced := []Problem{
		{Jar: "x.jar", Kind: KindAPI, Cause: "lib.jar", Details: []string{"A.e()"}},
		{Jar: "w.jar", Kind: KindAPI, Cause: "lib.jar", Details: []string{"F.g"}},
	}
	severe := map[string]bool{}
	details := map[string][]string{}
	for _, p := range Merge(found, introduced) {
		severe[p.Jar] = p.Severe
		details[p.Jar] = p.Details
	}
	if !severe["x.jar"] || severe["y.jar"] || !severe["z.jar"] || !severe["w.jar"] {
		t.Fatalf("severity = %v", severe)
	}
	if !slices.Equal(details["x.jar"], []string{"A.b()", "A.e()"}) {
		t.Fatalf("details = %v", details["x.jar"])
	}
}

func TestSummary(t *testing.T) {
	problems := []Problem{
		{Jar: "x.jar", Kind: KindAPI, Cause: "lib.jar"},
		{Jar: "lib.jar", Kind: KindMissing, Cause: "core"},
		{Jar: "lib.jar", Kind: KindJvmdg},
	}
	want := "may break x.jar; needs JvmDowngrader classes the modpack does not have; requires core, not in the modpack"
	if got := Summary("lib.jar", problems); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestIntroducedKeepsOnlyNew(t *testing.T) {
	old := jar("a-1.jar", []string{"a"}, []string{"b"})
	old.Index.Jvmdg = []string{"xyz/wagyourtail/jvmdg/j17/Stub"}
	problems := []Problem{
		{Jar: "a-2.jar", Kind: KindMissing, Cause: "b"},
		{Jar: "a-2.jar", Kind: KindMissing, Cause: "c"},
		{Jar: "a-2.jar", Kind: KindJvmdg, Cause: "JvmDowngrader 1.0 (x.jar)", Details: []string{"xyz.wagyourtail.jvmdg.j17.Stub", "xyz.wagyourtail.jvmdg.j21.New"}},
		{Jar: "other.jar", Kind: KindMissing, Cause: "b"},
	}
	var got []string
	for _, p := range Introduced(problems, "a-2.jar", old) {
		got = append(got, p.Jar+":"+p.Cause+":"+strings.Join(p.Details, ","))
	}
	want := []string{"a-2.jar:c:", "a-2.jar:JvmDowngrader 1.0 (x.jar):xyz.wagyourtail.jvmdg.j21.New", "other.jar:b:"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestReplacedInheritedFromPlatform(t *testing.T) {
	lib := func(members ...string) *Index {
		return &Index{Classes: map[string]*Class{"lib/Event": {Super: "cpw/mods/fml/common/eventhandler/Event", Members: members}}, Deps: map[string][]string{}}
	}
	user := &Index{Classes: map[string]*Class{}, Deps: map[string][]string{}, Refs: []ref{
		{Kind: refMethod, Owner: "lib/Event", Name: "setEnergy", Desc: "(I)V"},
		{Kind: refMethod, Owner: "lib/Event", Name: "func_1_a", Desc: "()V"},
	}}
	jars := []Jar{{Name: "lib-2.jar", Index: lib()}, {Name: "user.jar", Index: user}}
	old := map[string]*Index{"lib-2.jar": lib(member(refMethod, "setEnergy", "(I)V"), member(refMethod, "func_1_a", "()V"))}
	ps := Replaced(jars, old, Env{})
	if len(ps) != 1 || !slices.Equal(ps[0].Details, []string{"Event.setEnergy()"}) {
		t.Fatalf("got %+v", ps)
	}
}

func TestDuplicateMods(t *testing.T) {
	a := jar("a-1.jar", []string{"a"}, nil)
	a.Index.Mods = []string{"A"}
	b := jar("a-2.jar", []string{"a"}, nil)
	b.Index.Mods = []string{"a"}
	c := jar("lib.jar", []string{"a"}, nil)
	var got []string
	for _, p := range Check([]Jar{a, b, c}, Env{}) {
		if p.Kind == KindDuplicate {
			got = append(got, p.Jar+"<"+p.Cause)
		}
	}
	if want := []string{"a-1.jar<a-2.jar", "a-2.jar<a-1.jar"}; !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}
