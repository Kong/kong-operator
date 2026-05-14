package konnect

import (
	"fmt"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

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
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cert-ok",
		Namespace: "default",
	},
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
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: "12345",
			},
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
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cert-not-programmed",
		Namespace: "default",
	},
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
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cert-no-cp-ref",
		Namespace: "default",
	},
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
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cert-xns-ok",
		Namespace: "other-namespace",
	},
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
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: "99999",
			},
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
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cp-other",
		Namespace: "other-namespace",
	},
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
	ObjectMeta: metav1.ObjectMeta{
		Name:              "cert-being-deleted",
		Namespace:         "default",
		DeletionTimestamp: &metav1.Time{Time: time.Now()},
		Finalizers:        []string{"sni-0"},
	},
}

var testKongCertificateControlPlaneRefNotFound = &configurationv1alpha1.KongCertificate{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cert-cpref-not-found",
		Namespace: "default",
	},
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
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: "12345",
			},
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
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cert-cpref-not-programmed",
		Namespace: "default",
	},
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
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: "12345",
			},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sni-ok",
					Namespace: "default",
				},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cert-ref-not-found",
					Namespace: "default",
				},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sni-cert-ref-not-programmed",
					Namespace: "default",
				},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sni-cert-no-cpref",
					Namespace: "default",
				},
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
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sni-cert-being-deleted",
					Namespace: "default",
				},
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
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "sni-cp-ref-not-found",
				},
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
				TypeMeta: metav1.TypeMeta{
					APIVersion: "configuration.konghq.com/v1alpha1",
					Kind:       "KongSNI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sni-xns-no-grant",
					Namespace: "default",
				},
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
				TypeMeta: metav1.TypeMeta{
					APIVersion: "configuration.konghq.com/v1alpha1",
					Kind:       "KongSNI",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sni-xns-with-grant",
					Namespace: "default",
				},
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
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sni-to-cert",
						Namespace: "other-namespace",
					},
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
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "sni-cp-ref-not-programmed",
				},
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
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
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
				ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cert-ok",
			Namespace: "other-ns",
		},
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
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
					ID: "cross-ns-cert-id",
				},
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
			ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"},
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-to-cert",
			Namespace: "other-ns",
		},
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
