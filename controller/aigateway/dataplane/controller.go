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
	"time"

	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// Reconciler reconciles an AIGatewayDataPlane object.
type Reconciler struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode

	ClusterCASecretName      string
	ClusterCASecretNamespace string
	SecretLabelSelector      string
	CertTTL                  time.Duration

	// TypeConverter is injected via the TypeConverterProvider at controller
	// registration time.  It is used for both diff-before-apply and
	// structured-merge-diff based PodTemplateSpec merging.
	TypeConverter managedfields.TypeConverter

	// eventRecorder records Kubernetes events on AIGatewayDataPlane objects.
	eventRecorder events.EventRecorder
}

// base returns the shared generic reconciler wired with the AIGatewayDataPlane
// configuration.
func (r *Reconciler) base() *shareddataplane.Reconciler[
	*aigatewayv1alpha1.AIGatewayDataPlane,
	*konnectv1alpha1.KonnectAIGateway,
	*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate,
] {
	return &shareddataplane.Reconciler[
		*aigatewayv1alpha1.AIGatewayDataPlane,
		*konnectv1alpha1.KonnectAIGateway,
		*aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate,
	]{
		Client:                   r.Client,
		LoggingMode:              r.LoggingMode,
		ClusterCASecretName:      r.ClusterCASecretName,
		ClusterCASecretNamespace: r.ClusterCASecretNamespace,
		SecretLabelSelector:      r.SecretLabelSelector,
		CertTTL:                  r.CertTTL,
		TypeConverter:            r.TypeConverter,
		EventRecorder:            r.eventRecorder,
		Config:                   config,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorder(ControllerName)
	return r.base().SetupWithManager(ctx, mgr)
}

// Reconcile moves the current state of an AIGatewayDataPlane toward the desired state.
func (r *Reconciler) Reconcile(
	ctx context.Context,
	aigwdp *aigatewayv1alpha1.AIGatewayDataPlane,
) (ctrl.Result, error) {
	return r.base().Reconcile(ctx, aigwdp)
}
