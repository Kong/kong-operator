package createinputs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Hash fingerprints canonical JSON for resolved, user-controlled create inputs.
// Callers must not serialize an SDK request: its encoding can change across SDK versions.
func Hash(body json.RawMessage) (string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal create inputs: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}
