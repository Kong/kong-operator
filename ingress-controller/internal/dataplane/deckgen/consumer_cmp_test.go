package deckgen

import (
	"sort"
	"testing"

	"github.com/kong/go-database-reconciler/pkg/file"
	"github.com/stretchr/testify/assert"
)

func TestConsumerCmp(t *testing.T) {
	testCases := []struct {
		name     string
		input    []file.FConsumer
		expected []file.FConsumer
	}{
		{
			name: "sort by username",
			input: []file.FConsumer{
				{
					Username: new("b"),
				},
				{
					Username: new("a"),
				},
			},
			expected: []file.FConsumer{
				{
					Username: new("a"),
				},
				{
					Username: new("b"),
				},
			},
		},
		{
			name: "sort by custom_id",
			input: []file.FConsumer{
				{
					CustomID: new("b"),
				},
				{
					CustomID: new("a"),
				},
			},
			expected: []file.FConsumer{
				{
					CustomID: new("a"),
				},
				{
					CustomID: new("b"),
				},
			},
		},
		{
			name: "sort by username and custom_id",
			input: []file.FConsumer{
				{
					Username: new("b"),
					CustomID: new("b"),
				},
				{
					Username: new("a"),
					CustomID: new("a"),
				},
			},
			expected: []file.FConsumer{
				{
					Username: new("a"),
					CustomID: new("a"),
				},
				{
					Username: new("b"),
					CustomID: new("b"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sort.Sort(fConsumerByUsernameAndCustomID(tc.input))
			assert.Equal(t, tc.expected, tc.input)
		})
	}
}
