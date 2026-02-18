package konnect

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func Test_enqueueSecretsFromAPIAuthConfiguration(t *testing.T) {
	cases := []struct {
		name string
		obj  client.Object
		want []ctrl.Request
	}{
		{
			name: "not a KonnectAPIAuthConfiguration",
			obj:  nil,
			want: nil,
		},
		{
			name: "not secretRef type",
			obj: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type: konnectv1alpha1.KonnectAPIAuthTypeToken,
				},
			},
			want: nil,
		},
		{
			name: "secretRef nil",
			obj: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: nil,
				},
			},
			want: nil,
		},
		{
			name: "secretRef with empty namespace",
			obj: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Namespace: "ns1"},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "sec1", Namespace: ""},
				},
			},
			want: []ctrl.Request{{NamespacedName: types.NamespacedName{Namespace: "ns1", Name: "sec1"}}},
		},
		{
			name: "secretRef with explicit namespace",
			obj: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: ctrl.ObjectMeta{Namespace: "ns1"},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "sec2", Namespace: "ns2"},
				},
			},
			want: []ctrl.Request{{NamespacedName: types.NamespacedName{Namespace: "ns2", Name: "sec2"}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())
			defer q.ShutDown()

			enqueueSecretsFromAPIAuthConfiguration(tc.obj, q)

			// Collect all requests from the queue.
			var got []ctrl.Request
			for q.Len() > 0 {
				item, shutdown := q.Get()
				if shutdown {
					break
				}
				got = append(got, item)
				q.Done(item)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
