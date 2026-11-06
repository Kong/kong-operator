package envtest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/gwadmin"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	kgomanager "github.com/kong/kong-operator/v2/modules/manager"
	mgrconfig "github.com/kong/kong-operator/v2/modules/manager/config"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// gwAdminSecretLabel is the label the operator's Secret cache selects on, as
// in production.
const gwAdminSecretLabel = mgrconfig.DefaultSecretLabelSelector

// gwAdminRecoverWait bounds the recovery from a conflict with an object the
// operator doesn't see change: it relies on the cache resync (10s) or the
// conflict requeue (1m).
const gwAdminRecoverWait = 90 * time.Second

// startGWAdminReconciler starts the GWAdmin controller in a new envtest
// environment, with a manager whose Secret cache only holds the Secrets
// carrying the secret label selector and whose Pod cache only holds the
// gw-admin pods, as in production. now, when not nil, is the clock of the
// reconciler. It returns a client reading through the manager's cache, as the
// reconciler does, a reader reading the API server directly for the
// assertions, the namespace to work in, and the operator namespace, holding
// the Secrets only Kong Operator uses.
func startGWAdminReconciler(
	t *testing.T, ctx context.Context, now func() time.Time,
) (client.Client, client.Reader, string, string) {
	t.Helper()

	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	setupClient, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	operatorNS := CreateNamespace(ctx, t, setupClient)
	secretCache, err := kgomanager.SecretCacheByObject(gwAdminSecretLabel)
	require.NoError(t, err)
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get(), WithManagerOptions(func(o *manager.Options) {
		o.Cache.ByObject = secretCache
	}))

	// GWAdmin only applies built-in types through SSA.
	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, map[string]struct{}{})
	require.NoError(t, err)

	StartReconcilers(ctx, t, mgr, logs, &gwadmin.Reconciler{
		Client:              mgr.GetClient(),
		ClusterDomain:       "cluster.local",
		SecretLabelSelector: gwAdminSecretLabel,
		OperatorNamespace:   operatorNS.Name,
		TypeConverter:       ssaProvider,
		Now:                 now,
	})
	return mgr.GetClient(), mgr.GetAPIReader(), ns.Name, operatorNS.Name
}

