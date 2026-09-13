// Package backup implements the bounded, authenticated backup format. It does
// not establish a consistent cutover: callers must quiesce and export all domains
// before sealing, and must validate target ownership before importing anything.
package backup

import (
	"archive/tar"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"time"
)

const (
	Format      = 1
	MaxPayload  = 256 << 20
	MaxManifest = 1 << 20
	Iterations  = 600000
	headerSize  = 8 + 32 // magic and fresh KDF salt; both authenticated
	magic       = "GPBACK01"
	SecretEntry = "secrets.enc"
)

var ErrInvalid = errors.New("backup format or authentication invalid")
var names = []string{"config.json", "elasticsearch.json", "kafka.json", "mysql.sql", "plugins.json", "rabbitmq.json", "secrets.enc", "victoriametrics.native"}
var domains = []string{"mysql", "elasticsearch", "victoriametrics", "rabbitmq", "kafka", "plugins"}
var digestRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var versionRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var operationRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Domain struct {
	Cutover       time.Time        `json:"cutover"`
	Counts        map[string]int64 `json:"counts"`
	Start         time.Time        `json:"start"`
	End           time.Time        `json:"end"`
	Offsets       map[string]int64 `json:"offsets"`
	Drained       bool             `json:"drained"`
	CatalogDigest string           `json:"catalog_digest,omitempty"`
}

type Manifest struct {
	Format         int               `json:"format"`
	Schema         int               `json:"schema"`
	Platform       string            `json:"platform"`
	ProductVersion string            `json:"source_product_version"`
	ReleaseDigest  string            `json:"release_manifest_digest"`
	Operation      string            `json:"operation_id"`
	Started        time.Time         `json:"started"`
	Finished       time.Time         `json:"finished"`
	Complete       bool              `json:"complete"`
	Cipher         string            `json:"cipher"`
	KDF            string            `json:"kdf"`
	Iterations     int               `json:"iterations"`
	Files          []File            `json:"files"`
	Domains        map[string]Domain `json:"domains"`
}

