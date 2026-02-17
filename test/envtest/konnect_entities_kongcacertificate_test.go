package envtest

import (
	"fmt"
	"slices"
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

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
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
			konnect.WithMetricRecorder[configurationv1alpha1.KongCACertificate](&metricsmocks.MockRecorder{}),
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

	t.Run("Adopting an existing CA certificate", func(t *testing.T) {
		caCertID := uuid.NewString()

		w := setupWatch[configurationv1alpha1.KongCACertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on getting and updating CA certificates")
		sdk.CACertificatesSDK.EXPECT().GetCaCertificate(
			mock.Anything,
			caCertID,
			cp.GetKonnectID(),
		).Return(&sdkkonnectops.GetCaCertificateResponse{
			CACertificate: &sdkkonnectcomp.CACertificate{
				Cert: "test-cert",
				ID:   lo.ToPtr(caCertID),
			},
		}, nil)
		sdk.CACertificatesSDK.EXPECT().UpsertCaCertificate(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertCaCertificateRequest) bool {
				return req.CACertificateID == caCertID && req.ControlPlaneID == cp.GetKonnectID()
			}),
		).Return(nil, nil)

		t.Log("Creating a KongCACertificate to adopt the existing CA certificate")
		createdCACert := deploy.KongCACertificateAttachedToCP(t, ctx, clientNamespaced, cp,
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongCACertificate](commonv1alpha1.AdoptModeOverride, caCertID),
		)

		t.Logf("Waiting for KongCACertificate %s to be programmed and set Konnect ID", client.ObjectKeyFromObject(createdCACert))
		watchFor(t, ctx, w, apiwatch.Modified,
			func(caCert *configurationv1alpha1.KongCACertificate) bool {
				return caCert.Name == createdCACert.Name &&
					k8sutils.IsProgrammed(caCert) &&
					caCert.GetKonnectID() == caCertID
			},
			fmt.Sprintf("KongCACertificate didn't get Programmed status condition or didn't get the correct Konnect ID (%s) assigned", caCertID),
		)

		t.Log("Setting up SDK expecatations on CA certificate deletion")
		sdk.CACertificatesSDK.EXPECT().DeleteCaCertificate(mock.Anything, cp.GetKonnectID(), caCertID).Return(nil, nil)

		t.Logf("Deleting the KongCACertificate %s and waiting for it to disappear", client.ObjectKeyFromObject(createdCACert))
		require.NoError(t, clientNamespaced.Delete(ctx, createdCACert))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdCACert, waitTime, tickTime)

	})
}
