package mocks

import (
	"context"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/adminapi"
)

// KonnectClientFactory is a mock implementation of konnect.ClientFactory.
type KonnectClientFactory struct {
	Client *adminapi.KonnectClient
}

func (f *KonnectClientFactory) NewKonnectClient(context.Context) (*adminapi.KonnectClient, error) {
	return f.Client, nil
}
