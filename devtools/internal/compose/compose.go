// Package compose builds and runs the Compose commands of a local lifecycle.
package compose

import (
	"fmt"
	"os"
	"time"

	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
)

// Command is one Compose invocation: the project, the private environment
// file, the Compose files in order, and any enabled profiles.
type Command struct {
	Project  string
	EnvFile  string
	Files    []string
	Profiles []string
}

// Args renders the full argument vector, keeping the argument order the
// replaced helper used so Compose behaves identically.
func (c Command) Args(rest ...string) []string {
	args := []string{"docker", "compose", "--project-name", c.Project, "--env-file", c.EnvFile}
	for _, profile := range c.Profiles {
		args = append(args, "--profile", profile)
	}
	for _, file := range c.Files {
		args = append(args, "--file", file)
	}
	return append(args, rest...)
}

// Up starts or refreshes the dependency project and waits for its health.
func (c Command) Up(root string, env map[string]string) error {
	return proc.Run(c.Args("up", "--detach", "--wait", "--wait-timeout", "420"), root, env,
		"Compose dependency startup for "+c.Project, 0)
}

// Down stops the project without removing named volumes.
func (c Command) Down(root string, env map[string]string) error {
	return proc.Run(c.Args("down", "--remove-orphans"), root, env,
		"Compose dependency stop for "+c.Project, 0)
}

// Run executes another Compose subcommand.
func (c Command) Run(root string, env map[string]string, label string, timeout time.Duration, rest ...string) error {
	return proc.Run(c.Args(rest...), root, env, label, timeout)
}

// Warn reports a best-effort cleanup problem without failing the lifecycle.
func Warn(err error) { fmt.Fprintf(os.Stderr, "[gopulse] warning: %v\n", err) }