func TestGWAdminReconciler(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	// cl reads through the filtered cache, as the reconciler does; reader
	// reads the API server directly, and sees the unlabeled Secrets too.
	cl, reader, namespace, operatorNamespace := startGWAdminReconciler(t, ctx, nil)

	condition := func(ct *assert.CollectT, gw *operatorv1alpha1.GWAdmin, conditionType string) *metav1.Condition {
		return gwAdminCondition(ctx, ct, cl, gw, conditionType)
	}
	requireCondition := func(
		t *testing.T, gw *operatorv1alpha1.GWAdmin, conditionType string, status metav1.ConditionStatus, reason string, wait time.Duration,
	) {
		t.Helper()
		requireGWAdminCondition(ctx, t, cl, gw, conditionType, status, reason, wait)
	}

	t.Run("zero config provisions certificates, Services and the Deployment", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{
			Name: "gw-default", Namespace: namespace,
			Spec: operatorv1alpha1.GWAdminSpec{Deployment: &operatorv1alpha1.GWAdminDeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: consts.GWAdminContainerName,
					Env: []corev1.EnvVar{
						{Name: consts.GWAdminAPITLSCertFileEnvVar, Value: "/overridden"},
						{Name: "EXTRA", Value: "kept"},
					},
				}}}},
			}},
		}
		require.NoError(t, cl.Create(ctx, gw))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned)
			if cond == nil {
				return
			}
			assert.Equal(ct, metav1.ConditionTrue, cond.Status)
			if assert.NotNil(ct, gw.Status.CP) {
				assert.Equal(ct, "https://gw-default-cp."+namespace+".svc.cluster.local:8099", gw.Status.CP.ServerURL)
			}
		}, waitTime, tickTime)

		// Three certificate authorities, all in the operator namespace: the
		// published cp client CA, the API CA and the cp server CA.
		apiCA, cpServerCA := gwadmin.APICASecretName(gw), gwadmin.CPServerCASecretName(gw)
		operatorClient := gwadminref.OperatorClientTLSSecretName(gw)
		clientCA := gwadminref.CPClientCASecretName(gw)
		signers := map[string]*x509.Certificate{
			clientCA:   firstCertificate(t, getSecret(t, ctx, cl, operatorNamespace, clientCA).Data[corev1.TLSCertKey]),
			apiCA:      firstCertificate(t, getSecret(t, ctx, cl, operatorNamespace, apiCA).Data[corev1.TLSCertKey]),
			cpServerCA: firstCertificate(t, getSecret(t, ctx, cl, operatorNamespace, cpServerCA).Data[corev1.TLSCertKey]),
		}
		require.False(t, signers[clientCA].Equal(signers[apiCA]))
		require.False(t, signers[clientCA].Equal(signers[cpServerCA]))
		require.False(t, signers[apiCA].Equal(signers[cpServerCA]))
		for name, signer := range map[string]string{
			"gw-default-cp-client-ca-bundle": "",
			"gw-default-cp-server-ca-bundle": "",
			"gw-default-api-ca-bundle":       "",
			"gw-default-api-tls":             apiCA,
			"gw-default-cp-tls":              cpServerCA,
		} {
			s := getSecret(t, ctx, cl, namespace, name)
			assert.True(t, metav1.IsControlledBy(s, gw), name)
			assert.Equal(t, consts.SecretProvisioningAutomaticLabelValue, s.Labels[consts.SecretProvisioningLabelKey], name)
			assert.Equal(t, "true", s.Labels[gwAdminSecretLabel], name)
			if signer != "" {
				require.NoError(t, firstCertificate(t, s.Data[corev1.TLSCertKey]).CheckSignatureFrom(signers[signer]), name)
			}
		}
		for name, signer := range map[string]string{
			apiCA:          apiCA,
			cpServerCA:     cpServerCA,
			operatorClient: apiCA,
			clientCA:       clientCA,
		} {
			s := getSecret(t, ctx, cl, operatorNamespace, name)
			assert.Empty(t, s.OwnerReferences, name)
			assert.Equal(t, string(gw.UID), s.Labels[consts.GWAdminUIDLabel], name)
			assert.Equal(t, gw.Namespace, s.Labels[consts.GatewayOperatorManagedByNamespaceLabel], name)
			assert.Equal(t, gw.Name, s.Labels[consts.GatewayOperatorManagedByNameLabel], name)
			assert.Equal(t, "true", s.Labels[gwAdminSecretLabel], name)
			require.NoError(t, firstCertificate(t, s.Data[corev1.TLSCertKey]).CheckSignatureFrom(signers[signer]), name)
		}
		for bundle, signer := range map[string]string{
			"gw-default-cp-client-ca-bundle": clientCA,
			"gw-default-cp-server-ca-bundle": cpServerCA,
			"gw-default-api-ca-bundle":       apiCA,
		} {
			only, err := parseFirstCertificate(getSecret(t, ctx, cl, namespace, bundle).Data["ca.crt"])
			require.NoError(t, err)
			assert.True(t, only.Equal(signers[signer]), bundle)
			assert.Equal(t, 1, countCertificates(getSecret(t, ctx, cl, namespace, bundle).Data["ca.crt"]), bundle)
		}
		// The GWAdmin namespace holds no key only Kong Operator needs: no
		// certificate authority, the cp client CA included.
		for _, name := range []string{"gw-default-api-ca", "gw-default-operator-client-tls", "gw-default-cp-ca", "gw-default-cp-ca-next"} {
			assert.True(t, apierrors.IsNotFound(reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &corev1.Secret{})), name)
		}
		inNamespace := &corev1.SecretList{}
		require.NoError(t, reader.List(ctx, inNamespace, client.InNamespace(namespace)))
		for _, s := range inNamespace.Items {
			if !metav1.IsControlledBy(&s, gw) {
				continue
			}
			if c, err := parseFirstCertificate(s.Data[corev1.TLSCertKey]); err == nil {
				assert.False(t, c.IsCA && len(s.Data[corev1.TLSPrivateKeyKey]) > 0, "Secret %s holds a CA key", s.Name)
			}
		}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw))
		assert.Contains(t, gw.Finalizers, consts.GWAdminCleanupFinalizer)
		if assert.NotNil(t, gw.Status.TLS) {
			assert.Equal(t, "gw-default-cp-server-ca-bundle", gw.Status.TLS.CPServerCABundleSecretName)
			assert.Equal(t, "gw-default-api-ca-bundle", gw.Status.TLS.APICABundleSecretName)
		}

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			api := &corev1.Service{}
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "gw-default-api"}, api)) {
				assert.True(ct, api.Spec.PublishNotReadyAddresses)
				assert.True(ct, metav1.IsControlledBy(api, gw))
			}
			cp := &corev1.Service{}
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "gw-default-cp"}, cp)) {
				assert.True(ct, metav1.IsControlledBy(cp, gw))
			}
		}, waitTime, tickTime)

		var deployment appsv1.Deployment
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), &deployment))
		}, waitTime, tickTime)
		c := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.GWAdminContainerName)
		require.NotNil(t, c)
		env := map[string]string{}
		for _, e := range c.Env {
			env[e.Name] = e.Value
		}
		assert.Equal(t, consts.GWAdminAPITLSMountPath+"/tls.crt", env[consts.GWAdminAPITLSCertFileEnvVar], "the TLS wiring wins over the overlay")
		assert.Equal(t, "kept", env["EXTRA"])
		assert.NotEmpty(t, deployment.Spec.Template.Annotations[consts.GWAdminTLSChecksumAnnotation])
		assert.NotEmpty(t, deployment.Spec.Template.Annotations[consts.GWAdminTrustChecksumAnnotation])
		if assert.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken) {
			assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken, "gw-admin gets no token")
		}

		// Not Ready until the Deployment observed its spec: envtest runs no
		// Deployment controller, so it never does on its own.
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeReady,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady, waitTime)

		// Ready once the Deployment observed its spec, without waiting for
		// the pods: they only turn Ready once provisioned.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeReady); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
		}, waitTime, tickTime)

		// A spec change makes it wait for the Deployment again.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
				return
			}
			gw.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].Env[1].Value = "changed"
			assert.NoError(ct, cl.Update(ctx, gw))
		}, waitTime, tickTime)
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeReady,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady, waitTime)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := &appsv1.Deployment{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), d)) {
				return
			}
			c := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.GWAdminContainerName)
			if !assert.NotNil(ct, c) {
				return
			}
			assert.True(ct, slices.Contains(c.Env, corev1.EnvVar{Name: "EXTRA", Value: "changed"}), "the spec change reaches the Deployment")
		}, waitTime, tickTime)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeReady); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
		}, waitTime, tickTime)
	})

	t.Run("horizontal scaling creates an HPA and seeds the replicas with minReplicas", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{
			Name: "gw-hpa", Namespace: namespace,
			Spec: operatorv1alpha1.GWAdminSpec{Deployment: &operatorv1alpha1.GWAdminDeploymentOptions{
				Scaling: &operatorv1alpha1.GWAdminScaling{HorizontalScaling: &commonv1alpha1.HorizontalScaling{
					MinReplicas: new(int32(2)),
					MaxReplicas: 4,
				}},
			}},
		}
		require.NoError(t, cl.Create(ctx, gw))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var hpa autoscalingv2.HorizontalPodAutoscaler
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), &hpa)) {
				return
			}
			assert.Equal(ct, int32(4), hpa.Spec.MaxReplicas)
			if assert.NotNil(ct, hpa.Spec.MinReplicas) {
				assert.Equal(ct, int32(2), *hpa.Spec.MinReplicas)
			}
			assert.Equal(ct, gw.Name, hpa.Spec.ScaleTargetRef.Name)
		}, waitTime, tickTime)

		// The Deployment is seeded with minReplicas so that it scales up
		// before the first evaluation of the HPA.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := &appsv1.Deployment{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), d)) {
				return
			}
			if assert.NotNil(ct, d.Spec.Replicas) {
				assert.Equal(ct, int32(2), *d.Spec.Replicas)
			}
		}, waitTime, tickTime)
	})

	t.Run("an HPA scale-up above minReplicas is kept", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{
			Name: "gw-hpa-scaled", Namespace: namespace,
			Spec: operatorv1alpha1.GWAdminSpec{Deployment: &operatorv1alpha1.GWAdminDeploymentOptions{
				Scaling: &operatorv1alpha1.GWAdminScaling{HorizontalScaling: &commonv1alpha1.HorizontalScaling{
					MinReplicas: new(int32(2)),
					MaxReplicas: 4,
				}},
			}},
		}
		require.NoError(t, cl.Create(ctx, gw))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := &appsv1.Deployment{}
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), d)) && assert.NotNil(ct, d.Spec.Replicas) {
				assert.Equal(ct, int32(2), *d.Spec.Replicas)
			}
		}, waitTime, tickTime)

		// The HPA scales the Deployment up: the operator must not scale it
		// back down to minReplicas, also when it applies the Deployment again.
		scaleLikeHPA(t, ctx, cl, client.ObjectKeyFromObject(gw), 4)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
				return
			}
			gw.Spec.Deployment.Labels = map[string]string{"touched": "true"}
			assert.NoError(ct, cl.Update(ctx, gw))
		}, waitTime, tickTime)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := &appsv1.Deployment{}
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), d)) {
				assert.Equal(ct, "true", d.Labels["touched"])
			}
		}, waitTime, tickTime)
		require.Never(t, func() bool {
			d := &appsv1.Deployment{}
			if err := cl.Get(ctx, client.ObjectKeyFromObject(gw), d); err != nil {
				return false
			}
			return d.Spec.Replicas == nil || *d.Spec.Replicas != 4
		}, 3*time.Second, tickTime, "the operator must not reset the replicas set by the HPA")
	})

	t.Run("horizontal scaling without minReplicas seeds 1 replica and leaves the scale to the HPA", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{
			Name: "gw-hpa-nomin", Namespace: namespace,
			Spec: operatorv1alpha1.GWAdminSpec{Deployment: &operatorv1alpha1.GWAdminDeploymentOptions{
				Scaling: &operatorv1alpha1.GWAdminScaling{HorizontalScaling: &commonv1alpha1.HorizontalScaling{
					MaxReplicas: 3,
				}},
			}},
		}
		require.NoError(t, cl.Create(ctx, gw))

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			var hpa autoscalingv2.HorizontalPodAutoscaler
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), &hpa)) {
				assert.Equal(ct, int32(3), hpa.Spec.MaxReplicas)
			}
		}, waitTime, tickTime)

		// The Deployment is seeded with the HPA default minReplicas, 1.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := &appsv1.Deployment{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), d)) {
				return
			}
			owns, err := ownsField(d.ManagedFields, controllerpkgssa.FieldManager, "f:spec", "f:replicas")
			if assert.NoError(ct, err) {
				assert.True(ct, owns, "the operator seeds spec.replicas")
			}
			if assert.NotNil(ct, d.Spec.Replicas) {
				assert.Equal(ct, int32(1), *d.Spec.Replicas)
			}
		}, waitTime, tickTime)

		// The HPA scales the Deployment: the operator leaves it alone.
		scaleLikeHPA(t, ctx, cl, client.ObjectKeyFromObject(gw), 3)
		// Reconcile again, e.g. on a change of the GWAdmin.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
				return
			}
			gw.Spec.Deployment.Labels = map[string]string{"touched": "true"}
			assert.NoError(ct, cl.Update(ctx, gw))
		}, waitTime, tickTime)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := &appsv1.Deployment{}
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), d)) {
				assert.Equal(ct, "true", d.Labels["touched"])
			}
		}, waitTime, tickTime)
		require.Never(t, func() bool {
			d := &appsv1.Deployment{}
			if err := cl.Get(ctx, client.ObjectKeyFromObject(gw), d); err != nil {
				return false
			}
			return d.Spec.Replicas == nil || *d.Spec.Replicas != 3
		}, 3*time.Second, tickTime, "the operator must not reset the replicas set by the HPA")
	})

	t.Run("a labeled foreign Secret name conflict is reported and recovers once the Secret is gone", func(t *testing.T) {
		t.Parallel()

		foreign := &corev1.Secret{
			Name:       "gw-conflict-api-tls",
			Namespace:  namespace,
			Labels:     map[string]string{gwAdminSecretLabel: "true"},
			StringData: map[string]string{"owner": "someone-else"},
		}
		require.NoError(t, cl.Create(ctx, foreign))
		gw := &operatorv1alpha1.GWAdmin{Name: "gw-conflict", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))

		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretConflict, waitTime)
		// The transition is reported as a warning event.
		requireGWAdminWarning(ctx, t, reader, gw, operatorv1alpha1.GWAdminReasonSecretConflict)
		require.NoError(t, reader.Get(ctx, client.ObjectKeyFromObject(foreign), foreign))
		assert.Empty(t, foreign.OwnerReferences, "the foreign Secret is left alone")
		assert.Equal(t, map[string]string{gwAdminSecretLabel: "true"}, foreign.Labels, "the foreign Secret is left alone")

		require.NoError(t, cl.Delete(ctx, foreign))
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, gwAdminRecoverWait)
	})

	t.Run("an unlabeled foreign Secret name conflict is reported and recovers once the Secret is gone", func(t *testing.T) {
		t.Parallel()

		// Invisible to the operator's Secret cache: only the Create of the
		// operator's own Secret runs into it. Both writes opt out of
		// read-your-own-writes consistency: the cache never observes this
		// Secret, so the resource version they would record would make every
		// later read of that name wait for an event that never comes.
		foreign := &corev1.Secret{
			Name:       "gw-conflict-unlabeled-api-tls",
			Namespace:  namespace,
			StringData: map[string]string{"owner": "someone-else"},
		}
		require.NoError(t, cl.Create(ctx, foreign, client.DisableReadYourWritesConsistency))
		gw := &operatorv1alpha1.GWAdmin{Name: "gw-conflict-unlabeled", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))

		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretConflict, waitTime)
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeReady,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady, waitTime)
		require.NoError(t, reader.Get(ctx, client.ObjectKeyFromObject(foreign), foreign))
		assert.Empty(t, foreign.OwnerReferences, "the foreign Secret is left alone")
		assert.Empty(t, foreign.Labels, "the foreign Secret is left alone")
		assert.Equal(t, "someone-else", string(foreign.Data["owner"]), "the foreign Secret is left alone")

		require.NoError(t, cl.Delete(ctx, foreign, client.DisableReadYourWritesConsistency))
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, gwAdminRecoverWait)
		s := getSecret(t, ctx, cl, namespace, foreign.Name)
		assert.True(t, metav1.IsControlledBy(s, gw))
	})

	t.Run("a foreign cp Service is reported and Ready recovers once it is gone", func(t *testing.T) {
		t.Parallel()

		foreign := &corev1.Service{
			Name: "gw-svc-conflict-cp", Namespace: namespace,
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "someone-else"},
				Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
			},
		}
		require.NoError(t, cl.Create(ctx, foreign))
		// The operator would take a Service it doesn't see yet over.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(foreign), &corev1.Service{}))
		}, waitTime, tickTime)
		gw := &operatorv1alpha1.GWAdmin{Name: "gw-svc-conflict", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))

		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeReady,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonResourceConflict, waitTime)
		// Certificates don't depend on the Service.
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, waitTime)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(foreign), foreign))
		assert.Empty(t, foreign.OwnerReferences, "the foreign Service is left alone")
		assert.Equal(t, map[string]string{"app": "someone-else"}, foreign.Spec.Selector, "the foreign Service is left alone")
		for _, mf := range foreign.ManagedFields {
			assert.NotEqual(t, controllerpkgssa.FieldManager, mf.Manager, "the foreign Service is left alone")
		}
		// Nothing past the conflict is applied.
		assert.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gw), &appsv1.Deployment{})))

		require.NoError(t, cl.Delete(ctx, foreign))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeReady); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
		}, gwAdminRecoverWait, tickTime)
		svc := &corev1.Service{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(foreign), svc))
		assert.True(t, metav1.IsControlledBy(svc, gw))
	})

	t.Run("a Manual CA referencing a Secret managed by the operator is rejected", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{Name: "gw-ca-self", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, waitTime)

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
				return
			}
			gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{
				Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual,
				SecretRef:    &operatorv1alpha1.GWAdminSecretRef{Name: "gw-ca-self-cp-tls"},
			}}
			assert.NoError(ct, cl.Update(ctx, gw))
		}, waitTime, tickTime)

		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonSecretRefOperatorManaged, waitTime)
		// Its own Secret is not deleted from under it.
		getSecret(t, ctx, cl, namespace, "gw-ca-self-cp-tls")
	})

	t.Run("moving from an Automatic to a Manual CA publishes it once the pods trust it", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{Name: "gw-ca-switch", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))
		automaticCA := gwadminref.CPClientCASecretName(gw)
		// The pods run the Automatic CA.
		var trust0 string
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			trust0 = deploymentTrustChecksum(ctx, ct, cl, namespace, gw.Name)
			assert.NotEmpty(ct, trust0)
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeReady); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
		}, waitTime, tickTime)
		autoCA := firstCertificate(t, getSecret(t, ctx, cl, operatorNamespace, automaticCA).Data[corev1.TLSCertKey])
		// published returns the cp client CA published in the operator
		// namespace.
		published := func() (*x509.Certificate, error) {
			s := &corev1.Secret{}
			if err := reader.Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: automaticCA}, s); err != nil {
				return nil, err
			}
			return parseFirstCertificate(s.Data[corev1.TLSCertKey])
		}

		userCA := newCASecret(t, namespace, "gw-ca-switch-user-ca", "user-ca")
		require.NoError(t, cl.Create(ctx, userCA))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
				return
			}
			gw.Spec.TLS = &operatorv1alpha1.GWAdminTLS{ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{
				Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual,
				SecretRef:    &operatorv1alpha1.GWAdminSecretRef{Name: userCA.Name},
			}}
			assert.NoError(ct, cl.Update(ctx, gw))
		}, waitTime, tickTime)

		// First pass: the Manual CA is only added to the trust bundle, and
		// the Deployment rolls to trust it.
		var trust1 string
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			bundle := &corev1.Secret{}
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "gw-ca-switch-cp-client-ca-bundle"}, bundle)) {
				assert.Equal(ct, 2, countCertificates(bundle.Data["ca.crt"]))
			}
			trust1 = deploymentTrustChecksum(ctx, ct, cl, namespace, gw.Name)
			assert.NotEqual(ct, trust0, trust1, "the Deployment rolls to the new trust bundle")
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
				assert.Equal(ct, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason, cond.Message)
			}
		}, waitTime, tickTime)
		// Until the pods trust the Manual CA, the Automatic one stays
		// published.
		require.Never(t, func() bool {
			ca, err := published()
			return err != nil || !ca.Equal(autoCA)
		}, 3*time.Second, tickTime, "the Automatic CA must stay published until the pods trust the Manual one")

		// Once the Deployment trusting the Manual CA rolled out, the Manual
		// CA is published, under the same name.
		userCert := firstCertificate(t, userCA.Data[corev1.TLSCertKey])
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			ca, err := published()
			if !assert.NoError(ct, err) {
				return
			}
			if !ca.Equal(userCert) {
				markRolled(ctx, ct, cl, namespace, gw.Name, consts.GWAdminTrustChecksumAnnotation, trust1)
				assert.Fail(ct, "the Manual CA is not published")
			}
		}, waitTime, tickTime)
		// The user's CA is never modified.
		s := getSecret(t, ctx, cl, namespace, userCA.Name)
		assert.Equal(t, userCA.Data, s.Data)
		assert.Empty(t, s.OwnerReferences)
	})

	t.Run("a controlled Secret stripped of the secret label gets it back without a conflict", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{Name: "gw-relabel", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, waitTime)

		const name = "gw-relabel-api-tls"
		before := &corev1.Secret{}
		require.NoError(t, reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, before))
		// The label goes: the Secret drops out of the operator's cache.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			s := &corev1.Secret{}
			if !assert.NoError(ct, reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, s)) {
				return
			}
			delete(s.Labels, gwAdminSecretLabel)
			assert.NoError(ct, cl.Update(ctx, s))
		}, waitTime, tickTime)

		sawConflict := false
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil &&
				cond.Status == metav1.ConditionFalse {
				sawConflict = true
			}
			s := &corev1.Secret{}
			if assert.NoError(ct, reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, s)) {
				assert.Equal(ct, "true", s.Labels[gwAdminSecretLabel], "the label is restored")
			}
			// Back in the cache.
			assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &corev1.Secret{}))
		}, waitTime, tickTime)
		require.Never(t, func() bool {
			current := &operatorv1alpha1.GWAdmin{}
			if err := cl.Get(ctx, client.ObjectKeyFromObject(gw), current); err != nil {
				return false
			}
			c := apimeta.FindStatusCondition(current.Status.Conditions, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned)
			return c == nil || c.Status != metav1.ConditionTrue
		}, 3*time.Second, tickTime, "CertificateProvisioned must stay True")
		assert.False(t, sawConflict, "CertificateProvisioned must never turn False")
		// No SecretConflict was ever reported: events outlive the condition.
		events, err := gwAdminEvents(ctx, reader, gw, corev1.EventTypeWarning, operatorv1alpha1.GWAdminReasonSecretConflict)
		require.NoError(t, err)
		assert.Empty(t, events)

		// The certificate was relabeled, not reissued.
		after := getSecret(t, ctx, cl, namespace, name)
		assert.True(t, metav1.IsControlledBy(after, gw))
		assert.Equal(t, before.Data, after.Data)
	})

	t.Run("a foreign HPA is reported and Ready recovers once it is gone", func(t *testing.T) {
		t.Parallel()

		foreign := &autoscalingv2.HorizontalPodAutoscaler{
			Name: "gw-hpa-conflict", Namespace: namespace,
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "someone-else"},
				MaxReplicas:    5,
			},
		}
		require.NoError(t, cl.Create(ctx, foreign))
		// The operator would take an HPA it doesn't see yet over.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(foreign), &autoscalingv2.HorizontalPodAutoscaler{}))
		}, waitTime, tickTime)
		gw := &operatorv1alpha1.GWAdmin{
			Name: "gw-hpa-conflict", Namespace: namespace,
			Spec: operatorv1alpha1.GWAdminSpec{Deployment: &operatorv1alpha1.GWAdminDeploymentOptions{
				Scaling: &operatorv1alpha1.GWAdminScaling{HorizontalScaling: &commonv1alpha1.HorizontalScaling{
					MinReplicas: new(int32(2)),
					MaxReplicas: 3,
				}},
			}},
		}
		require.NoError(t, cl.Create(ctx, gw))

		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeReady,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonResourceConflict, waitTime)
		requireGWAdminWarning(ctx, t, reader, gw, operatorv1alpha1.GWAdminReasonResourceConflict)
		cond := apimeta.FindStatusCondition(gw.Status.Conditions, operatorv1alpha1.GWAdminConditionTypeReady)
		require.NotNil(t, cond)
		assert.Contains(t, cond.Message, `HorizontalPodAutoscaler "gw-hpa-conflict"`)

		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(foreign), foreign))
		assert.Empty(t, foreign.OwnerReferences, "the foreign HPA is left alone")
		assert.Equal(t, "someone-else", foreign.Spec.ScaleTargetRef.Name, "the foreign HPA is left alone")
		assert.Equal(t, int32(5), foreign.Spec.MaxReplicas, "the foreign HPA is left alone")

		require.NoError(t, cl.Delete(ctx, foreign))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeReady); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
		}, gwAdminRecoverWait, tickTime)
		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(gw), hpa))
		assert.True(t, metav1.IsControlledBy(hpa, gw))
		assert.Equal(t, gw.Name, hpa.Spec.ScaleTargetRef.Name)
	})

	t.Run("an invalid Manual cp Secret keeps the Deployment applied", func(t *testing.T) {
		t.Parallel()

		const name = "gw-cp-invalid"
		ca := newCASecret(t, namespace, name+"-user-cp-ca", "user-cp-ca")
		userCP := newServerCertificateSecret(t, ca, namespace, name+"-user-cp", name+"-cp."+namespace+".svc.cluster.local")
		require.NoError(t, cl.Create(ctx, userCP))

		gw := &operatorv1alpha1.GWAdmin{
			Name: name, Namespace: namespace,
			Spec: operatorv1alpha1.GWAdminSpec{TLS: &operatorv1alpha1.GWAdminTLS{
				CP: &operatorv1alpha1.GWAdminServerCertificate{
					Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual,
					SecretRef:    &operatorv1alpha1.GWAdminSecretRef{Name: userCP.Name},
				},
			}},
		}
		require.NoError(t, cl.Create(ctx, gw))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
				assert.Equal(ct, operatorv1alpha1.GWAdminReasonCertificateProvisioned, cond.Reason, cond.Message)
			}
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeReady); cond != nil {
				assert.Equal(ct, metav1.ConditionTrue, cond.Status, cond.Message)
			}
		}, waitTime, tickTime)
		copied := getSecret(t, ctx, cl, namespace, name+"-cp-tls")
		assert.Equal(t, userCP.Data[corev1.TLSCertKey], copied.Data[corev1.TLSCertKey])

		// The user's Secret turns invalid: its key no longer matches.
		other := newServerCertificateSecret(t, ca, namespace, "unused", "other.example.com")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			s := &corev1.Secret{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(userCP), s)) {
				return
			}
			s.Data[corev1.TLSPrivateKeyKey] = other.Data[corev1.TLSPrivateKeyKey]
			assert.NoError(ct, cl.Update(ctx, s))
		}, waitTime, tickTime)
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonInvalidSecret, waitTime)
		requireGWAdminWarning(ctx, t, reader, gw, operatorv1alpha1.GWAdminReasonInvalidSecret)

		// A spec change still reaches the Deployment, which keeps running
		// the last valid copy.
		const image = "kong/gw-admin:envtest-custom"
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
				return
			}
			gw.Spec.Deployment = &operatorv1alpha1.GWAdminDeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  consts.GWAdminContainerName,
					Image: image,
				}}}},
			}
			assert.NoError(ct, cl.Update(ctx, gw))
		}, waitTime, tickTime)
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			d := &appsv1.Deployment{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), d)) {
				return
			}
			if c := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.GWAdminContainerName); assert.NotNil(ct, c) {
				assert.Equal(ct, image, c.Image)
			}
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
		}, waitTime, tickTime)
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeReady,
			metav1.ConditionFalse, operatorv1alpha1.GWAdminReasonDependenciesNotReady, waitTime)
		assert.Equal(t, copied.Data, getSecret(t, ctx, cl, namespace, name+"-cp-tls").Data, "the last valid copy is kept")
	})

	t.Run("a Manual client CA renewed in place is published once trusted, the previous one dropped once the data planes moved", func(t *testing.T) {
		t.Parallel()

		userCA := newCASecret(t, namespace, "gw-rotation-user-ca", "ca-a")
		require.NoError(t, cl.Create(ctx, userCA))
		caA := firstCertificate(t, userCA.Data[corev1.TLSCertKey])

		gw := &operatorv1alpha1.GWAdmin{
			Name: "gw-rotation", Namespace: namespace,
			Spec: operatorv1alpha1.GWAdminSpec{TLS: &operatorv1alpha1.GWAdminTLS{
				ClientCA: &operatorv1alpha1.GWAdminCertificateSecret{
					Provisioning: operatorv1alpha1.GWAdminCertificateProvisioningManual,
					SecretRef:    &operatorv1alpha1.GWAdminSecretRef{Name: userCA.Name},
				},
			}},
		}
		require.NoError(t, cl.Create(ctx, gw))

		bundleSize := func() (int, error) {
			s := &corev1.Secret{}
			if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "gw-rotation-cp-client-ca-bundle"}, s); err != nil {
				return 0, err
			}
			return countCertificates(s.Data["ca.crt"]), nil
		}
		publishedIs := func(ca *x509.Certificate) error {
			s := &corev1.Secret{}
			if err := cl.Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: gwadminref.CPClientCASecretName(gw)}, s); err != nil {
				return err
			}
			published, err := parseFirstCertificate(s.Data[corev1.TLSCertKey])
			if err != nil {
				return err
			}
			if !published.Equal(ca) {
				return errors.New("another CA is published")
			}
			return nil
		}
		requireBundleSize := func(ct *assert.CollectT, want int) {
			n, err := bundleSize()
			if assert.NoError(ct, err) {
				assert.Equal(ct, want, n)
			}
		}
		// stays checks that cond holds over a few reconciles.
		stays := func(cond func() error, msg string) {
			t.Helper()
			require.Never(t, func() bool { return cond() != nil }, 3*time.Second, tickTime, msg)
		}

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			assert.NoError(ct, publishedIs(caA))
			requireBundleSize(ct, 1)
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
				assert.Equal(ct, operatorv1alpha1.GWAdminReasonCertificateProvisioned, cond.Reason, cond.Message)
			}
		}, waitTime, tickTime)
		// The cp server certificate never comes from the client CA.
		cpTLS := getSecret(t, ctx, cl, namespace, "gw-rotation-cp-tls")
		cpLeaf := firstCertificate(t, cpTLS.Data[corev1.TLSCertKey])
		require.Error(t, cpLeaf.CheckSignatureFrom(caA))
		require.NoError(t, cpLeaf.CheckSignatureFrom(firstCertificate(t,
			getSecret(t, ctx, cl, operatorNamespace, gwadmin.CPServerCASecretName(gw)).Data[corev1.TLSCertKey])))

		// A data plane, in another namespace the GWAdmin owner grants, runs a
		// certificate from A.
		dpNamespace := CreateNamespace(ctx, t, cl).Name
		grantDataPlanes(ctx, t, cl, namespace, dpNamespace)
		putDataPlane(ctx, t, cl, gw, dpNamespace, serverBundleChecksumOf(t, ctx, cl, gw), clientCAOf(t, ctx, cl, operatorNamespace, gw))

		// The user (or cert-manager) renews the CA in place.
		renewed := newCASecret(t, namespace, userCA.Name, "ca-b")
		caB := firstCertificate(t, renewed.Data[corev1.TLSCertKey])
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(userCA), userCA))
		userCA.Data = renewed.Data
		require.NoError(t, cl.Update(ctx, userCA))

		// Phase 1: both CAs are trusted, and the pods roll to trust both.
		var trust1 string
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			requireBundleSize(ct, 2)
			trust1 = deploymentTrustChecksum(ctx, ct, cl, namespace, gw.Name)
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
				assert.Equal(ct, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason, cond.Message)
			}
		}, waitTime, tickTime)
		// A stays published as long as the pods don't trust B.
		stays(func() error { return publishedIs(caA) }, "the published CA must stay A until the pods trust B")

		// Phase 2: once the pods trust B, B is published.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if err := publishedIs(caB); err != nil {
				markRolled(ctx, ct, cl, namespace, gw.Name, consts.GWAdminTrustChecksumAnnotation, trust1)
				assert.NoError(ct, err)
			}
		}, waitTime, tickTime)

		// A stays trusted as long as the data plane runs a certificate from
		// it, and the condition names it.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
				assert.Equal(ct, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason, cond.Message)
				assert.Contains(ct, cond.Message, "data plane Deployment "+dpNamespace+"/dp")
			}
		}, waitTime, tickTime)
		stays(func() error {
			if n, err := bundleSize(); err != nil || n != 2 {
				return fmt.Errorf("bundle size %d: %w", n, err)
			}
			return nil
		}, "A must stay trusted until the data plane runs a certificate from B")

		// Phase 3: the data plane rolls out with a certificate from B: A is
		// dropped.
		putDataPlane(ctx, t, cl, gw, dpNamespace, serverBundleChecksumOf(t, ctx, cl, gw), clientCAOf(t, ctx, cl, operatorNamespace, gw))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			requireBundleSize(ct, 1)
			markRolled(ctx, ct, cl, namespace, gw.Name, "", "")
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
				assert.Equal(ct, operatorv1alpha1.GWAdminReasonCertificateProvisioned, cond.Reason, cond.Message)
			}
		}, waitTime, tickTime)
		bundle := getSecret(t, ctx, cl, namespace, "gw-rotation-cp-client-ca-bundle")
		onlyCA, err := parseFirstCertificate(bundle.Data["ca.crt"])
		require.NoError(t, err)
		assert.True(t, onlyCA.Equal(caB), "the bundle holds the new CA only")
		// The cp server certificate was never touched.
		assert.Equal(t, cpTLS.Data, getSecret(t, ctx, cl, namespace, "gw-rotation-cp-tls").Data)
	})

	t.Run("rotating the CAs moves the cp server certificate once the data planes run the new server bundle", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{Name: "gw-rotate-server", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, waitTime)
		serverCA := gwadmin.CPServerCASecretName(gw)
		before := firstCertificate(t, getSecret(t, ctx, cl, operatorNamespace, serverCA).Data[corev1.TLSCertKey])

		// A data plane, in another namespace the GWAdmin owner grants, runs
		// the current server bundle.
		dpNamespace := CreateNamespace(ctx, t, cl).Name
		grantDataPlanes(ctx, t, cl, namespace, dpNamespace)
		putDataPlane(ctx, t, cl, gw, dpNamespace, serverBundleChecksumOf(t, ctx, cl, gw), "")

		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
				return
			}
			gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
			assert.NoError(ct, cl.Update(ctx, gw))
		}, waitTime, tickTime)

		// cpLeafOn reports whether the cp server certificate is signed by ca.
		cpLeafOn := func(ca *x509.Certificate) bool {
			cp := &corev1.Secret{}
			if cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "gw-rotate-server-cp-tls"}, cp) != nil {
				return false
			}
			leaf, err := parseFirstCertificate(cp.Data[corev1.TLSCertKey])
			return err == nil && leaf.CheckSignatureFrom(ca) == nil
		}
		// The new cp server CA joins the bundle; the certificate waits for
		// the data plane.
		var after *x509.Certificate
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			s := &corev1.Secret{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: serverCA}, s)) {
				return
			}
			var err error
			after, err = parseFirstCertificate(s.Data[corev1.TLSCertKey])
			if !assert.NoError(ct, err) || !assert.False(ct, after.Equal(before), "the cp server CA is rotated") {
				return
			}
			assert.Equal(ct, "1", s.Annotations["konghq.com/gw-admin-rotation-token"])
			bundle := &corev1.Secret{}
			if assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "gw-rotate-server-cp-server-ca-bundle"}, bundle)) {
				assert.Equal(ct, 2, countCertificates(bundle.Data["ca.crt"]))
			}
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
				assert.Contains(ct, cond.Message, "data plane Deployment "+dpNamespace+"/dp")
			}
		}, waitTime, tickTime)
		require.Never(t, func() bool { return !cpLeafOn(before) }, 3*time.Second, tickTime,
			"the cp server certificate must stay on the old CA until the data plane runs the new bundle")

		// The data plane rolls out with the new bundle: the certificate moves.
		putDataPlane(ctx, t, cl, gw, dpNamespace, serverBundleChecksumOf(t, ctx, cl, gw), "")
		require.Eventually(t, func() bool { return cpLeafOn(after) }, waitTime, tickTime,
			"the cp server certificate moves to the new CA")
	})

	t.Run("data planes count in a granted namespace only, and the skip annotation releases them", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{Name: "gw-grants", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, waitTime)
		serverCA := gwadmin.CPServerCASecretName(gw)
		serverCAOf := func(ct *assert.CollectT) *x509.Certificate {
			s := &corev1.Secret{}
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: operatorNamespace, Name: serverCA}, s)) {
				return nil
			}
			c, err := parseFirstCertificate(s.Data[corev1.TLSCertKey])
			assert.NoError(ct, err)
			return c
		}
		cpLeafOn := func(ca *x509.Certificate) bool {
			cp := &corev1.Secret{}
			if ca == nil || cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "gw-grants-cp-tls"}, cp) != nil {
				return false
			}
			leaf, err := parseFirstCertificate(cp.Data[corev1.TLSCertKey])
			return err == nil && leaf.CheckSignatureFrom(ca) == nil
		}
		annotate := func(annotations map[string]string) {
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
					return
				}
				if gw.Annotations == nil {
					gw.Annotations = map[string]string{}
				}
				maps.Copy(gw.Annotations, annotations)
				assert.NoError(ct, cl.Update(ctx, gw))
			}, waitTime, tickTime)
		}
		// rotate rotates the CAs and returns the new cp server CA.
		rotate := func(token string) *x509.Certificate {
			var before *x509.Certificate
			require.EventuallyWithT(t, func(ct *assert.CollectT) { before = serverCAOf(ct); assert.NotNil(ct, before) }, waitTime, tickTime)
			annotate(map[string]string{consts.GWAdminRotateCAsAnnotation: token})
			var after *x509.Certificate
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				after = serverCAOf(ct)
				assert.False(ct, after == nil || after.Equal(before), "the cp server CA is rotated")
			}, waitTime, tickTime)
			return after
		}

		// A labelled Deployment in a namespace nobody granted, which never runs
		// the current server bundle, holds nothing back.
		dpNamespace := CreateNamespace(ctx, t, cl).Name
		putDataPlane(ctx, t, cl, gw, dpNamespace, "stale", "")
		first := rotate("1")
		require.Eventually(t, func() bool { return cpLeafOn(first) }, waitTime, tickTime,
			"an ungranted data plane doesn't hold the cp server certificate back")

		// Once granted, it counts: the next rotation waits for it.
		grantDataPlanes(ctx, t, cl, namespace, dpNamespace)
		second := rotate("2")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := condition(ct, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
				assert.Contains(ct, cond.Message, "data plane Deployment "+dpNamespace+"/dp")
			}
		}, waitTime, tickTime)
		require.Never(t, func() bool { return cpLeafOn(second) }, 3*time.Second, tickTime,
			"the cp server certificate waits for the granted data plane")

		// The skip annotation releases it.
		annotate(map[string]string{consts.GWAdminRotationSkipDataPlanesAnnotation: "true"})
		require.Eventually(t, func() bool { return cpLeafOn(second) }, waitTime, tickTime,
			"the skip annotation stops the rotation from waiting for the data plane")
		requireGWAdminConditionMessage(ctx, t, cl, gw, consts.GWAdminRotationSkipDataPlanesAnnotation)
	})

	t.Run("a certificate authority written into a bundle Secret is never trusted", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{Name: "gw-rogue", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))
		requireCondition(t, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
			metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, waitTime)
		rogue := firstCertificate(t, newCASecret(t, namespace, "rogue", "rogue").Data[corev1.TLSCertKey])

		for _, name := range []string{"gw-rogue-cp-client-ca-bundle", "gw-rogue-api-ca-bundle", "gw-rogue-cp-server-ca-bundle"} {
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				b := &corev1.Secret{}
				if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, b)) {
					return
				}
				b.Data["ca.crt"] = append(append([]byte{}, b.Data["ca.crt"]...),
					pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rogue.Raw})...)
				assert.NoError(ct, cl.Update(ctx, b))
			}, waitTime, tickTime)
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				b := &corev1.Secret{}
				if !assert.NoError(ct, reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, b)) {
					return
				}
				assert.Equal(ct, 1, countCertificates(b.Data["ca.crt"]), "%s is rewritten without the rogue CA", name)
				assert.NotContains(ct, string(b.Data["ca.crt"]),
					string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rogue.Raw})), name)
			}, waitTime, tickTime)
		}
	})

	t.Run("deleting a GWAdmin deletes its Secrets in the operator namespace", func(t *testing.T) {
		t.Parallel()

		gw := &operatorv1alpha1.GWAdmin{Name: "gw-cleanup", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, gw))
		sibling := &operatorv1alpha1.GWAdmin{Name: "gw-cleanup-sibling", Namespace: namespace}
		require.NoError(t, cl.Create(ctx, sibling))
		for _, g := range []*operatorv1alpha1.GWAdmin{gw, sibling} {
			requireCondition(t, g, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
				metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, waitTime)
		}
		private := func(g *operatorv1alpha1.GWAdmin) (int, error) {
			list := &corev1.SecretList{}
			err := reader.List(ctx, list, client.InNamespace(operatorNamespace),
				client.MatchingLabels{consts.GWAdminUIDLabel: string(g.UID)})
			return len(list.Items), err
		}
		// The API CA, the cp server CA, the operator client certificate and
		// the published cp client CA.
		n, err := private(gw)
		require.NoError(t, err)
		require.Equal(t, 4, n)

		require.NoError(t, cl.Delete(ctx, gw))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.True(ct, apierrors.IsNotFound(reader.Get(ctx, client.ObjectKeyFromObject(gw), &operatorv1alpha1.GWAdmin{})),
				"the finalizer is removed")
			n, err := private(gw)
			if assert.NoError(ct, err) {
				assert.Zero(ct, n, "the private Secrets are deleted")
			}
		}, waitTime, tickTime)
		n, err = private(sibling)
		require.NoError(t, err)
		assert.Equal(t, 4, n, "the Secrets of other GWAdmins are left alone")
	})
}

