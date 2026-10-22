package konnect

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestDeletingUnmanagedSNIRetainsCertificateChecks(t *testing.T) {
	sni := &configurationv1alpha1.KongSNI{
		Name: "unmanaged", Namespace: "default", DeletionTimestamp: new(metav1.Now()),
		Finalizers: []string{KonnectCleanupFinalizer},
		Spec:       configurationv1alpha1.KongSNISpec{CertificateRef: commonv1alpha1.NamespacedRef{Name: "missing"}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(sni).
		WithStatusSubresource(&configurationv1alpha1.KongSNI{}).Build()
	_, err := handleKongCertificateRef(t.Context(), cl, sni)
	_, missing := errors.AsType[ReferencedKongCertificateDoesNotExistError](err)
	require.True(t, missing, "unmanaged SNIs must retain missing-certificate cleanup behavior")
}

func TestDeleteGatewaySNIWithInvalidCertificateReference(t *testing.T) {
	for _, state := range []string{"missing certificate", "revoked grants", "moved ControlPlane", "stale certificate", "deleting Gateway"} {
		t.Run(state, func(t *testing.T) {
			cp := testControlPlaneOK.DeepCopy()
			cp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name = "gateway-auth"
			auth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Name: "gateway-auth", Namespace: cp.Namespace,
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeToken, Token: "test-token", ServerURL: sdkmocks.SDKServerURL,
				},
				Status: konnectv1alpha1.KonnectAPIAuthConfigurationStatus{Conditions: []metav1.Condition{{
					Type: konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, Status: metav1.ConditionTrue,
					Reason: konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
				}}},
			}
			gateway := &gwtypes.Gateway{
				Name: "gateway", Namespace: cp.Namespace, UID: "gateway-uid", Finalizers: []string{"test-gateway-cleanup"},
			}
			extension := &konnectv1alpha2.KonnectExtension{
				Name: "gateway-extension", Namespace: gateway.Namespace,
				Labels: map[string]string{"gateway-operator.konghq.com/managed-by": "gateway"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway", Name: gateway.Name, UID: gateway.UID,
				}},
				Spec: konnectv1alpha2.KonnectExtensionSpec{Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
					ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: cp.Name},
					}},
				}},
			}
			sni := &configurationv1alpha1.KongSNI{
				Name: "listener-sni", Namespace: gateway.Namespace, Finalizers: []string{KonnectCleanupFinalizer},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway", Name: gateway.Name, UID: gateway.UID, Controller: new(true),
				}},
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{Name: "certificate", Namespace: new("certificates")},
				},
				Status: configurationv1alpha1.KongSNIStatus{Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
					ID: "original-sni-id", CertificateID: "original-certificate-id", ControlPlaneID: cp.Status.ID,
				}},
			}
			objects := []client.Object{cp, auth, gateway, extension, sni}
			if state != "missing certificate" {
				cert := testKongCertOK.DeepCopy()
				cert.Name, cert.Namespace = sni.Spec.CertificateRef.Name, *sni.Spec.CertificateRef.Namespace
				if state == "moved ControlPlane" {
					cert.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = "another-controlplane"
					cert.Status.Konnect.ControlPlaneID = "another-controlplane-id"
				}
				if state == "stale certificate" {
					cert.Generation++
				}
				objects = append(objects, cert)
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
				WithStatusSubresource(&configurationv1alpha1.KongSNI{}, &konnectv1alpha1.KonnectAPIAuthConfiguration{}).Build()
			if state == "deleting Gateway" {
				require.NoError(t, cl.Delete(t.Context(), gateway))
			}
			require.NoError(t, cl.Delete(t.Context(), sni))

			factory := sdkmocks.NewMockSDKFactory(t)
			factory.SDK.SNIsSDK.EXPECT().DeleteSniWithCertificate(mock.Anything,
				sdkkonnectops.DeleteSniWithCertificateRequest{
					ControlPlaneID: cp.Status.ID, CertificateID: "original-certificate-id", SNIID: "original-sni-id",
				},
			).Return(&sdkkonnectops.DeleteSniWithCertificateResponse{}, nil).Once()
			reconciler := NewKonnectEntityReconciler[configurationv1alpha1.KongSNI](
				factory, logging.DevelopmentMode, cl,
				WithMetricRecorder[configurationv1alpha1.KongSNI](&metricsmocks.MockRecorder{}),
			)
			key := client.ObjectKeyFromObject(sni)
			for range 6 {
				if err := cl.Get(t.Context(), key, sni); apierrors.IsNotFound(err) {
					break
				} else {
					require.NoError(t, err)
				}
				_, err := reconciler.Reconcile(t.Context(), sni)
				require.NoError(t, err)
			}
			require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), key, sni)))
		})
	}
}

