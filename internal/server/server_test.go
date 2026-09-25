package server

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/blackkriger/modhound/internal/backup"
	"github.com/blackkriger/modhound/internal/jarinfo"
	"github.com/blackkriger/modhound/internal/resolve"
)

func writeJar(t *testing.T, path, modid, payload string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	info, _ := w.Create("mcmod.info")
	info.Write([]byte(`[{"modid":"` + modid + `","name":"` + modid + `"}]`))
	data, _ := w.Create("payload.txt")
	data.Write([]byte(payload))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func clientPack(t *testing.T, dir string) *resolve.Pack {
	t.Helper()
	pack := &resolve.Pack{Root: filepath.Dir(dir), MCVersion: "1.7.10"}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		j, err := jarinfo.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		pack.Mods = append(pack.Mods, &resolve.Mod{Key: e.Name(), Name: e.Name(), FileName: e.Name(), Path: p, Jar: j})
	}
	return pack
}

func TestCompareAndSync(t *testing.T) {
	base := t.TempDir()
	client := filepath.Join(base, "client", "mods")
	srv := filepath.Join(base, "server", "mods")
	os.MkdirAll(client, 0o755)
	os.MkdirAll(filepath.Join(srv, "1.7.10"), 0o755)

	writeJar(t, filepath.Join(client, "AppleCore-3.3.13.jar"), "applecore", "new")
	writeJar(t, filepath.Join(srv, "AppleCore-3.3.10.jar"), "applecore", "old")
	writeJar(t, filepath.Join(client, "gtnhlib-0.11.51.jar"), "gtnhlib", "new")
	writeJar(t, filepath.Join(srv, "1.7.10", "gtnhlib-0.9.65.jar"), "gtnhlib", "old")
	writeJar(t, filepath.Join(client, "Patches-1.0.jar"), "patches", "client build")
	writeJar(t, filepath.Join(srv, "Patches-1.0.jar"), "patches", "server build")
	writeJar(t, filepath.Join(client, "Tome-2.0.jar"), "examplemod", "a")
	writeJar(t, filepath.Join(srv, "Magitek-0.3.jar"), "examplemod", "b")
	writeJar(t, filepath.Join(client, "Other-2.0.jar"), "other", "a")
	writeJar(t, filepath.Join(srv, "Other-1.0.jar"), "different", "b")
	writeJar(t, filepath.Join(srv, "Serverside-1.0.jar"), "serverside", "x")
	writeJar(t, filepath.Join(client, "ClientOnly-1.0.jar"), "clientonly", "x")

	pack := clientPack(t, client)
	diff, err := Compare(Snapshot(pack), pack.MCVersion, filepath.Join(base, "server"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range diff.Items {
		got[it.ServerFile] = it.ClientFile
	}
	want := map[string]string{"AppleCore-3.3.10.jar": "AppleCore-3.3.13.jar", "gtnhlib-0.9.65.jar": "gtnhlib-0.11.51.jar"}
	if len(got) != len(want) {
		t.Fatalf("diff = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("diff = %v, want %v", got, want)
		}
	}

	backups := filepath.Join(base, "backups")
	session := backup.Begin(backups, pack.Root)
	results, err := Sync(context.Background(), diff, session, pack.MCVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if !r.OK {
			t.Fatalf("sync %s: %s", r.Name, r.Error)
		}
	}
	if err := session.Save(); err != nil {
		t.Fatal(err)
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	for _, p := range []string{filepath.Join(srv, "AppleCore-3.3.13.jar"), filepath.Join(srv, "1.7.10", "gtnhlib-0.11.51.jar"), filepath.Join(srv, "Patches-1.0.jar"), filepath.Join(srv, "Serverside-1.0.jar")} {
		if !exists(p) {
			t.Fatalf("%s missing after sync", p)
		}
	}
	for _, p := range []string{filepath.Join(srv, "AppleCore-3.3.10.jar"), filepath.Join(srv, "1.7.10", "gtnhlib-0.9.65.jar"), filepath.Join(srv, "ClientOnly-1.0.jar")} {
		if exists(p) {
			t.Fatalf("%s present after sync", p)
		}
	}

	if again, _ := Compare(Snapshot(pack), pack.MCVersion, filepath.Join(base, "server")); len(again.Items) != 0 {
		t.Fatalf("server still behind after sync: %+v", again.Items)
	}

	all := backup.All(backups, pack.Root)
	if len(all) == 0 {
		t.Fatal("no backup session")
	}
	last := all[0]
	for _, r := range last.Restore(map[string]bool{KeyPrefix + "AppleCore-3.3.13.jar|AppleCore-3.3.13.jar": true}) {
		if !r.OK {
			t.Fatalf("restore %s: %s", r.Name, r.Error)
		}
	}
	if !exists(filepath.Join(srv, "AppleCore-3.3.10.jar")) || exists(filepath.Join(srv, "AppleCore-3.3.13.jar")) {
		t.Fatal("single restore did not bring back the old server file")
	}
	for _, r := range last.Restore(nil) {
		if !r.OK {
			t.Fatalf("restore %s: %s", r.Name, r.Error)
		}
	}
	if !exists(filepath.Join(srv, "1.7.10", "gtnhlib-0.9.65.jar")) || exists(filepath.Join(srv, "1.7.10", "gtnhlib-0.11.51.jar")) {
		t.Fatal("full restore did not bring back the old server file")
	}
}

func TestSyncRefusesLockedFiles(t *testing.T) {
	base := t.TempDir()
	client := filepath.Join(base, "client", "mods")
	srv := filepath.Join(base, "server", "mods")
	os.MkdirAll(client, 0o755)
	os.MkdirAll(srv, 0o755)
	writeJar(t, filepath.Join(client, "AppleCore-3.3.13.jar"), "applecore", "new")
	old := filepath.Join(srv, "AppleCore-3.3.10.jar")
	writeJar(t, old, "applecore", "old")
	pack := clientPack(t, client)
	diff, err := Compare(Snapshot(pack), pack.MCVersion, filepath.Join(base, "server"))
	if err != nil || len(diff.Items) != 1 {
		t.Fatalf("diff %v %v", diff, err)
	}
	if runtime.GOOS != "windows" {
		t.Skip("open files are locked only on Windows")
	}
	f, err := os.Open(old)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := Sync(context.Background(), diff, backup.Begin(filepath.Join(base, "backups"), pack.Root), pack.MCVersion); err != ErrInUse {
		t.Fatalf("err = %v, want ErrInUse", err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("old server file moved despite the lock")
	}
}

func TestOnlyKeepsUpdatedMods(t *testing.T) {
	d := &Diff{Items: []Item{{Name: "a", key: "a"}, {Name: "b", key: "b"}, {Name: "c", key: "c"}}}
	d.Only(map[string]bool{"b": true})
	if len(d.Items) != 1 || d.Items[0].Name != "b" {
		t.Fatalf("items = %+v", d.Items)
	}
}
