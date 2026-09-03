package helpers

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/test"
)

// CreateKongLicense creates a KongLicense resource
// read from the KONG_LICENSE_DATA environment variable, retrying creation
// a few times in case the cluster isn't fully ready yet.
// It returns an error if KONG_LICENSE_DATA is not set.
func CreateKongLicense(ctx context.Context, cl client.Client, generateNamePrefix string) (cleanup func() error, err error) {
	licenseData := test.KongLicenseData()
	if licenseData == "" {
		return nil, errors.New("KONG_LICENSE_DATA not set")
	}

	fmt.Println("INFO: creating KongLicense for tests")
	kongLicense := &configurationv1alpha1.KongLicense{
		GenerateName:     generateNamePrefix,
		RawLicenseString: licenseData,
		Enabled:          true,
	}

	const attempts = 5
	if err := retry.New(
		retry.OnRetry(func(n uint, err error) {
			fmt.Printf("WARNING: creating KongLicense attempt %d/%d - error: %s\n", n+1, attempts, err)
		}),
		retry.LastErrorOnly(true),
		retry.Delay(2*time.Second),
		retry.Attempts(attempts),
	).Do(func() error {
		return cl.Create(ctx, kongLicense)
	}); err != nil {
		return nil, fmt.Errorf("failed to create KongLicense: %w", err)
	}

	return func() error {
		if err := cl.Delete(context.Background(), kongLicense); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete KongLicense %s: %w", kongLicense.Name, err)
		}
		return nil
	}, nil
}

// CreateKongLicenseForTest is a [*testing.T] friendly wrapper around
// CreateKongLicense.
func CreateKongLicenseForTest(t *testing.T, ctx context.Context, cl client.Client, generateNamePrefix string) {
	t.Helper()

	cleanup, err := CreateKongLicense(ctx, cl, generateNamePrefix)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, cleanup())
	})
}