func sum(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func allowed(name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// crypt creates a fresh AES-256 key for every envelope, including secrets.enc.
// GCM owns nonce generation; the complete fixed header is authenticated as AAD.
func crypt(pass, salt []byte) (cipher.AEAD, error) {
	if len(pass) < 16 || len(pass) > 4096 {
		return nil, ErrInvalid
	}
	key, err := pbkdf2.Key(sha256.New, string(pass), salt, Iterations, 32)
	if err != nil {
		return nil, ErrInvalid
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalid
	}
	return cipher.NewGCMWithRandomNonce(block)
}
func encrypt(plain, pass []byte) ([]byte, error) {
	return encryptEnvelope(plain, pass, magic)
}

func encryptEnvelope(plain, pass []byte, kind string) ([]byte, error) {
	if len(plain) > MaxPayload {
		return nil, ErrInvalid
	}
	header := make([]byte, headerSize)
	copy(header, kind)
	if _, err := rand.Read(header[8:]); err != nil {
		return nil, err
	}
	aead, err := crypt(pass, header[8:])
	if err != nil {
		return nil, err
	}
	return aead.Seal(header, nil, plain, header), nil
}
func decrypt(blob, pass []byte) ([]byte, error) {
	return decryptEnvelope(blob, pass, magic)
}

func decryptEnvelope(blob, pass []byte, kind string) ([]byte, error) {
	if len(blob) < headerSize+28 || len(blob) > MaxPayload+headerSize+28 || string(blob[:8]) != kind {
		return nil, ErrInvalid
	}
	aead, err := crypt(pass, blob[8:headerSize])
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nil, blob[headerSize:], blob[:headerSize])
	if err != nil {
		return nil, ErrInvalid
	}
	return plain, nil
}

// SealSecrets keeps restorable credentials out of the logical plaintext payload.
// Consumers must use a typed, portable secret schema, not an environment dump.
func SealSecrets(plain, pass []byte) ([]byte, error) { return encryptEnvelope(plain, pass, "GPSECR01") }
func OpenSecrets(blob, pass []byte) ([]byte, error)  { return decryptEnvelope(blob, pass, "GPSECR01") }

// Seal returns a complete authenticated envelope; callers publish it atomically.
// Metadata supplied by the exporter is validated but not inferred from payloads.
func Seal(m Manifest, files map[string][]byte, pass []byte) ([]byte, error) {
	m.Format, m.Schema, m.Platform = Format, 1, "linux/amd64"
	m.Cipher, m.KDF, m.Iterations = "AES-256-GCM", "PBKDF2-HMAC-SHA256", Iterations
	m.Files = nil
	var total int64
	for name, data := range files {
		if !allowed(name) {
			return nil, ErrInvalid
		}
		total += int64(len(data))
		if total > MaxPayload-MaxManifest-16384 {
			return nil, ErrInvalid
		}
		m.Files = append(m.Files, File{name, int64(len(data)), sum(data)})
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	if err := validate(m); err != nil {
		return nil, err
	}
	// Refuse a plaintext or malformed secret entry even when the outer envelope
	// would hide it. Do not return secret data or parsing errors to diagnostics.
	secrets, err := OpenSecrets(files[SecretEntry], pass)
	if err != nil {
		return nil, ErrInvalid
	}
	clear(secrets)
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MaxManifest {
		return nil, ErrInvalid
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write := func(name string, data []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	if err = write("manifest.json", raw); err != nil {
		return nil, err
	}
	for _, f := range m.Files {
		if err = write(f.Path, files[f.Path]); err != nil {
			return nil, err
		}
	}
	if err = tw.Close(); err != nil {
		return nil, err
	}
	defer clear(buf.Bytes())
	return encrypt(buf.Bytes(), pass)
}

// Open authenticates before interpreting any archive bytes. No filesystem paths
// are extracted, and no data is returned until every entry has been verified.
func Open(blob, pass []byte) (Manifest, map[string][]byte, error) {
	plain, err := decrypt(blob, pass)
	if err != nil {
		return Manifest{}, nil, err
	}
	defer clear(plain)
	return parse(plain)
}
func parse(plain []byte) (Manifest, map[string][]byte, error) {
	bad := func() (Manifest, map[string][]byte, error) { return Manifest{}, nil, ErrInvalid }
	if len(plain) > MaxPayload {
		return bad()
	}
	reader := bytes.NewReader(plain)
	tr := tar.NewReader(reader)
	h, err := tr.Next()
	if err != nil || !regular(h) || h.Name != "manifest.json" || h.Size > MaxManifest {
		return bad()
	}
	raw, err := io.ReadAll(tr)
	if err != nil {
		return bad()
	}
	var m Manifest
	if strictJSON(raw, &m) != nil || validate(m) != nil {
		return bad()
	}
	files := map[string][]byte{}
	success := false
	defer func() {
		if !success {
			for _, data := range files {
				clear(data)
			}
		}
	}()
	for _, f := range m.Files {
		h, err := tr.Next()
		if err != nil || !regular(h) || h.Name != f.Path || h.Size != f.Size {
			return bad()
		}
		data, err := io.ReadAll(tr)
		if err != nil || sum(data) != f.SHA256 {
			clear(data)
			return bad()
		}
		files[f.Path] = data
	}
	if _, err = tr.Next(); err != io.EOF {
		return bad()
	}
	// Require an actual USTAR end marker, no trailing bytes/concatenated archives.
	// tar.Reader also accepts EOF without a marker, so check exact encoded length.
	expected := int64(512 + ((len(raw)+511)/512)*512 + 1024)
	for _, f := range m.Files {
		expected += 512 + ((f.Size+511)/512)*512
	}
	if int64(len(plain)) != expected || reader.Len() != 0 || !bytes.Equal(plain[len(plain)-1024:], make([]byte, 1024)) {
		return bad()
	}
	success = true
	return m, files, nil
}
func regular(h *tar.Header) bool {
	return h.Typeflag == tar.TypeReg && h.Size >= 0 && h.Size <= MaxPayload && h.Linkname == "" && len(h.PAXRecords) == 0 && h.Format == tar.FormatUSTAR
}
func validate(m Manifest) error {
	if m.Format != Format || m.Schema != 1 || m.Platform != "linux/amd64" || !versionRE.MatchString(m.ProductVersion) || !digestRE.MatchString(m.ReleaseDigest) || !operationRE.MatchString(m.Operation) || !m.Complete || m.Started.IsZero() || m.Finished.Before(m.Started) || m.Cipher != "AES-256-GCM" || m.KDF != "PBKDF2-HMAC-SHA256" || m.Iterations != Iterations || len(m.Files) != len(names) || len(m.Domains) != len(domains) {
		return ErrInvalid
	}
	var total int64
	for i, f := range m.Files {
		if f.Path != names[i] || f.Size < 0 || f.Size > MaxPayload || !digestRE.MatchString(f.SHA256) {
			return ErrInvalid
		}
		total += f.Size
		if total > MaxPayload-MaxManifest-16384 {
			return ErrInvalid
		}
	}
	var cutover time.Time
	for _, name := range domains {
		d, ok := m.Domains[name]
		if !ok || d.Cutover.Before(m.Started) || d.Cutover.After(m.Finished) || d.Start.IsZero() || d.End.Before(d.Start) || d.End.After(d.Cutover) || len(d.Counts) == 0 {
			return ErrInvalid
		}
		if cutover.IsZero() {
			cutover = d.Cutover
		} else if !cutover.Equal(d.Cutover) {
			return ErrInvalid
		}
		for k, v := range d.Counts {
			if k == "" || v < 0 {
				return ErrInvalid
			}
		}
		for k, v := range d.Offsets {
			if k == "" || v < 0 {
				return ErrInvalid
			}
		}
		if (name == "rabbitmq" || name == "kafka") && !d.Drained {
			return ErrInvalid
		}
		if name == "kafka" && d.Offsets == nil {
			return ErrInvalid
		}
		if name == "plugins" && !digestRE.MatchString(d.CatalogDigest) {
			return ErrInvalid
		}
	}
	return nil
}

// Reject duplicate keys as well as unknown fields, trailing objects, and nesting
// beyond the shallow portable schema, before decoding into typed structures.
func strictJSON(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 16 {
			return ErrInvalid
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				k, ok := key.(string)
				if !ok || seen[k] {
					return ErrInvalid
				}
				seen[k] = true
				if err = walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return ErrInvalid
		}
		_, err = d.Token()
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(value)
}
