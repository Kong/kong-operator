package createinputs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHash(t *testing.T) {
	first, err := Hash(map[string]any{"secret": "sensitive-value", "labels": map[string]string{"a": "1", "b": "2"}})
	require.NoError(t, err)
	same, err := Hash(map[string]any{"labels": map[string]string{"b": "2", "a": "1"}, "secret": "sensitive-value"})
	require.NoError(t, err)
	assert.Equal(t, first, same)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, first)
	assert.NotContains(t, first, "sensitive-value")
	changed, err := Hash(map[string]string{"secret": "rotated-value"})
	require.NoError(t, err)
	assert.NotEqual(t, first, changed)
	_, err = Hash(make(chan string))
	require.ErrorContains(t, err, "failed to marshal create inputs")
}
