package extensions

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	kcfgkonnect "github.com/kong/kong-operator/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	extensionserrors "github.com/kong/kong-operator/v2/controller/pkg/extensions/errors"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// errorProcessor is a Processor whose Process always returns the given error.
type errorProcessor struct {
	err error
}

func (p errorProcessor) Process(context.Context, client.Client, client.Object) (bool, error) {
	return false, p.err
}

func TestApplyExtensionsClassifiesInvalidEndpoint(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, operatorv1beta1.AddToScheme(s))

	dataPlane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "dataplane",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: konnectv1alpha1.SchemeGroupVersion.Group,
						Kind:  konnectv1alpha2.KonnectExtensionKind,
						Name:  "konnect-ext",
					},
				},
			},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(dataPlane).
		WithStatusSubresource(dataPlane).
		Build()

	stop, res, err := ApplyExtensions(t.Context(), cl, dataPlane.DeepCopy(), true,
		errorProcessor{err: fmt.Errorf("%w: control plane endpoint", extensionserrors.ErrInvalidEndpoint)})
	require.NoError(t, err)
	assert.True(t, stop, "the caller must stop: the condition carries the feedback")
	assert.True(t, res.IsZero(), "a misconfiguration must not requeue")

	var got operatorv1beta1.DataPlane
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(dataPlane), &got))
	condition, ok := k8sutils.GetCondition(kcfgkonnect.KonnectExtensionAppliedType, &got)
	require.True(t, ok, "expected the KonnectExtensionApplied condition to be patched, got: %+v", got.Status.Conditions)
	assert.Equal(t, metav1.ConditionFalse, condition.Status)
	assert.Equal(t, string(kcfgkonnect.InvalidEndpointReason), condition.Reason)
	assert.Contains(t, condition.Message, "control plane endpoint")
}
