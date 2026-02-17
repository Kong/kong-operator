package secrets

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlruntimelog "sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func Test_ensureContainerImageUpdated(t *testing.T) {
	for _, tt := range []struct {
		name          string
		originalImage string
		newImage      string
		expectedImage string
		updated       bool
		wantErr       string
	}{
		{
			name:          "invalid images produce an error",
			originalImage: "fake:invalid:image:2.7.0",
			newImage:      "kong/kong:2.7.0",
			wantErr:       "invalid container image found: fake:invalid:image:2.7.0",
		},
		{
			name:          "setting new image when existing is local with port is allowed",
			originalImage: "localhost:5000/kic:2.7.0",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "setting new local image is allowed",
			originalImage: "kong/kong:2.7.0",
			newImage:      "localhost:5000/kong:2.7.0",
			expectedImage: "localhost:5000/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "same image and version makes no changes",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       false,
		},
		{
			name:          "version added when not originally present",
			originalImage: "kong/kong",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "version is changed when a new one is provided",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong:3.0.0",
			expectedImage: "kong/kong:3.0.0",
			updated:       true,
		},
		{
			name:          "image is added when not originally present",
			originalImage: "",
			newImage:      "kong/kong",
			expectedImage: "kong/kong",
			updated:       true,
		},
		{
			name:          "image is changed when a new one is provided",
			originalImage: "kong/kong",
			newImage:      "kong/kong-gateway",
			expectedImage: "kong/kong-gateway",
			updated:       true,
		},
		{
			name:          "image and version are added when not originally present",
			originalImage: "",
			newImage:      "kong/kong-gateway:3.0.0",
			expectedImage: "kong/kong-gateway:3.0.0",
			updated:       true,
		},
		{
			name:          "image and version are changed when new ones are provided",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong-gateway:3.0.0",
			expectedImage: "kong/kong-gateway:3.0.0",
			updated:       true,
		},
		{
			name:          "image and version are changed when new ones are provided with local registry",
			originalImage: "kong/kong:2.7.0",
			newImage:      "localhost:5000/kong-gateway:3.0.0",
			expectedImage: "localhost:5000/kong-gateway:3.0.0",
			updated:       true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			container := generators.NewContainer("test", tt.originalImage, 80)
			updated, err := ensureContainerImageUpdated(&container, tt.newImage)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err.Error())
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.updated, updated)
			if updated {
				assert.NotEqual(t, tt.originalImage, container.Image)
			} else {
				assert.Equal(t, tt.originalImage, container.Image)
			}

			if tt.expectedImage != "" {
				assert.Equal(t, tt.expectedImage, container.Image)
			}
		})
	}
}

func TestLog(t *testing.T) {
	var buf bytes.Buffer
	logger := ctrlruntimelog.New(func(o *ctrlruntimelog.Options) {
		o.DestWriter = &buf
	})

	t.Run("info logging works both for values and pointers to objects", func(t *testing.T) {
		t.Cleanup(func() { buf.Reset() })
		log.Info(logger, "message about gw")
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		log.Info(logger, "message about gw")
		require.NotContains(t, buf.String(), "unexpected type processed for")
	})

	t.Run("debug logging works both for values and pointers to objects", func(t *testing.T) {
		t.Cleanup(func() { buf.Reset() })
		log.Debug(logger, "message about gw")
		require.NotContains(t, buf.String(), "unexpected type processed for")
		log.Debug(logger, "message about gw")
		require.NotContains(t, buf.String(), "unexpected type processed for")
	})

	t.Run("trace logging works both for values and pointers to objects", func(t *testing.T) {
		t.Cleanup(func() { buf.Reset() })
		log.Trace(logger, "message about gw")
		require.NotContains(t, buf.String(), "unexpected type processed for")
		t.Logf("log: %s", buf.String())
		buf.Reset()
		log.Trace(logger, "message about gw")
		require.NotContains(t, buf.String(), "unexpected type processed for")
		t.Logf("log: %s", buf.String())
	})

	t.Run("logging works and prints correct fields", func(t *testing.T) {
		t.Cleanup(func() { buf.Reset() })
		log.Info(logger, "message about gw")
		entry := struct {
			Level string `json:"level,omitempty"`
			Msg   string `json:"msg,omitempty"`
		}{}
		require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
		assert.Equal(t, "message about gw", entry.Msg)
		assert.Equal(t, "info", entry.Level)
	})
}

