package dataplane

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/test/helpers"
)

type testCallback struct {
	Callback Callback
	Name     string
}

func TestCallbackRun(t *testing.T) {
	ca := helpers.CreateCA(t)
	testCases := []struct {
		name      string
		dataplane *operatorv1beta1.DataPlane
		modifies  reflect.Type
		callbacks []testCallback
		cbErrors  []error
		// validate checks that the callback made an expected modification to subject and fails if it did not
		validate              func(t *testing.T, got any) bool
		subject               any
		dataplaneSubResources []controllerruntimeclient.Object
	}{
		{
			name: "no callbacks do nothing, successfully",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec:   operatorv1beta1.DataPlaneSpec{},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			subject: &k8sresources.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane-deployment",
					Namespace: "default",
				},
				Status: appsv1.DeploymentStatus{},
			},
			validate: func(t *testing.T, got any) bool {
				// expected here is a copy of the original subject. the noop callback should leave the subject
				// untouched
				expected := k8sresources.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-deployment",
						Namespace: "default",
					},
					Status: appsv1.DeploymentStatus{},
				}

				actual := got.(*k8sresources.Deployment)
				return cmp.Equal(expected, *actual)
			},
		},
		{
			name: "incorrect subject type is rejected",
			callbacks: []testCallback{
				{
					Name:     "test",
					Callback: noopCallback,
				},
			},
			cbErrors: []error{
				fmt.Errorf("callback manager expected type *resources.Deployment, got type *v1.Service"),
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec:   operatorv1beta1.DataPlaneSpec{},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			modifies: reflect.TypeFor[k8sresources.Deployment](),
			subject: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
			},
			validate: func(t *testing.T, got any) bool {
				return true
			},
		},
		{
			name: "failing callbacks return errors",
			callbacks: []testCallback{
				{
					Name:     "one",
					Callback: failCallback("red"),
				},
				{
					Name:     "two",
					Callback: failCallback("blue"),
				},
			},
			cbErrors: []error{
				fmt.Errorf("callback %s failed: %w", "one", fmt.Errorf("red")),
				fmt.Errorf("callback %s failed: %w", "two", fmt.Errorf("blue")),
			},
			dataplane: &operatorv1beta1.DataPlane{},
			modifies:  reflect.TypeFor[k8sresources.Deployment](),
			subject:   &k8sresources.Deployment{},
			validate: func(t *testing.T, got any) bool {
				return true
			},
		},
		{
			name: "single noop callback does nothing successfully",
			callbacks: []testCallback{
				{
					Name:     "test",
					Callback: noopCallback,
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec:   operatorv1beta1.DataPlaneSpec{},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			modifies: reflect.TypeFor[k8sresources.Deployment](),
			subject: &k8sresources.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane-deployment",
					Namespace: "default",
				},
				Status: appsv1.DeploymentStatus{},
			},
			validate: func(t *testing.T, got any) bool {
				// expected here is a copy of the original subject. the noop callback should leave the subject
				// untouched
				expected := k8sresources.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-deployment",
						Namespace: "default",
					},
					Status: appsv1.DeploymentStatus{},
				}

				actual := got.(*k8sresources.Deployment)
				return cmp.Equal(expected, *actual)
			},
		},
		{
			name: "single callback successfully modifies subject",
			callbacks: []testCallback{
				{
					Name:     "test",
					Callback: fakeStaticVolumeCallback("test"),
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec:   operatorv1beta1.DataPlaneSpec{},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			modifies: reflect.TypeFor[k8sresources.Deployment](),
			subject: &k8sresources.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane-deployment",
					Namespace: "default",
				},
				Status: appsv1.DeploymentStatus{},
			},
			validate: func(t *testing.T, got any) bool {
				subj, ok := got.(*k8sresources.Deployment)
				if !ok {
					return false
				}
				if len(subj.Spec.Template.Spec.Volumes) != 1 {
					return false
				}
				return subj.Spec.Template.Spec.Volumes[0].Name == "test"
			},
		},
		{
			name: "multiple callbacks successfully modify subject",
			callbacks: []testCallback{
				{
					Name:     "one",
					Callback: fakeStaticVolumeCallback("one"),
				},
				{
					Name:     "two",
					Callback: fakeStaticVolumeCallback("two"),
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec:   operatorv1beta1.DataPlaneSpec{},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			modifies: reflect.TypeFor[k8sresources.Deployment](),
			subject: &k8sresources.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane-deployment",
					Namespace: "default",
				},
				Status: appsv1.DeploymentStatus{},
			},
			validate: func(t *testing.T, got any) bool {
				subj, ok := got.(*k8sresources.Deployment)
				if !ok {
					return false
				}
				if len(subj.Spec.Template.Spec.Volumes) != 2 {
					return false
				}
				var one, two bool
				for _, v := range subj.Spec.Template.Spec.Volumes {
					if v.Name == "one" {
						one = true
					}
					if v.Name == "two" {
						two = true
					}
				}
				return one && two
			},
		},
		// in practice we expect controllers to discard modified resources and retry when some callbacks fail, but the
		// runner is designed to not abort if one failure occurs
		{
			name: "some callbacks can fail, successful callbacks still modify",
			callbacks: []testCallback{
				{
					Name:     "test",
					Callback: fakeStaticVolumeCallback("test"),
				},
				{
					Name:     "one",
					Callback: failCallback("red"),
				},
			},
			cbErrors: []error{
				fmt.Errorf("callback %s failed: %w", "one", fmt.Errorf("red")),
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec:   operatorv1beta1.DataPlaneSpec{},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			modifies: reflect.TypeFor[k8sresources.Deployment](),
			subject: &k8sresources.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane-deployment",
					Namespace: "default",
				},
				Status: appsv1.DeploymentStatus{},
			},
			validate: func(t *testing.T, got any) bool {
				subj, ok := got.(*k8sresources.Deployment)
				if !ok {
					return false
				}
				if len(subj.Spec.Template.Spec.Volumes) != 1 {
					return false
				}
				return subj.Spec.Template.Spec.Volumes[0].Name == "test"
			},
		},
		{
			name: "callback can add configuration derived from retrieved object",
			callbacks: []testCallback{
				{
					Name:     "test",
					Callback: fakeDynamicVolumeCallback("cert-purpose", "faketest"),
				},
			},
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       types.UID("949c1053-dad5-4dfd-8b97-be0105459ccb"),
				},
				Spec:   operatorv1beta1.DataPlaneSpec{},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			modifies: reflect.TypeFor[k8sresources.Deployment](),
			subject: &k8sresources.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane-deployment",
					Namespace: "default",
				},
				Status: appsv1.DeploymentStatus{},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-tls-secret-2",
						Namespace: "default",
						Labels: map[string]string{
							"cert-purpose":                           "faketest",
							"gateway-operator.konghq.com/managed-by": "dataplane",
						},
					},
					Data: helpers.TLSSecretData(t, ca,
						helpers.CreateCert(t, "*.test-admin-service.default.svc", ca.Cert, ca.Key),
					),
				},
			},
			validate: func(t *testing.T, got any) bool {
				subj, ok := got.(*k8sresources.Deployment)
				if !ok {
					return false
				}
				if len(subj.Spec.Template.Spec.Volumes) != 1 {
					return false
				}
				return subj.Spec.Template.Spec.Volumes[0].Name == "fake"
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ObjectsToAdd := []controllerruntimeclient.Object{
				tc.dataplane,
			}

			for _, dataplaneSubresource := range tc.dataplaneSubResources {
				k8sutils.SetOwnerForObject(dataplaneSubresource, tc.dataplane)
				ObjectsToAdd = append(ObjectsToAdd, dataplaneSubresource)
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(ObjectsToAdd...).
				WithStatusSubresource(tc.dataplane).
				Build()

			manager := CreateCallbackManager()
			runner := NewCallbackRunner(fakeClient)
			for _, cb := range tc.callbacks {
				require.NoError(t, manager.Register(cb.Callback, cb.Name))
			}
			errs := runner.For(tc.dataplane).Modifies(tc.modifies).Runs(manager).Do(context.Background(), tc.subject)
			errSort := func(a, b error) int {
				A := a.Error()
				B := b.Error()
				switch {
				case A == B:
					return 0
				case A > B:
					return 1
				case A < B:
					return -1
				}
				return 0
			}
			if tc.cbErrors == nil {
				require.Empty(t, errs)
			} else {
				slices.SortFunc(tc.cbErrors, errSort)
				slices.SortFunc(errs, errSort)
				require.Equal(t, tc.cbErrors, errs)
			}
			// callbacks operate on their subject by reference. we expect their modifications to appear on the original
			// input after the runner calls Do()
			require.True(t, tc.validate(t, tc.subject))
		})
	}
}

