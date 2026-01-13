package envtest

import (
	"fmt"
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/helpers/eventually"
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

	ns2 := deploy.Namespace(t, ctx, mgr.GetClient())

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)
	clientNamespaced2 := client.NewNamespacedClient(mgr.GetClient(), ns2.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane in a second namespace")
	apiAuth2 := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced2)
	cp2 := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced2, apiAuth2)

	t.Log("Creating KongReferenceGrant for KongVault -> KonnectGatewayControlPlane")
	_ = deploy.KongReferenceGrant(t, ctx, clientNamespaced,
		deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
			Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
			Kind:      "KongVault",
			Namespace: configurationv1alpha1.Namespace(""),
		}),
		deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
			Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
			Kind:  "KonnectGatewayControlPlane",
		}),
	)

	vaultWatch := setupWatch[configurationv1alpha1.KongVaultList](t, ctx, cl)

	t.Run("Cross namespace ref KongVault -> KonnectNamespacedRefControlPlane yields ResolvedRefs=False without KongReferenceGrant", func(t *testing.T) {
		const (
			vaultBackend   = "env-no-grant"
			vaultPrefix    = "env-vault-no-grant"
			vaultRawConfig = `{"prefix":"env_vault_no_grant"}`
		)

		createdVault := deploy.KongVaultAttachedToCP(t, ctx, cl, vaultBackend, vaultPrefix, []byte(vaultRawConfig), cp2)

		t.Log("Waiting for KongVault to get ResolvedRefs condition with status=False")
		watchFor(t, ctx, vaultWatch, apiwatch.Modified, func(kv *configurationv1alpha1.KongVault) bool {
			if kv.GetName() != createdVault.GetName() {
				return false
			}

			cpRef := kv.GetControlPlaneRef()
			if cpRef == nil {
				return false
			}

			if cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
				cpRef.KonnectNamespacedRef == nil ||
				cpRef.KonnectNamespacedRef.Name != cp2.GetName() ||
				cpRef.KonnectNamespacedRef.Namespace != cp2.GetNamespace() {
				return false
			}

			return k8sutils.HasConditionFalse(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, kv)
		}, "KongVault didn't get ResolvedRefs status condition set to False")

		require.NoError(t, cl.Delete(ctx, createdVault))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdVault, waitTime, tickTime)
	})

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

	t.Run("Adopting existing vault", func(t *testing.T) {
		vaultID := uuid.NewString()

		t.Log("Setting up SDK expectations for getting and updating vaults")
		sdk.VaultSDK.EXPECT().GetVault(
			mock.Anything,
			vaultID,
			cp.GetKonnectID(),
		).Return(
			&sdkkonnectops.GetVaultResponse{
				Vault: &sdkkonnectcomp.Vault{
					ID:     lo.ToPtr(vaultID),
					Name:   "test-vault",
					Prefix: "prefix",
					Config: map[string]any{},
				},
			}, nil,
		)
		sdk.VaultSDK.EXPECT().UpsertVault(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertVaultRequest) bool {
				return req.VaultID == vaultID && req.ControlPlaneID == cp.GetKonnectID()
			}),
		).Return(
			&sdkkonnectops.UpsertVaultResponse{}, nil,
		)

		t.Log("Creating a KongVault to adopt the existing vault")
		createdVault := deploy.KongVaultAttachedToCP(t, ctx, cl,
			"test-vault",
			"prefix",
			[]byte(`{"key":"value"}`),
			cp,
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongVault](commonv1alpha1.AdoptModeOverride, vaultID),
		)

		t.Logf("Watching for vault %s to be programmed and set Konnect ID", createdVault.Name)
		watchFor(t, ctx, vaultWatch, apiwatch.Modified, func(kv *configurationv1alpha1.KongVault) bool {
			return kv.Name == createdVault.Name &&
				k8sutils.IsProgrammed(kv) &&
				kv.GetKonnectID() == vaultID
		},
			fmt.Sprintf("KongVault didn't get Programmed status condition or didn't get the correct Konnect ID (%s) assigned", vaultID),
		)

		t.Log("Setting up SDK expectations for vault deletion")
		sdk.VaultSDK.EXPECT().DeleteVault(mock.Anything, cp.GetKonnectID(), vaultID).Return(nil, nil)

		t.Logf("Deleting KongVault %s", createdVault.Name)
		require.NoError(t, cl.Delete(ctx, createdVault))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdVault, waitTime, tickTime)
	})
}
