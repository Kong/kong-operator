package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
)

// testCase is a test case related to KongConsumer validation.
type testCase struct {
	Name                       string
	KongConsumer               configurationv1.KongConsumer
	KongConsumerStatus         *configurationv1.KongConsumerStatus
	Update                     func(*configurationv1.KongConsumer)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to KongConsumer validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongConsumer validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		requiredFields,
		updatesNotAllowedForStatus,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongconsumer-",
	Namespace:    "default",
}
