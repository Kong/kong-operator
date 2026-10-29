package gwadmin

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// testMaterial returns TLS material with distinct content per Secret, bundle
// being the content of the cp client trust bundle.
func testMaterial(bundle string) *tlsMaterial {
	tlsSecret := func(v string) *corev1.Secret {
		return &corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: []byte(v + "-crt"), corev1.TLSPrivateKeyKey: []byte(v + "-key")}}
	}
	return &tlsMaterial{
		clientBundle: &corev1.Secret{Data: map[string][]byte{consts.CACRT: []byte(bundle)}},
		apiBundle:    &corev1.Secret{Data: map[string][]byte{consts.CACRT: []byte("api-bundle")}},
		serverBundle: &corev1.Secret{Data: map[string][]byte{consts.CACRT: []byte("server-bundle")}},
		api:          tlsSecret("api"),
		cp:           tlsSecret("cp"),
		client:       tlsSecret("client"),
	}
}

func TestBuildDeployment(t *testing.T) {
	tc := managedfields.NewDeducedTypeConverter()
	m := testMaterial("bundle")

	t.Run("base", func(t *testing.T) {
		u, err := buildDeployment(logr.Discard(), tc, newGWAdmin(nil), m)
		require.NoError(t, err)
		d := &appsv1.Deployment{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, d))

		assert.Equal(t, m.checksum(), d.Spec.Template.Annotations[consts.GWAdminTLSChecksumAnnotation])
		assert.Equal(t, m.trustChecksum(), d.Spec.Template.Annotations[consts.GWAdminTrustChecksumAnnotation])
		assert.NotEqual(t, m.checksum(), m.trustChecksum())
		c := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.GWAdminContainerName)
		require.NotNil(t, c)
		assert.Equal(t, consts.DefaultGWAdminImage, c.Image)
		assert.Equal(t, []string{"server"}, c.Args)
		env := map[string]string{}
		for _, e := range c.Env {
			env[e.Name] = e.Value
		}
		assert.Equal(t, consts.GWAdminCPTLSMountPath+"/tls.crt", env[consts.GWAdminCPTLSCertFileEnvVar])
		assert.Equal(t, consts.GWAdminAPITLSMountPath+"/ca.crt", env[consts.GWAdminAPITLSClientCAFileEnvVar])
		assert.Equal(t, consts.GWAdminCPTLSMountPath+"/ca.crt", env[consts.GWAdminCPTLSClientCAFileEnvVar])
		assert.Equal(t, consts.GWAdminOpsPortName, c.ReadinessProbe.HTTPGet.Port.StrVal)

		// Each listener verifies its clients with the bundle of its own
		// trust domain.
		volumes := map[string]*corev1.Volume{}
		for i := range d.Spec.Template.Spec.Volumes {
			volumes[d.Spec.Template.Spec.Volumes[i].Name] = &d.Spec.Template.Spec.Volumes[i]
		}
		for name, want := range map[string][]string{
			apiTLSVolumeName: {"gw-api-tls", "gw-api-ca-bundle"},
			cpTLSVolumeName:  {"gw-cp-tls", "gw-cp-client-ca-bundle"},
		} {
			v := volumes[name]
			require.NotNil(t, v, name)
			require.NotNil(t, v.Projected, name)
			require.Len(t, v.Projected.Sources, 2, name)
			assert.Equal(t, want[0], v.Projected.Sources[0].Secret.Name, name)
			assert.Equal(t, want[1], v.Projected.Sources[1].Secret.Name, name)
		}
	})

	t.Run("overlay can't pin the checksum", func(t *testing.T) {
		gw := newGWAdmin(nil)
		gw.Spec.Deployment = &operatorv1alpha1.GWAdminDeploymentOptions{PodTemplateSpec: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: consts.GWAdminContainerName, Image: "custom/gw-admin:1"}}},
		}}
		gw.Spec.Deployment.PodTemplateSpec.Annotations = map[string]string{
			consts.GWAdminTLSChecksumAnnotation:   "pinned",
			consts.GWAdminTrustChecksumAnnotation: "pinned",
			"user":                                "kept",
		}

		u, err := buildDeployment(logr.Discard(), tc, gw, m)
		require.NoError(t, err)
		d := &appsv1.Deployment{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, d))
		assert.Equal(t, m.checksum(), d.Spec.Template.Annotations[consts.GWAdminTLSChecksumAnnotation])
		assert.Equal(t, m.trustChecksum(), d.Spec.Template.Annotations[consts.GWAdminTrustChecksumAnnotation])
		assert.Equal(t, "kept", d.Spec.Template.Annotations["user"])
		assert.Equal(t, "custom/gw-admin:1", k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.GWAdminContainerName).Image)
	})
}

