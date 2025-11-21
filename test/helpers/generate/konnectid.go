package generate

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// KonnectID generates a random KonnectID string for testing purposes.
func KonnectID(t *testing.T) string {
	t.Helper()

	u := make([]byte, 16)

	_, err := rand.Read(u)
	require.NoError(t, err)

	// Set UUID version (4) and variant (RFC 4122)
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80

	id := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		u[0:4],
		u[4:6],
		u[6:8],
		u[8:10],
		u[10:16],
	)

	return id
}
