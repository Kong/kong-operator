package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongPluginBinding validation.
type testCase struct {
	Name                       string
	KongPluginBinding          configurationv1alpha1.KongPluginBinding
	KongPluginBindingStatus    *configurationv1alpha1.KongPluginBindingStatus
	Update                     func(*configurationv1alpha1.KongPluginBinding)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to KongPluginBinding validation. The grouping is done by a common theme.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongPluginBinding validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		pluginRefTCs,
		targetsCombinationsTCs,
		crossTargetsTCs,
		wrongTargetsGroupKindTCs,
		updatesNotAllowedForStatusTCs,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-binding-",
	Namespace:    "default",
}
