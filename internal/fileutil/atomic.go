package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
)

// AtomicWrite writes data to a temp file in the same directory as path,
// then atomically renames it to the target. This prevents partial writes
// from corrupting the target file on crash or power loss.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return err
	}
	if runtime.GOOS == "windows" {
		// os.Rename on Windows cannot replace an existing file.
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
