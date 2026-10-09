package main

import "testing"

func TestUsageAndStrictParsing(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"help"}} {
		if code := run(args); code != 2 {
			t.Fatalf("run(%v) = %d, want 2", args, code)
		}
	}
	if _, err := parse([]string{"--scope", "observability", "--receipt=receipt.json"}); err != nil {
		t.Fatal(err)
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
