package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortalAPISpec_ToCreatePortal(t *testing.T) {
	spec := &PortalAPISpec{
		AuthenticationEnabled:   true,
		AutoApproveApplications: true,
		AutoApproveDevelopers:   true,
		DefaultAPIVisibility:    "test-value",
		DefaultPageVisibility:   "test-value",
		Description:             new("test-value"),
		DisplayName:             "test-value",
		Name:                    "test-value",
		RBACEnabled:             true,
	}
	result, err := spec.ToCreatePortal()
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestPortalAPISpec_ToUpdatePortal(t *testing.T) {
	spec := &PortalAPISpec{
		AuthenticationEnabled:   true,
		AutoApproveApplications: true,
		AutoApproveDevelopers:   true,
		DefaultAPIVisibility:    "test-value",
		DefaultPageVisibility:   "test-value",
		Description:             new("test-value"),
		DisplayName:             "test-value",
		Name:                    "test-value",
		RBACEnabled:             true,
	}
	result, err := spec.ToUpdatePortal()
	require.NoError(t, err)
	require.NotNil(t, result)
}
