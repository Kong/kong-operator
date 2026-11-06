package push_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/zapr"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	dataplanepush "github.com/kong/kong-operator/v2/ingress-controller/pkg/dataplane/push"
)

type statusClientMock struct {
	expectedValue *kong.Status
	expectedError error
}

func (c statusClientMock) Status(context.Context) (*kong.Status, error) {
	return c.expectedValue, c.expectedError
}

func TestDefaultConfigurationChangeDetector_HasConfigurationChanged(t *testing.T) {
	ctx := t.Context()
	testSHAs := [][]byte{
		[]byte("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"),
		[]byte("82e35a63ceba37e9646434c5dd412ea577147f1e4a41ccde1614253187e3dbf9"),
	}

	testCases := []struct {
		name              string
		oldSHA, newSHA    []byte
		targetConfigEmpty bool
		statusSHA         string
		statusError       error

		expectedResult bool
		expectError    bool
	}{
		{
			name:           "oldSHA != newSHA",
			oldSHA:         testSHAs[0],
			newSHA:         testSHAs[1],
			expectedResult: true,
		},
		{
			name:           "oldSHA == newSHA, but status signals crash",
			oldSHA:         testSHAs[0],
			newSHA:         testSHAs[0],
			statusSHA:      dataplanepush.WellKnownInitialHash,
			expectedResult: true,
		},
		{
			name:              "oldSHA == newSHA, status signals init hash and we're trying to push empty config",
			oldSHA:            testSHAs[0],
			newSHA:            testSHAs[0],
			targetConfigEmpty: true,
			statusSHA:         dataplanepush.WellKnownInitialHash,
			expectedResult:    false,
		},
		{
			name:           "oldSHA == newSHA and status signals same",
			oldSHA:         testSHAs[0],
			newSHA:         testSHAs[0],
			statusSHA:      string(testSHAs[0]),
			expectedResult: false,
		},
		{
			name:           "oldSHA == newSHA and status signals other",
			oldSHA:         testSHAs[0],
			newSHA:         testSHAs[0],
			statusSHA:      string(testSHAs[1]),
			expectedResult: false,
		},
		{
			name:        "oldSHA == newSHA, status returns error",
			oldSHA:      testSHAs[0],
			newSHA:      testSHAs[0],
			statusError: errors.New("getting kong status failure"),
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			statusClient := statusClientMock{
				expectedValue: &kong.Status{
					ConfigurationHash: tc.statusSHA,
				},
				expectedError: tc.statusError,
			}
			detector := dataplanepush.NewKongGatewayConfigurationChangeDetector(zapr.NewLogger(zap.NewNop()))

			result, err := detector.HasConfigurationChanged(ctx, tc.oldSHA, tc.newSHA, tc.targetConfigEmpty, statusClient)
			if tc.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, result)
		})
	}
}

func TestKonnectConfigurationChangeDetector(t *testing.T) {
	ctx := t.Context()
	testSHAs := [][]byte{
		[]byte("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"),
		[]byte("82e35a63ceba37e9646434c5dd412ea577147f1e4a41ccde1614253187e3dbf9"),
	}

	testCases := []struct {
		name           string
		oldSHA, newSHA []byte
		expectedResult bool
	}{
		{
			name:           "oldSHA != newSHA",
			oldSHA:         testSHAs[0],
			newSHA:         testSHAs[1],
			expectedResult: true,
		},
		{
			name:           "oldSHA == newSHA",
			oldSHA:         testSHAs[0],
			newSHA:         testSHAs[0],
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			detector := dataplanepush.NewKonnectConfigurationChangeDetector()

			result, err := detector.HasConfigurationChanged(ctx, tc.oldSHA, tc.newSHA, false, nil)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, result)
		})
	}
}
