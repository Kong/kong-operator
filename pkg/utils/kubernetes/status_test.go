package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcfgconsts "github.com/kong/kong-operator/api/common/consts"
	kcfgdataplane "github.com/kong/kong-operator/api/gateway-operator/dataplane"
)

type TestResource struct {
	Generation int64
	Conditions []metav1.Condition
}

func (r *TestResource) GetConditions() []metav1.Condition {
	return r.Conditions
}

func (r *TestResource) SetConditions(conditions []metav1.Condition) {
	r.Conditions = conditions
}

func (r *TestResource) GetGeneration() int64 {
	return r.Generation
}

func TestGetCondition(t *testing.T) {
	expected := metav1.Condition{
		Type:               "example",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	}
	for _, tt := range []struct {
		name          string
		conditions    []metav1.Condition
		condition     string
		expected      metav1.Condition
		expectedFound bool
	}{
		{
			"missing_condition_empty",
			[]metav1.Condition{},
			"not found",
			metav1.Condition{},
			false,
		},
		{
			"missing_condition",
			[]metav1.Condition{
				{
					Type:               "example",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				},
			},
			"not found",
			metav1.Condition{},
			false,
		},
		{
			"condition_found",
			[]metav1.Condition{
				expected,
			},
			"example",
			expected,
			true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource := &TestResource{
				Conditions: tt.conditions,
			}
			current, exists := GetCondition(kcfgconsts.ConditionType(tt.condition), resource)
			assert.Equal(t, tt.expected, current)
			assert.Equal(t, tt.expectedFound, exists)
		})
	}
}

func TestSetCondition(t *testing.T) {
	for _, tt := range []struct {
		name       string
		conditions []metav1.Condition
		condition  metav1.Condition
		expected   []metav1.Condition
	}{
		{
			"empty_set",
			[]metav1.Condition{},
			metav1.Condition{
				Type:   "example",
				Status: metav1.ConditionFalse,
				Reason: "some reason",
			},
			[]metav1.Condition{
				{
					Type:   "example",
					Status: metav1.ConditionFalse,
					Reason: "some reason",
				},
			},
		},
		{
			"replace_condition_1",
			[]metav1.Condition{
				{
					Type:   "example1",
					Status: metav1.ConditionFalse,
					Reason: "some reason 1",
				},
				{
					Type:   "example2",
					Status: metav1.ConditionFalse,
					Reason: "some reason 2",
				},
				{
					Type:   "example3",
					Status: metav1.ConditionFalse,
					Reason: "some reason 3",
				},
			},
			metav1.Condition{
				Type:   "example1",
				Status: metav1.ConditionTrue,
				Reason: "replaced reason",
			},
			[]metav1.Condition{
				{
					Type:   "example1",
					Status: metav1.ConditionTrue,
					Reason: "replaced reason",
				},
				{
					Type:   "example2",
					Status: metav1.ConditionFalse,
					Reason: "some reason 2",
				},
				{
					Type:   "example3",
					Status: metav1.ConditionFalse,
					Reason: "some reason 3",
				},
			},
		},
		{
			"replace_condition_2",
			[]metav1.Condition{
				{
					Type:   "example1",
					Status: metav1.ConditionFalse,
					Reason: "some reason 1",
				},
				{
					Type:   "example2",
					Status: metav1.ConditionFalse,
					Reason: "some reason 2",
				},
				{
					Type:   "example3",
					Status: metav1.ConditionFalse,
					Reason: "some reason 3",
				},
			},
			metav1.Condition{
				Type:   "example2",
				Status: metav1.ConditionTrue,
				Reason: "replaced reason",
			},
			[]metav1.Condition{
				{
					Type:   "example1",
					Status: metav1.ConditionFalse,
					Reason: "some reason 1",
				},
				{
					Type:   "example2",
					Status: metav1.ConditionTrue,
					Reason: "replaced reason",
				},
				{
					Type:   "example3",
					Status: metav1.ConditionFalse,
					Reason: "some reason 3",
				},
			},
		},
		{
			"replace_condition_3",
			[]metav1.Condition{
				{
					Type:   "example1",
					Status: metav1.ConditionFalse,
					Reason: "some reason 1",
				},
				{
					Type:   "example2",
					Status: metav1.ConditionFalse,
					Reason: "some reason 2",
				},
				{
					Type:   "example3",
					Status: metav1.ConditionTrue,
					Reason: "some reason 3",
				},
			},
			metav1.Condition{
				Type:   "example3",
				Status: metav1.ConditionTrue,
				Reason: "replaced reason",
			},
			[]metav1.Condition{
				{
					Type:   "example1",
					Status: metav1.ConditionFalse,
					Reason: "some reason 1",
				},
				{
					Type:   "example2",
					Status: metav1.ConditionFalse,
					Reason: "some reason 2",
				},
				{
					Type:   "example3",
					Status: metav1.ConditionTrue,
					Reason: "replaced reason",
				},
			},
		},
		{
			"add_condition_4",
			[]metav1.Condition{
				{
					Type:   "example1",
					Status: metav1.ConditionFalse,
					Reason: "some reason 1",
				},
				{
					Type:   "example2",
					Status: metav1.ConditionFalse,
					Reason: "some reason 2",
				},
				{
					Type:   "example3",
					Status: metav1.ConditionFalse,
					Reason: "some reason 3",
				},
			},
			metav1.Condition{
				Type:   "example4",
				Status: metav1.ConditionTrue,
				Reason: "new reason",
			},
			[]metav1.Condition{
				{
					Type:   "example1",
					Status: metav1.ConditionFalse,
					Reason: "some reason 1",
				},
				{
					Type:   "example2",
					Status: metav1.ConditionFalse,
					Reason: "some reason 2",
				},
				{
					Type:   "example3",
					Status: metav1.ConditionFalse,
					Reason: "some reason 3",
				},
				{
					Type:   "example4",
					Status: metav1.ConditionTrue,
					Reason: "new reason",
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource := &TestResource{
				Conditions: tt.conditions,
			}
			SetCondition(tt.condition, resource)
			assert.ElementsMatch(t, resource.GetConditions(), tt.expected)
		})
	}
}

func TestIsValidCondition(t *testing.T) {
	resource := &TestResource{
		Conditions: []metav1.Condition{
			{
				Type:   "example 1",
				Status: metav1.ConditionTrue,
			},
			{
				Type:   "example 2",
				Status: metav1.ConditionFalse,
			},
			{
				Type:   "example 3",
				Status: metav1.ConditionUnknown,
			},
		},
	}

	for _, tt := range []struct {
		name     string
		input    string
		expected bool
	}{
		{
			"missing_condition",
			"not found",
			false,
		},
		{
			"true_condition",
			"example 1",
			true,
		},
		{
			"false_condition",
			"example 2",
			false,
		},
		{
			"unknown_condition",
			"example 3",
			false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := HasConditionTrue(kcfgconsts.ConditionType(tt.input), resource)
			assert.Equal(t, tt.expected, current)
		})
	}
}