func TestMaybeCreateCertificateSecret(t *testing.T) {
	createDataPlane := func(nn types.NamespacedName, opt ...func(dp *operatorv1beta1.DataPlane)) *operatorv1beta1.DataPlane {
		dp := &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
		}
		for _, o := range opt {
			o(dp)
		}
		return dp
	}

	WithUUID := func(u types.UID) func(dp *operatorv1beta1.DataPlane) {
		return func(dp *operatorv1beta1.DataPlane) {
			dp.UID = u
		}
	}

	type NN = types.NamespacedName

	for _, keyType := range []certificate.KeyType{certificate.RSA, certificate.ECDSA} {
		t.Run("TestMaybeCreateCertificateSecret for key type "+string(keyType), func(t *testing.T) {
			testCases := []struct {
				name                     string
				dataPlane                *operatorv1beta1.DataPlane
				subject                  string
				mtlsCASecretNN           NN
				additionalMatchingLabels client.MatchingLabels
				expectedResult           op.Result
				expectedError            error
				objectList               client.ObjectList
			}{
				{
					name:      "no certificate secret exists and gets created as expected",
					dataPlane: createDataPlane(NN{Name: "dp-1", Namespace: "ns"}),
					subject:   "test-subject",
					mtlsCASecretNN: NN{
						Name:      "test-mtls-secret",
						Namespace: "ns",
					},
					additionalMatchingLabels: nil,
					expectedResult:           op.Created,
					expectedError:            nil,
				},
				{
					name:      "existing secret certificate gets deleted and re-created with it doesn't have the expected contents",
					dataPlane: createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234"))),
					subject:   "test-subject",
					mtlsCASecretNN: NN{
						Name:      "test-mtls-secret",
						Namespace: "ns",
					},
					additionalMatchingLabels: nil,
					objectList: &corev1.SecretList{
						Items: []corev1.Secret{
							func() corev1.Secret {
								dp := createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234")))

								labels := k8sresources.GetManagedLabelForOwner(dp)
								return corev1.Secret{
									ObjectMeta: metav1.ObjectMeta{
										Name:      "secret-1",
										Namespace: "ns",
										Labels:    labels,
										OwnerReferences: []metav1.OwnerReference{
											{
												Kind:       "DataPlane",
												APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
												UID:        types.UID("1234"),
											},
										},
									},
								}
							}(),
						},
					},
					expectedResult: op.Created,
					expectedError:  nil,
				},
				{
					name:      "when more than 1 secret exists, secrets are reduced",
					dataPlane: createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234"))),
					subject:   "test-subject",
					mtlsCASecretNN: NN{
						Name:      "test-mtls-secret",
						Namespace: "ns",
					},
					additionalMatchingLabels: nil,
					objectList: &corev1.SecretList{
						Items: []corev1.Secret{
							func() corev1.Secret {
								dp := createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234")))

								labels := k8sresources.GetManagedLabelForOwner(dp)
								return corev1.Secret{
									ObjectMeta: metav1.ObjectMeta{
										Name:      "secret-1",
										Namespace: "ns",
										Labels:    labels,
										OwnerReferences: []metav1.OwnerReference{
											{
												Kind:       "DataPlane",
												APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
												UID:        types.UID("1234"),
											},
										},
									},
								}
							}(),
							func() corev1.Secret {
								dp := createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234")))

								labels := k8sresources.GetManagedLabelForOwner(dp)
								return corev1.Secret{
									ObjectMeta: metav1.ObjectMeta{
										Name:      "secret-2",
										Namespace: "ns",
										Labels:    labels,
										OwnerReferences: []metav1.OwnerReference{
											{
												Kind:       "DataPlane",
												APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
												UID:        types.UID("1234"),
											},
										},
									},
								}
							}(),
						},
					},
					expectedResult: op.Noop,
					expectedError:  errors.New("number of secrets reduced"),
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					ctx := t.Context()

					scheme := runtime.NewScheme()
					require.NoError(t, corev1.AddToScheme(scheme))
					require.NoError(t, certificatesv1.AddToScheme(scheme))
					require.NoError(t, operatorv1beta1.AddToScheme(scheme))

					builder := fakectrlruntimeclient.
						NewClientBuilder().
						WithScheme(scheme).
						WithObjects(tc.dataPlane)
					if tc.objectList != nil {
						builder.WithLists(tc.objectList)
					}
					fakeClient := builder.Build()

					cert, key := certificate.MustGenerateCertPEMFormat(
						certificate.WithCommonName("Kong Operator CA"),
						certificate.WithCATrue(),
						certificate.WithKeyType(keyType),
					)
					caSecret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      tc.mtlsCASecretNN.Name,
							Namespace: tc.mtlsCASecretNN.Namespace,
						},
						Data: map[string][]byte{
							"tls.crt": cert,
							"tls.key": key,
						},
					}
					require.NoError(t, fakeClient.Create(ctx, caSecret))

					res, secret, err := EnsureCertificate(
						ctx,
						tc.dataPlane,
						tc.subject,
						tc.mtlsCASecretNN,
						[]certificatesv1.KeyUsage{
							certificatesv1.UsageServerAuth,
						},
						fakeClient,
						tc.additionalMatchingLabels,
					)

					if tc.expectedError != nil {
						require.Error(t, err)
						require.EqualError(t, tc.expectedError, "number of secrets reduced")
						return
					}

					require.Equal(t, tc.expectedResult, res)
					require.Equal(t, caSecret.Data["tls.crt"], secret.Data["ca.crt"], "created secret 'ca.crt' should be equal to CA cert's 'tls.crt'")

					_, ok := secret.Data["tls.crt"]
					require.True(t, ok, "generated secret does not contain 'tls.crt'")

					key, ok = secret.Data["tls.key"]
					require.True(t, ok, "generated secret does not contain 'tls.key'")
					tlsKeyPemBlock, _ := pem.Decode(key)
					require.NotNil(t, tlsKeyPemBlock)
					_, algorithm, err := ParsePrivateKey(tlsKeyPemBlock)
					require.NoError(t, err)
					require.Contains(t, strings.ToLower(algorithm.String()), keyType, "generated private key algorithm is not as expected")
				})
			}
		})
	}

}

