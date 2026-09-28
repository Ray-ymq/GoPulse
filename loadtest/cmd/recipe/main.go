package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Ray-ymq/GoPulse/loadtest/internal/recipe"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	flags := flag.NewFlagSet("phase18-recipe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var options recipe.GenerateOptions
	var inspect bool
	var dsnFile, passwordFile string
	flags.Uint64Var(&options.Seed, "seed", recipe.Seed, "deterministic recipe seed")
	flags.StringVar(&dsnFile, "dsn-file", "", "private owner-only file containing the MySQL DSN")
	flags.StringVar(&passwordFile, "password-file", "", "private owner-only file containing the load-test password")
	flags.StringVar(&options.Receipt, "receipt", "", "sanitized recipe receipt output")
	flags.StringVar(&options.Corpus, "corpus", "", "private load corpus output")
	flags.StringVar(&options.Credential, "credentials", "", "private load credential output")
	flags.BoolVar(&inspect, "inspect", false, "compute the deterministic descriptor without a database")
	flags.StringVar(&options.Candidate.Version, "candidate-version", "", "candidate version")
	flags.StringVar(&options.Candidate.Revision, "candidate-revision", "", "candidate revision")
	flags.StringVar(&options.Candidate.ManifestSHA256, "candidate-manifest-sha256", "", "candidate manifest digest")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return 2
	}
	if inspect {
		if err := writeInspect(options); err != nil {
			fmt.Fprintln(os.Stderr, "recipe inspection failed")
			return 1
		}
		return 0
	}
	if dsnFile == "" || passwordFile == "" || options.Receipt == "" || options.Corpus == "" || options.Credential == "" {
		fmt.Fprintln(os.Stderr, "recipe generation requires private DSN/password files and output paths")
		return 2
	}
	var err error
	if options.DSN, err = readPrivateValue(dsnFile); err != nil {
		fmt.Fprintln(os.Stderr, "read private recipe DSN failed")
		return 2
	}
	if options.Password, err = readPrivateValue(passwordFile); err != nil {
		fmt.Fprintln(os.Stderr, "read private recipe password failed")
		return 2
	}
	database, err := sql.Open("mysql", options.DSN)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open recipe database failed")
		return 1
	}
	defer database.Close()
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(1)
	database.SetConnMaxLifetime(3 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	if _, err := recipe.Generate(ctx, database, options); err != nil {
		if errors.Is(err, recipe.ErrNonEmptyTarget) {
			fmt.Fprintln(os.Stderr, "recipe target is not empty")
			return 3
		}
		fmt.Fprintln(os.Stderr, "recipe generation failed")
		return 1
	}
	return 0
}

func readPrivateValue(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 8192 || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("private value file is invalid")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(encoded))
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", errors.New("private value is empty or invalid")
	}
	return value, nil
}

func writeInspect(options recipe.GenerateOptions) error {
	if options.Receipt == "" {
		return errors.New("receipt path is required")
	}
	receipt := recipe.Inspect(options.Seed, options.Candidate, time.Now())
	return writeJSON(options.Receipt, receipt)
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}
