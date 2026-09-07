package ipfamily_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/v2/pkg/ipfamily"
)

func kubernetesServiceWithFamilies(families ...corev1.IPFamily) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
		Spec:       corev1.ServiceSpec{IPFamilies: families},
	}
}

func newFakeClient(t *testing.T, objs ...runtime.Object) *fake.ClientBuilder {
	t.Helper()
	return fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithRuntimeObjects(objs...)
}

func TestDetect(t *testing.T) {
	for _, tt := range []struct {
		name    string
		svc     *corev1.Service
		want    ipfamily.IPFamily
		wantErr bool
	}{
		{
			name: "ipv4 only",
			svc:  kubernetesServiceWithFamilies(corev1.IPv4Protocol),
			want: ipfamily.IPv4,
		},
		{
			name: "ipv6 only",
			svc:  kubernetesServiceWithFamilies(corev1.IPv6Protocol),
			want: ipfamily.IPv6,
		},
		{
			name: "dual stack",
			svc:  kubernetesServiceWithFamilies(corev1.IPv4Protocol, corev1.IPv6Protocol),
			want: ipfamily.Dual,
		},
		{
			name:    "service missing",
			svc:     nil,
			wantErr: true,
		},
		{
			name:    "no recognized families",
			svc:     kubernetesServiceWithFamilies(),
			wantErr: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var objs []runtime.Object
			if tt.svc != nil {
				objs = append(objs, tt.svc)
			}
			cl := newFakeClient(t, objs...).Build()

			got, err := ipfamily.Detect(t.Context(), cl)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolve(t *testing.T) {
	log := ctrllog.Log

	t.Run("explicit configuration wins over detection", func(t *testing.T) {
		cl := newFakeClient(t, kubernetesServiceWithFamilies(corev1.IPv6Protocol)).Build()
		got := ipfamily.Resolve(t.Context(), ipfamily.IPv4, cl, log)
		assert.Equal(t, ipfamily.IPv4, got)
	})

	t.Run("auto detects from cluster", func(t *testing.T) {
		cl := newFakeClient(t, kubernetesServiceWithFamilies(corev1.IPv4Protocol, corev1.IPv6Protocol)).Build()
		got := ipfamily.Resolve(t.Context(), ipfamily.Auto, cl, log)
		assert.Equal(t, ipfamily.Dual, got)
	})

	t.Run("auto falls back to ipv4 on detection failure", func(t *testing.T) {
		cl := newFakeClient(t).Build()
		got := ipfamily.Resolve(t.Context(), ipfamily.Auto, cl, log)
		assert.Equal(t, ipfamily.IPv4, got)
	})
}
