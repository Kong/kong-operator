package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
)

// testCase is a test case related to KongConsumerGroup validation.
type testCase struct {
	Name                       string
	KongConsumerGroup          configurationv1beta1.KongConsumerGroup
	KongConsumerGroupStatus    *configurationv1beta1.KongConsumerGroupStatus
	Update                     func(*configurationv1beta1.KongConsumerGroup)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to KongConsumerGroup validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongConsumerGroup validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		fields,
		controlPlaneRef,
		updatesNotAllowedForStatus,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongconsumergroup-",
	Namespace:    "default",
}
