package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEqualWithDefault_Int(t *testing.T) {
	var (
		def = 80
	)
	var a *int
	var b *int

	// both nil -> default vs default
	require.True(t, equalWithDefault(a, b, def))

	// nil vs value equal to default
	v := def
	b = &v
	assert.True(t, equalWithDefault(nil, b, def))

	// nil vs value not equal to default
	w := def + 1
	b = &w
	assert.False(t, equalWithDefault(nil, b, def))

	// zero value vs nil -> default vs default
	z := 0
	assert.True(t, equalWithDefault(&z, nil, def))

	// equal non-zero values
	aVal := 123
	bVal := 123
	assert.True(t, equalWithDefault(&aVal, &bVal, def))

	// different non-zero values
	bVal = 321
	assert.False(t, equalWithDefault(&aVal, &bVal, def))
}

func TestEqualWithDefault_String(t *testing.T) {
	// default empty string
	var def string

	// both nil
	assert.True(t, equalWithDefault(nil, nil, def))

	// empty string vs nil -> default("") vs default("")
	empty := ""
	assert.True(t, equalWithDefault(&empty, nil, def))

	// equal non-empty strings
	s1 := "foo"
	s2 := "foo"
	assert.True(t, equalWithDefault(&s1, &s2, def))

	// different non-empty strings
	s2 = "bar"
	assert.False(t, equalWithDefault(&s1, &s2, def))

	// non-empty vs default fallback
	def2 := "/"
	slash := "/"
	assert.True(t, equalWithDefault(&empty, &slash, def2))
}

func TestEqualWithDefault_NamedStringType(t *testing.T) {
	type proto string

	http := proto("http")
	// nil vs value equal to default
	assert.True(t, equalWithDefault[proto](nil, &http, "http"))

	// nil vs value not equal to default
	assert.False(t, equalWithDefault[proto](nil, &http, "https"))

	// empty value vs default fallback
	empty := proto("")
	assert.True(t, equalWithDefault[proto](&empty, nil, "http"))
}
