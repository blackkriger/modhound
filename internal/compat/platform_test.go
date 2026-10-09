package compat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformMissing(t *testing.T) {
	l := &Platform{Prefix: "org/lwjgl/", Foreign: "lwjgl3", Classes: map[string]*Class{
		"org/lwjgl/opengl/GL43":                  {Super: "java/lang/Object", Members: []string{member(refMethod, "glClearBufferSubData", "(IIJIILjava/nio/ByteBuffer;)V")}},
		"org/lwjgl/opengl/ContextCapabilities":   {Super: "java/lang/Object", Members: []string{member(refField, "OpenGL43", "Z")}},
		"org/lwjgl/util/vector/ReadableVector":   {Super: "java/lang/Object", Members: []string{member(refMethod, "length", "()F")}},
		"org/lwjgl/util/vector/ReadableVector2f": {Super: "java/lang/Object", Ifaces: []string{"org/lwjgl/util/vector/ReadableVector"}},
		"org/lwjgl/util/vector/ReadableVector3f": {Super: "java/lang/Object", Ifaces: []string{"org/lwjgl/util/vector/ReadableVector2f"}},
	}}
	cases := []struct {
		r    ref
		want bool
	}{
		{ref{Kind: refMethod, Owner: "org/lwjgl/opengl/GL43", Name: "glClearBufferSubData", Desc: "(IIJJIILjava/nio/ByteBuffer;)V"}, true},
		{ref{Kind: refMethod, Owner: "org/lwjgl/opengl/GL43", Name: "glClearBufferSubData", Desc: "(IIJIILjava/nio/ByteBuffer;)V"}, false},
		{ref{Kind: refField, Owner: "org/lwjgl/opengl/ContextCapabilities", Name: "OpenGL45", Desc: "Z"}, true},
		{ref{Kind: refMethod, Owner: "org/lwjgl/opengl/GL43", Name: "hashCode", Desc: "()I"}, false},
		{ref{Kind: refMethod, Owner: "org/lwjgl/opengl/GL45", Name: "glCreateBuffers", Desc: "()I"}, false},
		{ref{Kind: refMethod, Owner: "org/lwjgl/util/vector/ReadableVector3f", Name: "length", Desc: "()F"}, false},
	}
	for _, c := range cases {
		if got := l.missing(c.r); got != c.want {
			t.Errorf("%s.%s%s: got %v want %v", c.r.Owner, c.r.Name, c.r.Desc, got, c.want)
		}
	}
	if !l.foreign(&class{Name: "a/lwjgl3/Backend"}) || !l.foreign(&class{Name: "a/B", Classes: []string{"org/lwjgl/system/MemoryStack"}}) {
		t.Error("LWJGL3 classes must be foreign")
	}
	if l.foreign(&class{Name: "a/lwjgl2/Backend", Classes: []string{"org/lwjgl/opengl/GL45"}}) {
		t.Error("a missing class in a known package is not foreign")
	}
}

func TestFindLWJGL(t *testing.T) {
	root := t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(s), 0o644)
	}
	write(filepath.Join(root, "versions", "Forge 1.7.10", "Forge 1.7.10.json"), `{"id":"Forge 1.7.10","libraries":[{"name":"org.lwjgl.lwjgl:lwjgl:2.9.1"},{"name":"org.lwjgl.lwjgl:lwjgl_util:2.9.1"}]}`)
	write(filepath.Join(root, "versions", "1.12.2", "1.12.2.json"), `{"id":"1.12.2","libraries":[{"name":"org.lwjgl.lwjgl:lwjgl:2.9.4"}]}`)
	write(filepath.Join(root, "versions", "1.7.10", "1.7.10.json"), `{"id":"1.7.10","libraries":[{"name":"org.lwjgl.lwjgl:lwjgl:2.9.1","rules":[{"action":"allow"},{"action":"disallow","os":{"name":"osx"}}]},{"name":"org.lwjgl.lwjgl:lwjgl:2.9.2-nightly-20140822","rules":[{"action":"allow","os":{"name":"osx"}}]}]}`)
	write(filepath.Join(root, "libraries", "org", "lwjgl", "lwjgl", "lwjgl", "2.9.1", "lwjgl-2.9.1.jar"), "x")
	j := FindLWJGL(root, "1.7.10")
	if j == nil || j.Name != "LWJGL 2.9.1" || len(j.Paths) != 1 {
		t.Fatalf("got %+v", j)
	}
}
