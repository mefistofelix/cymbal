package cmd

import (
	"encoding/json"
	"os"
)

// writeJSON writes a versioned JSON envelope to stdout.
func writeJSON(data any) error {
	envelope := map[string]any{
		"version": "0.1",
		"results": data,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}
