package envtest

import (
	"fmt"
	"slices"
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

	"github.com/kong/kong-operator/controller/konnect"
	sdkmocks "github.com/kong/kong-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongCACertificate(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())
	const (
		tagName            = "tag1"
		conflictingTagName = "xconflictx"
	)

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCACertificate](konnectInfiniteSyncTime),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Log("Setting up SDK expectations on KongCACertificate creation")
	sdk.CACertificatesSDK.EXPECT().CreateCaCertificate(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
		mock.MatchedBy(func(input sdkkonnectcomp.CACertificate) bool {
			return input.Cert == deploy.TestValidCACertPEM &&
				slices.Contains(input.Tags, tagName)
		}),
	).Return(&sdkkonnectops.CreateCaCertificateResponse{
		CACertificate: &sdkkonnectcomp.CACertificate{
			ID: lo.ToPtr("12345"),
		},
	}, nil)

	w := setupWatch[configurationv1alpha1.KongCACertificateList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Log("Creating KongCACertificate")
	createdCert := deploy.KongCACertificateAttachedToCP(t, ctx, clientNamespaced, cp,
		func(obj client.Object) {
			cert := obj.(*configurationv1alpha1.KongCACertificate)
			cert.Spec.Tags = []string{tagName}
		},
	)

	t.Log("Waiting for KongCACertificate to be programmed")
	watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongCACertificate) bool {
		if c.GetName() != createdCert.GetName() {
			return false
		}
		return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				condition.Status == metav1.ConditionTrue
		})
	}, "KongCACertificate's Programmed condition should be true eventually")

	t.Log("Waiting for KongCACertificate to be created in the SDK")
	eventuallyAssertSDKExpectations(t, factory.SDK.CACertificatesSDK, waitTime, tickTime)

	t.Log("Setting up SDK expectations on KongCACertificate update")
	sdk.CACertificatesSDK.EXPECT().UpsertCaCertificate(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertCaCertificateRequest) bool {
		return r.CACertificateID == "12345" &&
			lo.Contains(r.CACertificate.Tags, "addedTag")
	})).Return(&sdkkonnectops.UpsertCaCertificateResponse{}, nil)

	t.Log("Patching KongCACertificate")
	certToPatch := createdCert.DeepCopy()
	certToPatch.Spec.Tags = append(certToPatch.Spec.Tags, "addedTag")
	require.NoError(t, clientNamespaced.Patch(ctx, certToPatch, client.MergeFrom(createdCert)))

	t.Log("Waiting for KongCACertificate to be updated in the SDK")
	eventuallyAssertSDKExpectations(t, factory.SDK.CACertificatesSDK, waitTime, tickTime)

	t.Log("Setting up SDK expectations on KongCACertificate deletion")
	sdk.CACertificatesSDK.EXPECT().DeleteCaCertificate(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), "12345").
		Return(&sdkkonnectops.DeleteCaCertificateResponse{}, nil)

	t.Log("Deleting KongCACertificate")
	require.NoError(t, cl.Delete(ctx, createdCert))

	t.Log("Waiting for KongCACertificate to be deleted in the SDK")
	eventuallyAssertSDKExpectations(t, factory.SDK.CACertificatesSDK, waitTime, tickTime)

	t.Run("should handle conflict in creation correctly", func(t *testing.T) {
		const (
			certID = "id-conflict"
		)
		t.Log("Setup mock SDK for creating CA certificate and listing CA certificates by UID")
		cpID := cp.GetKonnectStatus().GetKonnectID()
		sdk.CACertificatesSDK.EXPECT().
			CreateCaCertificate(mock.Anything, cpID,
				mock.MatchedBy(func(input sdkkonnectcomp.CACertificate) bool {
					return input.Cert == deploy.TestValidCACertPEM &&
						slices.Contains(input.Tags, conflictingTagName)
				}),
			).
			Return(nil,
				&sdkkonnecterrs.SDKError{
					StatusCode: 400,
					Body:       ErrBodyDataConstraintError,
				},
			)

		sdk.CACertificatesSDK.EXPECT().
			ListCaCertificate(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.ListCaCertificateRequest) bool {
					return req.ControlPlaneID == cpID
				}),
			).
			Return(
				&sdkkonnectops.ListCaCertificateResponse{
					Object: &sdkkonnectops.ListCaCertificateResponseBody{
						Data: []sdkkonnectcomp.CACertificate{
							{
								ID: lo.ToPtr(certID),
							},
						},
					},
				}, nil,
			)

		t.Log("Creating a KongCACertificate")
		deploy.KongCACertificateAttachedToCP(t, ctx, clientNamespaced, cp,
			func(obj client.Object) {
				cert := obj.(*configurationv1alpha1.KongCACertificate)
				cert.Spec.Tags = []string{conflictingTagName}
			},
		)

		t.Log("Watching for KongCACertificates to verify the created KongCACertificate gets programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongCACertificate) bool {
			return c.GetKonnectID() == certID && k8sutils.IsProgrammed(c)
		}, "KongCACertificate should be programmed and have ID in status after handling conflict")

		eventuallyAssertSDKExpectations(t, sdk.CACertificatesSDK, waitTime, tickTime)
	})

	t.Run("should handle konnectID control plane reference", func(t *testing.T) {
		t.Skip("konnectID control plane reference not supported yet: https://github.com/kong/kong-operator/issues/1469")
		t.Log("Setting up SDK expectations on KongCACertificate creation")
		sdk.CACertificatesSDK.EXPECT().CreateCaCertificate(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
			mock.MatchedBy(func(input sdkkonnectcomp.CACertificate) bool {
				return input.Cert == deploy.TestValidCACertPEM &&
					slices.Contains(input.Tags, tagName)
			}),
		).Return(&sdkkonnectops.CreateCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				ID: lo.ToPtr("12345"),
			},
		}, nil)

		t.Log("Creating KongCACertificate with ControlPlaneRef type=konnectID")
		createdCert := deploy.KongCACertificateAttachedToCP(t, ctx, clientNamespaced, cp,
			func(obj client.Object) {
				cert := obj.(*configurationv1alpha1.KongCACertificate)
				cert.Spec.Tags = []string{tagName}
			},
			deploy.WithKonnectIDControlPlaneRef(cp),
		)

		t.Log("Waiting for KongCACertificate to be programmed")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongCACertificate) bool {
			if c.GetName() != createdCert.GetName() {
				return false
			}
			if c.GetControlPlaneRef().Type != configurationv1alpha1.ControlPlaneRefKonnectID {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongCACertificate's Programmed condition should be true eventually")

		eventuallyAssertSDKExpectations(t, factory.SDK.CACertificatesSDK, waitTime, tickTime)
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id = "abc-12345"
		)

		tags := []string{"tag1"}

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1alpha1.KongCACertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KongCACertifcate creation")
		sdk.CACertificatesSDK.EXPECT().
			CreateCaCertificate(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.CACertificate) bool {
					return slices.Contains(req.Tags, "tag1")
				}),
			).
			Return(
				&sdkkonnectops.CreateCaCertificateResponse{
					CACertificate: &sdkkonnectcomp.CACertificate{
						ID: lo.ToPtr(id),
					},
				},
				nil,
			)

		created := deploy.KongCACertificateAttachedToCP(t, ctx, clientNamespaced, cp,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cert := obj.(*configurationv1alpha1.KongCACertificate)
				cert.Spec.Tags = tags
			},
		)
		eventuallyAssertSDKExpectations(t, factory.SDK.CACertificatesSDK, waitTime, tickTime)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("CACertificate didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for CACert to be get Programmed and ControlPlaneRefValid conditions with status=False")
		watchFor(t, ctx, w, apiwatch.Modified,
			conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongCACertificate didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)
	})
}
