package sdk

import (
	"context"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
)

// VaultSDK is the interface for Konnect Vault SDK.
type VaultSDK interface {
	CreateVault(ctx context.Context, controlPlaneID string, vault sdkkonnectcomp.Vault, opts ...sdkkonnectops.Option) (*sdkkonnectops.CreateVaultResponse, error)
	GetVault(ctx context.Context, vaultID string, controlPlaneID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.GetVaultResponse, error)
	UpsertVault(ctx context.Context, request sdkkonnectops.UpsertVaultRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.UpsertVaultResponse, error)
	DeleteVault(ctx context.Context, controlPlaneID string, vaultID string, opts ...sdkkonnectops.Option) (*sdkkonnectops.DeleteVaultResponse, error)
	ListVault(ctx context.Context, request sdkkonnectops.ListVaultRequest, opts ...sdkkonnectops.Option) (*sdkkonnectops.ListVaultResponse, error)
}
