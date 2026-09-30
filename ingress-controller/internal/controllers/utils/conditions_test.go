package utils_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/controllers/utils"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/kubernetes/object"
)

func TestEnsureProgrammedCondition(t *testing.T) {
	const testObjectGeneration = 2
	var (
		expectedProgrammedConditionTrue = metav1.Condition{
			Type:               string(configurationv1.ConditionProgrammed),
			Status:             metav1.ConditionTrue,
			ObservedGeneration: testObjectGeneration,
			Reason:             string(configurationv1.ReasonProgrammed),
			Message:            utils.ProgrammedConditionTrueMessage,
		}
		expectedProgrammedConditionFalse = metav1.Condition{
			Type:               string(configurationv1.ConditionProgrammed),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: testObjectGeneration,
			Reason:             string(configurationv1.ReasonInvalid),
			Message:            utils.ProgrammedConditionFalseInvalidMessage,
		}
		expectedProgrammedConditionUnknown = metav1.Condition{
			Type:               string(configurationv1.ConditionProgrammed),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: testObjectGeneration,
			Reason:             string(configurationv1.ReasonPending),
			Message:            utils.ProgrammedConditionFalsePendingMessage,
		}
	)

	testCases := []struct {
		name string

		configurationStatus object.ConfigurationStatus
		conditions          []metav1.Condition
		options             []utils.ProgrammedConditionOption

		expectedUpdatedConditions []metav1.Condition
		expectedUpdateNeeded      bool
	}{
		{
			name:                      "condition already present with correct status and observed generation",
			configurationStatus:       object.ConfigurationStatusSucceeded,
			conditions:                []metav1.Condition{expectedProgrammedConditionTrue},
			expectedUpdatedConditions: []metav1.Condition{expectedProgrammedConditionTrue},
			expectedUpdateNeeded:      false,
		},
		{
			name:                "condition present with correct status but older observed generation",
			configurationStatus: object.ConfigurationStatusSucceeded,
			conditions: []metav1.Condition{
				func() metav1.Condition {
					cond := expectedProgrammedConditionTrue
					cond.ObservedGeneration = 1
					return cond
				}(),
			},
			expectedUpdatedConditions: []metav1.Condition{expectedProgrammedConditionTrue},
			expectedUpdateNeeded:      true,
		},
		{
			name:                "condition present with correct observed generation but different status",
			configurationStatus: object.ConfigurationStatusFailed,
			conditions: []metav1.Condition{
				func() metav1.Condition {
					cond := expectedProgrammedConditionFalse
					cond.Status = metav1.ConditionTrue
					return cond
				}(),
			},
			expectedUpdatedConditions: []metav1.Condition{expectedProgrammedConditionFalse},
			expectedUpdateNeeded:      true,
		},
		{
			name:                "condition present with correct observed generation but different reason",
			configurationStatus: object.ConfigurationStatusFailed,
			conditions: []metav1.Condition{
				func() metav1.Condition {
					cond := expectedProgrammedConditionFalse
					cond.Reason = string("SomeOtherReason")
					return cond
				}(),
			},
			expectedUpdatedConditions: []metav1.Condition{expectedProgrammedConditionFalse},
			expectedUpdateNeeded:      true,
		},
		{
			name:                      "Unknown status should not modify existing Programmed condition",
			configurationStatus:       object.ConfigurationStatusUnknown,
			conditions:                []metav1.Condition{expectedProgrammedConditionTrue},
			expectedUpdatedConditions: []metav1.Condition{expectedProgrammedConditionTrue},
			expectedUpdateNeeded:      false,
		},
		{
			name:                      "empty conditions",
			configurationStatus:       object.ConfigurationStatusSucceeded,
			conditions:                nil,
			expectedUpdatedConditions: []metav1.Condition{expectedProgrammedConditionTrue},
			expectedUpdateNeeded:      true,
		},
		{
			name:                "condition for Unknown status with custom message",
			configurationStatus: object.ConfigurationStatusUnknown,
			conditions:          nil,
			options: []utils.ProgrammedConditionOption{
				utils.WithUnknownMessage("some other message"),
			},
			expectedUpdatedConditions: []metav1.Condition{
				func() metav1.Condition {
					cond := expectedProgrammedConditionUnknown
					cond.Message = "some other message"
					return cond
				}(),
			},
			expectedUpdateNeeded: true,
		},
		{
			name:                "condition for Succeeded status not affected by custom Unknown message",
			configurationStatus: object.ConfigurationStatusSucceeded,
			conditions:          nil,
			options: []utils.ProgrammedConditionOption{
				utils.WithUnknownMessage("some other message"),
			},
			expectedUpdatedConditions: []metav1.Condition{expectedProgrammedConditionTrue},
			expectedUpdateNeeded:      true,
		},
		{
			name:                "Failed condition with custom message",
			configurationStatus: object.ConfigurationStatusFailed,
			conditions:          nil,
			options: []utils.ProgrammedConditionOption{
				utils.WithFailedMessage("unresolved provider reference"),
			},
			expectedUpdatedConditions: []metav1.Condition{
				func() metav1.Condition {
					cond := expectedProgrammedConditionFalse
					cond.Message = "unresolved provider reference"
					return cond
				}(),
			},
			expectedUpdateNeeded: true,
		},
		{
			name:                "Failed condition ignores empty custom message",
			configurationStatus: object.ConfigurationStatusFailed,
			conditions:          nil,
			options: []utils.ProgrammedConditionOption{
				utils.WithFailedMessage(""),
			},
			expectedUpdatedConditions: []metav1.Condition{expectedProgrammedConditionFalse},
			expectedUpdateNeeded:      true,
		},
		{
			name:                "message-only change forces update",
			configurationStatus: object.ConfigurationStatusFailed,
			conditions: []metav1.Condition{
				func() metav1.Condition {
					cond := expectedProgrammedConditionFalse
					cond.Message = "old error"
					return cond
				}(),
			},
			options: []utils.ProgrammedConditionOption{
				utils.WithFailedMessage("new error"),
			},
			expectedUpdatedConditions: []metav1.Condition{
				func() metav1.Condition {
					cond := expectedProgrammedConditionFalse
					cond.Message = "new error"
					return cond
				}(),
			},
			expectedUpdateNeeded: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			conditions, updateNeeded := utils.EnsureProgrammedCondition(tc.configurationStatus, testObjectGeneration, tc.conditions, tc.options...)
			assert.Equal(t, tc.expectedUpdateNeeded, updateNeeded)

			ignoreLastTransitionTime := cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")
			diff := cmp.Diff(conditions, tc.expectedUpdatedConditions, ignoreLastTransitionTime)
			assert.Empty(t, diff, "conditions mismatch")
		})
	}
}

func TestEnsureProgrammedConditionPreservesTransitionTimeOnMessageChange(t *testing.T) {
	const generation = int64(2)
	transitionTime := metav1.Now()
	existing := []metav1.Condition{{
		Type:               string(configurationv1.ConditionProgrammed),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             string(configurationv1.ReasonInvalid),
		Message:            "old error",
		LastTransitionTime: transitionTime,
	}}

	conditions, updateNeeded := utils.EnsureProgrammedCondition(
		object.ConfigurationStatusFailed, generation, existing,
		utils.WithFailedMessage("new error"),
	)
	assert.True(t, updateNeeded)
	assert.Len(t, conditions, 1)
	assert.Equal(t, "new error", conditions[0].Message)
	assert.Equal(t, transitionTime, conditions[0].LastTransitionTime)
}
