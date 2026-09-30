package main

import "testing"

func TestCommandRejectsLoadParameterOverrides(t *testing.T) {
	if run([]string{"--target-rps", "1"}) != 2 {
		t.Fatal("accepted runtime workload override")
	}
}
