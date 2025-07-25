package translator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/ingress-controller/internal/dataplane/failures"
)

// This file contains unit test functions to test translation failures generated by translator.

func newResourceFailure(t *testing.T, reason string, objects ...client.Object) failures.ResourceFailure {
	failure, err := failures.NewResourceFailure(reason, objects...)
	require.NoError(t, err)
	return failure
}