// fakeDynamicVolumeCallback adds a volume to a Deployment, for testing purposes.
func fakeDynamicVolumeCallback(labelName, labelValue string) Callback {
	return func(ctx context.Context, d *operatorv1beta1.DataPlane, c controllerruntimeclient.Client, subj any) error {
		deployment, ok := subj.(*k8sresources.Deployment)
		if !ok {
			return fmt.Errorf("fakeDynamicVolumeCallback received a non-Deployment subject")
		}
		labels := k8sresources.GetManagedLabelForOwner(d)
		labels[labelName] = labelValue

		secrets, err := k8sutils.ListSecretsForOwner(ctx, c, d.GetUID(), labels)
		if err != nil {
			return fmt.Errorf("failed listing Secrets for %T %s/%s: %w",
				deployment, deployment.GetNamespace(), d.GetName(), err)
		}
		if len(secrets) < 1 {
			return fmt.Errorf("found no faketest Secrets for %T %s/%s",
				deployment, deployment.GetNamespace(), d.GetName())
		}
		if len(secrets) > 1 {
			return fmt.Errorf("too many faketest Secrets for %T %s/%s",
				deployment, deployment.GetNamespace(), d.GetName())
		}

		fakeVol := corev1.Volume{}
		fakeVol.Secret = &corev1.SecretVolumeSource{
			SecretName: secrets[0].Name,
		}
		fakeVol.Name = "fake"
		fakeMount := corev1.VolumeMount{
			Name:      "fake",
			ReadOnly:  true,
			MountPath: "/opt/fake",
		}
		_ = deployment.WithVolume(fakeVol).
			WithVolumeMount(fakeMount, consts.DataPlaneProxyContainerName).
			WithEnvVar(
				corev1.EnvVar{
					Name:  "CALLBACK_TEST",
					Value: "fake",
				},
				consts.DataPlaneProxyContainerName,
			)
		return nil
	}
}