// TestGWAdminReconcilerCARotationBlocked runs its own reconciler, with a
// clock it moves forward: certificates only become due for renewal weeks
// after they are issued.
func TestGWAdminReconcilerCARotationBlocked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	var offset atomic.Int64
	now := func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	cl, reader, namespace, operatorNamespace := startGWAdminReconciler(t, ctx, now)

	gw := &operatorv1alpha1.GWAdmin{Name: "gw-blocked", Namespace: namespace}
	require.NoError(t, cl.Create(ctx, gw))
	requireGWAdminCondition(ctx, t, cl, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
		metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCertificateProvisioned, waitTime)
	apiCA := gwadmin.APICASecretName(gw)
	caA := firstCertificate(t, getSecret(t, ctx, cl, operatorNamespace, apiCA).Data[corev1.TLSCertKey])

	// The CAs are rotated: the pods must roll to trust the new API CA before
	// the API certificates can move to it.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
			return
		}
		gw.Annotations = map[string]string{consts.GWAdminRotateCAsAnnotation: "1"}
		assert.NoError(ct, cl.Update(ctx, gw))
	}, waitTime, tickTime)
	requireGWAdminCondition(ctx, t, cl, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned,
		metav1.ConditionTrue, operatorv1alpha1.GWAdminReasonCARotationInProgress, waitTime)

	// The pods don't roll while the certificates pass their renewal time
	// (two thirds of their validity), short of expiring.
	offset.Store(int64(consts.GWAdminLeafTTL * 3 / 4))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		// Any change of the GWAdmin triggers a reconcile.
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
			return
		}
		if gw.Annotations["envtest/clock"] != "moved" {
			// Keep the rotation token: changing it would rotate again.
			gw.Annotations["envtest/clock"] = "moved"
			assert.NoError(ct, cl.Update(ctx, gw))
		}
		if cond := gwAdminCondition(ctx, ct, cl, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
			assert.Equal(ct, metav1.ConditionFalse, cond.Status, cond.Message)
			assert.Equal(ct, operatorv1alpha1.GWAdminReasonCARotationBlocked, cond.Reason, cond.Message)
			assert.Contains(ct, cond.Message, "gw-blocked-api-tls expires at")
		}
	}, waitTime, tickTime)
	requireGWAdminWarning(ctx, t, reader, gw, operatorv1alpha1.GWAdminReasonCARotationBlocked)
	ready := apimeta.FindStatusCondition(gw.Status.Conditions, operatorv1alpha1.GWAdminConditionTypeReady)
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionFalse, ready.Status)
	assert.Equal(t, operatorv1alpha1.GWAdminReasonDependenciesNotReady, ready.Reason)

	// Once the pods trust the new CA, the certificates move to it.
	var trust string
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		trust = deploymentTrustChecksum(ctx, ct, cl, namespace, gw.Name)
		assert.NotEmpty(ct, trust)
	}, waitTime, tickTime)
	caB := firstCertificate(t, getSecret(t, ctx, cl, operatorNamespace, apiCA).Data[corev1.TLSCertKey])
	require.False(t, caA.Equal(caB), "the API CA is rotated")
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		s := &corev1.Secret{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "gw-blocked-api-tls"}, s)) {
			return
		}
		leaf, err := parseFirstCertificate(s.Data[corev1.TLSCertKey])
		if !assert.NoError(ct, err) {
			return
		}
		if leaf.CheckSignatureFrom(caB) != nil {
			markRolled(ctx, ct, cl, namespace, gw.Name, consts.GWAdminTrustChecksumAnnotation, trust)
			assert.Fail(ct, "the certificate is still issued by the old CA")
			return
		}
		if cond := gwAdminCondition(ctx, ct, cl, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
			assert.Equal(ct, operatorv1alpha1.GWAdminReasonCARotationInProgress, cond.Reason, cond.Message)
		}
	}, waitTime, tickTime)
}

