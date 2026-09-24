package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/blackkriger/modhound/internal/resolve"
)

func TestInUse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mod.jar")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if InUse(path) {
		t.Fatal("a closed file is not in use")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if runtime.GOOS == "windows" && !InUse(path) {
		t.Fatal("an open file is in use")
	}
}

func TestMissingRequired(t *testing.T) {
	pack := &resolve.Pack{ModIDs: map[string]bool{"gtnhlib": true}}
	got := missingRequired(pack, []string{"Baubles"}, []string{"Forge", "gtnhlib@[0.9,)", "Baubles", "NewLib,MinecraftForge"})
	if len(got) != 1 || got[0] != "newlib" {
		t.Fatalf("got %v", got)
	}
}
