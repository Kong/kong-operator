package kubernetes

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TestResource struct {
	Conditions []metav1.Condition
}

func (r *TestResource) GetConditions() []metav1.Condition {
	return r.Conditions
}

func (r *TestResource) SetConditions(conditions []metav1.Condition) {
	r.Conditions = conditions
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resource := &TestResource{
				Conditions: tt.conditions,
			}
			current, exists := GetCondition(ConditionType(tt.condition), resource)
			assert.Equal(t, current, tt.expected)
			assert.Equal(t, exists, tt.expectedFound)
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
		tt := tt
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {

			current := IsValidCondition(ConditionType(tt.input), resource)
			assert.Equal(t, current, tt.expected)
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
					Type:   string(ReadyType),
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
					Type:   string(ReadyType),
					Status: metav1.ConditionFalse,
				},
			},
			false,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resource := &TestResource{
				tt.conditions,
			}
			current := IsReady(resource)
			assert.Equal(t, current, tt.expected)
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
					Type:   string(ReadyType),
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
					Type:   string(ReadyType),
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
					Type:   string(ReadyType),
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
					Type:   string(ReadyType),
					Status: metav1.ConditionTrue,
				},
			},
			false,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resource := &TestResource{
				tt.conditions,
			}
			SetReady(resource)
			current := IsReady(resource)
			assert.Equal(t, current, tt.expected)
		})
	}
}

func TestInitReady(t *testing.T) {
	resource := &TestResource{}
	InitReady(resource)
	conditions := resource.GetConditions()
	assert.Equal(t, 1, len(conditions))
	assert.Equal(t, string(ReadyType), conditions[0].Type)
	assert.Equal(t, string(DependenciesNotReadyReason), conditions[0].Reason)
	assert.Equal(t, DependenciesNotReadyMessage, conditions[0].Message)
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			current := &TestResource{tt.current}
			updated := &TestResource{tt.updated}
			assert.Equal(t, tt.expected, NeedsUpdate(current, updated))
			assert.Equal(t, tt.expected, NeedsUpdate(updated, current))
		})
	}

}