// grantDataPlanes creates a KongReferenceGrant, in the namespace of the
// GWAdmins, allowing the OnPremEventGateways of dpNamespace to reference
// them: the data planes of dpNamespace count for their rotations.
func grantDataPlanes(ctx context.Context, t *testing.T, cl client.Client, gwNamespace, dpNamespace string) {
	t.Helper()
	grant := &configurationv1alpha1.KongReferenceGrant{
		GenerateName: "gwadmin-data-planes-", Namespace: gwNamespace,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group: "eventgateway.konghq.com", Kind: "OnPremEventGateway",
				Namespace: configurationv1alpha1.Namespace(dpNamespace),
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{{
				Group: configurationv1alpha1.Group(operatorv1alpha1.SchemeGroupVersion.Group), Kind: operatorv1alpha1.GWAdminKind,
			}},
		},
	}
	require.NoError(t, cl.Create(ctx, grant))
	// The reconciler lists the grants from the manager cache, which cl reads
	// too: wait for it to hold this one, or a reconcile racing the cache
	// doesn't count the data planes in dpNamespace.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(grant), &configurationv1alpha1.KongReferenceGrant{}))
	}, waitTime, tickTime)
}

// requireGWAdminConditionMessage waits for the CertificateProvisioned
// condition of the GWAdmin to mention want.
func requireGWAdminConditionMessage(ctx context.Context, t *testing.T, cl client.Client, gw *operatorv1alpha1.GWAdmin, want string) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if cond := gwAdminCondition(ctx, ct, cl, gw, operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned); cond != nil {
			assert.Contains(ct, cond.Message, want)
		}
	}, waitTime, tickTime)
}

