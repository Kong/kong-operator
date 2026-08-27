/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// ensureHPA reconciles the HPA for the given AIGatewayDataPlane.
// If horizontal scaling is not configured, any existing HPA is deleted.
func (r *Reconciler) ensureHPA(
	ctx context.Context,
	logger logr.Logger,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
	deploymentName string,
) error {
	matchingLabels := k8sresources.GetManagedLabelForOwner(aigwdp)
	hpas, err := k8sutils.ListHPAsForOwner(ctx, r.Client, aigwdp.Namespace, aigwdp.UID, matchingLabels)
	if err != nil {
		return fmt.Errorf("failed listing HPAs for AIGatewayDataPlane %s/%s: %w", aigwdp.Namespace, aigwdp.Name, err)
	}

	if aigwdp.Spec.Deployment == nil ||
		aigwdp.Spec.Deployment.Scaling == nil ||
		aigwdp.Spec.Deployment.Scaling.HorizontalScaling == nil {
		if err := k8sreduce.ReduceHPAs(ctx, r.Client, hpas, k8sreduce.FilterNone); err != nil {
			return fmt.Errorf("failed reducing HPAs for AIGatewayDataPlane %s/%s: %w", aigwdp.Namespace, aigwdp.Name, err)
		}
		return nil
	}

	if len(hpas) > 1 {
		if err := k8sreduce.ReduceHPAs(ctx, r.Client, hpas, k8sreduce.FilterHPAs); err != nil {
			return fmt.Errorf("failed reducing HPAs for AIGatewayDataPlane %s/%s: %w", aigwdp.Namespace, aigwdp.Name, err)
		}
		return nil
	}

	hs := aigwdp.Spec.Deployment.Scaling.HorizontalScaling
	generatedHPA, err := k8sresources.GenerateHPA(aigwdp, &k8sresources.HPAScalingSpec{
		MinReplicas: hs.MinReplicas,
		MaxReplicas: hs.MaxReplicas,
		Metrics:     hs.Metrics,
		Behavior:    hs.Behavior,
	}, deploymentName)
	if err != nil {
		return err
	}

	if len(hpas) == 1 {
		existingHPA := &hpas[0]
		oldExistingHPA := existingHPA.DeepCopy()
		var updated bool
		updated, existingHPA.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingHPA.ObjectMeta, generatedHPA.ObjectMeta)
		if !cmp.Equal(existingHPA.Spec, generatedHPA.Spec) {
			existingHPA.Spec = generatedHPA.Spec
			updated = true
		}
		// op.Result and the returned object are unused; errors surface to the caller.
		_, _, err := patch.ApplyPatchIfNotEmpty(ctx, r.Client, logger, existingHPA, oldExistingHPA, updated)
		return err
	}

	if err := r.Create(ctx, generatedHPA); err != nil {
		return fmt.Errorf("failed creating HPA for AIGatewayDataPlane %s: %w", aigwdp.Name, err)
	}
	return nil
}
