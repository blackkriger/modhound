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

func TestRestoreAddedRemovesIt(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	added := filepath.Join(mods, "lib-2.0.jar")
	os.WriteFile(added, []byte("lib"), 0o644)

	s := Begin(root, mods)
	s.Add(Item{Key: "lib", Name: "Lib", Dir: mods, NewFile: "lib-2.0.jar", Added: true})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	res := All(root, mods)[0].Restore(map[string]bool{"lib|lib-2.0.jar": true})
	if len(res) != 1 || !res[0].OK || !res[0].Added {
		t.Fatalf("restore: %+v", res)
	}
	if _, err := os.Stat(added); !os.IsNotExist(err) {
		t.Fatal("added file is still there")
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

func TestRemovedSurvivesLaterAddAndRestores(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	gone := filepath.Join(mods, "old-1.0.jar")
	os.WriteFile(gone, []byte("old"), 0o644)

	s := Begin(root, mods)
	item := Item{Key: "old", Name: "Old", Dir: mods, OldFile: "old-1.0.jar", Removed: true}
	if _, err := s.Keep(fsx.Local, gone, "", &item); err != nil {
		t.Fatal(err)
	}
	s.Add(item)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatal("removed file is still in the mods folder")
	}

	os.WriteFile(filepath.Join(mods, "lib-1.0.jar"), []byte("lib"), 0o644)
	later := Begin(root, mods)
	later.ID += "-later"
	later.dir += "-later"
	later.Add(Item{Key: "lib", Dir: mods, NewFile: "lib-1.0.jar", Added: true})
	if err := later.Save(); err != nil {
		t.Fatal(err)
	}

	var restored []Result
	for _, sess := range All(root, mods) {
		restored = append(restored, sess.Restore(map[string]bool{"removed|old|old-1.0.jar": true})...)
	}
	if len(restored) != 1 || !restored[0].OK || !restored[0].Removed {
		t.Fatalf("restore: %+v", restored)
	}
	if b, _ := os.ReadFile(gone); string(b) != "old" {
		t.Fatal("removed file was not put back")
	}
}

func TestPurgeRemoved(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(mods, "gone-1.0.jar"), []byte("gone"), 0o644)
	os.WriteFile(filepath.Join(mods, "mod-1.0.jar"), []byte("old"), 0o644)
	s := Begin(root, mods)
	gone := Item{Key: "gone", Dir: mods, OldFile: "gone-1.0.jar", Removed: true}
	if _, err := s.Keep(fsx.Local, filepath.Join(mods, "gone-1.0.jar"), "", &gone); err != nil {
		t.Fatal(err)
	}
	s.Add(gone)
	upd := Item{Key: "mod", Dir: mods, OldFile: "mod-1.0.jar", NewFile: "mod-1.1.jar"}
	if _, err := s.Keep(fsx.Local, filepath.Join(mods, "mod-1.0.jar"), "", &upd); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mods, "mod-1.1.jar"), []byte("new"), 0o644)
	s.Add(upd)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	PurgeRemoved(root, mods, false, "")
	all := All(root, mods)
	if len(all) != 1 || len(all[0].Pending()) != 1 || all[0].Pending()[0].Key != "mod" {
		t.Fatalf("after purge: %+v", all)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "gone-1.0.jar")); !os.IsNotExist(err) {
		t.Fatal("removed backup is still stored")
	}
}

func TestRemoveKeepsEarlierUpdateBackup(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(mods, "a-1.0.jar"), []byte("1.0"), 0o644)
	up := Begin(root, mods)
	item := Item{Key: "a", Dir: mods, OldFile: "a-1.0.jar", NewFile: "a-1.1.jar"}
	if _, err := up.Keep(fsx.Local, filepath.Join(mods, "a-1.0.jar"), "", &item); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mods, "a-1.1.jar"), []byte("1.1"), 0o644)
	up.Add(item)
	if err := up.Save(); err != nil {
		t.Fatal(err)
	}
	rm := Begin(root, mods)
	rm.ID += "-later"
	rm.dir += "-later"
	gone := Item{Key: "a", Dir: mods, OldFile: "a-1.1.jar", Removed: true}
	if _, err := rm.Keep(fsx.Local, filepath.Join(mods, "a-1.1.jar"), "", &gone); err != nil {
		t.Fatal(err)
	}
	rm.Add(gone)
	if err := rm.Save(); err != nil {
		t.Fatal(err)
	}
	pending := 0
	for _, s := range All(root, mods) {
		pending += len(s.Pending())
	}
	if pending != 2 {
		t.Fatalf("pending = %d, want the update and the removal", pending)
	}
}

