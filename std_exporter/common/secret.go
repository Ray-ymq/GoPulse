package common

import (
	"runtime"
	"strings"
	"unsafe"
)

type argSpan struct {
	arg   int
	start int
}

// secretArgSpans locates command-line arguments carrying secret values and the
// byte offset within each argument where the value starts. It handles both the
// "--flag=value" and "--flag value" forms.
func secretArgSpans(args []string, secretFlags ...string) []argSpan {
	secret := make(map[string]struct{}, len(secretFlags))
	for _, f := range secretFlags {
		secret[f] = struct{}{}
	}

	var spans []argSpan
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if eq := strings.IndexByte(arg, '='); eq >= 0 {
			if _, ok := secret[strings.TrimLeft(arg[:eq], "-")]; ok {
				spans = append(spans, argSpan{arg: i, start: eq + 1})
			}
			continue
		}
		if _, ok := secret[strings.TrimLeft(arg, "-")]; ok && i+1 < len(args) {
			spans = append(spans, argSpan{arg: i + 1})
			i++
		}
	}
	return spans
}

// scrubArgValue overwrites s starting at start with '*', in place. On Linux the
// backing bytes of os.Args are the original argv memory, so this rewrites what
// /proc/<pid>/cmdline reports.
func scrubArgValue(s string, start int) {
	if start >= len(s) {
		return
	}
	b := unsafe.Slice(unsafe.StringData(s), len(s))
	for i := start; i < len(b); i++ {
		b[i] = '*'
	}
}

// ScrubSecretArgs masks secret values in the process command line so they are
// not exposed through ps or /proc/<pid>/cmdline. Pass the flag name without
// leading dashes for every sensitive parameter, e.g.
// ScrubSecretArgs(os.Args, "password", "token"). Callers must first copy any
// secret they still need into Go-owned memory (strings.Clone), because an
// argument's backing bytes are overwritten. It is a no-op off Linux, where
// argv is not backed by this memory.
func ScrubSecretArgs(args []string, secretFlags ...string) {
	if runtime.GOOS != "linux" {
		return
	}
	for _, sp := range secretArgSpans(args, secretFlags...) {
		scrubArgValue(args[sp.arg], sp.start)
	}
}
