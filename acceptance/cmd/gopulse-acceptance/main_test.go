package main

import "testing"

func TestUsageAndStrictParsing(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"help"}} {
		if code := run(args); code != 2 {
			t.Fatalf("run(%v) = %d, want 2", args, code)
		}
	}
	options, err := parse([]string{"--scope", "v1", "--manifest", "candidate/release-manifest.json", "--receipt=receipt.json", "--preflight", "--budget=30m", "--verify-evidence", "--publication", "publication.json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Scope != "v1" || !options.Preflight || options.Budget != "30m" || !options.VerifyEvidence || options.Publication != "publication.json" {
		t.Fatalf("V1 options were not parsed: %+v", options)
	}
	for _, args := range [][]string{{"--unknown"}, {"--scope"}, {"--self-test", "--keep"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("parse(%v) accepted invalid arguments", args)
		}
	}
}

func TestSelfTestMode(t *testing.T) {
	if code := run([]string{"compose", "--self-test"}); code != 0 {
		t.Fatalf("self-test exit = %d", code)
	}
}
