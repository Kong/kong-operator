package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchStringField(t *testing.T) {
	t.Run("matches plain strings", func(t *testing.T) {
		assert.True(t, matchStringField("value", "value"))
	})

	t.Run("matches string and pointer values", func(t *testing.T) {
		got := "value"
		assert.True(t, matchStringField("value", &got))
	})

	t.Run("matches nil and non-nil pointers on either side", func(t *testing.T) {
		want := "value"
		got := "value"
		var nilPtr *string

		assert.True(t, matchStringField(&want, got))
		assert.True(t, matchStringField(&want, &got))
		assert.True(t, matchStringField(nilPtr, ""))
		assert.True(t, matchStringField("", nilPtr))
	})

	t.Run("treats nil pointer as empty string", func(t *testing.T) {
		var got *string
		assert.True(t, matchStringField("", got))
	})

	t.Run("does not match different values", func(t *testing.T) {
		assert.False(t, matchStringField("value", "other"))
	})
}
