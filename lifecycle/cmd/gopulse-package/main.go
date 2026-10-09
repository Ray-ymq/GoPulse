// Command gopulse-package builds, verifies and promotes the immutable release
// candidate. It is the native delivery entry point that replaces the previous
// Python delivery scripts without changing the manifest, bundle, receipt or
// promotion contracts.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Ray-ymq/GoPulse/lifecycle/internal/packaging"
)

const usage = `usage: gopulse-package <command> [flags]

commands:
  run      build the candidate, verify it and optionally promote it
  build    push the candidate images and assemble the bundle
  verify   check an existing candidate and write its verification receipt
  promote  copy the verified candidate to its release tags
  env      print the validated image references of a candidate

flags:`

func main() {
	if err := run(os.Args[1:]); err != nil {
		var usageError *packaging.UsageError
		if errors.As(err, &usageError) {
			fmt.Fprintln(os.Stderr, "gopulse-package:", usageError.Message)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "gopulse-package:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return &packaging.UsageError{Message: "a command is required\n" + usage}
	}
	command := arguments[0]
	flags := flag.NewFlagSet("gopulse-package "+command, flag.ContinueOnError)
	flags.Usage = func() {
		fmt.Fprintln(os.Stderr, usage)
		flags.PrintDefaults()
	}
	repository := flags.String("repo", "", "repository root (default: the containing Git worktree)")
	output := flags.String("output", "dist", "candidate output directory")
	platforms := flags.String("platform", "linux/amd64", "candidate platforms, e.g. linux/amd64,linux/arm64")
	registry := flags.String("registry", "", "registry namespace (default: an owned loopback registry)")
	manifest := flags.String("manifest", "", "release manifest of an existing candidate")
	version := flags.String("version", "", "expected candidate version")
	revision := flags.String("revision", "", "expected candidate revision")
	runtimeGate := flags.Bool("runtime", false, "run the real runtime and Compose acceptance")
	promote := flags.Bool("promote", false, "promote the verified candidate to its release tags")
	if err := flags.Parse(arguments[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return &packaging.UsageError{Message: err.Error()}
	}
	if flags.NArg() != 0 {
		return &packaging.UsageError{Message: "unexpected argument: " + flags.Arg(0)}
	}
	switch command {
	case "run":
		return runCandidate(*repository, *registry, *platforms, *output, *runtimeGate, *promote)
	case "build":
		repo, err := packaging.OpenRepo(*repository)
		if err != nil {
			return err
		}
		if *registry == "" {
			return &packaging.UsageError{Message: "build requires --registry"}
		}
		path, err := packaging.Build(repo, *registry, *platforms, *output)
		if err != nil {
			return err
		}
		fmt.Println("Candidate complete; not externally published:", path)
		return nil
	case "verify":
		repo, err := packaging.OpenRepo(*repository)
		if err != nil {
			return err
		}
		if *manifest == "" {
			return &packaging.UsageError{Message: "verify requires --manifest"}
		}
		return packaging.Verify(repo, *manifest, *platforms, *runtimeGate)
	case "promote":
		if *manifest == "" {
			return &packaging.UsageError{Message: "promote requires --manifest"}
		}
		return packaging.Promote(*manifest)
	case "env":
		if *manifest == "" || *version == "" || *revision == "" {
			return &packaging.UsageError{Message: "env requires --manifest, --version and --revision"}
		}
		lines, err := packaging.CandidateEnv(*manifest, *version, *revision)
		if err != nil {
			return err
		}
		fmt.Println(strings.Join(lines, "\n"))
		return nil
	default:
		return &packaging.UsageError{Message: "unknown command: " + command + "\n" + usage}
	}
}

// runCandidate owns the whole delivery chain, including the registry it starts
// when the caller does not provide one.
func runCandidate(repository, registry, platforms, output string, runtimeGate, promote bool) error {
	repo, err := packaging.OpenRepo(repository)
	if err != nil {
		return err
	}
	namespace := registry
	if namespace == "" {
		owned, err := packaging.StartRegistry()
		if err != nil {
			return err
		}
		defer owned.Stop()
		namespace = owned.Namespace
	}
	path, err := packaging.Build(repo, namespace, platforms, output)
	if err != nil {
		return err
	}
	for index, platform := range strings.Split(platforms, ",") {
		if err := packaging.Verify(repo, path, platform, runtimeGate && index == 0); err != nil {
			return err
		}
	}
	if promote {
		return packaging.Promote(path)
	}
	fmt.Println("Candidate complete; not externally published:", path)
	return nil
}
