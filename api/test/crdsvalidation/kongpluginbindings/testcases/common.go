package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// kpbTestCase is a test case related to KongPluginBinding validation.
type kpbTestCase struct {
	Name                 string
	KongPluginBinding    configurationv1alpha1.KongPluginBinding
	ExpectedErrorMessage *string
}

// kpbTestCasesGroup is a group of test cases related to KongPluginBinding validation. The grouping is done by a common theme.
type kpbTestCasesGroup struct {
	Name      string
	TestCases []kpbTestCase
}

// TestCases is a collection of all test cases groups related to KongPluginBinding validation.
var TestCases = []kpbTestCasesGroup{}

func init() {
	TestCases = append(TestCases,
		pluginRefTCs,
		targetsCombinationsTCs,
		crossTargetsTCs,
		wrongTargetsGroupKindTCs,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-binding-",
	Namespace:    "default",
}
