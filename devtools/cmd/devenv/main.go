// Command devenv runs GoPulse's local development, observation, and isolated
// test lifecycles with owned state and bounded cleanup.
//
// The Makefile targets `deps`, `dev`, `dev-observe`, `stop`, `integration`, and
// `e2e` dispatch here. Every lifecycle keeps its state under
// .run/local/<workspace>/ so unrelated checkouts never share projects,
// volumes, or ports.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Ray-ymq/GoPulse/devtools/internal/devrun"
)

const usage = `usage: devenv <command> [options]

commands:
  deps [--scope business|observe] [--env-file PATH]   start the development dependencies
  dev [--env-file PATH]                               start the source development environment
  dev-observe [--env-file PATH]                       start the observation environment
  stop                                                stop everything this workspace owns
  monitor-image                                       prepare or reuse the Monitor image
`

type options struct {
	scope   string
	envFile string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	command := args[0]
	parsed, err := parseOptions(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopulse] ERROR: %v\n", err)
		return 2
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopulse] ERROR: %v\n", err)
		return 1
	}

	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-interrupted
		fmt.Fprintln(os.Stderr, "[gopulse] interrupted")
		os.Exit(130)
	}()

	switch command {
	case "deps":
		err = devrun.Deps(root, parsed.scope, parsed.envFile)
	case "dev":
		err = devrun.Dev(root, false, parsed.envFile)
	case "dev-observe":
		err = devrun.Dev(root, true, parsed.envFile)
	case "stop":
		err = devrun.Stop(root)
	case "monitor-image":
		err = devrun.MonitorImage(root)
	default:
		fmt.Fprintf(os.Stderr, "[gopulse] ERROR: unknown command: %s\n%s", command, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopulse] ERROR: %v\n", err)
		return 1
	}
	return 0
}

func parseOptions(args []string) (options, error) {
	parsed := options{}
	for index := 0; index < len(args); index++ {
		name := args[index]
		value := ""
		if key, inline, found := strings.Cut(name, "="); found {
			name, value = key, inline
		} else if strings.HasPrefix(name, "--") {
			if isFlagWithValue(name) {
				if index+1 >= len(args) {
					return options{}, fmt.Errorf("%s requires a value", name)
				}
				index++
				value = args[index]
			} else {
				return options{}, fmt.Errorf("unknown option: %s", name)
			}
		} else {
			return options{}, fmt.Errorf("unexpected argument: %s", name)
		}
		switch name {
		case "--scope":
			parsed.scope = value
		case "--env-file":
			parsed.envFile = value
		default:
			return options{}, fmt.Errorf("unknown option: %s", name)
		}
	}
	return parsed, nil
}

func isFlagWithValue(name string) bool {
	switch name {
	case "--scope", "--env-file":
		return true
	}
	return false
}
