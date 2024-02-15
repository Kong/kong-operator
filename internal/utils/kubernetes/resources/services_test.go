package resources

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetSelectorOverrides(t *testing.T) {
	testCases := []struct {
		name             string
		annotationValue  string
		expectedSelector map[string]string
		needsErr         bool
	}{
		{
			name:     "no annotation",
			needsErr: true,
		},
		{
			name:            "malformed annotation value",
			annotationValue: "malformedSelector",
			needsErr:        true,
		},
		{
			name:            "valid selector + incomplete selector 1",
			annotationValue: "app=test,app2",
			needsErr:        true,
		},
		{
			name:            "valid selector + incomplete selector 2",
			annotationValue: "app=test,app2=",
			needsErr:        true,
		},
		{
			name:            "valid selector + incomplete selector 3",
			annotationValue: "app=test,",
			needsErr:        true,
		},
		{
			name:            "single selector",
			annotationValue: "app=test",
			expectedSelector: map[string]string{
				"app": "test",
			},
			needsErr: false,
		},
		{
			name:            "multiple selectors",
			annotationValue: "app=test,app2=test2",
			expectedSelector: map[string]string{
				"app":  "test",
				"app2": "test2",
			},
			needsErr: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			newSelector, err := getSelectorOverrides(tc.annotationValue)
			if tc.needsErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.expectedSelector, newSelector)
		})
	}
}