// putDataPlane creates, or updates, the data plane Deployment "dp" in
// namespace dpNamespace of the GWAdmin, whose Pod template records the cp
// server bundle checksum and client CA fingerprint its pods mount, and marks
// it rolled out: a stand-in for the controller managing the data planes, and
// for the Deployment controller envtest doesn't run.
func putDataPlane(
	ctx context.Context, t *testing.T, cl client.Client, gw *operatorv1alpha1.GWAdmin,
	dpNamespace, serverBundleChecksum, clientCA string,
) {
	const name = "dp"
	t.Helper()
	annotations := map[string]string{
		consts.GWAdminDataPlaneServerBundleAnnotation: serverBundleChecksum,
		consts.GWAdminDataPlaneClientCAAnnotation:     clientCA,
	}
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		d := &appsv1.Deployment{}
		err := cl.Get(ctx, client.ObjectKey{Namespace: dpNamespace, Name: name}, d)
		switch {
		case apierrors.IsNotFound(err):
			labels := map[string]string{"app": name}
			d = &appsv1.Deployment{
				Name: name, Namespace: dpNamespace,
				Labels: map[string]string{
					consts.GWAdminDataPlaneNamespaceLabel: gw.Namespace,
					consts.GWAdminDataPlaneNameLabel:      gw.Name,
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: new(int32(1)),
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: annotations},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "dp", Image: "data-plane"}}},
					},
				},
			}
			if !assert.NoError(ct, cl.Create(ctx, d)) {
				return
			}
		case !assert.NoError(ct, err):
			return
		default:
			d.Spec.Template.Annotations = annotations
			if !assert.NoError(ct, cl.Update(ctx, d)) {
				return
			}
		}
		d.Status = appsv1.DeploymentStatus{
			ObservedGeneration:  d.Generation,
			Replicas:            1,
			UpdatedReplicas:     1,
			TerminatingReplicas: new(int32(0)),
		}
		assert.NoError(ct, cl.Status().Update(ctx, d))
	}, waitTime, tickTime)
}