func TestDeleteGatewaySNIWithMissingAncestor(t *testing.T) {
	for _, tt := range []struct {
		name      string
		wantError bool
	}{
		{name: "missing Gateway"},
		{name: "recreated Gateway"},
		{name: "missing KonnectExtension"},
		{name: "missing ControlPlane"},
		{name: "Gateway lookup failure", wantError: true},
		{name: "KonnectExtension lookup failure", wantError: true},
		{name: "ControlPlane lookup failure", wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cp := testControlPlaneOK.DeepCopy()
			gateway := &gwtypes.Gateway{Name: "gateway", Namespace: cp.Namespace, UID: "gateway-uid"}
			extension := &konnectv1alpha2.KonnectExtension{
				Name: "gateway-extension", Namespace: gateway.Namespace,
				Labels: map[string]string{"gateway-operator.konghq.com/managed-by": "gateway"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway", Name: gateway.Name, UID: gateway.UID,
				}},
				Spec: konnectv1alpha2.KonnectExtensionSpec{Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
					ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: cp.Name},
					}},
				}},
			}
			sni := &configurationv1alpha1.KongSNI{
				Name: "listener-sni", Namespace: gateway.Namespace,
				Finalizers: []string{KonnectCleanupFinalizer, "test.example/other-finalizer"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway", Name: gateway.Name, UID: gateway.UID, Controller: new(true),
				}},
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{Name: "missing-certificate"},
				},
				Status: configurationv1alpha1.KongSNIStatus{Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
					ID: "original-sni-id", CertificateID: "original-certificate-id", ControlPlaneID: cp.Status.ID,
				}},
			}
			objects := []client.Object{sni}
			if tt.name != "missing Gateway" {
				if tt.name == "recreated Gateway" {
					gateway.UID = "replacement-gateway-uid"
				}
				objects = append(objects, gateway)
			}
			if tt.name != "missing KonnectExtension" {
				objects = append(objects, extension)
			}
			if tt.name != "missing ControlPlane" {
				objects = append(objects, cp)
			}
			lookupErr := apierrors.NewServiceUnavailable("temporary Kubernetes API failure")
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
				WithStatusSubresource(sni).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						switch obj.(type) {
						case *gwtypes.Gateway:
							if tt.name == "Gateway lookup failure" {
								return lookupErr
							}
						case *konnectv1alpha2.KonnectGatewayControlPlane:
							if tt.name == "ControlPlane lookup failure" {
								return lookupErr
							}
						}
						return cl.Get(ctx, key, obj, opts...)
					},
					List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						if _, ok := list.(*konnectv1alpha2.KonnectExtensionList); ok && tt.name == "KonnectExtension lookup failure" {
							return lookupErr
						}
						return cl.List(ctx, list, opts...)
					},
				}).Build()
			require.NoError(t, cl.Delete(t.Context(), sni))
			key := client.ObjectKeyFromObject(sni)
			require.NoError(t, cl.Get(t.Context(), key, sni))
			originalStatus := sni.DeepCopy().Status
			reconciler := NewKonnectEntityReconciler[configurationv1alpha1.KongSNI](nil, logging.DevelopmentMode, cl)

			res, err := reconciler.Reconcile(t.Context(), sni)
			require.Equal(t, ctrl.Result{}, res)
			if tt.wantError {
				require.ErrorIs(t, err, lookupErr)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, cl.Get(t.Context(), key, sni))
			if tt.wantError {
				require.Contains(t, sni.Finalizers, KonnectCleanupFinalizer)
			} else {
				require.NotContains(t, sni.Finalizers, KonnectCleanupFinalizer)
			}
			require.Contains(t, sni.Finalizers, "test.example/other-finalizer")
			require.Equal(t, originalStatus, sni.Status)
		})
	}
}

