package sendconfig_test

import (
	"errors"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/failures"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/sendconfig"
	dataplanepush "github.com/kong/kong-operator/v2/ingress-controller/pkg/dataplane/push"
)

type testError struct{}

func (t testError) Error() string {
	return "test error"
}

func TestResponseParsingErrorAlias(t *testing.T) {
	err := error(sendconfig.NewResponseParsingError([]byte("body")))

	var publicErr dataplanepush.ResponseParsingError
	require.ErrorAs(t, err, &publicErr)
	require.Equal(t, []byte("body"), publicErr.ResponseBody())
}

func TestUpdateError(t *testing.T) {
	someResourceFailure := lo.Must(failures.NewResourceFailure("some reason", &corev1.Pod{
		Name:       "name",
		Namespace:  "namespace",
		Kind:       "Pod",
		APIVersion: "v1",
	}))

	updateErr := sendconfig.NewUpdateErrorWithoutResponseBody([]failures.ResourceFailure{someResourceFailure}, testError{})
	require.Equal(t, "test error", updateErr.Error())
	require.Len(t, updateErr.ResourceFailures(), 1)
	unwraps := errors.As(updateErr, &testError{})
	require.True(t, unwraps, "UpdateError should unwrap to inner error")
}
