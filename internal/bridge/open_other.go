//go:build !unix

package bridge

import (
	"errors"
	"os"
	"time"
)

// The bridge's no-follow file access needs openat(O_NOFOLLOW). On platforms
// without it, roots never open, so every fs call returns 404.

type rootHandle struct{}

type openKind int

const (
	wantFile openKind = iota
	wantDir
)

func openRootHandle(string) (*rootHandle, error) {
	return nil, errors.New("bridge fs access is only supported on unix")
}

func (h *rootHandle) close() {}

func (f *FS) openRel(*rootHandle, string, openKind) (*os.File, error) { return nil, errNotFound }

func openChild(*os.File, string, openKind) (*os.File, error) { return nil, errNotFound }

func statAt(*os.File, string) (int64, time.Time, bool) { return 0, time.Time{}, false }