func TestChecksums(t *testing.T) {
	m := testMaterial("bundle")
	base, trust := m.checksum(), m.trustChecksum()

	// Changing a leaf changes the checksum, not the trust checksum.
	m.api.Data[corev1.TLSCertKey] = []byte("renewed")
	assert.NotEqual(t, base, m.checksum())
	assert.Equal(t, trust, m.trustChecksum())

	// Neither the operator client certificate nor the cp server bundle is
	// mounted.
	m = testMaterial("bundle")
	m.serverBundle.Data[consts.CACRT] = []byte("other server bundle")
	assert.Equal(t, base, m.checksum())
	assert.Equal(t, trust, m.trustChecksum())
	m.client.Data[corev1.TLSCertKey] = []byte("renewed")
	assert.Equal(t, base, m.checksum())
	m.client = nil
	assert.Equal(t, base, m.checksum())

	// Changing a bundle changes both.
	m = testMaterial("other bundle")
	assert.NotEqual(t, base, m.checksum())
	assert.NotEqual(t, trust, m.trustChecksum())
	m = testMaterial("bundle")
	m.apiBundle.Data[consts.CACRT] = []byte("other api bundle")
	assert.NotEqual(t, base, m.checksum())
	assert.NotEqual(t, trust, m.trustChecksum())

	// The bundles are told apart: swapping them changes both.
	m = testMaterial("api-bundle")
	m.apiBundle.Data[consts.CACRT] = []byte("bundle")
	assert.NotEqual(t, base, m.checksum())
	assert.NotEqual(t, trust, m.trustChecksum())

	// A missing Secret differs from an empty one.
	m = testMaterial("")
	empty := m.checksum()
	m.clientBundle = nil
	assert.NotEqual(t, empty, m.checksum())
	assert.False(t, m.mountable())
	m = testMaterial("")
	m.apiBundle = nil
	assert.False(t, m.mountable())
	assert.True(t, testMaterial("").mountable())
	var nilMaterial *tlsMaterial
	assert.False(t, nilMaterial.mountable())
	m = testMaterial("")
	m.client = nil
	assert.True(t, m.mountable(), "the client certificate is not mounted")
}

func TestServicesAndStatus(t *testing.T) {
	gw := newGWAdmin(nil)
	api := generateAPIService(gw)
	assert.True(t, api.Spec.PublishNotReadyAddresses, "pods are reachable before they are Ready")
	assert.Equal(t, gwadminref.APIServiceLabels(gw.Name), api.Labels, "the API Service tells its GWAdmin and its role")

	setStatus(gw, "cluster.local")
	assert.Equal(t, "https://gw-cp.ns.svc.cluster.local:8099", gw.Status.CP.ServerURL)
	assert.Equal(t, "gw-api", gw.Status.API.ServiceName)
	assert.Equal(t, "gw-cp-server-ca-bundle", gw.Status.TLS.CPServerCABundleSecretName)
	assert.Equal(t, "gw-api-ca-bundle", gw.Status.TLS.APICABundleSecretName)

	gw.Spec.Network = &operatorv1alpha1.GWAdminNetworkOptions{CP: &operatorv1alpha1.GWAdminServiceOptions{
		Ports: []operatorv1alpha1.GWAdminServicePort{{Name: new(consts.GWAdminCPPortName), Port: 443}},
	}}
	setStatus(gw, "")
	assert.Equal(t, "https://gw-cp.ns.svc:443", gw.Status.CP.ServerURL)
}

