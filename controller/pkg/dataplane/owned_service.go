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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ServiceOptions mirrors the user-provided Service options of the specialized
// DataPlane APIs (e.g. spec.network.services.ingress). Each controller
// converts its API type into this shared representation.
type ServiceOptions struct {
	// Type determines how the Service is exposed.
	Type corev1.ServiceType
	// Annotations is an unstructured key value map stored with the Service resource.
	Annotations map[string]string
	// Labels are propagated to the Service.
	Labels map[string]string
	// ExternalTrafficPolicy describes how nodes distribute service traffic they
	// receive on one of the Service's externally-facing addresses.
	ExternalTrafficPolicy corev1.ServiceExternalTrafficPolicy
	// TrafficDistribution offers a way to express preferences for how traffic is
	// distributed to Service endpoints.
	TrafficDistribution *string
	// InternalTrafficPolicy describes how nodes distribute service traffic they
	// receive on the ClusterIP.
	InternalTrafficPolicy *corev1.ServiceInternalTrafficPolicy
	// Ports is the list of ports that are exposed by the Service.
	Ports []ServicePort
}

// ServicePort contains information on a user-provided Service port.
type ServicePort struct {
	// Name is the name of the port.
	Name *string
	// Port is the port that will be exposed by the Service.
	Port int32
	// TargetPort is the port on the pod the Service routes traffic to.
	TargetPort *intstr.IntOrString
	// NodePort is the port on each node on which the Service is exposed when
	// Type is NodePort or LoadBalancer.
	NodePort *int32
}

// ensureService reconciles the Service for the given DataPlane and returns the
// live Service object (with Status populated).
func (r *Reconciler[T, CP, Cert]) ensureService(
	ctx context.Context,
	logger logr.Logger,
	dp T,
) (*corev1.Service, error) {
	desired, err := BuildService(r.TypeConverter, dp, r.Config.Service)
	if err != nil {
		return nil, fmt.Errorf("failed to build %s Service for %s %s/%s: %w",
			r.Config.Service.Description, r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}

	result, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeWarning, "ServiceFailed", "ApplyService",
			"Failed to apply %s Service: %v", r.Config.Service.Description, err)
		return nil, fmt.Errorf("failed to apply %s Service for %s %s/%s: %w",
			r.Config.Service.Description, r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}
	switch result {
	case op.Created:
		log.Debug(logger, r.Config.Service.Description+" Service created", "name", desired.GetName())
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, "ServiceCreated", "CreateService",
			"%s Service %s created", r.Config.Service.Description, desired.GetName())
	case op.Updated:
		log.Debug(logger, r.Config.Service.Description+" Service updated", "name", desired.GetName())
		r.EventRecorder.Eventf(dp, nil, corev1.EventTypeNormal, "ServiceUpdated", "UpdateService",
			"%s Service %s updated", r.Config.Service.Description, desired.GetName())
	case op.Noop, op.Deleted:
	}

	// Fetch the live object so we get Status (SSA response does not include it).
	// The informer cache may not have caught up after a fresh create, so treat
	// NotFound as a transient condition: return nil and let the Owns() watch
	// trigger the next reconcile once the cache is populated.
	svc := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(desired), svc); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug(logger, r.Config.Service.Description+" Service not yet in cache, will retry on next reconcile",
				"name", desired.GetName())
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get %s Service for %s %s/%s: %w",
			r.Config.Service.Description, r.Config.Kind, dp.GetNamespace(), dp.GetName(), err)
	}
	return svc, nil
}

// BuildService constructs the desired Service. If the user has provided
// ServiceOptions, they are merged with the operator base via SMD:
// user-provided fields win on conflicts; the base supplies defaults (selector,
// default port) only when the user has not specified them.
func BuildService[T Object](
	tc managedfields.TypeConverter,
	dp T,
	cfg ServiceConfig[T],
) (client.Object, error) {
	base := GenerateBaseService(dp, cfg)

	opts := cfg.Options(dp)
	if opts == nil {
		return base, nil
	}

	userOverlay := GenerateServiceOverlay(dp, cfg, opts)
	return controllerpkgssa.MergeObjects(tc, base, userOverlay)
}

// GenerateBaseService returns the operator defaults for the Service: the pod
// selector and a default port. These are used only when the user has not
// provided conflicting values in ServiceOptions.
//
// Service.spec.ports is a list-map keyed by [port, protocol], so SSA merges
// ports by port number. Two ports with different port numbers but the same
// name would both be kept after the merge, causing Kubernetes to reject the
// Service (port names must be unique). To prevent this, any base port whose
// name is already used by a user-provided port is omitted here so the user's
// port wins cleanly.
func GenerateBaseService[T Object](dp T, cfg ServiceConfig[T]) *corev1.Service {
	// Collect user-provided port names so we can skip conflicting base ports.
	userPortNames := make(map[string]struct{})
	if opts := cfg.Options(dp); opts != nil {
		for _, p := range opts.Ports {
			if p.Name != nil {
				userPortNames[*p.Name] = struct{}{}
			}
		}
	}

	basePorts := []corev1.ServicePort{
		{
			Name:       cfg.DefaultPortName,
			Port:       cfg.DefaultPort,
			TargetPort: intstr.FromInt32(cfg.DefaultPort),
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
			Name:      dp.GetName() + cfg.NameSuffix,
			Namespace: dp.GetNamespace(),
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				consts.GatewayOperatorManagedByLabel:     cfg.ManagedByLabelValue,
				consts.GatewayOperatorManagedByNameLabel: dp.GetName(),
			},
			Ports: ports,
		},
	}
	k8sutils.SetOwnerForObject(svc, dp)
	return svc
}

// GenerateServiceOverlay builds a Service skeleton from the user-provided
// ServiceOptions. This is merged on top of the base by MergeObjects; base wins
// on conflicts (e.g. selector, default port).
func GenerateServiceOverlay[T Object](dp T, cfg ServiceConfig[T], opts *ServiceOptions) *corev1.Service {
	var ports []corev1.ServicePort
	for _, p := range opts.Ports {
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

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        dp.GetName() + cfg.NameSuffix,
			Namespace:   dp.GetNamespace(),
			Labels:      opts.Labels,
			Annotations: opts.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:  opts.Type,
			Ports: ports,
		},
	}

	if opts.ExternalTrafficPolicy != "" {
		svc.Spec.ExternalTrafficPolicy = opts.ExternalTrafficPolicy
	}

	if opts.TrafficDistribution != nil {
		svc.Spec.TrafficDistribution = opts.TrafficDistribution
	}

	if opts.InternalTrafficPolicy != nil {
		svc.Spec.InternalTrafficPolicy = opts.InternalTrafficPolicy
	}

	return svc
}
