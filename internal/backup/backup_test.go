package backup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blackkriger/modhound/internal/fsx"
)

func TestKeepAndRestore(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	oldPath := filepath.Join(mods, "mod-1.0.jar")
	newPath := filepath.Join(mods, "mod-1.1.jar")
	os.WriteFile(oldPath, []byte("old"), 0o644)

	s := Begin(root, mods)
	item := Item{Key: "k", Name: "Mod", Dir: mods, OldFile: "mod-1.0.jar", NewFile: "mod-1.1.jar"}
	if _, err := s.Keep(fsx.Local, oldPath, "", &item); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(newPath, []byte("new"), 0o644)
	s.Add(item)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	all := All(root, mods)
	if len(all) != 1 || len(all[0].Pending()) != 1 {
		t.Fatalf("sessions: %d", len(all))
	}
	last := all[0]
	res := last.Restore(map[string]bool{"k|mod-1.1.jar": true})
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("restore: %+v", res)
	}
	if b, _ := os.ReadFile(oldPath); string(b) != "old" {
		t.Fatal("old file was not put back")
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatal("new file is still there")
	}
	if len(All(root, mods)) != 0 {
		t.Fatal("a fully restored session is not offered again")
	}
}

func TestKeepsPreviousVersionPerMod(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	put := func(name, body string) { os.WriteFile(filepath.Join(mods, name), []byte(body), 0o644) }
	update := func(key, from, to string) {
		s := Begin(root, mods)
		item := Item{Key: key, Dir: mods, OldFile: from, NewFile: to}
		if _, err := s.Keep(fsx.Local, filepath.Join(mods, from), "", &item); err != nil {
			t.Fatal(err)
		}
		put(to, to)
		s.Add(item)
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	put("a-1.jar", "a-1.jar")
	put("b-1.jar", "b-1.jar")
	update("a", "a-1.jar", "a-2.jar")
	update("b", "b-1.jar", "b-2.jar")
	all := All(root, mods)
	if len(all) != 2 {
		t.Fatalf("both mods keep their previous version, got %d sessions", len(all))
	}
	update("a", "a-2.jar", "a-3.jar")
	var keys []string
	for _, s := range All(root, mods) {
		for _, it := range s.Pending() {
			keys = append(keys, it.Key+":"+it.OldFile)
		}
	}
	if len(keys) != 2 || keys[0] != "a:a-2.jar" || keys[1] != "b:b-1.jar" {
		t.Fatalf("expected a:a-2.jar and b:b-1.jar, got %v", keys)
	}
}

func TestSessionGrowsAcrossSaves(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	s := Begin(root, mods)
	for _, name := range []string{"a", "b"} {
		old := name + "-1.jar"
		os.WriteFile(filepath.Join(mods, old), []byte(old), 0o644)
		item := Item{Key: name, Dir: mods, OldFile: old, NewFile: name + "-2.jar"}
		if _, err := s.Keep(fsx.Local, filepath.Join(mods, old), "", &item); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(mods, item.NewFile), []byte(item.NewFile), 0o644)
		s.Add(item)
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	all := All(root, mods)
	if len(all) != 1 || len(all[0].Pending()) != 2 {
		t.Fatalf("sessions = %d, want one session with both mods", len(all))
	}
	for _, r := range all[0].Restore(nil) {
		if !r.OK {
			t.Fatalf("restore %s: %s", r.Key, r.Error)
		}
	}
	for _, name := range []string{"a-1.jar", "b-1.jar"} {
		if _, err := os.Stat(filepath.Join(mods, name)); err != nil {
			t.Fatalf("%s not restored", name)
		}
	}
}

func TestPruneFollowsTheFileNotTheProject(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	put := func(name string) { os.WriteFile(filepath.Join(mods, name), []byte(name), 0o644) }
	update := func(from, to string) {
		s := Begin(root, mods)
		item := Item{Key: "cf:1", Dir: mods, OldFile: from, NewFile: to}
		if _, err := s.Keep(fsx.Local, filepath.Join(mods, from), "", &item); err != nil {
			t.Fatal(err)
		}
		put(to)
		s.Add(item)
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
	}
	put("lib-api-1.jar")
	put("lib-1.jar")
	update("lib-api-1.jar", "lib-api-2.jar")
	update("lib-1.jar", "lib-2.jar")
	var kept []string
	for _, s := range All(root, mods) {
		for _, it := range s.Pending() {
			kept = append(kept, it.OldFile)
		}
	}
	if len(kept) != 2 {
		t.Fatalf("both jars of one project keep their backups, got %v", kept)
	}
}

func TestRestoreRefusesASecondVersion(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(mods, "mod-1.0.jar"), []byte("1"), 0o644)
	s := Begin(root, mods)
	item := Item{Key: "k", Dir: mods, OldFile: "mod-1.0.jar", NewFile: "mod-2.0.jar"}
	if _, err := s.Keep(fsx.Local, filepath.Join(mods, "mod-1.0.jar"), "", &item); err != nil {
		t.Fatal(err)
	}
	s.Add(item)
	s.Save()
	os.WriteFile(filepath.Join(mods, "mod-3.0.jar"), []byte("3"), 0o644)
	res := All(root, mods)[0].Restore(map[string]bool{"k|mod-2.0.jar": true})
	if len(res) != 1 || res[0].OK {
		t.Fatalf("restore must refuse while mod-3.0.jar is in the folder: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(mods, "mod-1.0.jar")); err == nil {
		t.Fatal("mod-1.0.jar was put back next to mod-3.0.jar")
	}
}
