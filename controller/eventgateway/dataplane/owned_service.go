/*
Copyright 2025 Kong, Inc.

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

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	log "github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureKafkaService reconciles the Kafka Service for the given DataPlane.
func (r *Reconciler) ensureKafkaService(
	ctx context.Context,
	logger logr.Logger,
	egdp *eventgatewayv1alpha1.KegDataPlane,
) error {
	desired, err := buildKafkaService(r.typeConverter, egdp)
	if err != nil {
		return fmt.Errorf("failed to build Kafka Service for DataPlane %s/%s: %w",
			egdp.Namespace, egdp.Name, err)
	}

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.typeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeWarning, "ServiceFailed", "ApplyService",
			"Failed to apply Kafka Service: %v", err)
		return fmt.Errorf("failed to apply Kafka Service for DataPlane %s/%s: %w",
			egdp.Namespace, egdp.Name, err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, "Kafka Service created", "name", desired.GetName())
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeNormal, "ServiceCreated", "CreateService",
			"Kafka Service %s created", desired.GetName())
	case op.Updated:
		log.Debug(logger, "Kafka Service updated", "name", desired.GetName())
		r.eventRecorder.Eventf(egdp, nil, corev1.EventTypeNormal, "ServiceUpdated", "UpdateService",
			"Kafka Service %s updated", desired.GetName())
	case op.Noop, op.Deleted:
	}
	return nil
}

// buildKafkaService constructs the desired Kafka Service. If the user has
// provided ServiceOptions, they are merged with the operator base via SMD:
// user-provided fields win on conflicts; the base supplies defaults (selector,
// default port) only when the user has not specified them.
func buildKafkaService(
	tc managedfields.TypeConverter,
	egdp *eventgatewayv1alpha1.KegDataPlane,
) (client.Object, error) {
	base := generateBaseKafkaService(egdp)

	if egdp.Spec.Network == nil || egdp.Spec.Network.Services == nil || egdp.Spec.Network.Services.Kafka == nil {
		return base, nil
	}

	userOverlay := generateKafkaServiceOverlay(egdp)
	return controllerpkgssa.MergeObjects(tc, base, userOverlay)
}

// generateBaseKafkaService returns the operator defaults for the Kafka Service:
// the pod selector and a default Kafka port. These are used only when the user
// has not provided conflicting values in ServiceOptions.
func generateBaseKafkaService(egdp *eventgatewayv1alpha1.KegDataPlane) *corev1.Service {
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      egdp.Name + "-kafka",
			Namespace: egdp.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				consts.GatewayOperatorManagedByLabel:     consts.DataPlaneManagedByLabelValue,
				consts.GatewayOperatorManagedByNameLabel: egdp.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "kafka",
					Port:       DefaultKafkaPort,
					TargetPort: intstr.FromInt32(DefaultKafkaPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	k8sutils.SetOwnerForObject(svc, egdp)
	return svc
}

// generateKafkaServiceOverlay builds a Service skeleton from the user-provided
// ServiceOptions. This is merged on top of the base by MergeObjects; base wins
// on conflicts (e.g. selector, default port).
func generateKafkaServiceOverlay(egdp *eventgatewayv1alpha1.KegDataPlane) *corev1.Service {
	kafka := egdp.Spec.Network.Services.Kafka

	var ports []corev1.ServicePort
	for _, p := range kafka.Ports {
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
	for k, v := range kafka.Labels {
		extraLabels[string(k)] = string(v)
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        egdp.Name + "-kafka",
			Namespace:   egdp.Namespace,
			Labels:      extraLabels,
			Annotations: kafka.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:  kafka.Type,
			Ports: ports,
		},
	}

	if kafka.ExternalTrafficPolicy != "" {
		svc.Spec.ExternalTrafficPolicy = kafka.ExternalTrafficPolicy
	}

	return svc
}
