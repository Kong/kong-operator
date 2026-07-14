package konnectapigw

import (
	"fmt"
	"slices"
	"strings"
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

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestKongCertificate(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCertificate](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.KongCertificate](&metricsmocks.MockRecorder{}),
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

	t.Run("base", func(t *testing.T) {
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)
		cpID := cp.GetKonnectStatus().GetKonnectID()

		t.Log("Setting up SDK expectations on KongCertificate creation")
		sdk.CertificatesSDK.EXPECT().
			CreateCertificate(mock.Anything, cpID,
				mock.MatchedBy(func(input sdkkonnectcomp.CertificateRequest) bool {
					return input.CertificateRequest2.Cert == deploy.TestValidCertPEM &&
						input.CertificateRequest2.Key == deploy.TestValidCertKeyPEM &&
						slices.Contains(input.CertificateRequest2.Tags, "tag1")
				}),
			).
			Return(&sdkkonnectops.CreateCertificateResponse{
				Certificate: &sdkkonnectcomp.Certificate{
					ID: new("cert-12345"),
				},
			}, nil)

		sdk.CertificatesSDK.EXPECT().
			ListCertificate(mock.Anything,
				mock.MatchedBy(func(input sdkkonnectops.ListCertificateRequest) bool {
					return input.ControlPlaneID == cpID
				}),
			).
			Return(&sdkkonnectops.ListCertificateResponse{
				Object: &sdkkonnectops.ListCertificateResponseBody{},
			}, nil)

		w := envtest.SetupWatch[configurationv1alpha1.KongCertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating KongCertificate")
		createdCert := deploy.KongCertificateAttachedToCP(t, ctx, clientNamespaced, cp,
			func(obj client.Object) {
				cert := obj.(*configurationv1alpha1.KongCertificate)
				cert.Spec.Tags = []string{"tag1", ops.KubernetesUIDLabelKey + ":12345"}
				cert.UID = "12345"
			},
		)

		t.Log("Waiting for KongCertificate to be programmed")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongCertificate) bool {
			if c.GetName() != createdCert.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
		}, "KongCertificate's Programmed condition should be true eventually")

		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.CertificatesSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on KongCertificate update")
		sdk.CertificatesSDK.EXPECT().UpsertCertificate(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertCertificateRequest) bool {
			return r.CertificateID == "cert-12345" &&
				slices.Contains(r.CertificateRequest.CertificateRequest2.Tags, "addedTag")
		})).Return(&sdkkonnectops.UpsertCertificateResponse{}, nil)

		t.Log("Patching KongCertificate")
		certToPatch := createdCert.DeepCopy()
		certToPatch.Spec.Tags = append(certToPatch.Spec.Tags, "addedTag")
		require.NoError(t, clientNamespaced.Patch(ctx, certToPatch, client.MergeFrom(createdCert)))

		t.Log("Waiting for KongCertificate to be updated in the SDK")
		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.CertificatesSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on KongCertificate deletion")
		sdk.CertificatesSDK.EXPECT().DeleteCertificate(mock.Anything, cpID, "cert-12345").
			Return(&sdkkonnectops.DeleteCertificateResponse{}, nil)

		t.Log("Deleting KongCertificate")
		require.NoError(t, cl.Delete(ctx, createdCert))

		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.CertificatesSDK, consts.WaitTime, consts.TickTime)
	})

	t.Run("should handle conflict in creation correctly", func(t *testing.T) {
		const (
			certID = "id-conflict"
		)
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)
		cpID := cp.GetKonnectStatus().GetKonnectID()

		w := envtest.SetupWatch[configurationv1alpha1.KongCertificateList](t, ctx, cl, client.InNamespace(ns.Name))
		t.Log("Creating a KongCertificate")
		createdCert := deploy.KongCertificateAttachedToCP(t, ctx, clientNamespaced, cp,
			func(obj client.Object) {
				cert := obj.(*configurationv1alpha1.KongCertificate)
				cert.Spec.Tags = []string{"xconflictx"}
			},
		)
		t.Log("Setup mock SDK for listing certificates by UID")
		sdk.CertificatesSDK.EXPECT().
			ListCertificate(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.ListCertificateRequest) bool {
					return req.ControlPlaneID == cpID &&
						strings.Contains(*req.Tags, "xconflictx")
				}),
			).
			Return(
				&sdkkonnectops.ListCertificateResponse{
					Object: &sdkkonnectops.ListCertificateResponseBody{
						Data: []sdkkonnectcomp.Certificate{
							{
								ID: new(certID),
							},
						},
					},
				}, nil,
			)

		t.Log("Watching for KongCertificates to verify the created KongCertificate gets programmed")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongCertificate) bool {
			if c.GetName() != createdCert.GetName() {
				return false
			}
			if !slices.Equal(c.Spec.Tags, createdCert.Spec.Tags) {
				return false
			}
			return c.GetKonnectID() == certID && k8sutils.IsProgrammed(c)
		}, "KongCertificate should be programmed and have ID in status after handling conflict")

		t.Log("Ensuring that the SDK's create and list methods are called")
		envtest.EventuallyAssertSDKExpectations(t, sdk.CertificatesSDK, consts.WaitTime, consts.TickTime)
	})

	t.Run("removing referenced CP sets the status conditions properly", func(t *testing.T) {
		const (
			id = "abc-12345"
		)

		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)
		cpID := cp.GetKonnectStatus().GetKonnectID()

		w := envtest.SetupWatch[configurationv1alpha1.KongCertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on KongCertifcate creation")
		sdk.CertificatesSDK.EXPECT().
			ListCertificate(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.ListCertificateRequest) bool {
					return req.ControlPlaneID == cpID &&
						strings.Contains(*req.Tags, "tag3")
				}),
			).
			Return(
				&sdkkonnectops.ListCertificateResponse{}, nil,
			)

		sdk.CertificatesSDK.EXPECT().
			CreateCertificate(
				mock.Anything,
				cp.GetKonnectID(),
				mock.MatchedBy(func(req sdkkonnectcomp.CertificateRequest) bool {
					return slices.Contains(req.CertificateRequest2.Tags, "tag3")
				}),
			).
			Return(
				&sdkkonnectops.CreateCertificateResponse{
					Certificate: &sdkkonnectcomp.Certificate{
						ID: new(id),
					},
				},
				nil,
			)

		created := deploy.KongCertificateAttachedToCP(t, ctx, clientNamespaced, cp,
			deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
			func(obj client.Object) {
				cert := obj.(*configurationv1alpha1.KongCertificate)
				cert.Spec.Tags = []string{"tag3"}
			},
		)
		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.CACertificatesSDK, consts.WaitTime, consts.TickTime)

		t.Log("Waiting for object to be programmed and get Konnect ID")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, envtest.ConditionProgrammedIsSetToTrueAndCPRefIsKonnectNamespacedRef(created, id),
			fmt.Sprintf("Certificate didn't get Programmed status condition or didn't get the correct %s Konnect ID assigned", id))

		t.Log("Deleting KonnectGatewayControlPlane")
		require.NoError(t, clientNamespaced.Delete(ctx, cp))

		t.Log("Waiting for CACert to be get Programmed and ControlPlaneRefValid conditions with status=False")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.ConditionsAreSetWhenReferencedControlPlaneIsMissing(created),
			"KongCACertificate didn't get Programmed and/or ControlPlaneRefValid status condition set to False",
		)
	})

	t.Run("Adopting an existing certificate", func(t *testing.T) {
		certID := uuid.NewString()

		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)
		cpID := cp.GetKonnectStatus().GetKonnectID()

		w := envtest.SetupWatch[configurationv1alpha1.KongCertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for getting and updating certificates")
		sdk.CertificatesSDK.EXPECT().GetCertificate(
			mock.Anything,
			certID,
			cpID,
		).Return(&sdkkonnectops.GetCertificateResponse{
			Certificate: &sdkkonnectcomp.Certificate{
				Cert: "test-cert",
				Key:  "test-key",
				ID:   new(certID),
			},
		}, nil)
		sdk.CertificatesSDK.EXPECT().UpsertCertificate(
			mock.Anything,
			mock.MatchedBy(func(req sdkkonnectops.UpsertCertificateRequest) bool {
				return req.CertificateID == certID && req.ControlPlaneID == cpID
			}),
		).Return(nil, nil)

		t.Log("Creating a KongCertificate to adopt the existing certificate")
		createdCert := deploy.KongCertificateAttachedToCP(
			t, ctx, clientNamespaced, cp,
			deploy.WithKonnectAdoptOptions[*configurationv1alpha1.KongCertificate](commonv1alpha1.AdoptModeOverride, certID),
		)

		t.Logf("Waiting for KongCertificate %s to be programmed and set Konnect ID", client.ObjectKeyFromObject(createdCert))
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			func(cert *configurationv1alpha1.KongCertificate) bool {
				return cert.Name == createdCert.Name &&
					k8sutils.IsProgrammed(cert) &&
					cert.GetKonnectID() == certID
			},
			fmt.Sprintf("KongCertificate didn't get Programmed status condition or didn't get the correct Konnect ID (%s) assigned", certID),
		)

		t.Log("Setting up SDK expectations for certificate deletion")
		sdk.CertificatesSDK.EXPECT().DeleteCertificate(mock.Anything, cpID, certID).Return(nil, nil)

		t.Logf("Deleting KongCertificate %s and waiting for it to disappear", client.ObjectKeyFromObject(createdCert))
		require.NoError(t, clientNamespaced.Delete(ctx, createdCert))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdCert, consts.WaitTime, consts.TickTime)
	})

	t.Run("cross-namespace SecretRef with KongReferenceGrant", func(t *testing.T) {
		const certID = "cert-xns-123"
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)
		cpID := cp.GetKonnectStatus().GetKonnectID()

		t.Log("Creating a Secret in a different namespace")
		secretNS := deploy.Namespace(t, ctx, cl)
		secret := deploy.Secret(t, ctx, cl,
			map[string][]byte{
				"tls.crt": []byte(deploy.TestValidCertPEM),
				"tls.key": []byte(deploy.TestValidCertKeyPEM),
			},
			func(obj client.Object) {
				obj.SetNamespace(secretNS.Name)
			},
		)

		w := envtest.SetupWatch[configurationv1alpha1.KongCertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating KongCertificate with cross-namespace SecretRef (no grant yet)")
		certType := configurationv1alpha1.KongCertificateSourceTypeSecretRef
		createdCert := &configurationv1alpha1.KongCertificate{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cert-xns-",
				Namespace:    ns.Name,
			},
			Spec: configurationv1alpha1.KongCertificateSpec{
				Type: &certType,
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: cp.GetName(),
					},
				},
				SecretRef: &commonv1alpha1.NamespacedRef{
					Name:      secret.Name,
					Namespace: new(secretNS.Name),
				},
			},
		}
		require.NoError(t, clientNamespaced.Create(ctx, createdCert))

		t.Log("Waiting for KongCertificate to have RefNotPermitted condition (no grant)")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongCertificate) bool {
			if c.GetName() != createdCert.GetName() {
				return false
			}
			return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
					condition.Status == metav1.ConditionFalse &&
					condition.Reason == configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted
			})
		}, "KongCertificate should have RefNotPermitted condition without KongReferenceGrant")

		t.Log("Creating KongReferenceGrant to allow the cross-namespace reference")
		deploy.KongReferenceGrant(t, ctx, cl,
			func(obj client.Object) {
				obj.SetNamespace(secretNS.Name)
			},
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:      "KongCertificate",
				Namespace: configurationv1alpha1.Namespace(ns.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: "core",
				Kind:  "Secret",
				Name:  new(configurationv1alpha1.ObjectName(secret.Name)),
			}),
		)

		t.Log("Setting up SDK expectations for certificate creation after grant")
		sdk.CertificatesSDK.EXPECT().
			ListCertificate(mock.Anything,
				mock.MatchedBy(func(input sdkkonnectops.ListCertificateRequest) bool {
					return input.ControlPlaneID == cpID
				}),
			).
			Return(&sdkkonnectops.ListCertificateResponse{
				Object: &sdkkonnectops.ListCertificateResponseBody{},
			}, nil)

		sdk.CertificatesSDK.EXPECT().
			CreateCertificate(mock.Anything, cpID,
				mock.MatchedBy(func(input sdkkonnectcomp.CertificateRequest) bool {
					return input.CertificateRequest2.Cert == deploy.TestValidCertPEM &&
						input.CertificateRequest2.Key == deploy.TestValidCertKeyPEM
				}),
			).
			Return(&sdkkonnectops.CreateCertificateResponse{
				Certificate: &sdkkonnectcomp.Certificate{
					ID: new(certID),
				},
			}, nil)

		t.Log("Waiting for KongCertificate to be programmed after grant is created")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongCertificate) bool {
			if c.GetName() != createdCert.GetName() {
				return false
			}
			hasResolvedRefs := lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
					condition.Status == metav1.ConditionTrue &&
					condition.Reason == configurationv1alpha1.KongReferenceGrantReasonResolvedRefs
			})
			hasProgrammed := lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
			return hasResolvedRefs && hasProgrammed
		}, "KongCertificate should have ResolvedRefs=True and be Programmed after KongReferenceGrant is created")

		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.CertificatesSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations for certificate deletion")
		sdk.CertificatesSDK.EXPECT().DeleteCertificate(mock.Anything, cpID, certID).Return(nil, nil)

		t.Logf("Deleting KongCertificate %s and waiting for it to disappear", client.ObjectKeyFromObject(createdCert))
		require.NoError(t, cl.Delete(ctx, createdCert))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdCert, consts.WaitTime, consts.TickTime)
	})

	t.Run("cross-namespace SecretRefAlt with KongReferenceGrant", func(t *testing.T) {
		const certID = "cert-xns-alt-123"
		cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)
		cpID := cp.GetKonnectStatus().GetKonnectID()

		t.Log("Creating Secrets in different namespaces")
		secretNS := deploy.Namespace(t, ctx, cl)
		secret := deploy.Secret(t, ctx, cl,
			map[string][]byte{
				"tls.crt": []byte(deploy.TestValidCertPEM),
				"tls.key": []byte(deploy.TestValidCertKeyPEM),
			},
			func(obj client.Object) {
				obj.SetNamespace(secretNS.Name)
			},
		)
		secretAlt := deploy.Secret(t, ctx, cl,
			map[string][]byte{
				"tls.crt": []byte(deploy.TestValidCertPEM),
				"tls.key": []byte(deploy.TestValidCertKeyPEM),
			},
			func(obj client.Object) {
				obj.SetNamespace(secretNS.Name)
			},
		)

		t.Log("Creating KongReferenceGrant to allow the cross-namespace references")
		grant := deploy.KongReferenceGrant(t, ctx, cl,
			func(obj client.Object) {
				obj.SetNamespace(secretNS.Name)
			},
			deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
				Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
				Kind:      "KongCertificate",
				Namespace: configurationv1alpha1.Namespace(ns.Name),
			}),
			deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
				Group: "core",
				Kind:  "Secret",
				// Allow all secrets by not specifying Name
			}),
		)
		_ = grant

		w := envtest.SetupWatch[configurationv1alpha1.KongCertificateList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations for certificate creation")
		sdk.CertificatesSDK.EXPECT().
			ListCertificate(mock.Anything,
				mock.MatchedBy(func(input sdkkonnectops.ListCertificateRequest) bool {
					return input.ControlPlaneID == cpID
				}),
			).
			Return(&sdkkonnectops.ListCertificateResponse{
				Object: &sdkkonnectops.ListCertificateResponseBody{},
			}, nil)

		sdk.CertificatesSDK.EXPECT().
			CreateCertificate(mock.Anything, cpID,
				mock.MatchedBy(func(input sdkkonnectcomp.CertificateRequest) bool {
					return input.CertificateRequest2.Cert == deploy.TestValidCertPEM &&
						input.CertificateRequest2.Key == deploy.TestValidCertKeyPEM
				}),
			).
			Return(&sdkkonnectops.CreateCertificateResponse{
				Certificate: &sdkkonnectcomp.Certificate{
					ID: new(certID),
				},
			}, nil)

		t.Log("Creating KongCertificate with cross-namespace SecretRef and SecretRefAlt")
		certType := configurationv1alpha1.KongCertificateSourceTypeSecretRef
		createdCert := &configurationv1alpha1.KongCertificate{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cert-xns-alt-",
				Namespace:    ns.Name,
			},
			Spec: configurationv1alpha1.KongCertificateSpec{
				Type: &certType,
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
						Name: cp.GetName(),
					},
				},
				SecretRef: &commonv1alpha1.NamespacedRef{
					Name:      secret.Name,
					Namespace: new(secretNS.Name),
				},
				SecretRefAlt: &commonv1alpha1.NamespacedRef{
					Name:      secretAlt.Name,
					Namespace: new(secretNS.Name),
				},
			},
		}
		require.NoError(t, clientNamespaced.Create(ctx, createdCert))

		t.Log("Waiting for KongCertificate to be programmed with ResolvedRefs=True")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(c *configurationv1alpha1.KongCertificate) bool {
			if c.GetName() != createdCert.GetName() {
				return false
			}
			hasResolvedRefs := lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
					condition.Status == metav1.ConditionTrue &&
					condition.Reason == configurationv1alpha1.KongReferenceGrantReasonResolvedRefs
			})
			hasProgrammed := lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
				return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
					condition.Status == metav1.ConditionTrue
			})
			return hasResolvedRefs && hasProgrammed
		}, "KongCertificate should have ResolvedRefs=True and be Programmed with both SecretRef and SecretRefAlt")

		envtest.EventuallyAssertSDKExpectations(t, factory.SDK.CertificatesSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations for certificate deletion")
		sdk.CertificatesSDK.EXPECT().DeleteCertificate(mock.Anything, cpID, certID).Return(nil, nil)

		t.Logf("Deleting KongCertificate %s and waiting for it to disappear", client.ObjectKeyFromObject(createdCert))
		require.NoError(t, cl.Delete(ctx, createdCert))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdCert, consts.WaitTime, consts.TickTime)
	})
}