func TestValidateGatewaySNICertificate(t *testing.T) {
	for _, tt := range []struct {
		name          string
		otherCP       bool
		stale         bool
		owned         bool
		missingOwner  bool
		replacedOwner bool
	}{
		{name: "same ControlPlane", owned: true},
		{name: "reject moving a Gateway binding to another ControlPlane", owned: true, otherCP: true},
		{name: "reject stale certificate status", owned: true, stale: true},
		{name: "reject missing Gateway owner", owned: true, missingOwner: true},
		{name: "reject recreated Gateway owner", owned: true, replacedOwner: true},
		{name: "unmanaged SNI retains independent ControlPlane behavior", otherCP: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cp := testControlPlaneOK.DeepCopy()
			cert := testKongCertOK.DeepCopy()
			certCP := cp.DeepCopy()
			certCP.Name = "certificate-cp"
			cert.Spec.ControlPlaneRef.KonnectNamespacedRef.Name = certCP.Name
			if tt.otherCP {
				certCP.Status.ID = "different-cp"
				cert.Status.Konnect.ControlPlaneID = certCP.Status.ID
			}
			if tt.stale {
				cert.Generation++
			}
			gateway := &gwtypes.Gateway{Name: "gateway", Namespace: cp.Namespace, UID: "gateway-uid"}
			extension := &konnectv1alpha2.KonnectExtension{
				Name: "gateway-extension", Namespace: gateway.Namespace,
				Labels: map[string]string{"gateway-operator.konghq.com/managed-by": "gateway"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway", Name: gateway.Name, UID: gateway.UID,
				}},
				Spec: konnectv1alpha2.KonnectExtensionSpec{Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
					ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
						Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: cp.Name},
					}},
				}},
			}
			sni := &configurationv1alpha1.KongSNI{Namespace: gateway.Namespace}
			if tt.owned {
				sni.OwnerReferences = []metav1.OwnerReference{{
					APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway", Name: gateway.Name, UID: gateway.UID, Controller: new(true),
				}}
			}
			objects := []client.Object{cp, certCP, cert, extension}
			if !tt.missingOwner {
				if tt.replacedOwner {
					gateway.UID = "replacement-gateway-uid"
				}
				objects = append(objects, gateway)
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
				WithStatusSubresource(&configurationv1alpha1.KongSNI{}).Build()
			err := validateGatewaySNICertificate(t.Context(), cl, sni, cert)
			if tt.owned && (tt.otherCP || tt.stale || tt.missingOwner || tt.replacedOwner) {
				require.Error(t, err)
				sni.Name = "listener-sni"
				sni.Spec.CertificateRef = commonv1alpha1.NamespacedRef{Name: cert.Name}
				sni.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
					ID: "original-sni-id", ControlPlaneID: cp.Status.ID, CertificateID: "original-certificate-id",
				}
				require.NoError(t, cl.Create(t.Context(), sni))
				res, err := handleKongCertificateRef(t.Context(), cl, sni)
				require.NoError(t, err)
				require.Positive(t, res.RequeueAfter)
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(sni), sni))
				require.Equal(t, "original-sni-id", sni.GetKonnectID(), "retain the original binding for cleanup")
				require.Equal(t, cp.Status.ID, sni.Status.Konnect.ControlPlaneID)
				require.Equal(t, "original-certificate-id", sni.Status.Konnect.CertificateID)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type handleCertRefTestCase[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]] struct {
	name                string
	ent                 TEnt
	objects             []client.Object
	expectResult        ctrl.Result
	expectError         bool
	expectErrorContains string
	// Returns true if the updated entity satisfy the assertion.
	// Returns false and error message if entity fails to satisfy it.
	updatedEntAssertions []func(TEnt) (ok bool, message string)
}

var testKongCertOK = &configurationv1alpha1.KongCertificate{
	Name:      "cert-ok",
	Namespace: "default",
	Spec: configurationv1alpha1.KongCertificateSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-ok",
			},
		},
		KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
			Cert: "===== BEGIN CERTIFICATE",
			Key:  "===== BEGIN PRIVATE KEY",
		},
	},
	Status: configurationv1alpha1.KongCertificateStatus{
		Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
			ID:             "12345",
			ControlPlaneID: "123456789",
		},
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testKongCertNotProgrammed = &configurationv1alpha1.KongCertificate{
	Name:      "cert-not-programmed",
	Namespace: "default",
	Status: configurationv1alpha1.KongCertificateStatus{
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionFalse,
			},
		},
	},
}

var testKongCertNoControlPlaneRef = &configurationv1alpha1.KongCertificate{
	Name:      "cert-no-cp-ref",
	Namespace: "default",
	Spec: configurationv1alpha1.KongCertificateSpec{
		KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
			Cert: "===== BEGIN CERTIFICATE",
			Key:  "===== BEGIN PRIVATE KEY",
		},
	},
	Status: configurationv1alpha1.KongCertificateStatus{
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testKongCertOKInOtherNS = &configurationv1alpha1.KongCertificate{
	Name:      "cert-xns-ok",
	Namespace: "other-namespace",
	Spec: configurationv1alpha1.KongCertificateSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-other",
			},
		},
		KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
			Cert: "===== BEGIN CERTIFICATE",
			Key:  "===== BEGIN PRIVATE KEY",
		},
	},
	Status: configurationv1alpha1.KongCertificateStatus{
		Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
			ID:             "99999",
			ControlPlaneID: "987654321",
		},
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testControlPlaneOKInOtherNS = &konnectv1alpha2.KonnectGatewayControlPlane{
	Name:      "cp-other",
	Namespace: "other-namespace",
	Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
		KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
			ID: "987654321",
		},
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testKongCertBeingDeleted = &configurationv1alpha1.KongCertificate{
	Name:              "cert-being-deleted",
	Namespace:         "default",
	DeletionTimestamp: &metav1.Time{Time: time.Now()},
	Finalizers:        []string{"sni-0"},
}