// serverBundleChecksumOf returns the checksum of the cp server bundle of the
// GWAdmin, as a data plane mounting it records it.
func serverBundleChecksumOf(t *testing.T, ctx context.Context, cl client.Client, gw *operatorv1alpha1.GWAdmin) string {
	t.Helper()
	return gwadminref.DataPlaneServerBundleChecksum(getSecret(t, ctx, cl, gw.Namespace, gw.Name+"-cp-server-ca-bundle"))
}

// clientCAOf returns the fingerprint of the cp client CA of the GWAdmin,
// published in the operator namespace, as a data plane running a
// certificate from it records it.
func clientCAOf(t *testing.T, ctx context.Context, cl client.Client, operatorNamespace string, gw *operatorv1alpha1.GWAdmin) string {
	t.Helper()
	return gwadminref.DataPlaneClientCAFingerprint(
		getSecret(t, ctx, cl, operatorNamespace, gwadminref.CPClientCASecretName(gw)).Data[corev1.TLSCertKey])
}

// markRolled reports the gw-admin Deployment as rolled out for its current
// Pod template, standing in for the Deployment controller envtest doesn't
// run. When annotation is not empty, only a Deployment whose Pod template
// carries it set to value is marked: the status update conflicts if the
// Deployment changed since it was read.
func markRolled(ctx context.Context, ct *assert.CollectT, cl client.Client, namespace, name, annotation, value string) {
	d := &appsv1.Deployment{}
	if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, d)) {
		return
	}
	if annotation != "" && !assert.Equal(ct, value, d.Spec.Template.Annotations[annotation],
		"the Deployment runs other TLS material") {
		return
	}
	replicas := int32(1)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	d.Status = appsv1.DeploymentStatus{
		ObservedGeneration:  d.Generation,
		Replicas:            replicas,
		UpdatedReplicas:     replicas,
		TerminatingReplicas: new(int32(0)),
	}
	assert.NoError(ct, cl.Status().Update(ctx, d))
}

