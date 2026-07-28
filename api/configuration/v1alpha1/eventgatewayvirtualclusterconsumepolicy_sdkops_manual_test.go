package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEventGatewayVirtualClusterConsumePolicyAPISpec_ToCreateRequest_ModifyHeadersAction(t *testing.T) {
	ctx := t.Context()
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	obj := &EventGatewayVirtualClusterConsumePolicy{
		Spec: EventGatewayVirtualClusterConsumePolicySpec{
			APISpec: EventGatewayVirtualClusterConsumePolicyAPISpec{
				EventGatewayVirtualClusterConsumePolicyConfig: &EventGatewayVirtualClusterConsumePolicyConfig{
					Type: EventGatewayVirtualClusterConsumePolicyConfigTypeModifyHeadersPolicyCreate,
					ModifyHeadersPolicyCreate: &EventGatewayModifyHeadersPolicyCreate{
						Name:        "add-header-1",
						Description: "Test Consume Policy to add a header",
						Labels: Labels{
							"app": "test1",
							"env": "test",
						},
						Config: EventGatewayModifyHeadersPolicyCreateConfig{
							Actions: []EventGatewayModifyHeaderAction{
								{
									Op: EventGatewayModifyHeaderActionTypeSet,
									Set: &EventGatewayModifyHeaderSetAction{
										Key:   "x-added-header",
										Value: "added-value",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	req, err := obj.ToCreateEventGatewayVirtualClusterConsumePolicyRequest(ctx, cl)
	if err != nil {
		t.Fatalf("ToCreateEventGatewayVirtualClusterConsumePolicyRequest() error = %v", err)
	}

	policy := req.GetEventGatewayConsumePolicyCreateModifyHeaders()
	if policy == nil {
		t.Fatal("expected modify headers policy in create request")
	}

	actions := policy.Config.Actions
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].EventGatewayModifyHeaderSetAction == nil {
		t.Fatal("expected set action to be populated")
	}
	if got, want := actions[0].EventGatewayModifyHeaderSetAction.Key, "x-added-header"; got != want {
		t.Fatalf("unexpected key: got %q want %q", got, want)
	}
	if got, want := actions[0].EventGatewayModifyHeaderSetAction.Value, "added-value"; got != want {
		t.Fatalf("unexpected value: got %q want %q", got, want)
	}
}
