package crdsvalidation_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/crdsvalidation/common"
	"github.com/kong/kong-operator/v2/test/envtest"
)

func TestDBBackedHybridControlPlane(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := scheme.Get()
	cfg, ns := envtest.Setup(t, ctx, scheme)

	const secretRefRequiredMsg = "secretRef is required when type is 'secretRef'"

	// dbcp returns a valid DBBackedHybridControlPlane, optionally mutated.
	dbcp := func(mutate func(*operatorv1beta1.DBBackedHybridControlPlane)) *operatorv1beta1.DBBackedHybridControlPlane {
		obj := &operatorv1beta1.DBBackedHybridControlPlane{
			ObjectMeta: common.CommonObjectMeta(ns.Name),
			Spec: operatorv1beta1.DBBackedHybridControlPlaneSpec{
				DeploymentOptions: operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions{
					License: operatorv1beta1.DBBackedHybridControlPlaneLicense{
						Type: operatorv1beta1.DBBackedHybridControlPlaneLicenseTypeSecretRef,
						SecretRef: &operatorv1beta1.DBBackedHybridControlPlaneLicenseSecretRef{
							Name: "license",
							Key:  "license",
						},
					},
					Database: operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions{
						Host:         "postgres",
						Port:         5432,
						Username:     "kong",
						DatabaseName: "kong",
						Password: operatorv1beta1.DBBackedHybridControlPlaneDatabasePassword{
							Type: operatorv1beta1.DBBackedHybridControlPlaneDatabasePasswordTypeSecretRef,
							SecretRef: &operatorv1beta1.DBBackedHybridControlPlaneDatabasePasswordSecretRef{
								Name: "postgres",
								Key:  "password",
							},
						},
					},
				},
			},
		}
		if mutate != nil {
			mutate(obj)
		}
		return obj
	}
	// object returns a valid DBBackedHybridControlPlane as unstructured, so that
	// test cases can remove fields that the typed object always serializes.
	object := func(mutate func(map[string]any)) *unstructured.Unstructured {
		t.Helper()
		data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dbcp(nil))
		require.NoError(t, err)
		mutate(data)
		obj := &unstructured.Unstructured{Object: data}
		obj.SetGroupVersionKind(operatorv1beta1.SchemeGroupVersion.WithKind("DBBackedHybridControlPlane"))
		return obj
	}
	removeField := func(fields ...string) func(map[string]any) {
		return func(data map[string]any) {
			unstructured.RemoveNestedField(data, fields...)
		}
	}
	stringMap := func(n int) map[string]string {
		m := make(map[string]string, n)
		for i := range n {
			m[fmt.Sprintf("key-%d", i)] = "value"
		}
		return m
	}

	t.Run("deployment options", func(t *testing.T) {
		withDeployment := func(mutate func(*operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions)) *operatorv1beta1.DBBackedHybridControlPlane {
			return dbcp(func(obj *operatorv1beta1.DBBackedHybridControlPlane) { mutate(&obj.Spec.DeploymentOptions) })
		}
		horizontalScaling := &operatorv1beta1.Scaling{
			HorizontalScaling: &commonv1alpha1.HorizontalScaling{MaxReplicas: 3},
		}

		common.TestCasesGroup[*unstructured.Unstructured]{
			{
				Name: "container without a name - invalid",
				TestObject: object(func(data map[string]any) {
					require.NoError(t, unstructured.SetNestedSlice(data, []any{
						map[string]any{"image": "kong/kong-gateway:3.16"},
					}, "spec", "deployment", "podTemplateSpec", "spec", "containers"))
				}),
				ExpectedErrorMessage: new("spec.deployment.podTemplateSpec.spec.containers[0].name: Required value"),
			},
		}.RunWithConfig(t, cfg, scheme)

		common.TestCasesGroup[*operatorv1beta1.DBBackedHybridControlPlane]{
			{
				Name:       "no podTemplateSpec - valid",
				TestObject: dbcp(nil),
			},
			{
				Name: "podTemplateSpec with proxy container - valid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.PodTemplateSpec = &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "proxy",
									Image: "kong/kong-gateway:3.16",
									Env: []corev1.EnvVar{
										{Name: "KONG_LOG_LEVEL", Value: "debug"},
									},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("250m"),
											corev1.ResourceMemory: resource.MustParse("64Mi"),
										},
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("1"),
											corev1.ResourceMemory: resource.MustParse("1Gi"),
										},
									},
								},
							},
						},
					}
				}),
			},
			{
				Name: "replicas - valid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.Replicas = new(int32(2))
				}),
			},
			{
				Name: "horizontal scaling - valid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.Scaling = horizontalScaling
				}),
			},
			{
				Name: "replicas and scaling - invalid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.Replicas = new(int32(2))
					d.Scaling = horizontalScaling
				}),
				ExpectedErrorMessage: new("Using both replicas and scaling fields is not allowed."),
			},
			{
				Name:       "replicas can be updated - valid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) { d.Replicas = new(int32(1)) }),
				Update: func(obj *operatorv1beta1.DBBackedHybridControlPlane) {
					obj.Spec.DeploymentOptions.Replicas = new(int32(3))
				},
			},
			{
				Name: "64 annotations and labels - valid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.Annotations = stringMap(64)
					d.Labels = stringMap(64)
				}),
			},
			{
				Name: "65 annotations - invalid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.Annotations = stringMap(65)
				}),
				ExpectedErrorMessage: new("spec.deployment.annotations: Too many"),
			},
			{
				Name: "65 labels - invalid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.Labels = stringMap(65)
				}),
				ExpectedErrorMessage: new("spec.deployment.labels: Too many"),
			},
			{
				Name:       "hardened defaults to disabled",
				TestObject: dbcp(nil),
				Assert: func(t *testing.T, obj *operatorv1beta1.DBBackedHybridControlPlane) {
					assert.Equal(t, commonv1alpha1.HardeningStateDisabled, obj.Spec.DeploymentOptions.Hardened)
				},
			},
			{
				Name: "hardened enabled - valid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.Hardened = commonv1alpha1.HardeningStateEnabled
				}),
			},
			{
				Name: "unsupported hardened value - invalid",
				TestObject: withDeployment(func(d *operatorv1beta1.DBBackedHybridControlPlaneDeploymentOptions) {
					d.Hardened = "bogus"
				}),
				ExpectedErrorMessage: new(`Unsupported value: "bogus"`),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("license", func(t *testing.T) {
		withLicense := func(mutate func(*operatorv1beta1.DBBackedHybridControlPlaneLicense)) *operatorv1beta1.DBBackedHybridControlPlane {
			return dbcp(func(obj *operatorv1beta1.DBBackedHybridControlPlane) { mutate(&obj.Spec.DeploymentOptions.License) })
		}

		common.TestCasesGroup[*unstructured.Unstructured]{
			{
				Name:                 "missing license - invalid",
				TestObject:           object(removeField("spec", "deployment", "license")),
				ExpectedErrorMessage: new("spec.deployment.license: Required value"),
			},
			{
				Name:                 "missing type - invalid",
				TestObject:           object(removeField("spec", "deployment", "license", "type")),
				ExpectedErrorMessage: new("spec.deployment.license.type: Required value"),
			},
			{
				Name:                 "secretRef type without secretRef - invalid",
				TestObject:           object(removeField("spec", "deployment", "license", "secretRef")),
				ExpectedErrorMessage: new(secretRefRequiredMsg),
			},
			{
				Name:                 "missing secret name - invalid",
				TestObject:           object(removeField("spec", "deployment", "license", "secretRef", "name")),
				ExpectedErrorMessage: new("spec.deployment.license.secretRef.name: Required value"),
			},
			{
				Name:                 "missing secret key - invalid",
				TestObject:           object(removeField("spec", "deployment", "license", "secretRef", "key")),
				ExpectedErrorMessage: new("spec.deployment.license.secretRef.key: Required value"),
			},
		}.RunWithConfig(t, cfg, scheme)

		common.TestCasesGroup[*operatorv1beta1.DBBackedHybridControlPlane]{
			{
				Name:       "license secretRef - valid",
				TestObject: dbcp(nil),
			},
			{
				Name: "unsupported type - invalid",
				TestObject: withLicense(func(l *operatorv1beta1.DBBackedHybridControlPlaneLicense) {
					l.Type = "configMap"
				}),
				ExpectedErrorMessage: new(`Unsupported value: "configMap"`),
			},
			{
				Name: "empty secret name - invalid",
				TestObject: withLicense(func(l *operatorv1beta1.DBBackedHybridControlPlaneLicense) {
					l.SecretRef.Name = ""
				}),
				ExpectedErrorMessage: new("spec.deployment.license.secretRef.name in body should be at least 1 chars long"),
			},
			{
				Name: "empty secret key - invalid",
				TestObject: withLicense(func(l *operatorv1beta1.DBBackedHybridControlPlaneLicense) {
					l.SecretRef.Key = ""
				}),
				ExpectedErrorMessage: new("spec.deployment.license.secretRef.key in body should be at least 1 chars long"),
			},
			{
				Name:       "secret reference can be updated - valid",
				TestObject: dbcp(nil),
				Update: func(obj *operatorv1beta1.DBBackedHybridControlPlane) {
					obj.Spec.DeploymentOptions.License.SecretRef = &operatorv1beta1.DBBackedHybridControlPlaneLicenseSecretRef{
						Name: "replacement-license",
						Key:  "license.jws",
					}
				},
			},
			{
				Name:       "secret reference cannot be removed on update - invalid",
				TestObject: dbcp(nil),
				Update: func(obj *operatorv1beta1.DBBackedHybridControlPlane) {
					obj.Spec.DeploymentOptions.License.SecretRef = nil
				},
				ExpectedUpdateErrorMessage: new(secretRefRequiredMsg),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("database", func(t *testing.T) {
		withDatabase := func(mutate func(*operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions)) *operatorv1beta1.DBBackedHybridControlPlane {
			return dbcp(func(obj *operatorv1beta1.DBBackedHybridControlPlane) { mutate(&obj.Spec.DeploymentOptions.Database) })
		}
		withTLS := func(mutate func(*operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS)) *operatorv1beta1.DBBackedHybridControlPlane {
			return withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
				db.TLS = &operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS{
					Required: operatorv1beta1.DatabaseTLSRequiredStateRequired,
					Verify:   operatorv1beta1.DatabaseTLSVerifyEnabled,
				}
				if mutate != nil {
					mutate(db.TLS)
				}
			})
		}
		secretRefs := func(n int) []corev1.SecretReference {
			refs := make([]corev1.SecretReference, 0, n)
			for i := range n {
				refs = append(refs, corev1.SecretReference{Name: fmt.Sprintf("ca-%d", i)})
			}
			return refs
		}
		tlsWithDefaultsRemoved := func(data map[string]any) {
			require.NoError(t, unstructured.SetNestedMap(data, map[string]any{
				"required": "required",
				"verify":   "enabled",
			}, "spec", "deployment", "database", "tls"))
		}

		common.TestCasesGroup[*unstructured.Unstructured]{
			{
				Name:                 "missing database - invalid",
				TestObject:           object(removeField("spec", "deployment", "database")),
				ExpectedErrorMessage: new("spec.deployment.database: Required value"),
			},
			{
				Name:                 "missing host - invalid",
				TestObject:           object(removeField("spec", "deployment", "database", "host")),
				ExpectedErrorMessage: new("spec.deployment.database.host: Required value"),
			},
			{
				Name:                 "missing username - invalid",
				TestObject:           object(removeField("spec", "deployment", "database", "username")),
				ExpectedErrorMessage: new("spec.deployment.database.username: Required value"),
			},
			{
				Name:                 "missing password - invalid",
				TestObject:           object(removeField("spec", "deployment", "database", "password")),
				ExpectedErrorMessage: new("spec.deployment.database.password: Required value"),
			},
			{
				Name:                 "missing password type - invalid",
				TestObject:           object(removeField("spec", "deployment", "database", "password", "type")),
				ExpectedErrorMessage: new("spec.deployment.database.password.type: Required value"),
			},
			{
				Name:                 "password secretRef type without secretRef - invalid",
				TestObject:           object(removeField("spec", "deployment", "database", "password", "secretRef")),
				ExpectedErrorMessage: new(secretRefRequiredMsg),
			},
			{
				Name: "port and databaseName default to 5432 and kong",
				TestObject: object(func(data map[string]any) {
					unstructured.RemoveNestedField(data, "spec", "deployment", "database", "port")
					unstructured.RemoveNestedField(data, "spec", "deployment", "database", "databaseName")
				}),
				Assert: func(t *testing.T, obj *unstructured.Unstructured) {
					port, found, err := unstructured.NestedInt64(obj.Object, "spec", "deployment", "database", "port")
					require.NoError(t, err)
					require.True(t, found)
					assert.Equal(t, int64(5432), port)
					name, found, err := unstructured.NestedString(obj.Object, "spec", "deployment", "database", "databaseName")
					require.NoError(t, err)
					require.True(t, found)
					assert.Equal(t, "kong", name)
				},
			},
			{
				Name: "missing tls required - invalid",
				TestObject: object(func(data map[string]any) {
					tlsWithDefaultsRemoved(data)
					unstructured.RemoveNestedField(data, "spec", "deployment", "database", "tls", "required")
				}),
				ExpectedErrorMessage: new("spec.deployment.database.tls.required: Required value"),
			},
			{
				Name: "missing tls verify - invalid",
				TestObject: object(func(data map[string]any) {
					tlsWithDefaultsRemoved(data)
					unstructured.RemoveNestedField(data, "spec", "deployment", "database", "tls", "verify")
				}),
				ExpectedErrorMessage: new("spec.deployment.database.tls.verify: Required value"),
			},
			{
				// An explicit 0 can only be sent through unstructured data: the typed field is omitted when it is 0.
				Name: "tls verifyDepth set to 0 - valid",
				TestObject: object(func(data map[string]any) {
					tlsWithDefaultsRemoved(data)
					require.NoError(t, unstructured.SetNestedField(data, int64(0), "spec", "deployment", "database", "tls", "verifyDepth"))
				}),
				Assert: func(t *testing.T, obj *unstructured.Unstructured) {
					depth, found, err := unstructured.NestedInt64(obj.Object, "spec", "deployment", "database", "tls", "verifyDepth")
					require.NoError(t, err)
					require.True(t, found)
					assert.Equal(t, int64(0), depth)
				},
			},
		}.RunWithConfig(t, cfg, scheme)

		common.TestCasesGroup[*operatorv1beta1.DBBackedHybridControlPlane]{
			{
				Name: "empty host - invalid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Host = ""
				}),
				ExpectedErrorMessage: new("spec.deployment.database.host in body should be at least 1 chars long"),
			},
			{
				Name: "empty username - invalid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Username = ""
				}),
				ExpectedErrorMessage: new("spec.deployment.database.username in body should be at least 1 chars long"),
			},
			{
				Name: "port 0 - invalid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Port = 0
				}),
				ExpectedErrorMessage: new("spec.deployment.database.port in body should be greater than or equal to 1"),
			},
			{
				Name: "port 1 - valid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Port = 1
				}),
			},
			{
				Name: "port 65535 - valid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Port = 65535
				}),
			},
			{
				Name: "port 65536 - invalid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Port = 65536
				}),
				ExpectedErrorMessage: new("spec.deployment.database.port in body should be less than or equal to 65535"),
			},
			{
				Name: "unsupported password type - invalid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Password.Type = "plain"
				}),
				ExpectedErrorMessage: new(`Unsupported value: "plain"`),
			},
			{
				Name: "empty password secret name - invalid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Password.SecretRef.Name = ""
				}),
				ExpectedErrorMessage: new("spec.deployment.database.password.secretRef.name in body should be at least 1 chars long"),
			},
			{
				Name: "empty password secret key - invalid",
				TestObject: withDatabase(func(db *operatorv1beta1.DBBackedHybridControlPlaneDatabaseConnectionOptions) {
					db.Password.SecretRef.Key = ""
				}),
				ExpectedErrorMessage: new("spec.deployment.database.password.secretRef.key in body should be at least 1 chars long"),
			},
			{
				Name:       "database connection can be updated - valid",
				TestObject: dbcp(nil),
				Update: func(obj *operatorv1beta1.DBBackedHybridControlPlane) {
					obj.Spec.DeploymentOptions.Database.Host = "postgres-replacement"
					obj.Spec.DeploymentOptions.Database.Port = 6432
				},
			},
			{
				Name:       "minimal tls - valid, defaults version and verifyDepth",
				TestObject: withTLS(nil),
				Assert: func(t *testing.T, obj *operatorv1beta1.DBBackedHybridControlPlane) {
					tls := obj.Spec.DeploymentOptions.Database.TLS
					require.NotNil(t, tls)
					assert.Equal(t, operatorv1beta1.DatabaseTLSVersionTLSv1_2, tls.Version)
					assert.Equal(t, 5, tls.VerifyDepth)
				},
			},
			{
				Name: "tls not required and not verified - valid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.Required = operatorv1beta1.DatabaseTLSRequiredStateNotRequired
					tls.Verify = operatorv1beta1.DatabaseTLSVerifyDisabled
				}),
			},
			{
				Name: "unsupported tls required value - invalid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.Required = "maybe"
				}),
				ExpectedErrorMessage: new(`Unsupported value: "maybe"`),
			},
			{
				Name: "unsupported tls verify value - invalid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.Verify = "yes"
				}),
				ExpectedErrorMessage: new(`Unsupported value: "yes"`),
			},
			{
				Name: "tls version tlsv1_1 - valid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.Version = operatorv1beta1.DatabaseTLSVersionTLSv1_1
				}),
			},
			{
				Name: "tls version tlsv1_3 - valid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.Version = operatorv1beta1.DatabaseTLSVersionTLSv1_3
				}),
			},
			{
				Name: "tls version any - valid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.Version = operatorv1beta1.DatabaseTLSVersionAny
				}),
			},
			{
				Name: "unsupported tls version - invalid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.Version = "tlsv1_0"
				}),
				ExpectedErrorMessage: new(`Unsupported value: "tlsv1_0"`),
			},
			{
				Name: "tls verifyDepth 10 - valid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.VerifyDepth = 10
				}),
			},
			{
				Name: "tls verifyDepth 11 - invalid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.VerifyDepth = 11
				}),
				ExpectedErrorMessage: new("spec.deployment.database.tls.verifyDepth in body should be less than or equal to 10"),
			},
			{
				Name: "tls verifyDepth -1 - invalid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.VerifyDepth = -1
				}),
				ExpectedErrorMessage: new("spec.deployment.database.tls.verifyDepth in body should be greater than or equal to 0"),
			},
			{
				Name: "10 trusted certificates - valid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.TrustedCertificates = secretRefs(10)
				}),
			},
			{
				Name: "11 trusted certificates - invalid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.TrustedCertificates = secretRefs(11)
				}),
				ExpectedErrorMessage: new("spec.deployment.database.tls.trustedCertificates: Too many"),
			},
			{
				Name: "client certificate - valid",
				TestObject: withTLS(func(tls *operatorv1beta1.DBBackedHybridControlPlaneDatabaseTLS) {
					tls.ClientCertificate = &corev1.SecretReference{Name: "client-cert"}
				}),
			},
		}.RunWithConfig(t, cfg, scheme)
	})

	t.Run("admin GUI service", func(t *testing.T) {
		withAdminGUI := func(mutate func(*operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions)) *operatorv1beta1.DBBackedHybridControlPlane {
			return dbcp(func(obj *operatorv1beta1.DBBackedHybridControlPlane) {
				obj.Spec.NetworkOptions.ServiceOptions.AdminUI = &operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions{}
				if mutate != nil {
					mutate(obj.Spec.NetworkOptions.ServiceOptions.AdminUI)
				}
			})
		}

		common.TestCasesGroup[*operatorv1beta1.DBBackedHybridControlPlane]{
			{
				Name:       "no admin GUI options - valid",
				TestObject: dbcp(nil),
			},
			{
				Name:       "empty admin GUI options - valid, defaults are applied",
				TestObject: withAdminGUI(nil),
				Assert: func(t *testing.T, obj *operatorv1beta1.DBBackedHybridControlPlane) {
					adminGUI := obj.Spec.NetworkOptions.ServiceOptions.AdminUI
					require.NotNil(t, adminGUI)
					assert.Equal(t, operatorv1beta1.AdminGUIStateEnabled, adminGUI.Enabled)
					assert.Equal(t, corev1.ServiceTypeLoadBalancer, adminGUI.Type)
					assert.Equal(t, int32(8445), adminGUI.Port)
				},
			},
			{
				Name: "disabled - valid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Enabled = operatorv1beta1.AdminGUIStateDisabled
				}),
			},
			{
				Name: "unsupported enabled value - invalid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Enabled = "on"
				}),
				ExpectedErrorMessage: new(`Unsupported value: "on"`),
			},
			{
				Name: "ClusterIP service type - valid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Type = corev1.ServiceTypeClusterIP
				}),
			},
			{
				Name: "NodePort service type - valid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Type = corev1.ServiceTypeNodePort
				}),
			},
			{
				Name: "ExternalName service type - invalid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Type = corev1.ServiceTypeExternalName
				}),
				ExpectedErrorMessage: new(`Unsupported value: "ExternalName"`),
			},
			{
				Name: "port 1 - valid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Port = 1
				}),
			},
			{
				Name: "port 65535 - valid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Port = 65535
				}),
			},
			{
				Name: "port 65536 - invalid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Port = 65536
				}),
				ExpectedErrorMessage: new("spec.network.services.adminGUI.port in body should be less than or equal to 65535"),
			},
			{
				Name: "negative port - invalid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Port = -1
				}),
				ExpectedErrorMessage: new("spec.network.services.adminGUI.port in body should be greater than or equal to 1"),
			},
			{
				Name: "service type can be updated - valid",
				TestObject: withAdminGUI(func(o *operatorv1beta1.DBBackedHybridControlPlaneServiceAdminGUIOptions) {
					o.Type = corev1.ServiceTypeLoadBalancer
				}),
				Update: func(obj *operatorv1beta1.DBBackedHybridControlPlane) {
					obj.Spec.NetworkOptions.ServiceOptions.AdminUI.Type = corev1.ServiceTypeClusterIP
				},
			},
		}.RunWithConfig(t, cfg, scheme)
	})
}