var testKongCertificateControlPlaneRefNotFound = &configurationv1alpha1.KongCertificate{
	Name:      "cert-cpref-not-found",
	Namespace: "default",
	Spec: configurationv1alpha1.KongCertificateSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-not-found",
			},
		},
	},
	Status: configurationv1alpha1.KongCertificateStatus{
		Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
			ID:             "12345",
			ControlPlaneID: "123456789",
		},
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

var testKongCertControlPlaneRefNotProgrammed = &configurationv1alpha1.KongCertificate{
	Name:      "cert-cpref-not-programmed",
	Namespace: "default",
	Spec: configurationv1alpha1.KongCertificateSpec{
		ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
			Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
				Name: "cp-not-programmed",
			},
		},
	},
	Status: configurationv1alpha1.KongCertificateStatus{
		Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
			ID:             "12345",
			ControlPlaneID: "123456789",
		},
		Conditions: []metav1.Condition{
			{
				Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
				Status: metav1.ConditionTrue,
			},
		},
	},
}

func TestHandleCertificateRef(t *testing.T) {
	testCases := []handleCertRefTestCase[configurationv1alpha1.KongSNI, *configurationv1alpha1.KongSNI]{
		{
			name: "has certificate ref and control plane ref",
			ent: &configurationv1alpha1.KongSNI{
				Name:      "sni-ok",
				Namespace: "default",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name: "cert-ok",
					},
				},
			},
			objects: []client.Object{
				testKongCertOK,
				testControlPlaneOK,
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongSNI) (bool, string){
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongSNI does not have KongCertificateRefValid condition set to True"
				},
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongSNI does not have ControlPlaneRefValid condition set to True"
				},
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return !lo.ContainsBy(ks.OwnerReferences, func(o metav1.OwnerReference) bool {
						return o.Kind == "KongCertificate" && o.Name == "cert-ok"
					}), "OwnerReference of KongSNI is set but shouldn't"
				},
			},
		},
		{
			name: "certificate ref not found",
			ent: &configurationv1alpha1.KongSNI{
				Name:      "cert-ref-not-found",
				Namespace: "default",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name: "cert-nonexist",
					},
				},
			},
			expectError:         true,
			expectErrorContains: "referenced Kong Certificate default/cert-nonexist does not exist",
			updatedEntAssertions: []func(*configurationv1alpha1.KongSNI) (bool, string){
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongSNI does not have KongCertificateRefValid condition set to False"
				},
			},
		},
		{
			name: "referenced KongCertificate not programmed",
			ent: &configurationv1alpha1.KongSNI{
				Name:      "sni-cert-ref-not-programmed",
				Namespace: "default",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name: "cert-not-programmed",
					},
				},
			},
			objects: []client.Object{
				testKongCertNotProgrammed,
			},
			expectError:  false,
			expectResult: ctrl.Result{},
			updatedEntAssertions: []func(*configurationv1alpha1.KongSNI) (bool, string){
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.GetConditions(), func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionFalse &&
							c.Message == fmt.Sprintf("Referenced KongCertificate %s/%s is not programmed yet",
								testKongCertNotProgrammed.Namespace, testKongCertNotProgrammed.Name)
					}), "KongSNI does not have KongCertificateRefValid condition set to False"
				},
			},
		},
		{
			name: "referenced KongCertificate has no ControlPlaneRef",
			ent: &configurationv1alpha1.KongSNI{
				Name:      "sni-cert-no-cpref",
				Namespace: "default",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name: "cert-no-cp-ref",
					},
				},
			},
			objects: []client.Object{
				testKongCertNoControlPlaneRef,
			},
			expectError: true,
			expectErrorContains: fmt.Sprintf("references a KongCertificate %s/%s which does not have a ControlPlane ref",
				testKongCertNoControlPlaneRef.Namespace, testKongCertNoControlPlaneRef.Name),
			updatedEntAssertions: []func(*configurationv1alpha1.KongSNI) (bool, string){
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongSNI does not have KongCertificateRefValid condition set to True"
				},
			},
		},
		{
			name: "referenced KongCertificate is being deleted",
			ent: &configurationv1alpha1.KongSNI{
				Name:      "sni-cert-being-deleted",
				Namespace: "default",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name: "cert-being-deleted",
					},
				},
			},
			objects: []client.Object{
				testKongCertBeingDeleted,
			},
			expectError:         true,
			expectErrorContains: fmt.Sprintf("referenced Kong Certificate %s/%s is being deleted", testKongCertBeingDeleted.Namespace, testKongCertBeingDeleted.Name),
		},
		{
			name: "ControlPlaneRef not found",
			ent: &configurationv1alpha1.KongSNI{
				Namespace: "default",
				Name:      "sni-cp-ref-not-found",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name: "cert-cpref-not-found",
					},
				},
			},
			objects: []client.Object{
				testKongCertificateControlPlaneRefNotFound,
			},
			expectError: true,
			expectErrorContains: fmt.Sprintf("referenced Control Plane %q does not exist",
				testKongCertificateControlPlaneRefNotFound.Spec.ControlPlaneRef.String(),
			),
		},
		{
			name: "cross-namespace certificate ref with no KongReferenceGrant",
			ent: &configurationv1alpha1.KongSNI{
				APIVersion: "configuration.konghq.com/v1alpha1",
				Kind:       "KongSNI",
				Name:       "sni-xns-no-grant",
				Namespace:  "default",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name:      "cert-xns-ok",
						Namespace: new("other-namespace"),
					},
				},
			},
			objects:      []client.Object{testKongCertOKInOtherNS},
			expectError:  false,
			expectResult: ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff},
			updatedEntAssertions: []func(*configurationv1alpha1.KongSNI) (bool, string){
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
							c.Status == metav1.ConditionFalse &&
							c.Reason == configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted
					}), "KongSNI does not have ResolvedRefs=False/RefNotPermitted condition"
				},
			},
		},
		{
			name: "cross-namespace certificate ref with KongReferenceGrant",
			ent: &configurationv1alpha1.KongSNI{
				APIVersion: "configuration.konghq.com/v1alpha1",
				Kind:       "KongSNI",
				Name:       "sni-xns-with-grant",
				Namespace:  "default",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name:      "cert-xns-ok",
						Namespace: new("other-namespace"),
					},
				},
			},
			objects: []client.Object{
				testKongCertOKInOtherNS,
				testControlPlaneOKInOtherNS,
				&configurationv1alpha1.KongReferenceGrant{
					Name:      "sni-to-cert",
					Namespace: "other-namespace",
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongSNI",
								Namespace: "default",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongCertificate",
							},
						},
					},
				},
			},
			expectError:  false,
			expectResult: ctrl.Result{},
			updatedEntAssertions: []func(*configurationv1alpha1.KongSNI) (bool, string){
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
							c.Status == metav1.ConditionTrue &&
							c.Reason == configurationv1alpha1.KongReferenceGrantReasonResolvedRefs
					}), "KongSNI does not have ResolvedRefs=True/ResolvedRefs condition"
				},
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongSNI does not have KongCertificateRefValid=True condition"
				},
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongSNI does not have ControlPlaneRefValid=True condition"
				},
			},
		},
		{
			name: "ControlPlaneRef not programmed",
			ent: &configurationv1alpha1.KongSNI{
				Namespace: "default",
				Name:      "sni-cp-ref-not-programmed",
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NamespacedRef{
						Name: "cert-cpref-not-programmed",
					},
				},
			},
			objects: []client.Object{
				testKongCertControlPlaneRefNotProgrammed,
				testControlPlaneNotProgrammed,
			},
			expectError:  false,
			expectResult: ctrl.Result{Requeue: true},
			updatedEntAssertions: []func(*configurationv1alpha1.KongSNI) (bool, string){
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongSNI does not have KongCertificateRefValid condition set to True"
				},
				func(ks *configurationv1alpha1.KongSNI) (bool, string) {
					return lo.ContainsBy(ks.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongSNI does not have ControlPlaneRefValid condition set to False"
				},
			},
		},
	}

	testHandleCertificateRef(t, testCases)
}

func testHandleCertificateRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	t *testing.T, testCases []handleCertRefTestCase[T, TEnt],
) {
	t.Helper()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, configurationv1alpha1.AddToScheme(scheme))
			require.NoError(t, konnectv1alpha1.AddToScheme(scheme))
			require.NoError(t, konnectv1alpha2.AddToScheme(scheme))
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(tc.ent).WithObjects(tc.objects...).
				// WithStatusSubresource is required for updating status of handled entity.
				WithStatusSubresource(tc.ent).Build()
			// Save GVK before the status update: the fake client clears TypeMeta
			// when writing back the result, breaking GetObjectKind().GroupVersionKind()
			// which is needed for cross-namespace reference grant checks.
			savedGVK := tc.ent.GetObjectKind().GroupVersionKind()
			require.NoError(t, fakeClient.SubResource("status").Update(t.Context(), tc.ent))
			tc.ent.GetObjectKind().SetGroupVersionKind(savedGVK)

			res, err := handleKongCertificateRef(t.Context(), fakeClient, tc.ent)

			updatedEnt := tc.ent.DeepCopyObject().(TEnt)
			require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(tc.ent), updatedEnt))
			for _, assertion := range tc.updatedEntAssertions {
				ok, msg := assertion(updatedEnt)
				require.True(t, ok, msg)
			}

			if tc.expectError {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErrorContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectResult, res)
		})
	}
}