func TestIsReady(t *testing.T) {
	for _, tt := range []struct {
		name       string
		conditions []metav1.Condition
		expected   bool
	}{
		{
			"empty",
			[]metav1.Condition{},
			false,
		},
		{
			"true",
			[]metav1.Condition{
				{
					Type:   string(kcfgdataplane.ReadyType),
					Status: metav1.ConditionTrue,
				},
			},
			true,
		},
		{
			"false_ready_missing",
			[]metav1.Condition{
				{
					Type:   "otherType",
					Status: metav1.ConditionTrue,
				},
			},
			false,
		},
		{
			"false_not_ready",
			[]metav1.Condition{
				{
					Type:   "otherType",
					Status: metav1.ConditionTrue,
				},
				{
					Type:   string(kcfgdataplane.ReadyType),
					Status: metav1.ConditionFalse,
				},
			},
			false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource := &TestResource{
				Conditions: tt.conditions,
			}
			current := IsReady(resource)
			assert.Equal(t, tt.expected, current)
		})
	}
}

func TestSetReady(t *testing.T) {
	for _, tt := range []struct {
		name       string
		conditions []metav1.Condition
		expected   bool
	}{
		{
			"empty_should_be_ready",
			[]metav1.Condition{},
			true,
		},
		{
			"override_no_other_conditions",
			[]metav1.Condition{
				{
					Type:   string(kcfgdataplane.ReadyType),
					Status: metav1.ConditionFalse,
				},
			},
			true,
		},
		{
			"true_ready_missing",
			[]metav1.Condition{
				{
					Type:   "otherType",
					Status: metav1.ConditionTrue,
				},
			},
			true,
		},
		{
			"false_ready_missing",
			[]metav1.Condition{
				{
					Type:   "otherType",
					Status: metav1.ConditionFalse,
				},
			},
			false,
		},
		{
			"unknown_ready_missing",
			[]metav1.Condition{
				{
					Type:   "otherType",
					Status: metav1.ConditionUnknown,
				},
			},
			false,
		},
		{
			"true_override_ready",
			[]metav1.Condition{
				{
					Type:   "otherType",
					Status: metav1.ConditionTrue,
				},
				{
					Type:   string(kcfgdataplane.ReadyType),
					Status: metav1.ConditionFalse,
				},
			},
			true,
		},
		{
			"false_override_ready",
			[]metav1.Condition{
				{
					Type:   "otherType",
					Status: metav1.ConditionFalse,
				},
				{
					Type:   string(kcfgdataplane.ReadyType),
					Status: metav1.ConditionTrue,
				},
			},
			false,
		},
		{
			"unknown_override_ready",
			[]metav1.Condition{
				{
					Type:   "otherType",
					Status: metav1.ConditionUnknown,
				},
				{
					Type:   string(kcfgdataplane.ReadyType),
					Status: metav1.ConditionTrue,
				},
			},
			false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource := &TestResource{
				Conditions: tt.conditions,
			}
			SetReady(resource)
			current := IsReady(resource)
			assert.Equal(t, tt.expected, current)
		})
	}
}

