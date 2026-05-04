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

	t.Run("types wrapping string are supported", func(t *testing.T) {
		type MyString string
		type MyStringPtr *string

		assert.True(t, matchStringField(MyString("value"), MyString("value")))
		assert.True(t, matchStringField(MyString("value"), MyStringPtr(func() *string { s := "value"; return &s }())))
		assert.True(t, matchStringField(MyString("value"), "value"))
		assert.True(t, matchStringField(MyStringPtr(new("value")), "value"))
	})
}

func TestStringValueGeneric(t *testing.T) {
	t.Run("returns string value for string input", func(t *testing.T) {
		assert.Equal(t, "value", stringValueGeneric("value"))
		assert.Equal(t, "value", stringValueGeneric(new("value")))
	})

	t.Run("returns dereferenced value for non-nil pointer input", func(t *testing.T) {
		got := "value"
		assert.Equal(t, "value", stringValueGeneric(&got))
	})

	t.Run("returns empty string for nil pointer input", func(t *testing.T) {
		var nilPtr *string
		assert.Empty(t, stringValueGeneric(nilPtr))
	})
}
