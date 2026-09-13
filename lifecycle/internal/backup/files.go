package backup

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var ErrSource = errors.New("backup secret source must be a private regular file containing 16..4096 bytes")
var ErrCapacity = errors.New("insufficient backup capacity")
var ErrDestination = errors.New("backup destination must be an absent basename in a private directory")

// ReadPassphrase reads only an explicitly selected private source. Passwords are
// never accepted as arguments or environment variables. Bytes are used exactly;
// callers should clear the returned slice and must not log the source contents.
func ReadPassphrase(path string) ([]byte, error) {
	f, err := openPrivate(path)
	if err != nil {
		return nil, ErrSource
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) < 16 || len(b) > 4096 {
		clear(b)
		return nil, ErrSource
	}
	return b, nil
}

func openPrivate(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	s, err := f.Stat()
	if err != nil || !s.Mode().IsRegular() || s.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, ErrSource
	}
	return f, nil
}

// Read opens a bounded private archive without following its final symlink.
func Read(path string) ([]byte, error) {
	f, err := openPrivate(path)
	if err != nil {
		return nil, ErrInvalid
	}
	defer f.Close()
	s, err := f.Stat()
	if err != nil || s.Size() < headerSize+28 || s.Size() > MaxPayload+headerSize+28 {
		return nil, ErrInvalid
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxPayload+headerSize+29))
	if err != nil || len(b) > MaxPayload+headerSize+28 {
		clear(b)
		return nil, ErrInvalid
	}
	return b, nil
}

// Publish atomically installs an already sealed archive without replacing any
// existing destination. Temporary ciphertext stays in the installation-private
// directory. The caller must hold the lifecycle operation lock. A killed process
// may leave a private .backup-pending-* ciphertext, never a completion filename.
func Publish(ctx context.Context, dir, name string, blob []byte) error {
	if name == "." || name == ".." || filepath.Base(name) != name || name == "" || len(blob) < headerSize+28 || len(blob) > MaxPayload+headerSize+28 || string(blob[:8]) != magic {
		return ErrDestination
	}
	// An os.Root anchors subsequent operations to the verified directory inode,
	// preventing path-component replacement from redirecting the final link.
	s, err := os.Lstat(dir)
	if err != nil || !s.IsDir() || s.Mode().Perm()&0077 != 0 {
		return ErrDestination
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return ErrDestination
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(s, actual) {
		return ErrDestination
	}
	if _, err = root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return ErrDestination
	}
	d, err := root.Open(".")
	if err != nil {
		return ErrDestination
	}
	defer d.Close()
	var stat syscall.Statfs_t
	if err = syscall.Fstatfs(int(d.Fd()), &stat); err != nil {
		return ErrCapacity
	}
	// Leave a modest reserve for state/diagnostic writes on the same filesystem.
	if stat.Bavail*uint64(stat.Bsize) < uint64(len(blob))+(16<<20) {
		return ErrCapacity
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Randomness comes from the standard library; O_EXCL prevents accidental
	// adoption even in the vanishingly unlikely event of a name collision.
	pending := ".backup-pending-" + rand.Text()
	f, err := root.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrDestination
	}
	defer root.Remove(pending)
	for start := 0; start < len(blob) && err == nil; {
		if err = ctx.Err(); err != nil {
			break
		}
		end := min(start+(1<<20), len(blob))
		var n int
		n, err = f.Write(blob[start:end])
		start += n
		if err == nil && n == 0 {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// A hard link publishes atomically with no-clobber semantics. This link is
	// local ciphertext publication, not an accepted archive hardlink entry.
	if err = root.Link(pending, name); err != nil {
		return ErrDestination
	}
	if err = root.Remove(pending); err != nil {
		return err
	}
	return d.Sync()
}