// deploymentTrustChecksum returns the trust checksum of the Pod template of
// the gw-admin Deployment.
func deploymentTrustChecksum(ctx context.Context, ct *assert.CollectT, cl client.Client, namespace, name string) string {
	d := &appsv1.Deployment{}
	if !assert.NoError(ct, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, d)) {
		return ""
	}
	return d.Spec.Template.Annotations[consts.GWAdminTrustChecksumAnnotation]
}

// gwAdminCondition reads the GWAdmin, and returns its condition of the given
// type.
func gwAdminCondition(
	ctx context.Context, ct *assert.CollectT, cl client.Client, gw *operatorv1alpha1.GWAdmin, conditionType string,
) *metav1.Condition {
	if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gw), gw)) {
		return nil
	}
	cond := apimeta.FindStatusCondition(gw.Status.Conditions, conditionType)
	assert.NotNil(ct, cond, conditionType)
	return cond
}

// requireGWAdminCondition waits for the condition of the GWAdmin to have the
// given status and reason.
func requireGWAdminCondition(
	ctx context.Context, t *testing.T, cl client.Client, gw *operatorv1alpha1.GWAdmin,
	conditionType string, status metav1.ConditionStatus, reason string, wait time.Duration,
) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if cond := gwAdminCondition(ctx, ct, cl, gw, conditionType); cond != nil {
			assert.Equal(ct, status, cond.Status, cond.Message)
			assert.Equal(ct, reason, cond.Reason, cond.Message)
		}
	}, wait, tickTime)
}

