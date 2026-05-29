package v1alpha1

import "testing"

func TestEventGatewayVirtualClusterConsumePolicyAPISpec_ToCreateRequest_ModifyHeadersAction(t *testing.T) {
	spec := &EventGatewayVirtualClusterConsumePolicyAPISpec{
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
	}

	req, err := spec.ToCreateEventGatewayVirtualClusterConsumePolicyRequest()
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
