package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Ray-ymq/GoPulse/perflab/internal/k6"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	flags := flag.NewFlagSet("gopulse-load", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var mode, configPath, previousReport string
	var dryRun bool
	flags.StringVar(&mode, "mode", "", "boundary, refine, or soak")
	flags.StringVar(&configPath, "config", "", "reviewed k6 load configuration")
	flags.StringVar(&previousReport, "previous-report", "", "boundary/refine report used by refine or soak")
	flags.BoolVar(&dryRun, "dry-run", false, "print the stage plan without starting k6")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return 2
	}
	if mode == "" || configPath == "" {
		fmt.Fprintln(os.Stderr, "load requires --mode and --config")
		return 2
	}
	parsedMode, err := parseMode(mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	configPath = resolveConfigPath(root, configPath)
	config, digest, err := k6.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := config.Validate(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	boundary := k6.BoundaryRange{}
	if parsedMode != k6.ModeBoundary {
		boundary, err = k6.LoadPreviousBoundary(root, config, previousReport)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	plan, err := k6.BuildPlan(config, parsedMode, boundary)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if dryRun {
		encoded, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(encoded))
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner := k6.NewRunner(root, configPath, config, digest)
	report, err := runner.Run(ctx, parsedMode, boundary)
	if err != nil {
		if runner.ReportPath != "" {
			fmt.Fprintf(os.Stderr, "load report: %s\n", runner.ReportPath)
		}
		fmt.Fprintln(os.Stderr, "load execution failed:", err)
		return 1
	}
	fmt.Printf("[gopulse] load mode=%s report=%s judgement=%s\n", parsedMode, runner.ReportPath, report.Boundary.CapacityJudgement)
	return 0
}

func parseMode(value string) (k6.Mode, error) {
	switch k6.Mode(strings.ToLower(strings.TrimSpace(value))) {
	case k6.ModeBoundary:
		return k6.ModeBoundary, nil
	case k6.ModeRefine:
		return k6.ModeRefine, nil
	case k6.ModeSoak:
		return k6.ModeSoak, nil
	default:
		return "", fmt.Errorf("unsupported MODE %q; expected boundary, refine, or soak", value)
	}
}

func repositoryRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "perflab", "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not locate repository root")
		}
		current = parent
	}
}

func resolveConfigPath(root, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	if _, err := os.Stat(value); err == nil {
		absolute, _ := filepath.Abs(value)
		return absolute
	}
	return filepath.Join(root, value)
}
