package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveTreeMerges(t *testing.T) {
	src, dst := filepath.Join(t.TempDir(), "backups"), filepath.Join(t.TempDir(), "local", "backups")
	put := func(p, body string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	put(filepath.Join(src, "pack", "s1", "session.json"), "old")
	put(filepath.Join(src, "pack", "s1", "mod-1.jar"), "jar")
	if err := moveTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "pack", "s1", "mod-1.jar")); string(b) != "jar" {
		t.Fatal("backup was not moved")
	}
	put(filepath.Join(src, "pack", "s2", "session.json"), "new")
	put(filepath.Join(src, "pack", "s1", "session.json"), "conflict")
	if err := moveTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "pack", "s2", "session.json")); string(b) != "new" {
		t.Fatal("new session was not merged")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "pack", "s1", "session.json")); string(b) != "old" {
		t.Fatal("existing file was overwritten")
	}
	if _, err := os.Stat(filepath.Join(src, "pack", "s1", "session.json")); err != nil {
		t.Fatal("a conflicting file must stay where it was")
	}
}