// gwAdminEvents returns the events of the given type and reason recorded on
// the GWAdmin.
func gwAdminEvents(
	ctx context.Context, reader client.Reader, gw *operatorv1alpha1.GWAdmin, eventType, reason string,
) ([]eventsv1.Event, error) {
	list := &eventsv1.EventList{}
	if err := reader.List(ctx, list, client.InNamespace(gw.Namespace)); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(list.Items, func(e eventsv1.Event) bool {
		return e.Regarding.Kind != operatorv1alpha1.GWAdminKind || e.Regarding.Name != gw.Name ||
			e.Type != eventType || e.Reason != reason
	}), nil
}

// requireGWAdminWarning waits for a warning event with the given reason to be
// recorded on the GWAdmin.
func requireGWAdminWarning(ctx context.Context, t *testing.T, reader client.Reader, gw *operatorv1alpha1.GWAdmin, reason string) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		events, err := gwAdminEvents(ctx, reader, gw, corev1.EventTypeWarning, reason)
		if assert.NoError(ct, err) {
			assert.NotEmpty(ct, events, "no warning event %s recorded on GWAdmin %s", reason, gw.Name)
		}
	}, waitTime, tickTime)
}

// newServerCertificateSecret returns a Secret holding a server certificate
// for dnsNames issued by ca, labeled so that the operator's Secret cache
// holds it.
func newServerCertificateSecret(t *testing.T, ca *corev1.Secret, namespace, name string, dnsNames ...string) *corev1.Secret {
	t.Helper()
	certPEM, keyPEM := gwAdminTestCertificate(t, &x509.Certificate{
		Subject:     pkix.Name{CommonName: dnsNames[0]},
		DNSNames:    dnsNames,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		NotAfter:    time.Now().Add(30 * 24 * time.Hour),
	}, ca)
	return &corev1.Secret{
		Name: name, Namespace: namespace,
		Labels: map[string]string{gwAdminSecretLabel: "true"},
		Type:   corev1.SecretTypeTLS,
		// ca.crt carries the issuer, as cert-manager does: the data planes
		// verify the cp listener with it.
		Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM, "ca.crt": ca.Data[corev1.TLSCertKey]},
	}
}

// ownsField reports whether manager owns the field at path, through any of
// its managed fields entries.
func ownsField(entries []metav1.ManagedFieldsEntry, manager string, path ...string) (bool, error) {
	for _, e := range entries {
		if e.Manager != manager || e.FieldsV1 == nil {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(e.FieldsV1.GetRawBytes(), &fields); err != nil {
			return false, err
		}
		found := true
		for _, p := range path {
			next, ok := fields[p].(map[string]any)
			if !ok {
				found = false
				break
			}
			fields = next
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// newCASecret returns a Secret holding a new certificate authority, labeled
// so that the operator's Secret cache holds it.
func newCASecret(t *testing.T, namespace, name, commonName string) *corev1.Secret {
	t.Helper()
	certPEM, keyPEM := gwAdminTestCertificate(t, &x509.Certificate{
		Subject:               pkix.Name{CommonName: commonName},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
	}, nil)
	return &corev1.Secret{
		Name: name, Namespace: namespace,
		Labels: map[string]string{gwAdminSecretLabel: "true"},
		Type:   corev1.SecretTypeTLS,
		Data:   map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM},
	}
}

// gwAdminTestCertificate creates an ECDSA certificate from template, valid
// from 5 minutes ago, signed by the certificate authority Secret parent, or
// self-signed when parent is nil. It returns it with its key, PEM encoded.
func gwAdminTestCertificate(t *testing.T, template *x509.Certificate, parent *corev1.Secret) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	template.SerialNumber = serial
	template.NotBefore = time.Now().Add(-5 * time.Minute)
	signer, signerKey := template, any(key)
	if parent != nil {
		signer = firstCertificate(t, parent.Data[corev1.TLSCertKey])
		block, _ := pem.Decode(parent.Data[corev1.TLSPrivateKeyKey])
		require.NotNil(t, block)
		signerKey, err = x509.ParseECPrivateKey(block.Bytes)
		require.NoError(t, err)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, signer, &key.PublicKey, signerKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func getSecret(t *testing.T, ctx context.Context, cl client.Client, namespace, name string) *corev1.Secret {
	t.Helper()
	s := &corev1.Secret{}
	require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, s), name)
	return s
}

func firstCertificate(t *testing.T, data []byte) *x509.Certificate {
	t.Helper()
	cert, err := parseFirstCertificate(data)
	require.NoError(t, err)
	return cert
}

func parseFirstCertificate(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func countCertificates(data []byte) int {
	n := 0
	for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
		n++
	}
	return n
}

// scaleLikeHPA scales the Deployment through its scale subresource, as the
// HPA controller does: envtest runs no kube-controller-manager. Like the HPA,
// which re-asserts its count on every sync, it scales the Deployment again
// when it has been reverted, since a reconcile working from a cache that has
// not observed the scale yet may revert it once. It returns once the count
// has stayed for 5 seconds.
func scaleLikeHPA(t *testing.T, ctx context.Context, cl client.Client, key client.ObjectKey, replicas int32) {
	t.Helper()

	var scaledAt time.Time
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		d := &appsv1.Deployment{}
		if !assert.NoError(ct, cl.Get(ctx, key, d)) {
			return
		}
		scale := &autoscalingv1.Scale{}
		if !assert.NoError(ct, cl.SubResource("scale").Get(ctx, d, scale)) {
			return
		}
		if scale.Spec.Replicas != replicas {
			scale.Spec.Replicas = replicas
			assert.NoError(ct, cl.SubResource("scale").Update(ctx, d,
				client.WithSubResourceBody(scale),
				client.FieldOwner("horizontal-pod-autoscaler"),
			))
			scaledAt = time.Now()
			ct.Errorf("Deployment scaled to %d replicas, waiting for the count to stay", replicas)
			return
		}
		assert.GreaterOrEqual(ct, time.Since(scaledAt), 5*time.Second,
			"Deployment replicas set by the HPA must stay for 5s")
	}, waitTime, tickTime)
}
