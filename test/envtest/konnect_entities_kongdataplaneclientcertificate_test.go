package envtest

import (
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
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
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKongDataPlaneClientCertificate(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongDataPlaneClientCertificate](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongDataPlaneClientCertificate](&metricsmocks.MockRecorder{}),
		),
	)

	ns2 := deploy.Namespace(t, ctx, mgr.GetClient())
	t.Log("Setting up clients")
	clientOptions := client.Options{
		Scheme: scheme.Get(),
	}
	cl, err := client.NewWithWatch(mgr.GetConfig(), clientOptions)
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	cl2, err := client.NewWithWatch(mgr.GetConfig(), clientOptions)
	require.NoError(t, err)
	clientNamespaced2 := client.NewNamespacedClient(mgr.GetClient(), ns2.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Log("Setting up SDK expectations on KongDataPlaneClientCertificate creation")
	const dpCertID = "dp-cert-id"
	sdk.DataPlaneCertificatesSDK.EXPECT().CreateDataplaneCertificate(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
		mock.MatchedBy(func(input *sdkkonnectcomp.DataPlaneClientCertificateRequest) bool {
			return input.Cert == deploy.TestValidCACertPEM
		}),
	).Return(&sdkkonnectops.CreateDataplaneCertificateResponse{
		DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
			Item: &sdkkonnectcomp.DataPlaneClientCertificate{
				ID:   lo.ToPtr(dpCertID),
				Cert: lo.ToPtr(deploy.TestValidCACertPEM),
			},
		},
	}, nil)

	w := setupWatch[configurationv1alpha1.KongDataPlaneClientCertificateList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Log("Creating KongDataPlaneClientCertificate")
	createdCert := deploy.KongDataPlaneClientCertificateAttachedToCP(t, ctx, clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
	)

	t.Log("Waiting for KongDataPlaneClientCertificate to be programmed")
	watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongDataPlaneClientCertificate) bool {
		if c.GetName() != createdCert.GetName() {
			return false
		}
		return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				condition.Status == metav1.ConditionTrue
		})
	}, "KongDataPlaneClientCertificate's Programmed condition should be true eventually")

	t.Log("Waiting for KongDataPlaneClientCertificate to be created in the SDK")
	eventuallyAssertSDKExpectations(t, factory.SDK.DataPlaneCertificatesSDK, waitTime, tickTime)

	t.Log("Setting up SDK expectations on KongDataPlaneClientCertificate deletion")
	sdk.DataPlaneCertificatesSDK.EXPECT().DeleteDataplaneCertificate(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), dpCertID).
		Return(&sdkkonnectops.DeleteDataplaneCertificateResponse{}, nil)

	t.Log("Deleting KongDataPlaneClientCertificate")
	require.NoError(t, cl.Delete(ctx, createdCert))

	t.Log("Waiting for KongDataPlaneClientCertificate to be deleted in the SDK")
	eventuallyAssertSDKExpectations(t, factory.SDK.DataPlaneCertificatesSDK, waitTime, tickTime)

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id = "abc-12345"
		)

		t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
		apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

		w := setupWatch[configurationv1alpha1.KongDataPlaneClientCertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KongDataPlaneClientCertificate creation")
		sdk.DataPlaneCertificatesSDK.EXPECT().
			CreateDataplaneCertificate(
				mock.Anything,
				cp.GetKonnectID(),
				mock.Anything,
			).
			Return(
				&sdkkonnectops.CreateDataplaneCertificateResponse{
					DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
						Item: &sdkkonnectcomp.DataPlaneClientCertificate{
							ID: lo.ToPtr(id),
						},
					},
				},
				nil,
			)

		created := deploy.KongDataPlaneClientCertificateAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		)
		eventuallyAssertSDKExpectations(t, factory.SDK.DataPlaneCertificatesSDK, waitTime, tickTime)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, conditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("DataPlaneClientCertificate didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for DataPlaneClientCertificate to be get Programmed and ControlPlaneRefValid conditions with status=False")
		watchFor(t, ctx, w, apiwatch.Modified,
			conditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongDataPlaneClientCertificate didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)
	})

	t.Run("Adopting existing dataplane certificate", func(t *testing.T) {
		dpCertID := uuid.NewString()
		w := setupWatch[configurationv1alpha1.KongDataPlaneClientCertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting DataPlane certificates")
		sdk.DataPlaneCertificatesSDK.EXPECT().GetDataplaneCertificate(
			mock.Anything,
			cp.GetKonnectID(),
			dpCertID,
		).Return(&sdkkonnectops.GetDataplaneCertificateResponse{
			DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
				Item: &sdkkonnectcomp.DataPlaneClientCertificate{
					ID:   lo.ToPtr(dpCertID),
					Cert: lo.ToPtr(deploy.TestValidCertPEM),
				},
			},
		}, nil)

		t.Log("Creating a KongDataPlaneClientCertificate for adopting it")
		createdDPCert := deploy.KongDataPlaneClientCertificateAttachedToCP(t, ctx, clientNamespaced,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongDataPlaneClientCertificate](commonv1alpha1.AdoptModeMatch, dpCertID),
		)

		t.Logf("Waiting for KongDataPlaneClientCertificate %s/%s to be programmed and set Konnect ID", ns.Name, createdDPCert.Name)
		watchFor(t, ctx, w, apiwatch.Modified, func(dpCert *configurationv1alpha1.KongDataPlaneClientCertificate) bool {
			return dpCert.Name == createdDPCert.Name &&
				k8sutils.IsProgrammed(dpCert) &&
				dpCert.GetKonnectID() == dpCertID
		},
			fmt.Sprintf("KongDataPlaneCertificate didn't get Programmed status condition or didn't get the correct Konnect ID %s assigned", dpCertID),
		)
	})

	t.Run("Cross namespace ref KongDataPlaneClientCertificate -> KonnectNamespacedRefControlPlane yields ResolvedRefs=False without KongReferenceGrant", func(t *testing.T) {
		w := setupWatch[configurationv1alpha1.KongDataPlaneClientCertificateList](t, ctx, cl2, client.InNamespace(ns2.Name))

		t.Log("Don't setting SDK expectations on DataPlaneClientCertificate creation as we do not expect any operations to be made upstream")

		t.Log("Creating a KongDataPlaneClientCertificate with ControlPlaneRef type=konnectNamespacedRef")
		createdCert := deploy.KongDataPlaneClientCertificateAttachedToCP(t, ctx, clientNamespaced2,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp, ns.Name),
		)

		t.Log("Waiting for KongDataPlaneClientCertificate to get ResolvedRefs condition with status=False")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongDataPlaneClientCertificate) bool {
			if c.GetName() != createdCert.GetName() {
				return false
			}

			cpRef := c.GetControlPlaneRef()
			if cpRef == nil {
				return false
			}

			if cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
				cpRef.KonnectNamespacedRef == nil ||
				cpRef.KonnectNamespacedRef.Name != cp.GetName() ||
				cpRef.KonnectNamespacedRef.Namespace != cp.GetNamespace() {
				return false
			}
			return k8sutils.HasConditionFalse(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, c)
		}, "KongDataPlaneClientCertificate didn't get ResolvedRefs status condition set to False")
	})

	t.Run("Cross namespace ref KongDataPlaneClientCertificate -> KonnectNamespacedRefControlPlane yields ResolvedRefs=True with valid KongReferenceGrant", func(t *testing.T) {
		const id = "dp-cert-cross-ns-1234"

		w := setupWatch[configurationv1alpha1.KongDataPlaneClientCertificateList](t, ctx, cl2, client.InNamespace(ns2.Name))

		t.Log("Setting up SDK expectations on DataPlaneClientCertificate creation")
		sdk.DataPlaneCertificatesSDK.EXPECT().
			CreateDataplaneCertificate(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(input *sdkkonnectcomp.DataPlaneClientCertificateRequest) bool {
					return input.Cert == deploy.TestValidCACertPEM
				}),
			).
			Return(
				&sdkkonnectops.CreateDataplaneCertificateResponse{
					DataPlaneClientCertificateResponse: &sdkkonnectcomp.DataPlaneClientCertificateResponse{
						Item: &sdkkonnectcomp.DataPlaneClientCertificate{
							ID: lo.ToPtr(id),
						},
					},
				},
				nil,
			)

		_ = deploy.KongReferenceGrant(t, ctx, clientNamespaced,
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:      "KongDataPlaneClientCertificate",
				Namespace: configurationv1alpha1.Namespace(ns2.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
				Kind:  "KonnectGatewayControlPlane",
			}),
		)

		t.Log("Creating a KongDataPlaneClientCertificate with ControlPlaneRef type=konnectNamespacedRef")
		createdCert := deploy.KongDataPlaneClientCertificateAttachedToCP(t, ctx, clientNamespaced2,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp, ns.Name),
		)

		t.Log("Waiting for KongDataPlaneClientCertificate to get ResolvedRefs condition with status=True")
		watchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongDataPlaneClientCertificate) bool {
			if c.GetName() != createdCert.GetName() {
				return false
			}

			cpRef := c.GetControlPlaneRef()
			if cpRef == nil {
				return false
			}

			if cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
				cpRef.KonnectNamespacedRef == nil ||
				cpRef.KonnectNamespacedRef.Name != cp.GetName() ||
				cpRef.KonnectNamespacedRef.Namespace != cp.GetNamespace() {
				return false
			}
			return k8sutils.HasConditionTrue(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs, c)
		}, "KongDataPlaneClientCertificate didn't get ResolvedRefs status condition set to True")

		eventuallyAssertSDKExpectations(t, factory.SDK.DataPlaneCertificatesSDK, waitTime, tickTime)
	})
}
