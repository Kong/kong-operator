package ops

import (
	"errors"
	"fmt"
	"testing"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestClearInstanceFromError(t *testing.T) {
	t.Parallel()

	const trace = "kong:trace:1234"

	testCases := []struct {
		name  string
		err   error
		check func(t *testing.T, cleared error)
	}{
		{
			name: "BadRequestError",
			err:  &sdkkonnecterrs.BadRequestError{Status: 400, Instance: trace},
			check: func(t *testing.T, cleared error) {
				e, ok := errors.AsType[*sdkkonnecterrs.BadRequestError](cleared)
				require.True(t, ok)
				assert.Empty(t, e.Instance)
			},
		},
		{
			name: "InternalError (500)",
			err:  &sdkkonnecterrs.InternalError{Status: 500, Instance: trace},
			check: func(t *testing.T, cleared error) {
				e, ok := errors.AsType[*sdkkonnecterrs.InternalError](cleared)
				require.True(t, ok)
				assert.Empty(t, e.Instance)
			},
		},
		{
			name: "InternalServerError",
			err:  &sdkkonnecterrs.InternalServerError{Status: 500, Instance: trace},
			check: func(t *testing.T, cleared error) {
				e, ok := errors.AsType[*sdkkonnecterrs.InternalServerError](cleared)
				require.True(t, ok)
				assert.Empty(t, e.Instance)
			},
		},
		{
			name: "ServiceUnavailable",
			err:  &sdkkonnecterrs.ServiceUnavailable{Status: 503, Instance: trace},
			check: func(t *testing.T, cleared error) {
				e, ok := errors.AsType[*sdkkonnecterrs.ServiceUnavailable](cleared)
				require.True(t, ok)
				assert.Empty(t, e.Instance)
			},
		},
		{
			name: "NotAvailableError",
			err:  &sdkkonnecterrs.NotAvailableError{Status: 503, Instance: trace},
			check: func(t *testing.T, cleared error) {
				e, ok := errors.AsType[*sdkkonnecterrs.NotAvailableError](cleared)
				require.True(t, ok)
				assert.Empty(t, e.Instance)
			},
		},
		{
			name: "wrapped InternalError",
			err:  fmt.Errorf("update failed: %w", &sdkkonnecterrs.InternalError{Status: 500, Instance: trace}),
			check: func(t *testing.T, cleared error) {
				e, ok := errors.AsType[*sdkkonnecterrs.InternalError](cleared)
				require.True(t, ok)
				assert.Empty(t, e.Instance)
				assert.NotContains(t, cleared.Error(), trace)
			},
		},
		{
			name: "unrelated error is returned as is",
			err:  assert.AnError,
			check: func(t *testing.T, cleared error) {
				assert.Equal(t, assert.AnError, cleared)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cleared := ClearInstanceFromError(tc.err)
			assert.NotContains(t, cleared.Error(), trace)
			tc.check(t, cleared)
		})
	}
}

func TestSetKonnectEntityProgrammedConditionFalseIsStableAcross5xx(t *testing.T) {
	t.Parallel()

	var obj aiconfigurationv1alpha1.AIGatewayModelProvider

	SetKonnectEntityProgrammedConditionFalse(&obj, "Failed",
		&sdkkonnecterrs.InternalError{Status: 500, Title: "Internal Error", Instance: "kong:trace:aaa"})
	first, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &obj)
	require.True(t, ok)

	SetKonnectEntityProgrammedConditionFalse(&obj, "Failed",
		&sdkkonnecterrs.InternalError{Status: 500, Title: "Internal Error", Instance: "kong:trace:bbb"})
	second, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &obj)
	require.True(t, ok)

	assert.Equal(t, first.Message, second.Message)
}
