package certificates

import (
	"fmt"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	certutils "github.com/kong/kong-operator/v2/controller/dataplane/utils/certificates"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

func TestCreateKonnectCert(t *testing.T) {
	testCases := []struct {
		name                  string
		dataplane             *operatorv1beta1.DataPlane
		noCertificateCRD      bool
		opts                  []CertOpt
		wantErr               error
		dataplaneSubResources []controllerruntimeclient.Object
		// the following are core aspects of the certificate that we can test without building the entire struct
		// wantIssuerName is effectively the toggle for checking anything. if it is not set, the test should
		// not build any certificates
		wantIssuerName string
		wantIssuerKind string
		wantDNSNames   []string
		// extraChecks defines extra checks for the generated certificate.
		// it returns false with error message if the check fails.
		extraChecks func(certmanagerv1.Certificate) (bool, string)
	}{
		{
			name: "no issuer completes successfully",
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
		},
		{
			name: "cluster issuer set, no existing cert resources",
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
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							KonnectCertificateOptions: &operatorv1beta1.KonnectCertificateOptions{
								Issuer: operatorv1beta1.NamespacedName{
									Name: "test-issuer",
								},
							},
						},
					},
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			wantIssuerName: "test-issuer",
			wantIssuerKind: "ClusterIssuer",
			wantDNSNames:   []string{"test-dataplane.test-namespace.dataplane.konnect"},
		},
		{
			name: "issuer set, no existing cert resources",
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
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							KonnectCertificateOptions: &operatorv1beta1.KonnectCertificateOptions{
								Issuer: operatorv1beta1.NamespacedName{
									Namespace: "default",
									Name:      "test-issuer",
								},
							},
						},
					},
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			wantIssuerName: "test-issuer",
			wantIssuerKind: "Issuer",
			wantDNSNames:   []string{"test-dataplane.test-namespace.dataplane.konnect"},
		},
		{
			name: "reduce certificates when DataPlane does not set KonnectCertificateOptions",
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
			dataplaneSubResources: []controllerruntimeclient.Object{
				&certmanagerv1.Certificate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-certificate",
						Namespace: "test-namespace",
					},
				},
			},
		},
		{
			name:             "dataplane with no KonnectCertificateOptions should be OK when certificate CRD not installed",
			noCertificateCRD: true,
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
		},
		{
			name: "add WithSecretLabel option",
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
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							KonnectCertificateOptions: &operatorv1beta1.KonnectCertificateOptions{
								Issuer: operatorv1beta1.NamespacedName{
									Name: "test-issuer",
								},
							},
						},
					},
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			opts: []CertOpt{
				WithSecretLabel("key", "value"),
			},
			wantIssuerName: "test-issuer",
			wantIssuerKind: "ClusterIssuer",
			wantDNSNames:   []string{"test-dataplane.test-namespace.dataplane.konnect"},
			extraChecks: func(cert certmanagerv1.Certificate) (bool, string) {
				if cert.Spec.SecretTemplate == nil || cert.Spec.SecretTemplate.Labels == nil {
					return false, "spec.secretTemplate.labels not specified"
				}
				if cert.Spec.SecretTemplate.Labels["key"] != "value" {
					return false, "spec.secretTemplate.labels does not contain label 'key:value'"
				}
				return true, ""
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

			testScheme := scheme.Get()
			if !tc.noCertificateCRD {
				utilruntime.Must(certmanagerv1.AddToScheme(testScheme))
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(ObjectsToAdd...).
				WithStatusSubresource(tc.dataplane).
				Build()

			ctx := t.Context()
			err := CreateKonnectCert(ctx, logr.Discard(), tc.dataplane, fakeClient, tc.opts...)
			require.Equal(t, tc.wantErr, err)

			if tc.noCertificateCRD {
				t.Logf("Skip checking of certificates because certificate CRD not installed")
				return
			}

			labels := k8sresources.GetManagedLabelForOwner(tc.dataplane)
			labels[consts.CertPurposeLabel] = KonnectDataPlaneCertPurpose
			labels[certutils.ManagerUIDLabel] = string(tc.dataplane.UID)

			certs, err := certutils.ListCMCertificatesForOwner(
				ctx,
				fakeClient,
				tc.dataplane.Namespace,
				tc.dataplane.UID,
				labels,
			)
			require.NoError(t, err)
			if tc.wantIssuerName != "" {
				require.Len(t, certs, 1)
				require.Equal(t, tc.wantIssuerName, certs[0].Spec.IssuerRef.Name)
				require.Equal(t, tc.wantIssuerKind, certs[0].Spec.IssuerRef.Kind)
				require.Equal(t, tc.wantIssuerKind, certs[0].Spec.IssuerRef.Kind)
			} else {
				require.Empty(t, certs)
			}
			if tc.extraChecks != nil {
				ok, msg := tc.extraChecks(certs[0])
				require.True(t, ok, msg)
			}
		})
	}
}

