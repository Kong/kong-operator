package conditions

import (
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

// KonnectEntityIsProgrammed asserts that the Programmed condition
// is set to True and the Konnect status fields are populated.
func KonnectEntityIsProgrammed(
	t assert.TestingT,
	obj interface {
		GetKonnectStatus() *konnectv1alpha2.KonnectEntityStatus
		GetConditions() []metav1.Condition
	},
) {
	konnectStatus := obj.GetKonnectStatus()
	if !assert.NotNil(t, konnectStatus) {
		return
	}
	assert.NotEmpty(t, konnectStatus.GetKonnectID(), "empty Konnect ID")
	assert.NotEmpty(t, konnectStatus.GetOrgID(), "empty Org ID")
	assert.NotEmpty(t, konnectStatus.GetServerURL(), "empty Server URL")

	conditionTypeProgrammed := konnectv1alpha1.KonnectEntityProgrammedConditionType
	assert.True(t,
		lo.ContainsBy(obj.GetConditions(),
			func(c metav1.Condition) bool {
				return c.Type == conditionTypeProgrammed &&
					c.Status == metav1.ConditionTrue
			},
		),
		"condition %s is not set to True", conditionTypeProgrammed,
	)
}
