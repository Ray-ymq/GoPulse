package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/Ray-ymq/GoPulse/loadtest/internal/load"
)

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type candidateManifest struct {
	Version  string `json:"version"`
	Revision string `json:"revision"`
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run intentionally exposes no rate, window, VU, timeout, or gate flags. The
// formal command accepts a validated profile and binding inputs only; the
// profile is the sole source for execution parameters.
func run(arguments []string) int {
	flags := flag.NewFlagSet("phase19-load", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var profilePath, baseURL, corpusPath, credentialsPath, candidatePath, workdir string
	var repeat int
	flags.StringVar(&profilePath, "profile", "", "validated Phase 19 capacity profile")
	flags.StringVar(&baseURL, "base-url", "", "product edge base URL")
	flags.StringVar(&corpusPath, "corpus", "", "private deterministic corpus")
	flags.StringVar(&credentialsPath, "credentials", "", "private load credentials")
	flags.StringVar(&candidatePath, "candidate-manifest", "", "immutable candidate manifest")
	flags.StringVar(&workdir, "workdir", "", "private repetition work directory")
	flags.IntVar(&repeat, "repeat", 0, "repetition number from the frozen profile")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return 2
	}
	if profilePath == "" || baseURL == "" || corpusPath == "" || credentialsPath == "" || candidatePath == "" || workdir == "" || repeat == 0 {
		fmt.Fprintln(os.Stderr, "phase19 load requires profile, base-url, corpus, credentials, candidate-manifest, workdir, and repeat")
		return 2
	}

	profile, profileDigest, err := load.LoadProfile(profilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "capacity profile is invalid")
		return 2
	}
	corpus, err := load.LoadCorpus(corpusPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load corpus is invalid")
		return 2
	}
	credentials, err := load.LoadCredentials(credentialsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load credentials are invalid")
		return 2
	}
	candidate, err := readCandidate(candidatePath, profile.TargetCandidateVersion)
	if err != nil {
		fmt.Fprintln(os.Stderr, "candidate manifest is invalid")
		return 2
	}
	if err := prepareWorkdir(workdir); err != nil {
		fmt.Fprintln(os.Stderr, "capacity workdir is invalid")
		return 2
	}
	repeatDir := filepath.Join(workdir, fmt.Sprintf("repeat-%02d", repeat))
	if err := os.MkdirAll(repeatDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "create repetition directory failed")
		return 2
	}
	if info, statErr := os.Stat(repeatDir); statErr != nil || info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintln(os.Stderr, "repetition directory must be private")
		return 2
	}
	reportPath := filepath.Join(repeatDir, "load-report.json")
	if _, statErr := os.Stat(reportPath); statErr == nil {
		fmt.Fprintln(os.Stderr, "refusing to overwrite an existing repetition report")
		return 2
	} else if !errors.Is(statErr, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "inspect repetition report failed")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, err = load.RunCapacity(ctx, load.CapacityRunConfig{
		BaseURL: baseURL, Corpus: corpus, Credentials: credentials,
		Profile: profile, ProfileDigest: profileDigest, Candidate: candidate,
		Repeat: repeat, ReportPath: reportPath,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "capacity load execution failed")
		return 1
	}
	return 0
}

func readCandidate(path, expectedVersion string) (load.CandidateBinding, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return load.CandidateBinding{}, err
	}
	var manifest candidateManifest
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if err := decoder.Decode(&manifest); err != nil {
		return load.CandidateBinding{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return load.CandidateBinding{}, errors.New("candidate manifest contains trailing JSON")
	}
	if manifest.Version != expectedVersion || !revisionPattern.MatchString(manifest.Revision) {
		return load.CandidateBinding{}, errors.New("candidate version or revision differs from profile")
	}
	digest := sha256.Sum256(encoded)
	return load.CandidateBinding{Version: manifest.Version, Revision: manifest.Revision, ManifestSHA256: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

func prepareWorkdir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("workdir must be a private directory")
	}
	return nil
}
