package resources

import (
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/apis/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// GeneratePodDisruptionBudgetForDataPlane generates a PodDisruptionBudget for the given DataPlane.
func GeneratePodDisruptionBudgetForDataPlane(dataplane *operatorv1beta1.DataPlane) (*policyv1.PodDisruptionBudget, error) {
	if dataplane.Spec.Resources.PodDisruptionBudget == nil {
		return nil, fmt.Errorf("cannot generate PodDisruptionBudget for DataPlane which doesn't have PodDisruptionBudget defined")
	}

	labels := GetManagedLabelForOwner(dataplane)
	labels["app"] = dataplane.Name
	labels[consts.OperatorLabelSelector] = dataplane.Status.Selector

	pdbSpec := dataplane.Spec.Resources.PodDisruptionBudget.Spec
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dataplane.Name,
			Namespace: dataplane.Namespace,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			// Selector is defined dynamically based on the labels of the DataPlane.
			Selector: &metav1.LabelSelector{
				MatchLabels: client.MatchingLabels{
					// Only pods with the same app and selector label as the DataPlane's Deployment should be
					// considered for the PDB.
					"app":                        dataplane.Name,
					consts.OperatorLabelSelector: dataplane.Status.Selector,
				},
			},
			// The rest of the fields is directly copied from the DP's PDB spec.
			MinAvailable:               pdbSpec.MinAvailable,
			MaxUnavailable:             pdbSpec.MaxUnavailable,
			UnhealthyPodEvictionPolicy: pdbSpec.UnhealthyPodEvictionPolicy,
		},
	}

	k8sutils.SetOwnerForObject(pdb, dataplane)

	return pdb, nil
}
