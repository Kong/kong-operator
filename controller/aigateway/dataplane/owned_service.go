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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureIngressService reconciles the Ingress Service for the given AIGatewayDataPlane.
func (r *Reconciler) ensureIngressService(
	ctx context.Context,
	logger logr.Logger,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) error {
	desired, err := buildIngressService(r.TypeConverter, aigwdp)
	if err != nil {
		return fmt.Errorf("failed to build Ingress Service for AIGatewayDataPlane %s/%s: %w",
			aigwdp.Namespace, aigwdp.Name, err)
	}

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeWarning, "ServiceFailed", "ApplyService",
			"Failed to apply Ingress Service: %v", err)
		return fmt.Errorf("failed to apply Ingress Service for AIGatewayDataPlane %s/%s: %w",
			aigwdp.Namespace, aigwdp.Name, err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Ingress Service created", "name", desired.GetName())
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeNormal, "ServiceCreated", "CreateService",
			"Ingress Service %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Ingress Service updated", "name", desired.GetName())
		r.eventRecorder.Eventf(aigwdp, nil, corev1.EventTypeNormal, "ServiceUpdated", "UpdateService",
			"Ingress Service %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}
	return nil
}

// buildIngressService constructs the desired Ingress Service. If the user has
// provided ServiceOptions, they are merged with the operator base via SMD:
// user-provided fields win on conflicts; the base supplies defaults (selector,
// default port) only when the user has not specified them.
func buildIngressService(
	tc managedfields.TypeConverter,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) (client.Object, error) {
	base := generateBaseIngressService(aigwdp)

	if aigwdp.Spec.Network == nil || aigwdp.Spec.Network.Services == nil || aigwdp.Spec.Network.Services.Ingress == nil {
		return base, nil
	}

	userOverlay := generateIngressServiceOverlay(aigwdp)
	return controllerpkgssa.MergeObjects(tc, base, userOverlay)
}

// generateBaseIngressService returns the operator defaults for the Ingress Service:
// the pod selector and a default ingress port. These are used only when the user
// has not provided conflicting values in ServiceOptions.
//
// Service.spec.ports is a list-map keyed by [port, protocol], so SSA merges
// ports by port number. Two ports with different port numbers but the same
// name would both be kept after the merge, causing Kubernetes to reject the
// Service (port names must be unique). To prevent this, any base port whose
// name is already used by a user-provided port is omitted here so the user's
// port wins cleanly.
func generateBaseIngressService(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *corev1.Service {
	// Collect user-provided port names so we can skip conflicting base ports.
	userPortNames := make(map[string]struct{})
	if aigwdp.Spec.Network != nil &&
		aigwdp.Spec.Network.Services != nil &&
		aigwdp.Spec.Network.Services.Ingress != nil {
		for _, p := range aigwdp.Spec.Network.Services.Ingress.Ports {
			if p.Name != nil {
				userPortNames[*p.Name] = struct{}{}
			}
		}
	}

	basePorts := []corev1.ServicePort{
		{
			Name:       "ingress",
			Port:       DefaultIngressPort,
			TargetPort: intstr.FromInt32(DefaultIngressPort),
			Protocol:   corev1.ProtocolTCP,
		},
	}
	var ports []corev1.ServicePort
	for _, bp := range basePorts {
		if _, clash := userPortNames[bp.Name]; !clash {
			ports = append(ports, bp)
		}
	}
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      aigwdp.Name + "-ingress",
			Namespace: aigwdp.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				consts.GatewayOperatorManagedByLabel:     consts.AIGatewayDataPlaneManagedByLabelValue,
				consts.GatewayOperatorManagedByNameLabel: aigwdp.Name,
			},
			Ports: ports,
		},
	}
	k8sutils.SetOwnerForObject(svc, aigwdp)
	return svc
}

// generateIngressServiceOverlay builds a Service skeleton from the user-provided
// ServiceOptions. This is merged on top of the base by MergeObjects; base wins
// on conflicts (e.g. selector, default port).
func generateIngressServiceOverlay(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane) *corev1.Service {
	ingress := aigwdp.Spec.Network.Services.Ingress

	var ports []corev1.ServicePort
	for _, p := range ingress.Ports {
		sp := corev1.ServicePort{
			Port:     p.Port,
			Protocol: corev1.ProtocolTCP,
		}
		if p.Name != nil {
			sp.Name = *p.Name
		}
		if p.TargetPort != nil {
			sp.TargetPort = *p.TargetPort
		}
		if p.NodePort != nil {
			sp.NodePort = *p.NodePort
		}
		ports = append(ports, sp)
	}

	extraLabels := make(map[string]string)
	for k, v := range ingress.Labels {
		extraLabels[string(k)] = string(v)
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        aigwdp.Name + "-ingress",
			Namespace:   aigwdp.Namespace,
			Labels:      extraLabels,
			Annotations: ingress.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:  ingress.Type,
			Ports: ports,
		},
	}

	if ingress.ExternalTrafficPolicy != "" {
		svc.Spec.ExternalTrafficPolicy = ingress.ExternalTrafficPolicy
	}

	if ingress.TrafficDistribution != nil {
		svc.Spec.TrafficDistribution = ingress.TrafficDistribution
	}

	if ingress.InternalTrafficPolicy != nil {
		svc.Spec.InternalTrafficPolicy = ingress.InternalTrafficPolicy
	}

	return svc
}
