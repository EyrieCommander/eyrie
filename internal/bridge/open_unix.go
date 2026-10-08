//go:build unix

package bridge

import (
	"errors"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// rootHandle is a root directory opened once at startup. Every lookup
// starts from this descriptor, so replacing the root's pathname later (for
// example with a symlink to somewhere else) has no effect.
type rootHandle struct {
	fd int
}

func openRootHandle(path string) (*rootHandle, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return &rootHandle{fd: fd}, nil
}

func (h *rootHandle) close() { _ = unix.Close(h.fd) }

type openKind int

const (
	wantFile openKind = iota
	wantDir
)

// classify turns an openat failure on name into a bridge error: a symlink
// is denied, anything else is not found.
func classify(dirfd int, name string, err error) error {
	var st unix.Stat_t
	if serr := unix.Fstatat(dirfd, name, &st, unix.AT_SYMLINK_NOFOLLOW); serr == nil && st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return errDenied
	}
	if errors.Is(err, unix.ELOOP) {
		return errDenied
	}
	return errNotFound
}

// openRel opens rel ("." or a cleaned slash path) beneath the root without
// ever following a symlink: each component is opened with openat relative
// to the previous descriptor and O_NOFOLLOW. The final open is O_NONBLOCK so
// a FIFO or device can't block the request; the kind is then checked with
// fstat on the opened descriptor, so a swap after any earlier check can't
// change what is read. Deny-list checks run on every component name.
func (f *FS) openRel(h *rootHandle, rel string, kind openKind) (*os.File, error) {
	comps := []string{}
	if rel != "." {
		comps = strings.Split(rel, "/")
	}
	for _, c := range comps {
		if f.denied(c) {
			return nil, errDenied
		}
	}
	// A fresh open file description, not dup: dup shares the directory read
	// offset with the root handle, so one ReadDir would empty later ones.
	dirfd, err := unix.Openat(h.fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errNotFound
	}
	for i, c := range comps {
		last := i == len(comps)-1
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if !last || kind == wantDir {
			flags |= unix.O_DIRECTORY
		}
		if last {
			flags |= unix.O_NONBLOCK
		}
		nfd, err := unix.Openat(dirfd, c, flags, 0)
		if err != nil {
			e := classify(dirfd, c, err)
			unix.Close(dirfd)
			if errors.Is(err, unix.ENOTDIR) && e == errNotFound {
				if last && kind == wantDir {
					return nil, errNotDir
				}
			}
			return nil, e
		}
		unix.Close(dirfd)
		dirfd = nfd
	}
	var st unix.Stat_t
	if err := unix.Fstat(dirfd, &st); err != nil {
		unix.Close(dirfd)
		return nil, errNotFound
	}
	mode := st.Mode & unix.S_IFMT
	switch kind {
	case wantDir:
		if mode != unix.S_IFDIR {
			unix.Close(dirfd)
			return nil, errNotDir
		}
	case wantFile:
		if mode != unix.S_IFREG {
			unix.Close(dirfd)
			return nil, errNotFile
		}
		// Regular file: clear O_NONBLOCK so reads behave normally.
		if fl, err := unix.FcntlInt(uintptr(dirfd), unix.F_GETFL, 0); err == nil {
			_, _ = unix.FcntlInt(uintptr(dirfd), unix.F_SETFL, fl&^unix.O_NONBLOCK)
		}
	}
	return os.NewFile(uintptr(dirfd), rel), nil
}

// openChild opens one entry of an already-open directory, no follow,
// non-blocking, and checks its kind by fstat. Used by search.
func openChild(dir *os.File, name string, kind openKind) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
	if kind == wantDir {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(int(dir.Fd()), name, flags, 0)
	if err != nil {
		return nil, errNotFound
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		unix.Close(fd)
		return nil, errNotFound
	}
	mode := st.Mode & unix.S_IFMT
	if (kind == wantDir && mode != unix.S_IFDIR) || (kind == wantFile && mode != unix.S_IFREG) {
		unix.Close(fd)
		return nil, errNotFile
	}
	if kind == wantFile {
		if fl, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0); err == nil {
			_, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFL, fl&^unix.O_NONBLOCK)
		}
	}
	return os.NewFile(uintptr(fd), name), nil
}

// statAt stats name inside the open directory d without following links.
func statAt(d *os.File, name string) (int64, time.Time, bool) {
	var st unix.Stat_t
	if err := unix.Fstatat(int(d.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, time.Time{}, false
	}
	return st.Size, time.Unix(st.Mtim.Unix()), true
}
