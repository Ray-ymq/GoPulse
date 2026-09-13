package plugin

import (
	"encoding/json"
	"flag"
	"io"
)

// RunPortableTransfer is a private pipe protocol for the lifecycle container.
// Its stdout is secret-bearing transport, not diagnostics. The executable
// enforces pipe-only descriptors; callers must encrypt the private JSON half.
func RunPortableTransfer(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 || (args[0] != "export" && args[0] != "import") {
		return operationFailed()
	}
	fs := flag.NewFlagSet("plugin-state", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", "", "offline plugin storage")
	packages := fs.String("packages", "/opt/gopulse/packages", "image-owned catalog packages")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *root == "" {
		return operationFailed()
	}
	type transport struct {
		Public  json.RawMessage `json:"public"`
		Private json.RawMessage `json:"private"`
	}
	if args[0] == "export" {
		public, private, err := ExportPortable(*root, *packages)
		if err != nil {
			return err
		}
		defer func() {
			for _, raw := range private.Plugins {
				clear(raw)
			}
		}()
		publicJSON, err := json.Marshal(public)
		if err != nil {
			return operationFailed()
		}
		privateJSON, err := json.Marshal(private)
		if err != nil {
			return operationFailed()
		}
		defer clear(privateJSON)
		if len(publicJSON)+len(privateJSON) > MaxPortableBytes-64 {
			return operationFailed()
		}
		return json.NewEncoder(out).Encode(transport{publicJSON, privateJSON})
	}
	raw, err := io.ReadAll(io.LimitReader(in, MaxPortableBytes+1))
	if err != nil {
		return operationFailed()
	}
	defer clear(raw)
	var value transport
	if decodePortable(raw, &value) != nil {
		return operationFailed()
	}
	defer clear(value.Private)
	return ImportPortable(*root, *packages, value.Public, value.Private)
}