func TestHandleCertificateRefKongService(t *testing.T) {
	testCases := []handleCertRefTestCase[configurationv1alpha1.KongService, *configurationv1alpha1.KongService]{
		{
			name: "same-NS cert ref, cert found",
			ent: &configurationv1alpha1.KongService{
				Name: "svc", Namespace: "default",
				Spec: configurationv1alpha1.KongServiceSpec{
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-ok"},
						Host:                 "example.com",
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			objects: []client.Object{
				testKongCertOK,
				testControlPlaneOK,
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongService) (bool, string){
				func(svc *configurationv1alpha1.KongService) (bool, string) {
					return lo.ContainsBy(svc.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongService does not have KongCertificateRefValid condition set to True"
				},
			},
		},
		{
			name: "cert not found",
			ent: &configurationv1alpha1.KongService{
				Name: "svc", Namespace: "default",
				Spec: configurationv1alpha1.KongServiceSpec{
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-nonexist"},
						Host:                 "example.com",
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			expectError:         true,
			expectErrorContains: "does not exist",
			updatedEntAssertions: []func(*configurationv1alpha1.KongService) (bool, string){
				func(svc *configurationv1alpha1.KongService) (bool, string) {
					return lo.ContainsBy(svc.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongService does not have KongCertificateRefValid condition set to False"
				},
			},
		},
	}

	testHandleCertificateRef(t, testCases)
}

func TestHandleCertificateRefKongServiceCrossNS(t *testing.T) {
	s := scheme.Get()

	certInOtherNS := &configurationv1alpha1.KongCertificate{
		Name:      "cert-ok",
		Namespace: "other-ns",
		Spec: configurationv1alpha1.KongCertificateSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: "cp-ok",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "===== BEGIN CERTIFICATE",
				Key:  "===== BEGIN PRIVATE KEY",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ID:             "cross-ns-cert-id",
				ControlPlaneID: "123456789",
			},
			Conditions: []metav1.Condition{
				{
					Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	svcEntity := func() *configurationv1alpha1.KongService {
		return &configurationv1alpha1.KongService{
			Name: "svc", Namespace: "default",
			Spec: configurationv1alpha1.KongServiceSpec{
				KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
					ClientCertificateRef: &commonv1alpha1.NamespacedRef{
						Name:      "cert-ok",
						Namespace: new("other-ns"),
					},
					Host: "example.com",
				},
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
				},
			},
		}
	}

	grant := &configurationv1alpha1.KongReferenceGrant{
		Name:      "svc-to-cert",
		Namespace: "other-ns",
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{
				{
					Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
					Kind:      "KongService",
					Namespace: configurationv1alpha1.Namespace("default"),
				},
			},
			To: []configurationv1alpha1.ReferenceGrantTo{
				{
					Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
					Kind:  "KongCertificate",
				},
			},
		},
	}

	// certInOtherNS references a CP named "cp-ok" with no namespace set, so GetCPForRef
	// resolves the namespace from cert.GetNamespace() = "other-ns".
	cpOKInOtherNS := testControlPlaneOK.DeepCopy()
	cpOKInOtherNS.Namespace = "other-ns"

	t.Run("cross-NS cert ref without KongReferenceGrant", func(t *testing.T) {
		ent := svcEntity()
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(ent, certInOtherNS).
			WithStatusSubresource(ent).
			WithInterceptorFuncs(populateGVKOnGet(s)).
			Build()
		// Re-fetch ent through the interceptor so its GVK is populated (required for cross-NS checks).
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), ent))

		res, err := handleKongCertificateRef(t.Context(), cl, ent)
		// When no KongReferenceGrant covers the cross-namespace ref, the handler patches
		// ResolvedRefs=False/RefNotPermitted and returns a non-error requeue (no error propagated).
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, res)

		updatedSvc := &configurationv1alpha1.KongService{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), updatedSvc))
		require.True(t, lo.ContainsBy(updatedSvc.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
				c.Status == metav1.ConditionFalse &&
				c.Reason == configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted
		}), "KongService does not have ResolvedRefs=False/RefNotPermitted condition")
	})

	t.Run("cross-NS cert ref with valid KongReferenceGrant", func(t *testing.T) {
		ent := svcEntity()
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(ent, certInOtherNS, grant, cpOKInOtherNS).
			WithStatusSubresource(ent).
			WithInterceptorFuncs(populateGVKOnGet(s)).
			Build()
		// Re-fetch ent through the interceptor so its GVK is populated (required for cross-NS checks).
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), ent))

		res, err := handleKongCertificateRef(t.Context(), cl, ent)
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		updatedSvc := &configurationv1alpha1.KongService{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), updatedSvc))
		require.True(t, lo.ContainsBy(updatedSvc.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
		}), "KongService does not have KongCertificateRefValid condition set to True")
	})

}

