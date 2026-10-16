package createinputs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Hash fingerprints a resolved SDK request body before operator metadata is added.
func Hash(body any) (string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal create inputs: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}
