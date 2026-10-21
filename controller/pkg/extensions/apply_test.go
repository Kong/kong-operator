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

// dataPlaneWithExtension is a DataPlane whose extension reference validates, so
// ApplyExtensions reaches the processor.
func dataPlaneWithExtension() *operatorv1beta1.DataPlane {
	return &operatorv1beta1.DataPlane{
		Name:       "dataplane",
		Namespace:  "default",
		Generation: 1,
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
}

// TestApplyExtensionsClassifies pins, for every classified extension failure,
// the condition the caller relies on: status, reason, message, and the stop
// semantics that keep the reconciler from requeueing.
func TestApplyExtensionsClassifies(t *testing.T) {
	testCases := []struct {
		name       string
		err        error
		wantReason string
	}{
		{
			name:       "cross-namespace reference",
			err:        extensionserrors.ErrCrossNamespaceReference,
			wantReason: string(kcfgkonnect.RefNotPermittedReason),
		},
		{
			name:       "extension not found",
			err:        extensionserrors.ErrKonnectExtensionNotFound,
			wantReason: string(kcfgkonnect.InvalidExtensionRefReason),
		},
		{
			name:       "cluster certificate not found",
			err:        extensionserrors.ErrClusterCertificateNotFound,
			wantReason: string(kcfgkonnect.InvalidSecretRefReason),
		},
		{
			name:       "extension not ready",
			err:        extensionserrors.ErrKonnectExtensionNotReady,
			wantReason: string(kcfgkonnect.KonnectExtensionNotReadyReason),
		},
		{
			name:       "endpoint value that cannot be used",
			err:        fmt.Errorf("%w: control plane endpoint", extensionserrors.ErrInvalidEndpoint),
			wantReason: string(kcfgkonnect.InvalidEndpointReason),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := scheme.Scheme
			require.NoError(t, operatorv1beta1.AddToScheme(s))

			dataPlane := dataPlaneWithExtension()
			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(dataPlane).
				WithStatusSubresource(dataPlane).
				Build()

			stop, res, err := ApplyExtensions(t.Context(), cl, dataPlane.DeepCopy(), true, errorProcessor{err: tc.err})
			require.NoError(t, err)
			assert.True(t, stop, "the caller must stop: the condition carries the feedback")
			assert.True(t, res.IsZero(), "a misconfiguration must not requeue")

			var got operatorv1beta1.DataPlane
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(dataPlane), &got))
			condition, ok := k8sutils.GetCondition(kcfgkonnect.KonnectExtensionAppliedType, &got)
			require.True(t, ok, "expected the KonnectExtensionApplied condition to be patched, got: %+v", got.Status.Conditions)
			assert.Equal(t, metav1.ConditionFalse, condition.Status)
			assert.Equal(t, tc.wantReason, condition.Reason)
			assert.Equal(t, tc.err.Error(), condition.Message)
		})
	}
}
