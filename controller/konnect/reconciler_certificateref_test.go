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

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/controller/konnect/constraints"
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
		Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
			KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
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
		Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
			KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
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
		Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
			KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
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
					CertificateRef: commonv1alpha1.NameRef{
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
					CertificateRef: commonv1alpha1.NameRef{
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
					CertificateRef: commonv1alpha1.NameRef{
						Name: "cert-not-programmed",
					},
				},
			},
			objects: []client.Object{
				testKongCertNotProgrammed,
			},
			expectError:  false,
			expectResult: ctrl.Result{Requeue: true},
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
					CertificateRef: commonv1alpha1.NameRef{
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
					CertificateRef: commonv1alpha1.NameRef{
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
					CertificateRef: commonv1alpha1.NameRef{
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
			name: "ControlPlaneRef not programmed",
			ent: &configurationv1alpha1.KongSNI{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "sni-cp-ref-not-programmed",
				},
				Spec: configurationv1alpha1.KongSNISpec{
					CertificateRef: commonv1alpha1.NameRef{
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
			require.NoError(t, fakeClient.SubResource("status").Update(t.Context(), tc.ent))

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
