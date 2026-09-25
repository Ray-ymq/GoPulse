package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Ray-ymq/GoPulse/loadtest/internal/load"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	flags := flag.NewFlagSet("phase18-load", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var baseURL, corpusPath, credentialsPath, reportPath, diagnosticReportPath, cookieName string
	var virtualUsers int
	var activeWorkers int
	var saturate bool
	var requestTimeout, warmup, steady, burst time.Duration
	var steadyRPS, burstRPS float64
	flags.StringVar(&baseURL, "base-url", "", "product edge base URL")
	flags.StringVar(&corpusPath, "corpus", "", "private deterministic corpus")
	flags.StringVar(&credentialsPath, "credentials", "", "private load credentials")
	flags.StringVar(&reportPath, "report", "", "sanitized load report output")
	flags.StringVar(&diagnosticReportPath, "diagnostic-report", "", "private per-second diagnostic report output")
	flags.StringVar(&cookieName, "cookie-name", "gopulse_session", "session cookie name")
	flags.IntVar(&virtualUsers, "vus", 1024, "fixed virtual user count")
	flags.BoolVar(&saturate, "saturate", false, "run closed-loop traffic with a fixed active worker count")
	flags.IntVar(&activeWorkers, "active-workers", 0, "closed-loop active worker count; requires --saturate")
	flags.DurationVar(&requestTimeout, "request-timeout", 5*time.Second, "per-attempt timeout")
	flags.DurationVar(&warmup, "warmup", 5*time.Minute, "ramp-to-steady warmup")
	flags.DurationVar(&steady, "steady", 15*time.Minute, "steady window")
	flags.DurationVar(&burst, "burst", 2*time.Minute, "burst window")
	flags.Float64Var(&steadyRPS, "steady-rps", 150, "steady target requests per second")
	flags.Float64Var(&burstRPS, "burst-rps", 300, "burst target requests per second")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return 2
	}
	if baseURL == "" || corpusPath == "" || credentialsPath == "" || reportPath == "" {
		fmt.Fprintln(os.Stderr, "load requires base-url, corpus, credentials, and report")
		return 2
	}
	if saturate {
		if activeWorkers < 1 || activeWorkers > virtualUsers {
			fmt.Fprintln(os.Stderr, "saturation requires active-workers between 1 and vus")
			return 2
		}
		steadyRPS, burstRPS, burst = 0, 0, 0
	} else if activeWorkers != 0 {
		fmt.Fprintln(os.Stderr, "active-workers requires --saturate")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, err = load.Run(ctx, load.Config{
		BaseURL: baseURL, CookieName: cookieName, Corpus: corpus, Credentials: credentials,
		VirtualUsers: virtualUsers, ActiveWorkers: activeWorkers, RequestTimeout: requestTimeout,
		Warmup: warmup, Steady: steady, Burst: burst,
		SteadyRPS: steadyRPS, BurstRPS: burstRPS, ReportPath: reportPath,
		DiagnosticReportPath: diagnosticReportPath,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "load execution failed")
		return 1
	}
	return 0
}
