package license

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func TestCompareLicense(t *testing.T) {
	now := time.Now()
	testCases := []struct {
		name           string
		license1       *configurationv1alpha1.KongLicense
		license2       *configurationv1alpha1.KongLicense
		expectedResult bool
	}{
		{
			name: "The newer one should win",
			license1: &configurationv1alpha1.KongLicense{
				CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Second)),
				Name:              "alpha",
			},
			license2: &configurationv1alpha1.KongLicense{
				CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Second)),
				Name:              "beta",
			},
			expectedResult: false,
		},
		{
			name: "If the creationTimestamp equals, the one with lexical smaller name should win",
			license1: &configurationv1alpha1.KongLicense{
				CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Second)),
				Name:              "alpha",
			},
			license2: &configurationv1alpha1.KongLicense{
				CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Second)),
				Name:              "beta",
			},
			expectedResult: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedResult,
				compareKongLicense(tc.license1, tc.license2),
				"Should return expected compare results between two licenses")
		})
	}
}

func TestKongLicenseController_pickLicense(t *testing.T) {
	now := time.Now()
	testCases := []struct {
		name              string
		licenses          []*configurationv1alpha1.KongLicense
		expectedNil       bool
		chosenLicenseName string
	}{
		{
			name:        "No licenses in cache - should return nil",
			licenses:    []*configurationv1alpha1.KongLicense{},
			expectedNil: true,
		},
		{
			name: "Should choose the newest one",
			licenses: []*configurationv1alpha1.KongLicense{
				{
					CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Second)),
					Name:              "older",
				},
				{
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Second)),
					Name:              "newer",
				},
				{
					CreationTimestamp: metav1.NewTime(now.Add(-2 * time.Second)),
					Name:              "newest",
				},
			},
			expectedNil:       false,
			chosenLicenseName: "newest",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &KongV1Alpha1KongLicenseReconciler{
				LicenseCache: NewLicenseCache(),
			}
			for _, l := range tc.licenses {
				err := r.LicenseCache.Add(l)
				require.NoError(t, err, "Should have no error in adding KongLicense to cache")
			}
			chosenLicense := r.pickLicenseInCache()
			if tc.expectedNil {
				require.Nil(t, chosenLicense, "Should get no license")
			} else {
				require.NotNil(t, chosenLicense, "Should return an available license")
				require.Equal(t, tc.chosenLicenseName, chosenLicense.Name,
					"Should choose expected KongLicense")
			}
		})
	}
}

func TestKongLicenseController_disabledLicenseEviction(t *testing.T) {
	now := time.Now()

	license := func(name string, creation metav1.Time, enabled bool) *configurationv1alpha1.KongLicense {
		return &configurationv1alpha1.KongLicense{
			Name:              name,
			CreationTimestamp: creation,
			Enabled:           enabled,
			RawLicenseString:  `{"license":"` + name + `"}`,
		}
	}

	testCases := []struct {
		name              string
		licenses          []*configurationv1alpha1.KongLicense
		disabledName      string
		expectedNil       bool
		chosenLicenseName string
	}{
		{
			name: "Disabling the only cached license evicts it and leaves nothing picked",
			licenses: []*configurationv1alpha1.KongLicense{
				license("only-license", metav1.NewTime(now.Add(-time.Minute)), true),
			},
			disabledName: "only-license",
			expectedNil:  true,
		},
		{
			name: "Disabling the chosen license repicks the remaining one",
			licenses: []*configurationv1alpha1.KongLicense{
				license("older", metav1.NewTime(now.Add(-2*time.Minute)), true),
				license("chosen", metav1.NewTime(now.Add(-time.Minute)), true),
			},
			disabledName:      "chosen",
			expectedNil:       false,
			chosenLicenseName: "older",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			scheme.AddKnownTypes(configurationv1alpha1.GroupVersion, &configurationv1alpha1.KongLicense{})
			objs := make([]client.Object, 0, len(tc.licenses))
			for _, l := range tc.licenses {
				objs = append(objs, l.DeepCopy())
			}
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(&configurationv1alpha1.KongLicense{}).Build()

			r := &KongV1Alpha1KongLicenseReconciler{
				Client:       cl,
				Log:          logr.Discard(),
				Scheme:       scheme,
				LicenseCache: NewLicenseCache(),
			}
			// Cache every enabled license, as a reconcile of an enabled license would.
			for _, l := range tc.licenses {
				if l.Enabled {
					require.NoError(t, r.LicenseCache.Add(l.DeepCopy()))
				}
			}
			r.setChosenLicense(r.pickLicenseInCache())
			require.NotNil(t, r.getChosenLicense(), "precondition: a license should be picked")

			// Disable the license in the cluster and reconcile it.
			disabled := &configurationv1alpha1.KongLicense{}
			require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Name: tc.disabledName}, disabled))
			disabled.Enabled = false
			require.NoError(t, cl.Update(context.Background(), disabled))
			_, err := r.Reconcile(context.Background(), reconcile.Request{Name: tc.disabledName})
			require.NoError(t, err)

			_, exists, err := r.LicenseCache.Get(disabled)
			require.NoError(t, err)
			require.False(t, exists, "disabled license should be evicted from the cache")

			chosen := r.getChosenLicense()
			if tc.expectedNil {
				require.Nil(t, chosen, "nothing should remain picked")
			} else {
				require.NotNil(t, chosen, "a remaining license should be picked")
				require.Equal(t, tc.chosenLicenseName, chosen.Name)
			}
		})
	}
}