func TestHandleCertificateRefKongUpstream(t *testing.T) {
	testCases := []handleCertRefTestCase[configurationv1alpha1.KongUpstream, *configurationv1alpha1.KongUpstream]{
		{
			name: "same-NS cert ref, cert found",
			ent: &configurationv1alpha1.KongUpstream{
				Name: "upstream", Namespace: "default",
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-ok"},
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			objects: []client.Object{
				testKongCertOK,
				testControlPlaneOK,
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongUpstream) (bool, string){
				func(u *configurationv1alpha1.KongUpstream) (bool, string) {
					return lo.ContainsBy(u.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongUpstream does not have KongCertificateRefValid condition set to True"
				},
			},
		},
		{
			name: "cert not found",
			ent: &configurationv1alpha1.KongUpstream{
				Name: "upstream", Namespace: "default",
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-nonexist"},
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			expectError:         true,
			expectErrorContains: "referenced Kong Certificate default/cert-nonexist does not exist",
			updatedEntAssertions: []func(*configurationv1alpha1.KongUpstream) (bool, string){
				func(u *configurationv1alpha1.KongUpstream) (bool, string) {
					return lo.ContainsBy(u.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongUpstream does not have KongCertificateRefValid condition set to False"
				},
			},
		},
		{
			name: "cert ref nil — no conditions set",
			ent: &configurationv1alpha1.KongUpstream{
				Name: "upstream", Namespace: "default",
				Spec: configurationv1alpha1.KongUpstreamSpec{
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongUpstream) (bool, string){
				func(u *configurationv1alpha1.KongUpstream) (bool, string) {
					return !lo.ContainsBy(u.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType
					}), "KongUpstream should not have KongCertificateRefValid condition when ref is nil"
				},
			},
		},
		{
			name: "referenced KongCertificate not programmed",
			ent: &configurationv1alpha1.KongUpstream{
				Name: "upstream", Namespace: "default",
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-not-programmed"},
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			objects:      []client.Object{testKongCertNotProgrammed},
			expectResult: ctrl.Result{},
			expectError:  false,
			updatedEntAssertions: []func(*configurationv1alpha1.KongUpstream) (bool, string){
				func(u *configurationv1alpha1.KongUpstream) (bool, string) {
					return lo.ContainsBy(u.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongUpstream does not have KongCertificateRefValid condition set to False"
				},
			},
		},
		{
			name: "referenced KongCertificate is being deleted",
			ent: &configurationv1alpha1.KongUpstream{
				Name: "upstream", Namespace: "default",
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-being-deleted"},
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			objects:             []client.Object{testKongCertBeingDeleted},
			expectError:         true,
			expectErrorContains: fmt.Sprintf("referenced Kong Certificate %s/%s is being deleted", testKongCertBeingDeleted.Namespace, testKongCertBeingDeleted.Name),
			updatedEntAssertions: []func(*configurationv1alpha1.KongUpstream) (bool, string){
				func(u *configurationv1alpha1.KongUpstream) (bool, string) {
					return lo.ContainsBy(u.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongUpstream does not have KongCertificateRefValid condition set to False"
				},
			},
		},
		{
			name: "referenced KongCertificate has no ControlPlaneRef",
			ent: &configurationv1alpha1.KongUpstream{
				Name: "upstream", Namespace: "default",
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-no-cp-ref"},
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			objects:     []client.Object{testKongCertNoControlPlaneRef},
			expectError: true,
			expectErrorContains: fmt.Sprintf("references a KongCertificate %s/%s which does not have a ControlPlane ref",
				testKongCertNoControlPlaneRef.Namespace, testKongCertNoControlPlaneRef.Name),
			updatedEntAssertions: []func(*configurationv1alpha1.KongUpstream) (bool, string){
				func(u *configurationv1alpha1.KongUpstream) (bool, string) {
					return lo.ContainsBy(u.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongUpstream does not have KongCertificateRefValid condition set to True"
				},
			},
		},
		{
			name: "ControlPlaneRef not found",
			ent: &configurationv1alpha1.KongUpstream{
				Namespace: "default", Name: "upstream-cp-not-found",
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-cpref-not-found"},
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			objects:     []client.Object{testKongCertificateControlPlaneRefNotFound},
			expectError: true,
			expectErrorContains: fmt.Sprintf("referenced Control Plane %q does not exist",
				testKongCertificateControlPlaneRefNotFound.Spec.ControlPlaneRef.String()),
		},
		{
			name: "ControlPlaneRef not programmed",
			ent: &configurationv1alpha1.KongUpstream{
				Namespace: "default", Name: "upstream-cp-not-programmed",
				Spec: configurationv1alpha1.KongUpstreamSpec{
					KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
						ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: "cert-cpref-not-programmed"},
					},
					ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
						Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
					},
				},
			},
			objects:      []client.Object{testKongCertControlPlaneRefNotProgrammed, testControlPlaneNotProgrammed},
			expectError:  false,
			expectResult: ctrl.Result{Requeue: true},
			updatedEntAssertions: []func(*configurationv1alpha1.KongUpstream) (bool, string){
				func(u *configurationv1alpha1.KongUpstream) (bool, string) {
					return lo.ContainsBy(u.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
					}), "KongUpstream does not have KongCertificateRefValid condition set to True"
				},
				func(u *configurationv1alpha1.KongUpstream) (bool, string) {
					return lo.ContainsBy(u.Status.Conditions, func(c metav1.Condition) bool {
						return c.Type == konnectv1alpha1.ControlPlaneRefValidConditionType && c.Status == metav1.ConditionFalse
					}), "KongUpstream does not have ControlPlaneRefValid condition set to False"
				},
			},
		},
	}
	testHandleCertificateRef(t, testCases)
}

func TestHandleCertificateRefKongUpstreamCrossNS(t *testing.T) {
	s := scheme.Get()

	certInOtherNS := &configurationv1alpha1.KongCertificate{
		Name:      "cert-ok",
		Namespace: "other-ns",
		Spec: configurationv1alpha1.KongCertificateSpec{
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: "cp-ok",
				},
			},
			KongCertificateAPISpec: configurationv1alpha1.KongCertificateAPISpec{
				Cert: "===== BEGIN CERTIFICATE",
				Key:  "===== BEGIN PRIVATE KEY",
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ID:             "cross-ns-cert-id",
				ControlPlaneID: "123456789",
			},
			Conditions: []metav1.Condition{
				{
					Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	upstreamEntity := func() *configurationv1alpha1.KongUpstream {
		return &configurationv1alpha1.KongUpstream{
			Name: "upstream", Namespace: "default",
			Spec: configurationv1alpha1.KongUpstreamSpec{
				KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
					ClientCertificateRef: &commonv1alpha1.NamespacedRef{
						Name:      "cert-ok",
						Namespace: new("other-ns"),
					},
				},
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
				},
			},
		}
	}

	grant := &configurationv1alpha1.KongReferenceGrant{
		Name:      "upstream-to-cert",
		Namespace: "other-ns",
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{
				{
					Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
					Kind:      "KongUpstream",
					Namespace: configurationv1alpha1.Namespace("default"),
				},
			},
			To: []configurationv1alpha1.ReferenceGrantTo{
				{
					Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
					Kind:  "KongCertificate",
				},
			},
		},
	}

	cpOKInOtherNS := testControlPlaneOK.DeepCopy()
	cpOKInOtherNS.Namespace = "other-ns"

	t.Run("cross-NS cert ref without KongReferenceGrant", func(t *testing.T) {
		ent := upstreamEntity()
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(ent, certInOtherNS).
			WithStatusSubresource(ent).
			WithInterceptorFuncs(populateGVKOnGet(s)).
			Build()
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), ent))

		res, err := handleKongCertificateRef(t.Context(), cl, ent)
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, res)

		updatedUpstream := &configurationv1alpha1.KongUpstream{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), updatedUpstream))
		require.True(t, lo.ContainsBy(updatedUpstream.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
				c.Status == metav1.ConditionFalse &&
				c.Reason == configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted
		}), "KongUpstream does not have ResolvedRefs=False/RefNotPermitted condition")
	})

	t.Run("cross-NS cert ref with valid KongReferenceGrant", func(t *testing.T) {
		ent := upstreamEntity()
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(ent, certInOtherNS, grant, cpOKInOtherNS).
			WithStatusSubresource(ent).
			WithInterceptorFuncs(populateGVKOnGet(s)).
			Build()
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), ent))

		res, err := handleKongCertificateRef(t.Context(), cl, ent)
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		updatedUpstream := &configurationv1alpha1.KongUpstream{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(ent), updatedUpstream))
		require.True(t, lo.ContainsBy(updatedUpstream.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KongCertificateRefValidConditionType && c.Status == metav1.ConditionTrue
		}), "KongUpstream does not have KongCertificateRefValid condition set to True")
		require.True(t, lo.ContainsBy(updatedUpstream.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
				c.Status == metav1.ConditionTrue &&
				c.Reason == configurationv1alpha1.KongReferenceGrantReasonResolvedRefs
		}), "KongUpstream does not have ResolvedRefs=True condition")

		// CertificateID is set in-memory on ent after the final patch; it is not re-fetched.
		require.NotNil(t, ent.Status.Konnect)
		require.Equal(t, "cross-ns-cert-id", ent.Status.Konnect.CertificateID)
	})
}
