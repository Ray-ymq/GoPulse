package scenario

import (
	"reflect"
	"testing"
)

func TestDirectArgsLowDiskUsesToolIdentity(t *testing.T) {
	got := directArgs(1000, 1001, "989", "/bundle", "/install", true)
	want := []string{
		"run", "--rm", "--network", "host", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--user", "1000:1001", "--group-add", "989", "--tmpfs", "/tmp",
		"-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", "/bundle:/bundle:ro",
		"--tmpfs", "/install:rw,size=1048576,mode=0700,uid=1000,gid=1001",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("direct args = %#v, want %#v", got, want)
	}
}
