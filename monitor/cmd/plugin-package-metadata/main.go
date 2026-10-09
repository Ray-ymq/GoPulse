// Command plugin-package-metadata emits the canonical v2 manifest and schema.
// It does not register the resulting archive as a trusted official release.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Ray-ymq/GoPulse/monitor/internal/plugin"
)

func main() {
	directory := flag.String("directory", "", "package directory containing the executable")
	version := flag.String("version", "", "three-part release version")
	arch := flag.String("arch", "", "Linux architecture")
	source := flag.String("source", "redis", "official source")
	flag.Parse()
	if err := plugin.WritePackageMetadata(*directory, *version, *arch, *source); err != nil {
		fmt.Fprintln(os.Stderr, "package metadata generation failed")
		os.Exit(1)
	}
}
