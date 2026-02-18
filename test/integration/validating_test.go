package integration

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test/helpers"
)

func TestDataPlaneValidation(t *testing.T) {
	t.Parallel()
	namespace, _ := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("running tests for validation performed during reconciling")
	testDataPlaneReconcileValidation(t, namespace)
}

func testDataPlaneReconcileValidation(t *testing.T, namespace *corev1.Namespace) {
	testCases := []struct {
		name             string
		dataplane        *operatorv1beta1.DataPlane
		creationErr      bool
		validatingOK     bool
		conditionMessage string
	}{
		{
			name: "reconciler:validating_error_with_empty_deployoptions",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
					Name:      uuid.NewString(),
				},
			},
			creationErr:      true,
			conditionMessage: "DataPlane requires an image",
		},
	}

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataplane, err := dataplaneClient.Create(GetCtx(), tc.dataplane, metav1.CreateOptions{})
			if tc.creationErr {
				require.Error(t, err, "should return error when create dataplane for case %s", tc.name)
				return
			}

			require.NoErrorf(t, err, "should not return error when create dataplane for case %s", tc.name)

			if tc.validatingOK {
				t.Logf("%s: verifying deployments managed by the dataplane", t.Name())
				w, err := GetClients().K8sClient.AppsV1().Deployments(namespace.Name).Watch(GetCtx(), metav1.ListOptions{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					LabelSelector: fmt.Sprintf("%s=%s", consts.GatewayOperatorManagedByLabel, consts.DataPlaneManagedLabelValue),
				})
				require.NoError(t, err)
				t.Cleanup(func() { w.Stop() })
				for {
					select {
					case <-GetCtx().Done():
						t.Fatalf("context expired: %v", GetCtx().Err())
					case event := <-w.ResultChan():
						deployment, ok := event.Object.(*appsv1.Deployment)
						require.True(t, ok)
						if deployment.Status.AvailableReplicas < deployment.Status.ReadyReplicas {
							continue
						}
						if !lo.ContainsBy(deployment.OwnerReferences, func(or metav1.OwnerReference) bool {
							return or.UID == dataplane.UID
						}) {
							continue
						}

						return
					}
				}
			} else {
				t.Logf("%s: verifying DataPlane conditions", t.Name())
				w, err := dataplaneClient.Watch(GetCtx(), metav1.ListOptions{
					TypeMeta: metav1.TypeMeta{
						Kind:       "DataPlane",
						APIVersion: operatorv1beta1.SchemeGroupVersion.String(),
					},
					FieldSelector: "metadata.name=" + tc.dataplane.Name,
				})
				require.NoError(t, err)
				t.Cleanup(func() { w.Stop() })
				for {
					select {
					case <-GetCtx().Done():
						t.Fatalf("context expired: %v", GetCtx().Err())
					case event := <-w.ResultChan():
						dataplane, ok := event.Object.(*operatorv1beta1.DataPlane)
						require.True(t, ok)

						var cond metav1.Condition
						for _, condition := range dataplane.Status.Conditions {
							if condition.Type == string(kcfgdataplane.ReadyType) {
								cond = condition
								break
							}
						}
						t.Log("verifying conditions of invalid dataplanes")
						if cond.Status != metav1.ConditionFalse {
							t.Logf("Ready condition status should be false")
							continue
						}
						if cond.Message != tc.conditionMessage {
							t.Logf("Ready condition message should be the same as expected")
							continue
						}

						return
					}
				}
			}
		})
	}
}
