//go:build !windows

package install

func InUse(string) bool {
	return false
}
