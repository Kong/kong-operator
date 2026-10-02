package onpremconfig

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/status"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

// stubStatusClient serves a fixed configuration status and failure message.
type stubStatusClient struct {
	configStatus status.ConfigurationStatus
	message      string
	enableState  bool
}

func (s *stubStatusClient) AreKubernetesObjectReportsEnabled() bool { return s.enableState }
func (s *stubStatusClient) KubernetesObjectConfigurationStatus(_ client.Object) status.ConfigurationStatus {
	return s.configStatus
}
func (s *stubStatusClient) KubernetesObjectIsConfigured(_ client.Object) bool {
	return s.configStatus == status.ConfigurationStatusSucceeded
}
func (s *stubStatusClient) KubernetesObjectConfigurationStatusMessage(_ client.Object) string {
	return s.message
}

func onPremModelFixture(name string, generation int64) *aiconfigurationv1alpha1.AIGatewayModel {
	model := &aiconfigurationv1alpha1.AIGatewayModel{
		Namespace:  "default",
		Name:       name,
		Generation: generation,
		Spec: aiconfigurationv1alpha1.AIGatewayModelSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
		},
	}
	return model
}

func newReconcilerClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayModel{}).Build()
}

func getProgrammedCondition(t *testing.T, cl client.Client, nn client.ObjectKey) metav1.Condition {
	t.Helper()
	model := &aiconfigurationv1alpha1.AIGatewayModel{}
	require.NoError(t, cl.Get(t.Context(), nn, model))
	var programmed *metav1.Condition
	for i := range model.Status.Conditions {
		if model.Status.Conditions[i].Type == string(configurationv1.ConditionProgrammed) {
			programmed = &model.Status.Conditions[i]
		}
	}
	require.NotNil(t, programmed, "Programmed condition should be present")
	return *programmed
}

func TestReconcilerWritesProgrammedCondition(t *testing.T) {
	t.Parallel()

	model := onPremModelFixture("model-a", 3)
	cl := newReconcilerClient(t, model)
	rec := &AIGatewayModelReconciler{}
	rec.SetCommonFields(
		cl, nil, logr.Discard(), 0, changenotifier.New(),
		&stubStatusClient{configStatus: status.ConfigurationStatusSucceeded, enableState: true},
		nil,
	)

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(model)})
	require.NoError(t, err)

	cond := getProgrammedCondition(t, cl, client.ObjectKeyFromObject(model))
	require.Equal(t, metav1.ConditionTrue, cond.Status)
	require.Equal(t, string(configurationv1.ReasonProgrammed), cond.Reason)
	require.Equal(t, int64(3), cond.ObservedGeneration)
}

func TestReconcilerWritesFailedConditionWithMessage(t *testing.T) {
	t.Parallel()

	model := onPremModelFixture("model-a", 3)
	cl := newReconcilerClient(t, model)
	rec := &AIGatewayModelReconciler{}
	rec.SetCommonFields(
		cl, nil, logr.Discard(), 0, changenotifier.New(),
		&stubStatusClient{
			configStatus: status.ConfigurationStatusFailed,
			message:      "AIGatewayModel default/model-a: spec.apiSpec is required",
			enableState:  true,
		},
		nil,
	)

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(model)})
	require.NoError(t, err)

	cond := getProgrammedCondition(t, cl, client.ObjectKeyFromObject(model))
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(configurationv1.ReasonInvalid), cond.Reason)
	require.Equal(t, "AIGatewayModel default/model-a: spec.apiSpec is required", cond.Message)
}

func TestReconcilerSkipsStatusWhenReportsDisabled(t *testing.T) {
	t.Parallel()

	model := onPremModelFixture("model-a", 3)
	cl := newReconcilerClient(t, model)
	rec := &AIGatewayModelReconciler{}
	rec.SetCommonFields(
		cl, nil, logr.Discard(), 0, changenotifier.New(),
		&stubStatusClient{configStatus: status.ConfigurationStatusSucceeded, enableState: false},
		nil,
	)

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(model)})
	require.NoError(t, err)

	stored := &aiconfigurationv1alpha1.AIGatewayModel{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(model), stored))
	require.Empty(t, stored.Status.Conditions)
}
