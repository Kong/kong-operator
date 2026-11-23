package eventgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// statusApplyHookClient calls hook before each apply on the status
// subresource, then applies it.
type statusApplyHookClient struct {
	client.Client

	hook func(ctx context.Context, obj runtime.ApplyConfiguration) error
}

func (c statusApplyHookClient) Status() client.SubResourceWriter {
	return statusApplyHookWriter{SubResourceWriter: c.Client.Status(), hook: c.hook}
}

type statusApplyHookWriter struct {
	client.SubResourceWriter

	hook func(ctx context.Context, obj runtime.ApplyConfiguration) error
}

func (w statusApplyHookWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	if err := w.hook(ctx, obj); err != nil {
		return err
	}
	return w.SubResourceWriter.Apply(ctx, obj, opts...)
}

// appliedObject is what identifies the object a status apply is for.
type appliedObject struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string    `json:"name"`
		UID  types.UID `json:"uid"`
	} `json:"metadata"`
}

// TestOnPremEventGatewayRecreatedStaticKey verifies that the status the
// controller applies on an entity it read before the entity was deleted and
// created again doesn't land on the one created again: the fingerprint of the
// value of the static key it replaces would stop its push for good.
func TestOnPremEventGatewayRecreatedStaticKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	// Uncached, so that the key is deleted and created again while the
	// controller's cache may still hold the one deleted.
	direct, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)

	keyName := client.ObjectKey{Namespace: ns.Name, Name: "key"}
	newKey := func() *configurationv1alpha1.EventGatewayStaticKey {
		return objectFromYAML[configurationv1alpha1.EventGatewayStaticKey](t, ns.Name, `
metadata: {name: key}
spec:`+onPremEventGatewayParentRef("gateway")+`
  apiSpec:
    name: key
    value: {type: secretRef, secretRef: {name: key-secret, key: key}}
`)
	}

	// recreate deletes the key, its finalizers removed, and creates it again,
	// returning the UID of the new one.
	recreate := func(ctx context.Context) (types.UID, error) {
		old := &configurationv1alpha1.EventGatewayStaticKey{}
		if err := direct.Get(ctx, keyName, old); err != nil {
			return "", err
		}
		if err := direct.Patch(ctx, old, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":null}}`))); err != nil {
			return "", err
		}
		if err := direct.Delete(ctx, old); err != nil && !apierrors.IsNotFound(err) {
			return "", err
		}
		deadline := time.Now().Add(waitTime)
		for {
			err := direct.Get(ctx, keyName, &configurationv1alpha1.EventGatewayStaticKey{})
			if apierrors.IsNotFound(err) {
				break
			}
			if err != nil {
				return "", err
			}
			if time.Now().After(deadline) {
				return "", errors.New("the static key wasn't deleted")
			}
			time.Sleep(tickTime)
		}
		key := newKey()
		if err := direct.Create(ctx, key); err != nil {
			return "", err
		}
		return key.UID, nil
	}

	// Armed with the UID of the key, the hook deletes it and creates it again
	// before the next status apply of the controller on it, as if this
	// happened between the read of the key and the write of its status.
	var (
		mu          sync.Mutex
		armedFor    types.UID
		recreated   types.UID
		recreateErr error
	)
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	r := gwAdmin.reconciler(ctx, mgr)
	r.Client = statusApplyHookClient{Client: r.Client, hook: func(ctx context.Context, obj runtime.ApplyConfiguration) error {
		data, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		var applied appliedObject
		if err := json.Unmarshal(data, &applied); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if armedFor == "" || applied.Kind != "EventGatewayStaticKey" || applied.Metadata.Name != keyName.Name || applied.Metadata.UID != armedFor {
			return nil
		}
		armedFor = ""
		recreated, recreateErr = recreate(ctx)
		if recreateErr != nil {
			return fmt.Errorf("failed to recreate the static key: %w", recreateErr)
		}
		return nil
	}}
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig, r)

	gwAdmin.provision(ctx)
	require.NoError(t, cl.Create(ctx, &corev1.Secret{
		Name: "key-secret", Namespace: ns.Name,
		Data: map[string][]byte{"key": []byte("c2VjcmV0LWtleS0xMjM0NTY3ODkwMTIzNDU2")},
	}))
	gateway := objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: gateway}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	require.NoError(t, cl.Create(ctx, gateway))
	require.NoError(t, cl.Create(ctx, newKey()))

	key := &configurationv1alpha1.EventGatewayStaticKey{}
	inputsApplied := func() *metav1.Condition {
		return apimeta.FindStatusCondition(key.GetConditions(), konnectv1alpha1.CreateInputsAppliedConditionType)
	}
	var pushedHash string
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if assert.NoError(ct, cl.Get(ctx, keyName, key)) {
			assert.True(ct, apimeta.IsStatusConditionTrue(key.GetConditions(), "Programmed"))
			assert.NotEmpty(ct, key.Status.CreateInputsHash, "the push records the fingerprint of the key")
			pushedHash = key.Status.CreateInputsHash
		}
	}, waitTime, tickTime)

	// Its value changed, the key stops the push.
	secret := &corev1.Secret{}
	require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "key-secret"}, secret))
	secret.Data = map[string][]byte{"key": []byte("b3RoZXItc2VjcmV0LWtleS0xMjM0NTY3ODkw")}
	require.NoError(t, cl.Update(ctx, secret))
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if assert.NoError(ct, cl.Get(ctx, keyName, key)) {
			if cond := inputsApplied(); assert.NotNil(ct, cond) {
				assert.Equal(ct, konnectv1alpha1.CreateInputsReasonValueChanged, cond.Reason)
			}
			assert.Equal(ct, pushedHash, key.Status.CreateInputsHash)
		}
	}, waitTime, tickTime)

	// The way out the key tells of: deleted and created again, it has no
	// fingerprint and is pushed with its new value. Changing its description
	// makes the controller apply its status, read before it was created again.
	mu.Lock()
	armedFor = key.UID
	mu.Unlock()
	require.NoError(t, cl.Patch(ctx, key, client.RawPatch(types.MergePatchType, []byte(`{"spec":{"apiSpec":{"description":"recreated"}}}`))))
	var newUID types.UID
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		mu.Lock()
		defer mu.Unlock()
		assert.NoError(ct, recreateErr)
		assert.NotEmpty(ct, recreated, "the static key must be created again before a status apply")
		newUID = recreated
	}, waitTime, tickTime)
	require.NotEqual(t, key.UID, newUID)

	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(ctx, keyName, key)) || !assert.Equal(ct, newUID, key.UID) {
			return
		}
		assert.True(ct, apimeta.IsStatusConditionTrue(key.GetConditions(), "Programmed"), "%v", key.GetConditions())
		if cond := inputsApplied(); assert.NotNil(ct, cond) {
			assert.NotEqual(ct, konnectv1alpha1.CreateInputsReasonValueChanged, cond.Reason, cond.Message)
		}
		assert.NotEmpty(ct, key.Status.CreateInputsHash)
		assert.NotEqual(ct, pushedHash, key.Status.CreateInputsHash, "the fingerprint of the key it replaces isn't recorded on it")
	}, waitTime, tickTime)
}
