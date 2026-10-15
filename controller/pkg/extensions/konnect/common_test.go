package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func dataPlaneWithProxyEnv(env ...corev1.EnvVar) *operatorv1beta1.DataPlane {
	return &operatorv1beta1.DataPlane{
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{Name: consts.DataPlaneProxyContainerName, Env: env}},
							},
						},
					},
				},
			},
		},
	}
}

func TestIsHybridClusterType(t *testing.T) {
	tests := []struct {
		name        string
		clusterType konnectv1alpha2.KonnectExtensionClusterType
		want        bool
	}{
		{name: "ControlPlane", clusterType: konnectv1alpha2.ClusterTypeControlPlane, want: true},
		{name: "ControlPlaneGroup", clusterType: konnectv1alpha2.ClusterTypeControlPlaneGroup, want: true},
		{name: "K8sIngressController", clusterType: konnectv1alpha2.ClusterTypeK8sIngressController, want: false},
		{name: "empty", clusterType: "", want: false},
		{name: "unknown", clusterType: "Unknown", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsHybridClusterType(tt.clusterType))
		})
	}
}

func TestAdminAPIDisabled(t *testing.T) {
	adminListen := func(v string) corev1.EnvVar { return corev1.EnvVar{Name: "KONG_ADMIN_LISTEN", Value: v} }
	hybrid := konnectv1alpha2.ClusterTypeControlPlane

	tests := []struct {
		name string
		dp   *operatorv1beta1.DataPlane
		ct   konnectv1alpha2.KonnectExtensionClusterType
		want bool
	}{
		{name: "hybrid, no env", dp: dataPlaneWithProxyEnv(), ct: hybrid, want: true},
		{name: "hybrid group, no env", dp: dataPlaneWithProxyEnv(), ct: konnectv1alpha2.ClusterTypeControlPlaneGroup, want: true},
		{name: "hybrid, nil DataPlane", dp: nil, ct: hybrid, want: true},
		{name: "hybrid, nil pod template", dp: &operatorv1beta1.DataPlane{}, ct: hybrid, want: true},
		{name: "hybrid, user value", dp: dataPlaneWithProxyEnv(adminListen("0.0.0.0:8444 ssl")), ct: hybrid, want: false},
		{name: "hybrid, off", dp: dataPlaneWithProxyEnv(adminListen("off")), ct: hybrid, want: true},
		{name: "hybrid, padded off", dp: dataPlaneWithProxyEnv(adminListen(" off ")), ct: hybrid, want: true},
		{name: "hybrid, empty value", dp: dataPlaneWithProxyEnv(adminListen("")), ct: hybrid, want: true},
		{
			name: "hybrid, valueFrom only",
			dp: dataPlaneWithProxyEnv(corev1.EnvVar{
				Name:      "KONG_ADMIN_LISTEN",
				ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{}},
			}),
			ct:   hybrid,
			want: true,
		},
		{name: "K8sIngressController", dp: dataPlaneWithProxyEnv(), ct: konnectv1alpha2.ClusterTypeK8sIngressController, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, AdminAPIDisabled(tt.dp, tt.ct))
		})
	}
}

func TestDataPlaneAdminAPIDisabled(t *testing.T) {
	const ns = "test-namespace"

	ext := func(ct konnectv1alpha2.KonnectExtensionClusterType) *konnectv1alpha2.KonnectExtension {
		return &konnectv1alpha2.KonnectExtension{
			Name: "ext", Namespace: ns,
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect: &konnectv1alpha2.KonnectExtensionControlPlaneStatus{ClusterType: ct},
				Conditions: []metav1.Condition{
					{Type: konnectv1alpha2.KonnectExtensionReadyConditionType, Status: metav1.ConditionTrue},
				},
			},
		}
	}
	withRef := func(dp *operatorv1beta1.DataPlane) *operatorv1beta1.DataPlane {
		dp.Namespace = ns
		dp.Spec.Extensions = []commonv1alpha1.ExtensionRef{{
			Group: konnectv1alpha1.SchemeGroupVersion.Group,
			Kind:  konnectv1alpha2.KonnectExtensionKind,
			Name:  "ext",
		}}
		return dp
	}

	tests := []struct {
		name    string
		dp      *operatorv1beta1.DataPlane
		ext     *konnectv1alpha2.KonnectExtension
		want    bool
		wantErr bool
	}{
		{name: "no extension referenced", dp: dataPlaneWithProxyEnv(), want: false},
		{name: "hybrid", dp: withRef(dataPlaneWithProxyEnv()), ext: ext(konnectv1alpha2.ClusterTypeControlPlane), want: true},
		{
			name: "hybrid with user KONG_ADMIN_LISTEN",
			dp:   withRef(dataPlaneWithProxyEnv(corev1.EnvVar{Name: "KONG_ADMIN_LISTEN", Value: "0.0.0.0:8444 ssl"})),
			ext:  ext(konnectv1alpha2.ClusterTypeControlPlane),
			want: false,
		},
		{name: "K8sIngressController", dp: withRef(dataPlaneWithProxyEnv()), ext: ext(konnectv1alpha2.ClusterTypeK8sIngressController), want: false},
		{name: "extension not found", dp: withRef(dataPlaneWithProxyEnv()), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := fake.NewClientBuilder().WithScheme(scheme.Get())
			if tt.ext != nil {
				b = b.WithObjects(tt.ext)
			}
			got, err := DataPlaneAdminAPIDisabled(t.Context(), b.Build(), tt.dp)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