// fakeStaticVolumeCallback returns a callback that adds a garbage volume with a given name to a DataPlane's Deployment.
func fakeStaticVolumeCallback(name string) Callback {
	return func(ctx context.Context, d *operatorv1beta1.DataPlane, c controllerruntimeclient.Client, subj any) error {
		deployment, ok := subj.(*k8sresources.Deployment)
		if !ok {
			return fmt.Errorf("fakeStaticVolumeCallback received a non-Deployment subject")
		}
		fakeVol := corev1.Volume{
			Name: name,
		}
		fakeVol.EmptyDir = &corev1.EmptyDirVolumeSource{}
		fakeMount := corev1.VolumeMount{
			Name:      name,
			ReadOnly:  true,
			MountPath: "/opt/fake",
		}
		_ = deployment.WithVolume(fakeVol).WithVolumeMount(fakeMount, consts.DataPlaneProxyContainerName)
		return nil
	}
}

// noopCallback does nothing.
func noopCallback(_ context.Context, _ *operatorv1beta1.DataPlane, _ controllerruntimeclient.Client, _ any) error {
	return nil
}

// failCallback does nothing, unsuccessfully, with the provided error text.
func failCallback(text string) func(_ context.Context, _ *operatorv1beta1.DataPlane, _ controllerruntimeclient.Client, _ any) error {
	return func(_ context.Context, _ *operatorv1beta1.DataPlane, _ controllerruntimeclient.Client, _ any) error {
		return fmt.Errorf("%s", text)
	}
}
