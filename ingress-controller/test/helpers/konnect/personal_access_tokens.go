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

// CreateTestPersonalAccessToken creates a personal access token for the user
// associated with the provided token or default access token if no token provided,
// and returns the created token string.
// This token is time limited and will expire after 1 hour.
// The created token will be automatically deleted after the test finishes.
func CreateTestPersonalAccessToken(ctx context.Context, t *testing.T) string {
	t.Helper()

	s := sdk.New(accessToken(), serverURLOpt())

	me, err := s.Me.GetUsersMe(ctx)
	require.NoError(t, err)
	require.NotNil(t, me)
	require.NotNil(t, me.User)
	require.NotNil(t, me.User.ID)

	var (
		tokenID      string
		tokenCreated string
		tokenName    = fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixMilli())
	)
	createServiceAccountToken := retry.Do(func() error {
		createResp, err := s.PersonalAccessTokens.
			CreatePersonalAccessToken(ctx, *me.User.ID,
				&sdkkonnectcomp.PersonalAccessTokenCreateRequest{
					Name:      tokenName,
					ExpiresAt: time.Now().Add(time.Hour),
				},
			)
		if err != nil {
			return err
		}

		if createResp == nil {
			return fmt.Errorf("failed to create personal access token: response is nil")
		}

		if createResp.GetStatusCode() != http.StatusCreated {
			body, err := io.ReadAll(createResp.RawResponse.Body)
			if err != nil {
				body = []byte(err.Error())
			}
			return fmt.Errorf("failed to create personal access token: code %d, message %s", createResp.GetStatusCode(), body)
		}

		if createResp.PersonalAccessTokenCreateResponse == nil ||
			createResp.PersonalAccessTokenCreateResponse.ID == "" ||
			createResp.PersonalAccessTokenCreateResponse.KonnectToken == "" {
			return fmt.Errorf(
				"failed to create personal access token: response fields are missing (status code %d)",
				createResp.GetStatusCode(),
			)
		}

		tokenID = createResp.PersonalAccessTokenCreateResponse.ID
		tokenCreated = createResp.PersonalAccessTokenCreateResponse.KonnectToken
		return nil
	}, retry.Attempts(5), retry.Delay(time.Second))
	require.NoError(t, createServiceAccountToken)

	t.Cleanup(func() {
		fmt.Printf("deleting test Konnect Personal Access Token: %q", tokenID)
		err := retry.Do(
			func() error { //nolint:contextcheck
				_, err := s.PersonalAccessTokens.DeletePersonalAccessToken(ctx, *me.User.ID, tokenID)
				return err
			},
			retry.Attempts(5),
			retry.Delay(time.Second),
		)
		if err != nil {
			// Don't fail the test if cleanup fails, just log the error.
			// Cleanup job will eventually clean up konnect.
			fmt.Printf("failed to delete test Konnect Personal Access Token %q: %v", tokenID, err)
		}
	})

	t.Logf("created test Konnect Personal Access Token: %q (ID:%q)", tokenName, tokenID)
	return tokenCreated
}
