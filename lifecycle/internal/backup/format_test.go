package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testPass = []byte("test-only passphrase, not a product credential")

func fixture(t *testing.T) (Manifest, map[string][]byte) {
	t.Helper()
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	m := Manifest{ProductVersion: "1.13.4", ReleaseDigest: "sha256:" + strings.Repeat("a", 64), Operation: strings.Repeat("b", 64), Started: now, Finished: now.Add(time.Second), Complete: true, Domains: map[string]Domain{}}
	for _, name := range domains {
		m.Domains[name] = Domain{Cutover: now, Start: now, End: now, Counts: map[string]int64{"records": 0}, Offsets: map[string]int64{}, Drained: true, CatalogDigest: "sha256:" + strings.Repeat("c", 64)}
	}
	files := map[string][]byte{}
	for _, name := range names {
		files[name] = []byte("logical export fixture")
	}
	var err error
	files[SecretEntry], err = SealSecrets([]byte(`{"test_secret":"never plaintext in payload"}`), testPass)
	if err != nil {
		t.Fatal(err)
	}
	return m, files
}

func TestAuthenticatedRoundTrip(t *testing.T) {
	m, files := fixture(t)
	blob, err := Seal(m, files, testPass)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, files["mysql.sql"]) || bytes.Contains(blob, []byte("test_secret")) {
		t.Fatal("plaintext leaked")
	}
	got, restored, err := Open(blob, testPass)
	if err != nil || got.ProductVersion != m.ProductVersion || got.ReleaseDigest != m.ReleaseDigest {
		t.Fatalf("roundtrip: %v", err)
	}
	for name, want := range files {
		if !bytes.Equal(restored[name], want) {
			t.Fatalf("changed entry %s", name)
		}
	}
	if _, err := OpenSecrets(blob, testPass); !errors.Is(err, ErrInvalid) {
		t.Fatal("archive envelope accepted as secret entry")
	}
	secret, err := OpenSecrets(restored[SecretEntry], testPass)
	if err != nil || !bytes.Contains(secret, []byte("test_secret")) {
		t.Fatal("secret not recoverable")
	}
	clear(secret)
	if _, _, err = Open(blob, []byte("different sufficiently long passphrase")); !errors.Is(err, ErrInvalid) {
		t.Fatal("wrong passphrase accepted")
	}
	blob[len(blob)-1] ^= 1
	if _, data, err := Open(blob, testPass); !errors.Is(err, ErrInvalid) || data != nil {
		t.Fatal("tamper returned payload")
	}
}

func TestArchiveBoundary(t *testing.T) {
	m, files := fixture(t)
	blob, err := Seal(m, files, testPass)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decrypt(blob, testPass)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plain)
	valid, _, err := parse(plain)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(valid)
	oversized := valid
	oversized.Files = append([]File(nil), valid.Files...)
	oversized.Files[0].Size = MaxPayload + 1
	oversizedRaw, _ := json.Marshal(oversized)
	build := func(manifest []byte, change func(int, *tar.Header) []byte) []byte {
		var b bytes.Buffer
		w := tar.NewWriter(&b)
		if err := w.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(manifest)), Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		w.Write(manifest)
		for i, f := range valid.Files {
			data := files[f.Path]
			h := &tar.Header{Name: f.Path, Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(data)), Format: tar.FormatUSTAR}
			if change != nil {
				if replacement := change(i, h); replacement != nil {
					data = replacement
				}
			}
			if err := w.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if h.Typeflag == tar.TypeReg {
				if _, err := w.Write(data); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	cases := map[string][]byte{
		"oversized-entry": build(oversizedRaw, nil),
		"absolute": build(raw, func(i int, h *tar.Header) []byte {
			if i == 0 {
				h.Name = "/config.json"
			}
			return nil
		}),
		"traversal": build(raw, func(i int, h *tar.Header) []byte {
			if i == 0 {
				h.Name = "../config.json"
			}
			return nil
		}),
		"symlink": build(raw, func(i int, h *tar.Header) []byte {
			if i == 0 {
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "/tmp/foreign"
				h.Size = 0
			}
			return nil
		}),
		"hardlink": build(raw, func(i int, h *tar.Header) []byte {
			if i == 0 {
				h.Typeflag = tar.TypeLink
				h.Linkname = "/tmp/foreign"
				h.Size = 0
			}
			return nil
		}),
		"duplicate": build(raw, func(i int, h *tar.Header) []byte {
			if i == 1 {
				h.Name = valid.Files[0].Path
			}
			return nil
		}),
		"checksum": build(raw, func(i int, h *tar.Header) []byte {
			if i == 0 {
				return bytes.Repeat([]byte{'X'}, int(h.Size))
			}
			return nil
		}),
		"unknown-field":   build(bytes.Replace(raw, []byte(`"format":1`), []byte(`"required_future":1,"format":1`), 1), nil),
		"duplicate-key":   build(bytes.Replace(raw, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1), nil),
		"incomplete":      build(bytes.Replace(raw, []byte(`"complete":true`), []byte(`"complete":false`), 1), nil),
		"missing-trailer": plain[:len(plain)-1024],
		"trailing":        append(append([]byte{}, plain...), 0),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, data, err := parse(input); err == nil || data != nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestExportContract(t *testing.T) {
	m, files := fixture(t)
	d := m.Domains["kafka"]
	d.Drained = false
	m.Domains["kafka"] = d
	if _, err := Seal(m, files, testPass); err == nil {
		t.Fatal("undrained queue accepted")
	}
	d.Drained = true
	d.Cutover = m.Finished
	m.Domains["kafka"] = d
	if _, err := Seal(m, files, testPass); err == nil {
		t.Fatal("inconsistent cutover accepted")
	}
	d.Cutover = m.Started
	m.Domains["kafka"] = d
	files[SecretEntry] = []byte("plaintext credentials")
	if _, err := Seal(m, files, testPass); err == nil {
		t.Fatal("plaintext secret entry accepted")
	}
}

func TestPrivatePublication(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m, files := fixture(t)
	blob, err := Seal(m, files, testPass)
	if err != nil {
		t.Fatal(err)
	}
	if err = Publish(context.Background(), dir, "archive.gpb", blob); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "archive.gpb")
	read, err := Read(path)
	if err != nil || !bytes.Equal(read, blob) {
		t.Fatal("published ciphertext changed")
	}
	if err = Publish(context.Background(), dir, "archive.gpb", blob); !errors.Is(err, ErrDestination) {
		t.Fatal("overwrote existing archive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = Publish(ctx, dir, "cancelled.gpb", blob); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled publication succeeded")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("left cancelled or pending archive")
	}
	if err = os.Symlink(path, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err = Read(filepath.Join(dir, "link")); err == nil {
		t.Fatal("followed archive symlink")
	}
}

func TestPrivatePassphraseSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(path, testPass, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPassphrase(path)
	if err != nil || !bytes.Equal(got, testPass) {
		t.Fatal("private source not read exactly")
	}
	clear(got)
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadPassphrase(path); !errors.Is(err, ErrSource) {
		t.Fatal("public source accepted")
	}
}