func TestCPServiceOptions(t *testing.T) {
	assert.Nil(t, cpServiceOptions(newGWAdmin(nil)), "not configured")

	for _, tc := range []struct {
		name       string
		targetPort *intstr.IntOrString
		want       intstr.IntOrString
	}{
		{name: "defaults to the cp listener", want: intstr.FromString(consts.GWAdminCPPortName)},
		{name: "the user's port number is kept", targetPort: new(intstr.FromInt32(9999)), want: intstr.FromInt32(9999)},
		{name: "the user's port name is kept", targetPort: new(intstr.FromString("custom")), want: intstr.FromString("custom")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gw := newGWAdmin(nil)
			gw.Spec.Network = &operatorv1alpha1.GWAdminNetworkOptions{CP: &operatorv1alpha1.GWAdminServiceOptions{
				Ports: []operatorv1alpha1.GWAdminServicePort{
					{Name: new("public"), Port: 443, TargetPort: tc.targetPort},
				},
			}}

			o := cpServiceOptions(gw)
			require.NotNil(t, o)
			assert.Equal(t, corev1.ServiceTypeClusterIP, o.Type, "the data planes run in the cluster")
			require.Len(t, o.Ports, 1)
			assert.Equal(t, "public", *o.Ports[0].Name)
			assert.Equal(t, int32(443), o.Ports[0].Port)
			require.NotNil(t, o.Ports[0].TargetPort)
			assert.Equal(t, tc.want, *o.Ports[0].TargetPort)
			if tc.targetPort == nil {
				assert.Nil(t, gw.Spec.Network.CP.Ports[0].TargetPort, "the spec is not mutated")
			}

			// The built Service targets it.
			obj, err := buildCPService(managedfields.NewDeducedTypeConverter(), gw)
			require.NoError(t, err)
			svc := &corev1.Service{}
			u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			require.NoError(t, err)
			require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(u, svc))
			var found bool
			for _, p := range svc.Spec.Ports {
				if p.Port == 443 {
					found = true
					assert.Equal(t, tc.want, p.TargetPort)
				}
			}
			assert.True(t, found, "port 443 in %v", svc.Spec.Ports)
		})
	}

	t.Run("traffic distribution and internal traffic policy reach the Service", func(t *testing.T) {
		gw := newGWAdmin(nil)
		gw.Spec.Network = &operatorv1alpha1.GWAdminNetworkOptions{CP: &operatorv1alpha1.GWAdminServiceOptions{
			TrafficDistribution:   new(corev1.ServiceTrafficDistributionPreferSameZone),
			InternalTrafficPolicy: new(corev1.ServiceInternalTrafficPolicyLocal),
		}}
		obj, err := buildCPService(managedfields.NewDeducedTypeConverter(), gw)
		require.NoError(t, err)
		u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		require.NoError(t, err)
		svc := &corev1.Service{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(u, svc))
		require.NotNil(t, svc.Spec.TrafficDistribution)
		assert.Equal(t, corev1.ServiceTrafficDistributionPreferSameZone, *svc.Spec.TrafficDistribution)
		require.NotNil(t, svc.Spec.InternalTrafficPolicy)
		assert.Equal(t, corev1.ServiceInternalTrafficPolicyLocal, *svc.Spec.InternalTrafficPolicy)
	})
}

func TestBuildDeployment_ServiceAccountToken(t *testing.T) {
	tc := managedfields.NewDeducedTypeConverter()
	build := func(t *testing.T, gw *operatorv1alpha1.GWAdmin) *appsv1.Deployment {
		t.Helper()
		u, err := buildDeployment(logr.Discard(), tc, gw, testMaterial("bundle"))
		require.NoError(t, err)
		d := &appsv1.Deployment{}
		require.NoError(t, runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, d))
		return d
	}

	d := build(t, newGWAdmin(nil))
	require.NotNil(t, d.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *d.Spec.Template.Spec.AutomountServiceAccountToken, "gw-admin never talks to the Kubernetes API")

	// The user's Pod template overlay can ask for one, e.g. for a sidecar.
	gw := newGWAdmin(nil)
	gw.Spec.Deployment = &operatorv1alpha1.GWAdminDeploymentOptions{PodTemplateSpec: &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{AutomountServiceAccountToken: new(true)},
	}}
	d = build(t, gw)
	require.NotNil(t, d.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.True(t, *d.Spec.Template.Spec.AutomountServiceAccountToken)
}
