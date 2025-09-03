package envtest

import (
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/apis/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongVault(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongVault](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongVault](&metricsmocks.MockRecorder{}),
		),
	}
	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	vaultWatch := setupWatch[configurationv1alpha1.KongVaultList](t, ctx, cl)

	t.Run("should create, update and delete vault successfully", func(t *testing.T) {
		const (
			vaultBackend     = "env"
			vaultPrefix      = "env-vault"
			vaultRawConfig   = `{"prefix":"env_vault"}`
			vaultDespription = "test-env-vault"

			vaultID = "vault-12345"
		)

		t.Log("Setting up mock SDK for vault creation")
		sdk.VaultSDK.EXPECT().CreateVault(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.MatchedBy(func(input sdkkonnectcomp.Vault) bool {
			return input.Name == vaultBackend && input.Prefix == vaultPrefix
		})).Return(&sdkkonnectops.CreateVaultResponse{
			Vault: &sdkkonnectcomp.Vault{
				ID: lo.ToPtr(vaultID),
			},
		}, nil)

		vault := deploy.KongVaultAttachedToCP(t, ctx, cl, vaultBackend, vaultPrefix, []byte(vaultRawConfig), cp)

		t.Log("Waiting for KongVault to be programmed")
		watchFor(t, ctx, vaultWatch, apiwatch.Modified, func(v *configurationv1alpha1.KongVault) bool {
			return v.GetKonnectID() == vaultID && k8sutils.IsProgrammed(v)
		}, "KongVault didn't get Programmed status condition or didn't get the correct (vault-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.VaultSDK, waitTime, tickTime)

		t.Log("Setting up mock SDK for vault update")
		sdk.VaultSDK.EXPECT().UpsertVault(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertVaultRequest) bool {
			return r.VaultID == vaultID &&
				r.Vault.Name == vaultBackend &&
				r.Vault.Description != nil && *r.Vault.Description == vaultDespription
		})).Return(&sdkkonnectops.UpsertVaultResponse{}, nil)

		t.Log("Patching KongVault")
		vaultToPatch := vault.DeepCopy()
		vaultToPatch.Spec.Description = vaultDespription
		require.NoError(t, clientNamespaced.Patch(ctx, vaultToPatch, client.MergeFrom(vault)))

		eventuallyAssertSDKExpectations(t, factory.SDK.ConsumersSDK, waitTime, tickTime)

		t.Log("Setting up mock SDK for vault deletion")
		sdk.VaultSDK.EXPECT().DeleteVault(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), vaultID).
			Return(&sdkkonnectops.DeleteVaultResponse{}, nil)

		t.Log("Deleting KongVault")
		require.NoError(t, cl.Delete(ctx, vault))

		eventuallyAssertSDKExpectations(t, factory.SDK.VaultSDK, waitTime, tickTime)
	})

	t.Run("should correctly handle conflict on create", func(t *testing.T) {
		const (
			vaultBackend   = "env-conflict"
			vaultPrefix    = "env-vault-conflict"
			vaultRawConfig = `{"prefix":"env_vault_conflict"}`
			vaultID        = "vault-conflict-id"
		)

		t.Log("Setting up mock SDK for vault creation with conflict")
		sdk.VaultSDK.EXPECT().CreateVault(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.MatchedBy(func(input sdkkonnectcomp.Vault) bool {
			return input.Name == vaultBackend && input.Prefix == vaultPrefix
		})).Return(nil, &sdkkonnecterrs.SDKError{
			StatusCode: 400,
			Body: `{
					"code": 3,
					"message": "data constraint error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"type": "ERROR_TYPE_REFERENCE",
							"field": "name",
							"messages": [
								"name (type: unique) constraint failed"
							]
						}
					]
				}`,
		})

		sdk.VaultSDK.EXPECT().ListVault(
			mock.Anything,
			mock.MatchedBy(func(r sdkkonnectops.ListVaultRequest) bool {
				return r.ControlPlaneID == cp.GetKonnectStatus().GetKonnectID() &&
					r.Tags != nil && strings.Contains(*r.Tags, "k8s-uid")
			},
			)).Return(&sdkkonnectops.ListVaultResponse{
			Object: &sdkkonnectops.ListVaultResponseBody{
				Data: []sdkkonnectcomp.Vault{
					{
						ID: lo.ToPtr(vaultID),
					},
				},
			},
		}, nil)

		vault := deploy.KongVaultAttachedToCP(t, ctx, cl, vaultBackend, vaultPrefix, []byte(vaultRawConfig), cp)

		t.Log("Waiting for KongVault to be programmed")
		watchFor(t, ctx, vaultWatch, apiwatch.Modified, func(v *configurationv1alpha1.KongVault) bool {
			if v.GetName() != vault.GetName() {
				return false
			}

			return lo.ContainsBy(v.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongVault's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.VaultSDK, waitTime, tickTime)
	})

	t.Run("should handle konnectID control plane reference", func(t *testing.T) {
		t.Skip("konnectID control plane reference not supported yet: https://github.com/kong/kong-operator/issues/1469")
		const (
			vaultBackend   = "env"
			vaultPrefix    = "env-vault"
			vaultRawConfig = `{"prefix":"env_vault"}`
			vaultID        = "vault-12345"
		)

		t.Log("Setting up mock SDK for vault creation")
		sdk.VaultSDK.EXPECT().CreateVault(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), mock.MatchedBy(func(input sdkkonnectcomp.Vault) bool {
			return input.Name == vaultBackend && input.Prefix == vaultPrefix
		})).Return(&sdkkonnectops.CreateVaultResponse{
			Vault: &sdkkonnectcomp.Vault{
				ID: lo.ToPtr(vaultID),
			},
		}, nil)

		t.Log("Creating a KongVault with ControlPlaneRef type=konnectID")
		vault := deploy.KongVaultAttachedToCP(t, ctx, cl, vaultBackend, vaultPrefix, []byte(vaultRawConfig), cp,
			deploy.WithKonnectIDControlPlaneRef(cp),
		)

		t.Log("Waiting for KongVault to be programmed")
		watchFor(t, ctx, vaultWatch, apiwatch.Modified, func(v *configurationv1alpha1.KongVault) bool {
			if vault.GetName() != v.GetName() {
				return false
			}
			if vault.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectID {
				return false
			}
			return v.GetKonnectID() == vaultID && k8sutils.IsProgrammed(v)
		}, "KongVault didn't get Programmed status condition or didn't get the correct (vault-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.VaultSDK, waitTime, tickTime)
	})
}