func TestRestoreRemovedByFile(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	s := Begin(root, mods)
	for _, name := range []string{"dup-1.0.jar", "dup-1.1.jar"} {
		os.WriteFile(filepath.Join(mods, name), []byte(name), 0o644)
		it := Item{Key: "dup", Dir: mods, OldFile: name, Removed: true}
		if _, err := s.Keep(fsx.Local, filepath.Join(mods, name), "", &it); err != nil {
			t.Fatal(err)
		}
		s.Add(it)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	res := All(root, mods)[0].Restore(map[string]bool{"removed|dup|dup-1.1.jar": true})
	if len(res) != 1 || !res[0].OK || res[0].File != "dup-1.1.jar" {
		t.Fatalf("restore: %+v", res)
	}
}

func TestRestoreRemovedServerPair(t *testing.T) {
	root, mods, srv := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(mods, "a-1.1.jar"), []byte("client"), 0o644)
	os.WriteFile(filepath.Join(srv, "a-0.9.jar"), []byte("server"), 0o644)
	s := Begin(root, mods)
	client := Item{Key: "a", Dir: mods, OldFile: "a-1.1.jar", Removed: true}
	if _, err := s.Keep(fsx.Local, filepath.Join(mods, "a-1.1.jar"), "", &client); err != nil {
		t.Fatal(err)
	}
	s.Add(client)
	server := Item{Key: "server:a", Dir: srv, OldFile: "a-0.9.jar", Removed: true, Pair: "a|a-1.1.jar"}
	if _, err := s.Keep(fsx.Local, filepath.Join(srv, "a-0.9.jar"), "", &server); err != nil {
		t.Fatal(err)
	}
	s.Add(server)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	res := All(root, mods)[0].Restore(map[string]bool{"removed|a|a-1.1.jar": true})
	if len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("restore: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(srv, "a-0.9.jar")); err != nil {
		t.Fatal("server copy was not restored")
	}
}

func TestUndoRemovalLeavesEarlierUpdate(t *testing.T) {
	root, mods := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(mods, "foo-1.jar"), []byte("1"), 0o644)
	up := Begin(root, mods)
	upd := Item{Key: "foo", Dir: mods, OldFile: "foo-1.jar", NewFile: "foo-2.jar"}
	if _, err := up.Keep(fsx.Local, filepath.Join(mods, "foo-1.jar"), "", &upd); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mods, "foo-2.jar"), []byte("2"), 0o644)
	up.Add(upd)
	if err := up.Save(); err != nil {
		t.Fatal(err)
	}
	rm := Begin(root, mods)
	rm.ID += "-later"
	rm.dir += "-later"
	gone := Item{Key: "foo", Dir: mods, OldFile: "foo-2.jar", Removed: true}
	if _, err := rm.Keep(fsx.Local, filepath.Join(mods, "foo-2.jar"), "", &gone); err != nil {
		t.Fatal(err)
	}
	rm.Add(gone)
	if err := rm.Save(); err != nil {
		t.Fatal(err)
	}
	var res []Result
	for _, s := range All(root, mods) {
		res = append(res, s.Restore(map[string]bool{RemovedKey("foo", "foo-2.jar"): true})...)
	}
	if len(res) != 1 || !res[0].Removed || !res[0].OK {
		t.Fatalf("restore: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(mods, "foo-2.jar")); string(b) != "2" {
		t.Fatal("foo-2 is not back")
	}
	if _, err := os.Stat(filepath.Join(mods, "foo-1.jar")); !os.IsNotExist(err) {
		t.Fatal("the earlier update was undone too")
	}
}

func TestPurgeKeepsServerCopyWhenClientWasRestored(t *testing.T) {
	root, mods, srv := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(srv, "a-1.0.jar"), []byte("server"), 0o644)
	s := Begin(root, mods)
	client := Item{Key: "a", Dir: mods, OldFile: "a-1.0.jar", Removed: true, Restored: true}
	server := Item{Key: "server:a", Dir: srv, OldFile: "a-1.0.jar", Removed: true, Pair: RemovedKey("a", "a-1.0.jar")}
	if _, err := s.Keep(fsx.Local, filepath.Join(srv, "a-1.0.jar"), "", &server); err != nil {
		t.Fatal(err)
	}
	s.Add(client)
	s.Add(server)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	PurgeRemoved(root, mods, false, "")
	if _, err := os.Stat(s.Stored(server)); err != nil {
		t.Fatal("server backup of a restored client mod was purged")
	}
}

func TestPurgeDropsServerCopyOnceModIsBack(t *testing.T) {
	root, mods, srv := t.TempDir(), t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(srv, "a-1.0.jar"), []byte("server"), 0o644)
	s := Begin(root, mods)
	client := Item{Key: "a", Dir: mods, OldFile: "a-1.0.jar", Removed: true, Restored: true}
	server := Item{Key: "server:a", Dir: srv, OldFile: "a-1.0.jar", Removed: true, Pair: RemovedKey("a", "a-1.0.jar")}
	if _, err := s.Keep(fsx.Local, filepath.Join(srv, "a-1.0.jar"), "", &server); err != nil {
		t.Fatal(err)
	}
	s.Add(client)
	s.Add(server)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(srv, "a-1.1.jar"), []byte("synced again"), 0o644)
	if n := PurgeRemoved(root, mods, false, ""); n != 1 {
		t.Fatalf("purged %d, want the stale server copy", n)
	}
	if _, err := os.Stat(s.Stored(server)); !os.IsNotExist(err) {
		t.Fatal("stale server backup kept")
	}
}