func TestMountAndUseKonnectCert(t *testing.T) {
	staticUID := types.UID(uuid.NewString())
	testCases := []struct {
		name                  string
		dataplane             *operatorv1beta1.DataPlane
		wantErr               error
		deployment            *k8sresources.Deployment
		dataplaneSubResources []controllerruntimeclient.Object
		wantEnvVar            []corev1.EnvVar
		wantVolume            *corev1.Volume
		wantVolumeMount       *corev1.VolumeMount
	}{
		{
			name: "no issuer completes successfully",
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
		},
		{
			name: "excess secrets",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       staticUID,
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							KonnectCertificateOptions: &operatorv1beta1.KonnectCertificateOptions{
								Issuer: operatorv1beta1.NamespacedName{
									Name: "test-issuer",
								},
							},
						},
					},
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			wantErr: fmt.Errorf("too many %s Secrets for Deployment test-namespace/test-dataplane", KonnectDataPlaneCertPurpose),
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-tls-secret-1",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/cert-purpose": KonnectDataPlaneCertPurpose,
							"gateway-operator.konghq.com/managed-by":   "dataplane",
							certutils.ManagerUIDLabel:                  string(staticUID),
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-tls-secret-2",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/cert-purpose": KonnectDataPlaneCertPurpose,
							"gateway-operator.konghq.com/managed-by":   "dataplane",
							certutils.ManagerUIDLabel:                  string(staticUID),
						},
					},
				},
			},
		},
		{
			name: "no secrets",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       staticUID,
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							KonnectCertificateOptions: &operatorv1beta1.KonnectCertificateOptions{
								Issuer: operatorv1beta1.NamespacedName{
									Name: "test-issuer",
								},
							},
						},
					},
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			wantErr:               fmt.Errorf("no %s Secrets for Deployment test-namespace/test-dataplane", KonnectDataPlaneCertPurpose),
			dataplaneSubResources: []controllerruntimeclient.Object{},
		},
		{
			name: "normal secret",
			dataplane: &operatorv1beta1.DataPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "test-namespace",
					UID:       staticUID,
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							KonnectCertificateOptions: &operatorv1beta1.KonnectCertificateOptions{
								Issuer: operatorv1beta1.NamespacedName{
									Name: "test-issuer",
								},
							},
						},
					},
				},
				Status: operatorv1beta1.DataPlaneStatus{},
			},
			dataplaneSubResources: []controllerruntimeclient.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dataplane-tls-secret-1",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/cert-purpose": KonnectDataPlaneCertPurpose,
							"gateway-operator.konghq.com/managed-by":   "dataplane",
							certutils.ManagerUIDLabel:                  string(staticUID),
						},
					},
				},
			},
			deployment: &k8sresources.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane-deployment",
					Namespace: "default",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:         consts.DataPlaneProxyContainerName,
									Env:          []corev1.EnvVar{},
									VolumeMounts: []corev1.VolumeMount{},
								},
							},
							Volumes: []corev1.Volume{},
						},
					},
				},
			},
			wantEnvVar: []corev1.EnvVar{
				{
					Name:  "KONG_CLUSTER_CERT",
					Value: "/var/konnect-client-certificate/tls.crt",
				},
				{
					Name:  "KONG_CLUSTER_CERT_KEY",
					Value: "/var/konnect-client-certificate/tls.key",
				},
			},
			wantVolume: &corev1.Volume{
				Name: DataPlaneKonnectClientCertificateName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "test-dataplane-tls-secret-1",
					},
				},
			},
			wantVolumeMount: &corev1.VolumeMount{
				Name:      DataPlaneKonnectClientCertificateName,
				ReadOnly:  true,
				MountPath: "/var/konnect-client-certificate/",
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

			testScheme := scheme.Get()
			utilruntime.Must(certmanagerv1.AddToScheme(testScheme))

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(ObjectsToAdd...).
				WithStatusSubresource(tc.dataplane).
				Build()

			ctx := t.Context()

			deployment := &k8sresources.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane-deployment",
					Namespace: "test-namespace",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:         consts.DataPlaneProxyContainerName,
									Env:          []corev1.EnvVar{},
									VolumeMounts: []corev1.VolumeMount{},
								},
							},
							Volumes: []corev1.Volume{},
						},
					},
				},
			}

			err := MountAndUseKonnectCert(ctx, logr.Discard(), tc.dataplane, fakeClient, deployment)
			require.Equal(t, tc.wantErr, err)

			if len(tc.wantEnvVar) > 0 {
				actual := map[string]string{}
				for _, ev := range deployment.Spec.Template.Spec.Containers[0].Env {
					actual[ev.Name] = ev.Value
				}
				for _, ev := range tc.wantEnvVar {
					val, ok := actual[ev.Name]
					require.True(t, ok)
					assert.Equal(t, ev.Value, val)
				}
			}
			if tc.wantVolume != nil {
				assert.Equal(t, *tc.wantVolume, deployment.Spec.Template.Spec.Volumes[0])
			}
			if tc.wantVolumeMount != nil {
				assert.Equal(t, *tc.wantVolumeMount, deployment.Spec.Template.Spec.Containers[0].VolumeMounts[0])
			}
		})
	}
}
