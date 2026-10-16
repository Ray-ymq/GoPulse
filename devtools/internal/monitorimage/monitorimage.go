// Package monitorimage defines the shared latest tag and source-aware reuse
// check for the development and integration Monitor image.
package monitorimage

import (
	"encoding/json"

	"github.com/Ray-ymq/GoPulse/devtools/internal/proc"
)

const Tag = "gopulse/monitor:latest"
const InputLabel = "io.gopulse.monitor.input-digest"

// Reusable checks the image itself, not a workspace-local receipt: another
// checkout or build may have replaced the shared latest tag. The module input
// digest is the cache identity; the Git revision is only build metadata.
func Reusable(root, input string, environment map[string]string) bool {
	labels, err := proc.Capture([]string{
		"docker", "image", "inspect", "--format", "{{json .Config.Labels}}", Tag,
	}, root, environment)
	return err == nil && matches(labels, input)
}

func matches(raw, input string) bool {
	if input == "" {
		return false
	}
	var labels map[string]string
	return json.Unmarshal([]byte(raw), &labels) == nil &&
		labels[InputLabel] == input
}
