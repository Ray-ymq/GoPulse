// Command gopulse-acceptance is the single native acceptance entry point.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Ray-ymq/GoPulse/acceptance/internal/scenario"
)

const usage = `usage: gopulse-acceptance <command> [options]

commands:
  compose [--scope business|observability] [--keep] [--self-test] [--receipt PATH]
  business [--keep] [--receipt PATH]
  observe [--keep] [--receipt PATH]
  plugins [--keep] [--receipt PATH]
  alerts [--keep] [--receipt PATH]
  roles [--keep] [--receipt PATH]
  pages [--keep] [--receipt PATH]
  lifecycle [--install clean|reuse] [--install-path PATH] [--manifest PATH] [--platform PLATFORM]
  reconcile-accounts [--keep] [--receipt PATH]
`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	command := args[0]
	options, err := parse(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopulse-acceptance] ERROR: %v\n%s", err, usage)
		return 2
	}
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopulse-acceptance] ERROR: %v\n", err)
		return 1
	}
	switch command {
	case "compose":
		err = scenario.Compose(root, options)
	case "business":
		err = scenario.Business(root, options)
	case "observe":
		err = scenario.Observe(root, options)
	case "plugins":
		err = scenario.Plugins(root, options)
	case "alerts":
		err = scenario.Alerts(root, options)
	case "roles":
		err = scenario.Roles(root, options)
	case "pages":
		err = scenario.Pages(root, options)
	case "reconcile-accounts":
		err = scenario.ReconcileAccounts(root, options)
	case "lifecycle":
		err = scenario.Lifecycle(root, options)
	default:
		err = scenario.Unsupported(command)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopulse-acceptance] ERROR: %v\n", err)
		return 1
	}
	return 0
}

func parse(args []string) (scenario.Options, error) {
	options := scenario.Options{}
	for index := 0; index < len(args); index++ {
		name := args[index]
		value := ""
		if key, inline, found := strings.Cut(name, "="); found {
			name, value = key, inline
		} else if strings.HasPrefix(name, "--") {
			switch name {
			case "--keep", "--self-test":
				value = "true"
			case "--scope", "--receipt", "--manifest", "--candidate", "--install", "--install-path", "--platform":
				if index+1 >= len(args) {
					return scenario.Options{}, fmt.Errorf("%s requires a value", name)
				}
				index++
				value = args[index]
			default:
				return scenario.Options{}, fmt.Errorf("unknown option: %s", name)
			}
		} else {
			return scenario.Options{}, fmt.Errorf("unexpected argument: %s", name)
		}
		switch name {
		case "--keep":
			options.Keep = true
		case "--self-test":
			options.SelfTest = true
		case "--scope":
			options.Scope = value
		case "--receipt":
			options.Receipt = value
		case "--manifest":
			options.Manifest = value
		case "--candidate":
			options.Candidate = value
		case "--install":
			options.Install = value
		case "--install-path":
			options.InstallPath = value
		case "--platform":
			options.Platform = value
		default:
			return scenario.Options{}, fmt.Errorf("unknown option: %s", name)
		}
	}
	if options.SelfTest && options.Keep {
		return scenario.Options{}, fmt.Errorf("--self-test cannot be combined with --keep")
	}
	return options, nil
}
