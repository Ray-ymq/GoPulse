// Command plugin-package builds one deterministic official plugin archive.
//
// The archive bytes are a published contract: every contract version uses fixed
// archive settings, so the frozen legacy, Phase 14 and current packages stay
// byte reproducible. The command only decides validation, package layout,
// permissions and metadata.
package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

const (
	sourceRedis            = "redis"
	contractVersionLegacy  = 1
	contractVersionCurrent = 2
)

var archPattern = regexp.MustCompile(`^[a-z0-9]+$`)

// usageError separates invalid invocation from a failed build or environment, so
// the exit status stays distinguishable for callers and acceptance scripts.
type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

type options struct {
	source          string
	version         string
	output          string
	binary          string
	arch            string
	repoRoot        string
	contractVersion int
}

func main() {
	output, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "plugin-package: %v\n", err)
		var usage usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
	fmt.Println(output)
}

// run validates the invocation, assembles the package and writes the archive.
// It returns the absolute archive path for the caller to print.
func run(arguments []string) (string, error) {
	opts, err := parse(arguments)
	if err != nil {
		return "", err
	}
	if opts.arch == "" {
		opts.arch, err = goEnvironment("GOARCH")
		if err != nil {
			return "", err
		}
	}
	if opts.version == "" {
		root, rootErr := repositoryRoot(opts.repoRoot)
		if rootErr != nil {
			return "", rootErr
		}
		data, readErr := os.ReadFile(filepath.Join(root, "VERSION"))
		if readErr != nil {
			return "", readErr
		}
		opts.version = strings.TrimSpace(string(data))
	}
	if err = validate(opts); err != nil {
		return "", err
	}
	if _, err = exec.LookPath("gzip"); err != nil {
		return "", fmt.Errorf("gzip is required to build the archive")
	}
	if opts.output == "" {
		root, rootErr := repositoryRoot(opts.repoRoot)
		if rootErr != nil {
			return "", rootErr
		}
		opts.output = filepath.Join(root, ".run", "packages", fmt.Sprintf("gopulse-%s-exporter-%s-linux-%s.tar.gz", opts.source, opts.version, opts.arch))
	}
	output, err := filepath.Abs(opts.output)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp("", "gopulse-plugin-package-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	directory := filepath.Join(staging, "package")
	entrypoint := filepath.ToSlash(filepath.Join("bin", "gopulse-"+opts.source+"-exporter"))
	if err = os.MkdirAll(filepath.Join(directory, "bin"), 0o755); err != nil {
		return "", err
	}
	if err = stageEntrypoint(opts, filepath.Join(directory, entrypoint)); err != nil {
		return "", err
	}
	files := []string{"plugin.json"}
	if opts.contractVersion == contractVersionLegacy {
		digest, digestErr := fileDigest(filepath.Join(directory, entrypoint))
		if digestErr != nil {
			return "", digestErr
		}
		manifest, manifestErr := plugin.LegacyManifest(opts.version, opts.arch, entrypoint, digest)
		if manifestErr != nil {
			return "", manifestErr
		}
		if err = os.WriteFile(filepath.Join(directory, "plugin.json"), manifest, 0o644); err != nil {
			return "", err
		}
	} else {
		if err = plugin.WritePackageMetadata(directory, opts.version, opts.arch, opts.source); err != nil {
			return "", err
		}
		files = append(files, "config.schema.json")
	}
	// Set modes explicitly: a restrictive umask must not reach the published bytes.
	for _, name := range files {
		if err = os.Chmod(filepath.Join(directory, name), 0o644); err != nil {
			return "", err
		}
	}
	if err = os.Chmod(filepath.Join(directory, entrypoint), 0o755); err != nil {
		return "", err
	}
	files = append(files, entrypoint)
	archive := filepath.Join(staging, "package.tar")
	if err = writeTar(directory, archive, files); err != nil {
		return "", err
	}
	if err = writeGzip(archive, output); err != nil {
		return "", err
	}
	return output, nil
}

func parse(arguments []string) (options, error) {
	opts := options{source: sourceRedis, contractVersion: contractVersionCurrent}
	flags := flag.NewFlagSet("plugin-package", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.source, "source", sourceRedis, "official plugin source")
	flags.StringVar(&opts.version, "version", "", "three-part plugin version")
	flags.StringVar(&opts.output, "output", "", "archive output path")
	flags.StringVar(&opts.binary, "binary", "", "prebuilt exporter executable")
	flags.StringVar(&opts.arch, "arch", "", "Linux architecture")
	flags.StringVar(&opts.repoRoot, "repo-root", "", "repository root used for source builds and defaults")
	flags.IntVar(&opts.contractVersion, "contract-version", contractVersionCurrent, "manifest contract version")
	if err := flags.Parse(arguments); err != nil {
		return options{}, usageError{err.Error()}
	}
	if flags.NArg() != 0 {
		return options{}, usageError{fmt.Sprintf("unknown argument %q", flags.Arg(0))}
	}
	return opts, nil
}

func validate(opts options) error {
	if opts.contractVersion != contractVersionLegacy && opts.contractVersion != contractVersionCurrent {
		return usageError{"--contract-version must be 1 or 2"}
	}
	if opts.contractVersion == contractVersionLegacy && opts.source != sourceRedis {
		return usageError{"only Redis publishes a legacy contract"}
	}
	if _, ok := plugin.LookupOfficial(opts.source + "-exporter"); !ok {
		return usageError{fmt.Sprintf("unknown source %q", opts.source)}
	}
	if _, err := plugin.CompareSemver(opts.version, opts.version); err != nil {
		return usageError{"--version must be a three-part SemVer"}
	}
	if !archPattern.MatchString(opts.arch) {
		return usageError{fmt.Sprintf("invalid architecture %q", opts.arch)}
	}
	if opts.binary != "" {
		info, err := os.Stat(opts.binary)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return usageError{"--binary must be an executable regular file"}
		}
	}
	return nil
}

