package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRejectsInvalidArgumentsBeforeLoadingConfiguration(t *testing.T) {
	for _, args := range [][]string{nil, {"up", "extra"}, {"invalid"}} {
		err := run(args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "usage: migrate <up|down>") {
			t.Fatalf("run(%v) error = %v, want usage error", args, err)
		}
	}
}
