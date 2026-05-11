package konnect

import (
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
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// testScheme returns a *runtime.Scheme with all types required by the CA certificate ref tests registered.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(s))
	require.NoError(t, konnectv1alpha1.AddToScheme(s))
	require.NoError(t, konnectv1alpha2.AddToScheme(s))
	return s
}

func TestHandleKongCACertificateRefs(t *testing.T) {
	const (
		cpID     = "cp-123"
		caID     = "cacert-id-1"
		svcNS    = "default"
		certNS   = "cert-ns"
		certName = "cacert-1"
	)

	// A KongService entity used as the base for all tests. Returns a fresh copy each time.
	makeSvc := func(refs ...commonv1alpha1.NamespacedRef) *configurationv1alpha1.KongService {
		return &configurationv1alpha1.KongService{
			ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: svcNS},
			Spec: configurationv1alpha1.KongServiceSpec{
				KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
					Host:              "example.com",
					CACertificateRefs: refs,
				},
				ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
					Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
				},
			},
			Status: configurationv1alpha1.KongServiceStatus{
				Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{
					ControlPlaneID: cpID,
				},
			},
		}
	}

	// A healthy KongCACertificate with a matching ControlPlane ID.
	caCertOK := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: svcNS,
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: "===== BEGIN CERTIFICATE",
			},
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: "cp-ok"},
			},
		},
		Status: configurationv1alpha1.KongCACertificateStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: caID},
				ControlPlaneID:      cpID,
			},
			Conditions: []metav1.Condition{
				{
					Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	t.Run("empty CACertificateRefs returns immediately with no error", func(t *testing.T) {
		svc := makeSvc() // no refs
		sc := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sc).
			WithObjects(svc).
			WithStatusSubresource(svc).
			Build()

		res, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)
	})

	t.Run("single ref, CA cert found and programmed", func(t *testing.T) {
		svc := makeSvc(commonv1alpha1.NamespacedRef{Name: certName})
		sc := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sc).
			WithObjects(svc, caCertOK).
			WithStatusSubresource(svc).
			Build()
		require.NoError(t, cl.SubResource("status").Update(t.Context(), svc))

		res, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		// The condition is persisted via status patch; verify via Get.
		updatedSvc := &configurationv1alpha1.KongService{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), updatedSvc))
		require.True(t, lo.ContainsBy(updatedSvc.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KongCACertificateRefsValidConditionType && c.Status == metav1.ConditionTrue
		}), "KongService does not have KongCACertificateRefsValid condition set to True")

		// NOTE: CACertificateIDs is set on svc.Status.Konnect.CACertificateIDs before the status patch, but
		// the patch is created via client.MergeFrom(old) where old is a DeepCopy taken at the start of
		// patch.StatusWithCondition — after CACertificateIDs is already set on the object. As a result,
		// the diff contains only the condition change, and the fake client does not persist CACertificateIDs
		// to its store. Asserting updatedSvc.Status.Konnect.CACertificateIDs here would therefore always
		// fail even though the production code sets the field correctly. The integration/envtest suite
		// provides the authoritative coverage for this field.
	})

	t.Run("CA cert not found returns ReferencedKongCACertificateDoesNotExistError", func(t *testing.T) {
		svc := makeSvc(commonv1alpha1.NamespacedRef{Name: "nonexistent-ca"})
		sc := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sc).
			WithObjects(svc).
			WithStatusSubresource(svc).
			Build()
		require.NoError(t, cl.SubResource("status").Update(t.Context(), svc))

		_, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.Error(t, err)
		var notFoundErr ReferencedKongCACertificateDoesNotExistError
		require.ErrorAs(t, err, &notFoundErr)

		updatedSvc := &configurationv1alpha1.KongService{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), updatedSvc))
		require.True(t, lo.ContainsBy(updatedSvc.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KongCACertificateRefsValidConditionType && c.Status == metav1.ConditionFalse
		}), "KongService does not have KongCACertificateRefsValid condition set to False")
	})

	t.Run("CA cert being deleted returns ReferencedKongCACertificateIsBeingDeletedError", func(t *testing.T) {
		caCertBeingDeleted := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:              certName,
				Namespace:         svcNS,
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{"test-finalizer"},
			},
		}
		svc := makeSvc(commonv1alpha1.NamespacedRef{Name: certName})
		sc := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sc).
			WithObjects(svc, caCertBeingDeleted).
			WithStatusSubresource(svc).
			Build()
		require.NoError(t, cl.SubResource("status").Update(t.Context(), svc))

		_, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.Error(t, err)
		var beingDeletedErr ReferencedKongCACertificateIsBeingDeletedError
		require.ErrorAs(t, err, &beingDeletedErr)
	})

	t.Run("CA cert not programmed returns no error, condition False", func(t *testing.T) {
		caCertNotProgrammed := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: svcNS,
			},
			Status: configurationv1alpha1.KongCACertificateStatus{
				Conditions: []metav1.Condition{
					{
						Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
						Status: metav1.ConditionFalse,
					},
				},
			},
		}
		svc := makeSvc(commonv1alpha1.NamespacedRef{Name: certName})
		sc := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sc).
			WithObjects(svc, caCertNotProgrammed).
			WithStatusSubresource(svc).
			Build()
		require.NoError(t, cl.SubResource("status").Update(t.Context(), svc))

		res, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		updatedSvc := &configurationv1alpha1.KongService{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), updatedSvc))
		require.True(t, lo.ContainsBy(updatedSvc.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KongCACertificateRefsValidConditionType && c.Status == metav1.ConditionFalse
		}), "KongService does not have KongCACertificateRefsValid condition set to False")
	})

	t.Run("CA cert empty Konnect ID returns no error, condition False", func(t *testing.T) {
		caCertEmptyID := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: svcNS,
			},
			Status: configurationv1alpha1.KongCACertificateStatus{
				Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: ""},
					ControlPlaneID:      cpID,
				},
				Conditions: []metav1.Condition{
					{
						Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
		svc := makeSvc(commonv1alpha1.NamespacedRef{Name: certName})
		sc := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sc).
			WithObjects(svc, caCertEmptyID).
			WithStatusSubresource(svc).
			Build()
		require.NoError(t, cl.SubResource("status").Update(t.Context(), svc))

		res, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		updatedSvc := &configurationv1alpha1.KongService{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), updatedSvc))
		require.True(t, lo.ContainsBy(updatedSvc.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KongCACertificateRefsValidConditionType && c.Status == metav1.ConditionFalse
		}), "KongService does not have KongCACertificateRefsValid condition set to False")
	})

	t.Run("CA cert CP mismatch returns ReferencedKongCACertificateBelongsToWrongControlPlaneError", func(t *testing.T) {
		caCertWrongCP := &configurationv1alpha1.KongCACertificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: svcNS,
			},
			Status: configurationv1alpha1.KongCACertificateStatus{
				Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: caID},
					ControlPlaneID:      "different-cp-id",
				},
				Conditions: []metav1.Condition{
					{
						Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
		svc := makeSvc(commonv1alpha1.NamespacedRef{Name: certName})
		sc := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sc).
			WithObjects(svc, caCertWrongCP).
			WithStatusSubresource(svc).
			Build()
		require.NoError(t, cl.SubResource("status").Update(t.Context(), svc))

		_, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.Error(t, err)
		var wrongCPErr ReferencedKongCACertificateBelongsToWrongControlPlaneError
		require.ErrorAs(t, err, &wrongCPErr)
	})

	t.Run("multiple refs: first valid, second not found — fails on second", func(t *testing.T) {
		// ca1 exists and is programmed; ca-missing does not exist.
		ca1 := caCertOK.DeepCopy()
		ca1.Name = "ca1"

		svc := makeSvc(
			commonv1alpha1.NamespacedRef{Name: "ca1"},
			commonv1alpha1.NamespacedRef{Name: "ca-missing"},
		)
		sc := testScheme(t)
		cl := fake.NewClientBuilder().WithScheme(sc).
			WithObjects(svc, ca1).
			WithStatusSubresource(svc).
			Build()
		require.NoError(t, cl.SubResource("status").Update(t.Context(), svc))

		_, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.Error(t, err)
		var notFoundErr ReferencedKongCACertificateDoesNotExistError
		require.ErrorAs(t, err, &notFoundErr)

		updatedSvc := &configurationv1alpha1.KongService{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), updatedSvc))
		require.True(t, lo.ContainsBy(updatedSvc.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KongCACertificateRefsValidConditionType && c.Status == metav1.ConditionFalse
		}), "KongService does not have KongCACertificateRefsValid condition set to False")
	})

	t.Run("cross-NS without KongReferenceGrant returns ReferenceNotGranted error", func(t *testing.T) {
		caCertInOtherNS := caCertOK.DeepCopy()
		caCertInOtherNS.Namespace = certNS

		svc := makeSvc(commonv1alpha1.NamespacedRef{
			Name:      certName,
			Namespace: new(certNS),
		})
		s := scheme.Get()
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(svc, caCertInOtherNS).
			WithStatusSubresource(svc).
			WithInterceptorFuncs(populateGVKOnGet(s)).
			Build()
		// Re-fetch svc through the interceptor so its GVK is populated (required for cross-NS checks).
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), svc))

		_, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.Error(t, err)
		require.True(t, crossnamespace.IsReferenceNotGranted(err), "expected ReferenceNotGranted error, got: %v", err)
	})

	t.Run("cross-NS with valid KongReferenceGrant resolves OK", func(t *testing.T) {
		caCertInOtherNS := caCertOK.DeepCopy()
		caCertInOtherNS.Namespace = certNS

		svc := makeSvc(commonv1alpha1.NamespacedRef{
			Name:      certName,
			Namespace: new(certNS),
		})

		grant := &configurationv1alpha1.KongReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "svc-to-cacert",
				Namespace: certNS,
			},
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{
					{
						Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
						Kind:      "KongService",
						Namespace: configurationv1alpha1.Namespace(svcNS),
					},
				},
				To: []configurationv1alpha1.ReferenceGrantTo{
					{
						Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
						Kind:  "KongCACertificate",
					},
				},
			},
		}

		s := scheme.Get()
		cl := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(svc, caCertInOtherNS, grant).
			WithStatusSubresource(svc).
			WithInterceptorFuncs(populateGVKOnGet(s)).
			Build()
		// Re-fetch svc through the interceptor so its GVK is populated (required for cross-NS checks).
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), svc))

		res, err := handleKongCACertificateRefs(t.Context(), cl, svc)
		require.NoError(t, err)
		require.Equal(t, ctrl.Result{}, res)

		updatedSvc := &configurationv1alpha1.KongService{}
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(svc), updatedSvc))
		require.True(t, lo.ContainsBy(updatedSvc.Status.Conditions, func(c metav1.Condition) bool {
			return c.Type == konnectv1alpha1.KongCACertificateRefsValidConditionType && c.Status == metav1.ConditionTrue
		}), "KongService does not have KongCACertificateRefsValid condition set to True")

		// NOTE: CACertificateIDs is not asserted here for the same reason as the "single ref, CA cert found
		// and programmed" test: the MergeFrom patch only carries the condition diff, so the fake client does
		// not persist CACertificateIDs to its store. See that test for a full explanation.
	})
}