// stageEntrypoint installs the exporter executable at its catalog path. A caller
// may hand over an already built binary; otherwise the matching module is built
// from the repository at the requested architecture.
func stageEntrypoint(opts options, destination string) error {
	if opts.binary != "" {
		return copyExecutable(opts.binary, destination)
	}
	root, err := repositoryRoot(opts.repoRoot)
	if err != nil {
		return err
	}
	command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-buildid=", "-o", destination, "./cmd/"+opts.source+"-exporter")
	command.Dir = filepath.Join(root, "exporters", opts.source)
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+opts.arch)
	command.Stdout, command.Stderr = os.Stderr, os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("building the %s exporter failed: %w", opts.source, err)
	}
	return os.Chmod(destination, 0o755)
}

func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err = io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err = output.Close(); err != nil {
		return err
	}
	return os.Chmod(destination, 0o755)
}

// writeTar keeps the historical archive settings without depending on GNU tar,
// which is not available with the same flags on the macOS system tar.
func writeTar(directory, archive string, files []string) error {
	output, err := os.Create(archive)
	if err != nil {
		return err
	}
	writer := tar.NewWriter(output)
	for _, name := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		input, openErr := os.Open(path)
		if openErr != nil {
			output.Close()
			return openErr
		}
		info, statErr := input.Stat()
		if statErr != nil {
			input.Close()
			output.Close()
			return statErr
		}
		header, headerErr := tar.FileInfoHeader(info, "")
		if headerErr != nil {
			input.Close()
			output.Close()
			return headerErr
		}
		header.Name = name
		header.Mode = int64(info.Mode().Perm())
		header.ModTime = time.Unix(0, 0)
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		header.Format = tar.FormatUSTAR
		if err = writer.WriteHeader(header); err == nil {
			_, err = io.Copy(writer, input)
		}
		closeErr := input.Close()
		if err != nil {
			output.Close()
			return err
		}
		if closeErr != nil {
			output.Close()
			return closeErr
		}
	}
	if err = writer.Close(); err != nil {
		output.Close()
		return err
	}
	if err = output.Close(); err != nil {
		return err
	}
	return normalizeTarHeaders(archive)
}

// normalizeTarHeaders clears the device fields the historical GNU tar writer
// emitted for regular files. archive/tar writes octal zeroes there, so
// normalize them before the archive is compressed.
func normalizeTarHeaders(archive string) error {
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	for offset := 0; offset+512 <= len(data); {
		header := data[offset : offset+512]
		allZero := true
		for _, value := range header {
			if value != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			break
		}
		for index := 329; index < 345; index++ {
			header[index] = 0
		}
		for index := 148; index < 156; index++ {
			header[index] = ' '
		}
		var checksum int
		for _, value := range header {
			checksum += int(value)
		}
		copy(header[148:156], []byte(fmt.Sprintf("%06o\x00 ", checksum)))
		rawSize := strings.Trim(string(header[124:136]), " \x00")
		size, parseErr := strconv.ParseInt(rawSize, 8, 64)
		if parseErr != nil || size < 0 {
			return errors.New("invalid USTAR member size")
		}
		offset += 512 + ((int(size) + 511) / 512 * 512)
	}
	if remainder := len(data) % 10240; remainder != 0 {
		data = append(data, make([]byte, 10240-remainder)...)
	}
	return os.WriteFile(archive, data, 0o600)
}

// writeGzip omits the name and timestamp so repeated builds stay identical.
func writeGzip(archive, output string) error {
	temporary := output + ".tmp"
	file, err := os.Create(temporary)
	if err != nil {
		return err
	}
	command := exec.Command("gzip", "-n", "-9", "-c", archive)
	command.Stdout, command.Stderr = file, os.Stderr
	runErr := command.Run()
	closeErr := file.Close()
	if runErr != nil {
		os.Remove(temporary)
		return fmt.Errorf("gzip failed: %w", runErr)
	}
	if closeErr != nil {
		os.Remove(temporary)
		return closeErr
	}
	if err = os.Rename(temporary, output); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func goEnvironment(name string) (string, error) {
	output, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s failed: %w", name, err)
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", fmt.Errorf("go env %s returned an empty value", name)
	}
	return value, nil
}

// repositoryRoot locates the checkout that owns VERSION and exporters/ so defaults
// and source builds work from any working directory inside the repository.
func repositoryRoot(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "VERSION")); err == nil {
			if info, err := os.Stat(filepath.Join(directory, "exporters")); err == nil && info.IsDir() {
				return directory, nil
			}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("the repository root cannot be located; pass --repo-root")
		}
		directory = parent
	}
}
