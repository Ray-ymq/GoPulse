// Package control implements the Linux product lifecycle. Docker diagnostics are
// deliberately not forwarded: they may contain interpolated application secrets.
package control

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

type State struct {
	Schema      int    `json:"schema"`
	Version     string `json:"version"`
	Manifest    string `json:"manifest_digest"`
	Project     string `json:"project"`
	Token       string `json:"installation_token"`
	Port        int    `json:"edge_port"`
	Phase       string `json:"phase"`
	Operation   string `json:"operation_id"`
	FailedStage string `json:"failed_stage,omitempty"`
}

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func atomicFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func privateDir(path string) error {
	s, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !s.IsDir() || s.Mode().Perm()&0077 != 0 {
		return errors.New("installation directory must be a private real directory (0700)")
	}
	return nil
}
func readPrivate(path string) ([]byte, error) {
	s, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !s.Mode().IsRegular() || s.Mode().Perm()&0077 != 0 {
		return nil, errors.New("unsafe private file permissions")
	}
	return os.ReadFile(path)
}
func lock(dir string) (func(), error) {
	fd, err := syscall.Open(filepath.Join(dir, ".lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); syscall.Close(fd) }, nil
}
func (c *Controller) save() error {
	b, e := json.MarshalIndent(c.state, "", "  ")
	if e != nil {
		return e
	}
	return atomicFile(filepath.Join(c.dir, "state.json"), append(b, '\n'))
}