func TestInitReady(t *testing.T) {
	resource := &TestResource{}
	InitReady(resource)
	conditions := resource.GetConditions()
	assert.Len(t, conditions, 1)
	assert.Equal(t, string(kcfgdataplane.ReadyType), conditions[0].Type)
	assert.Equal(t, string(kcfgdataplane.DependenciesNotReadyReason), conditions[0].Reason)
	assert.Equal(t, kcfgdataplane.DependenciesNotReadyMessage, conditions[0].Message)
	assert.NotEmpty(t, conditions[0].LastTransitionTime)
}

func TestNeedsUpdate(t *testing.T) {
	defaultCondition := metav1.Condition{
		Type:    "type",
		Reason:  "reason",
		Message: "message",
		Status:  metav1.StatusSuccess,
	}

	for _, tt := range []struct {
		name     string
		current  []metav1.Condition
		updated  []metav1.Condition
		expected bool
	}{
		{
			"all empty",
			[]metav1.Condition{},
			[]metav1.Condition{},
			false,
		},
		{
			"one empty",
			[]metav1.Condition{},
			[]metav1.Condition{defaultCondition},
			true,
		},
		{
			"single equal condition",
			[]metav1.Condition{defaultCondition},
			[]metav1.Condition{defaultCondition},
			false,
		},
		{
			"different type",
			[]metav1.Condition{defaultCondition},
			[]metav1.Condition{
				{
					Type:    "some other type",
					Reason:  "reason",
					Message: "message",
					Status:  metav1.StatusSuccess,
				},
			},
			true,
		},
		{
			"different reason",
			[]metav1.Condition{defaultCondition},
			[]metav1.Condition{
				{
					Type:    "some type",
					Reason:  "other reason",
					Message: "message",
					Status:  metav1.StatusSuccess,
				},
			},
			true,
		},
		{
			"different message",
			[]metav1.Condition{defaultCondition},
			[]metav1.Condition{
				{
					Type:    "some type",
					Reason:  "reason",
					Message: "other message",
					Status:  metav1.StatusSuccess,
				},
			},
			true,
		},
		{
			"different status",
			[]metav1.Condition{defaultCondition},
			[]metav1.Condition{
				{
					Type:    "some type",
					Reason:  "reason",
					Message: "message",
					Status:  metav1.StatusFailure,
				},
			},
			true,
		},
		{
			"one more condition status",
			[]metav1.Condition{defaultCondition},
			[]metav1.Condition{
				defaultCondition,
				{
					Type:    "some other type",
					Reason:  "reason",
					Message: "message",
					Status:  metav1.StatusSuccess,
				},
			},
			true,
		},
		{
			"different order",
			[]metav1.Condition{
				{
					Type:    "some other type",
					Reason:  "reason",
					Message: "message",
					Status:  metav1.StatusSuccess,
				},
				defaultCondition,
			},
			[]metav1.Condition{
				defaultCondition,
				{
					Type:    "some other type",
					Reason:  "reason",
					Message: "message",
					Status:  metav1.StatusSuccess,
				},
			},
			false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := &TestResource{
				Conditions: tt.current,
			}
			updated := &TestResource{
				Conditions: tt.updated,
			}
			assert.Equal(t, tt.expected, ConditionsNeedsUpdate(current, updated))
			assert.Equal(t, tt.expected, ConditionsNeedsUpdate(updated, current))
		})
	}
}
