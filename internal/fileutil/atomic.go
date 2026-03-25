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
		// NOTE: On Windows, os.Rename cannot atomically replace an existing file.
		// This two-step remove+rename has a TOCTOU race where another process could
		// create the target between removal and rename. A proper fix would use
		// MoveFileEx with MOVEFILE_REPLACE_EXISTING via golang.org/x/sys/windows.
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
