package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchStringField(t *testing.T) {
	type namedString string

	t.Run("matches plain strings", func(t *testing.T) {
		assert.True(t, matchStringField("value", "value"))
	})

	t.Run("matches named string types and pointers", func(t *testing.T) {
		want := namedString("value")
		got := namedString("value")
		assert.True(t, matchStringField(want, &got))
	})

	t.Run("treats nil pointer as empty string", func(t *testing.T) {
		var got *string
		assert.True(t, matchStringField("", got))
	})

	t.Run("does not match different values", func(t *testing.T) {
		assert.False(t, matchStringField("value", "other"))
	})
}
