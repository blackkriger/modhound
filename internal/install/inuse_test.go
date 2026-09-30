package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