func TestParsePrivateKey(t *testing.T) {
	tests := []struct {
		name             string
		keyType          x509.PublicKeyAlgorithm
		expectedAlg      x509.SignatureAlgorithm
		expectedKeyType  any
		rsaFormatPKCS8   bool
		expectedErrorMsg string
	}{
		{
			name:            "valid ECDSA private key",
			keyType:         x509.ECDSA,
			expectedAlg:     x509.ECDSAWithSHA256,
			expectedKeyType: &ecdsa.PrivateKey{},
		},
		{
			name:            "valid RSA private key PKCS1 format",
			keyType:         x509.RSA,
			expectedAlg:     x509.SHA256WithRSA,
			expectedKeyType: &rsa.PrivateKey{},
		},
		{
			name:            "valid RSA private key PKCS8 format",
			keyType:         x509.RSA,
			rsaFormatPKCS8:  true,
			expectedAlg:     x509.SHA256WithRSA,
			expectedKeyType: &rsa.PrivateKey{},
		},
		{
			name:             "unsupported key type",
			keyType:          x509.DSA,
			expectedErrorMsg: "unsupported key type: DSA PRIVATE KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var privKeyBytes []byte
			var err error

			switch tt.keyType {
			case x509.ECDSA:
				privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)
				privKeyBytes, err = x509.MarshalECPrivateKey(privKey)
				require.NoError(t, err)
			case x509.RSA:
				privKey, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				if tt.rsaFormatPKCS8 {
					privKeyBytes, err = x509.MarshalPKCS8PrivateKey(privKey)
					require.NoError(t, err)
				} else {
					privKeyBytes = x509.MarshalPKCS1PrivateKey(privKey)
				}
			default:
				privKeyBytes = []byte{}
			}

			pemBlockType := tt.keyType.String() + " PRIVATE KEY"
			if tt.rsaFormatPKCS8 {
				pemBlockType = "PRIVATE KEY"
			}
			pemBlock := &pem.Block{
				Type:  pemBlockType,
				Bytes: privKeyBytes,
			}

			priv, alg, err := ParsePrivateKey(pemBlock)
			if tt.expectedErrorMsg != "" {
				require.Error(t, err)
				assert.Equal(t, tt.expectedErrorMsg, err.Error())
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedAlg, alg)
			assert.IsType(t, tt.expectedKeyType, priv)
		})
	}
}
