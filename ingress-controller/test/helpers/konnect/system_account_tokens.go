package konnect

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/konnect/sdk"
)

func CreateTestSystemAccountToken(ctx context.Context, t *testing.T, systemAccountID string) (string, string) {
	t.Helper()

	s := sdk.New(accessToken(), serverURLOpt())

	var (
		systemAccountToken     string
		systemAccountTokenID   string
		systemAccountTokenName = fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixMilli())
	)
	err := retry.Do(func() error {
		createResp, err := s.SystemAccountsAccessTokens.
			PostSystemAccountsIDAccessTokens(ctx,
				systemAccountID,
				&sdkkonnectcomp.CreateSystemAccountAccessToken{
					Name:      systemAccountTokenName,
					ExpiresAt: time.Now().Add(time.Hour),
				},
			)
		if err != nil {
			return err
		}

		if createResp == nil {
			return fmt.Errorf("failed to create system account token: response is nil")
		}

		if createResp.GetStatusCode() != http.StatusCreated {
			body, err := io.ReadAll(createResp.RawResponse.Body)
			if err != nil {
				body = []byte(err.Error())
			}
			return fmt.Errorf("failed to create system account token: code %d, message %s", createResp.GetStatusCode(), body)
		}

		if createResp.SystemAccountAccessTokenCreated == nil ||
			createResp.SystemAccountAccessTokenCreated.ID == nil ||
			createResp.SystemAccountAccessTokenCreated.Token == nil {
			return fmt.Errorf(
				"failed to create system account token: response fields are missing (status code %d)",
				createResp.GetStatusCode(),
			)
		}

		systemAccountToken = *createResp.SystemAccountAccessTokenCreated.Token
		systemAccountTokenID = *createResp.SystemAccountAccessTokenCreated.ID
		return nil
	}, retry.Attempts(5), retry.Delay(time.Second))
	require.NoError(t, err)

	t.Cleanup(func() {
		fmt.Printf("deleting test system account token: %q (ID: %q)\n", systemAccountTokenName, systemAccountTokenID)
		err := retry.Do(
			func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, err := s.SystemAccountsAccessTokens.DeleteSystemAccountsIDAccessTokensID(ctx, systemAccountID, systemAccountTokenID)
				return err
			},
			retry.Attempts(5),
			retry.Delay(time.Second),
		)
		if err != nil {
			// Don't fail the test if cleanup fails, just log the error.
			// Cleanup job will eventually clean up konnect.
			fmt.Printf("failed to delete test system account token %q (ID: %q): %v\n", systemAccountTokenName, systemAccountTokenID, err)
		}
	})

	t.Logf("created test system account token : %q (ID:%q)", systemAccountTokenName, systemAccountTokenID)
	return systemAccountTokenID, systemAccountToken
}
