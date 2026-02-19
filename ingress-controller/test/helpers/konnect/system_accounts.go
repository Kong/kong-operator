package konnect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/konnect/sdk"
	"github.com/kong/kong-operator/v2/ingress-controller/test"
)

// CreateTestSystemAccount creates a system account in Konnect for testing purposes and returns its ID.
// The system account is deleted during test cleanup.
func CreateTestSystemAccount(ctx context.Context, t *testing.T) string {
	t.Helper()

	s := sdk.New(accessToken(), serverURLOpt())

	var (
		systemAccountID   string
		systemAccountName = fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixMilli())
	)
	err := retry.Do(func() error {
		createResp, err := s.SystemAccounts.
			PostSystemAccounts(ctx,
				&sdkkonnectcomp.CreateSystemAccount{
					Name:           systemAccountName,
					Description:    "Test system account for Kong Ingress Controller integration tests",
					KonnectManaged: new(false),
				},
			)
		if err != nil {
			return err
		}

		if createResp == nil {
			return fmt.Errorf("failed to create system account: response is nil")
		}

		if createResp.GetStatusCode() != http.StatusCreated {
			body, err := io.ReadAll(createResp.RawResponse.Body)
			if err != nil {
				body = []byte(err.Error())
			}
			return fmt.Errorf("failed to create system account: code %d, message %s", createResp.GetStatusCode(), body)
		}

		if createResp.SystemAccount == nil ||
			createResp.SystemAccount.ID == nil {
			return fmt.Errorf(
				"failed to create system account: response fields are missing (status code %d)",
				createResp.GetStatusCode(),
			)
		}

		systemAccountID = *createResp.SystemAccount.ID
		return nil
	}, retry.Attempts(5), retry.Delay(time.Second))
	require.NoError(t, err)

	assignRole(ctx, t, s.SystemAccountsRoles, systemAccountID, sdkkonnectcomp.RoleNameCreator)
	assignRole(ctx, t, s.SystemAccountsRoles, systemAccountID, sdkkonnectcomp.RoleNameAdmin)

	t.Cleanup(func() {
		fmt.Printf("deleting test system account: %q\n", systemAccountID)
		err := retry.Do(
			func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, err := s.SystemAccounts.DeleteSystemAccountsID(ctx, systemAccountID)
				return err
			},
			retry.Attempts(5),
			retry.Delay(time.Second),
		)
		if err != nil {
			// Don't fail the test if cleanup fails, just log the error.
			// Cleanup job will eventually clean up konnect.
			fmt.Printf("failed to delete test system account %q: %v\n", systemAccountID, err)
		}
	})

	t.Logf("created test system account: %q (ID:%q)", systemAccountName, systemAccountID)
	return systemAccountID
}

func assignRole(ctx context.Context, t *testing.T, s *sdkkonnectgo.SystemAccountsRoles, systemAccountID string, role sdkkonnectcomp.RoleName) {
	roleAssignResp, err := s.PostSystemAccountsAccountIDAssignedRoles(ctx, systemAccountID,
		&sdkkonnectcomp.AssignRole{
			RoleName:       new(role),
			EntityTypeName: new(sdkkonnectcomp.EntityTypeNameControlPlanes),
			EntityID:       new("*"),
			EntityRegion:   new(sdkkonnectcomp.AssignRoleEntityRegion(test.KonnectServerRegion())),
		},
	)
	require.NoError(t, err)
	if roleAssignResp.AssignedRole.ID == nil {
		t.Fatal("warning: assigned role response is missing ID field, cannot log assigned role ID")
	}
	t.Logf("assigned %q role (ID:%q)to system account %q for all control planes",
		role, *roleAssignResp.AssignedRole.ID, systemAccountID,
	)
}
