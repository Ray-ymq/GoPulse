package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestNativeNodeArchiveChecksDigestAndEntrypoint(t *testing.T) {
	var data bytes.Buffer
	zipped := gzip.NewWriter(&data)
	archive := tar.NewWriter(zipped)
	binary := []byte("\x7fELFofficial-fixture")
	if err := archive.WriteHeader(&tar.Header{Name: "node_exporter-1.9.1.linux-amd64/node_exporter", Mode: 0755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipped.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data.Bytes())
	digest := hex.EncodeToString(sum[:])
	got, err := extractNativeNode(data.Bytes(), digest, "1.9.1", "amd64")
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatal(err)
	}
	if _, err := extractNativeNode(data.Bytes(), "bad", "1.9.1", "amd64"); err == nil {
		t.Fatal("接受了错误校验和")
	}
	if _, err := extractNativeNode(data.Bytes(), digest, "1.9.1", "arm64"); err == nil {
		t.Fatal("接受了错误架构")
	}
}
