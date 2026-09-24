package launchers

import (
	"os"
	"path/filepath"
	"strings"
)

func Default() string {
	config, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(config, ".minecraft")
	entries, err := os.ReadDir(filepath.Join(dir, "mods"))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if !e.IsDir() && (strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".zip")) {
			return dir
		}
	}
	return ""
}
