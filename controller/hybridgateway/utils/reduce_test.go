package utils_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
)

func makeUnstructuredWithTimestamp(name string, t time.Time) unstructured.Unstructured {
	obj := unstructured.Unstructured{}
	obj.SetName(name)
	obj.SetCreationTimestamp(metav1.Time{Time: t})
	return obj
}

func TestKeepYoungest(t *testing.T) {
	now := time.Now()
	type testCase struct {
		name     string
		objs     []unstructured.Unstructured
		expected []string
	}

	testCases := []testCase{
		{
			name: "returns all but youngest",
			objs: []unstructured.Unstructured{
				makeUnstructuredWithTimestamp("obj1", now.Add(-10*time.Minute)),
				makeUnstructuredWithTimestamp("obj2", now.Add(-5*time.Minute)),
				makeUnstructuredWithTimestamp("obj3", now.Add(-1*time.Minute)),
			},
			expected: []string{"obj1", "obj2"},
		},
		{
			name:     "returns empty slice for empty slice",
			objs:     []unstructured.Unstructured{},
			expected: []string{},
		},
		{
			name:     "returns empty slice for single element",
			objs:     []unstructured.Unstructured{makeUnstructuredWithTimestamp("only", now)},
			expected: []string{},
		},
		{
			name: "returns all but youngest when youngest is first",
			objs: []unstructured.Unstructured{
				makeUnstructuredWithTimestamp("obj1", now.Add(1*time.Minute)),
				makeUnstructuredWithTimestamp("obj2", now.Add(-5*time.Minute)),
				makeUnstructuredWithTimestamp("obj3", now.Add(-10*time.Minute)),
			},
			expected: []string{"obj2", "obj3"},
		},
		{
			name: "returns all but youngest when youngest is last",
			objs: []unstructured.Unstructured{
				makeUnstructuredWithTimestamp("obj1", now.Add(-10*time.Minute)),
				makeUnstructuredWithTimestamp("obj2", now.Add(-5*time.Minute)),
				makeUnstructuredWithTimestamp("obj3", now.Add(2*time.Minute)),
			},
			expected: []string{"obj1", "obj2"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := utils.KeepYoungest(tc.objs)
			var names []string
			for _, obj := range result {
				names = append(names, obj.GetName())
			}
			assert.ElementsMatch(t, tc.expected, names)
		})
	}
}

func makeUnstructuredWithProgrammedCondition(name string, programmed bool) unstructured.Unstructured {
	obj := unstructured.Unstructured{}
	obj.SetName(name)
	condition := map[string]any{
		"type":   "Programmed",
		"status": "False",
	}
	if programmed {
		condition["status"] = "True"
	}
	obj.Object["status"] = map[string]any{
		"conditions": []any{condition},
	}
	return obj
}

func makeUnstructuredWithNoConditions(name string) unstructured.Unstructured {
	obj := unstructured.Unstructured{}
	obj.SetName(name)
	return obj
}

func TestKeepProgrammed(t *testing.T) {
	type testCase struct {
		name     string
		objs     []unstructured.Unstructured
		expected []string
	}

	testCases := []testCase{
		{
			name: "returns only not programmed objects",
			objs: []unstructured.Unstructured{
				makeUnstructuredWithProgrammedCondition("obj1", true),
				makeUnstructuredWithProgrammedCondition("obj2", false),
				makeUnstructuredWithProgrammedCondition("obj3", false),
			},
			expected: []string{"obj2", "obj3"},
		},
		{
			name: "returns none if all are programmed",
			objs: []unstructured.Unstructured{
				makeUnstructuredWithProgrammedCondition("obj1", true),
				makeUnstructuredWithProgrammedCondition("obj2", true),
			},
			expected: []string{},
		},
		{
			name: "returns only not programmed when mixed and one has no conditions",
			objs: []unstructured.Unstructured{
				makeUnstructuredWithProgrammedCondition("obj1", true),
				makeUnstructuredWithNoConditions("obj2"),
				makeUnstructuredWithProgrammedCondition("obj3", false),
			},
			expected: []string{"obj2", "obj3"},
		},
		{
			name: "returns none if all have no conditions",
			objs: []unstructured.Unstructured{
				makeUnstructuredWithNoConditions("obj1"),
				makeUnstructuredWithNoConditions("obj2"),
			},
			expected: []string{},
		},
		{
			name:     "returns empty slice for empty input",
			objs:     []unstructured.Unstructured{},
			expected: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := utils.KeepProgrammed(tc.objs)
			var names []string
			for _, obj := range result {
				names = append(names, obj.GetName())
			}
			assert.ElementsMatch(t, tc.expected, names)
		})
	}
}
