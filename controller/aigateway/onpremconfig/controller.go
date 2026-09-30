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

// Package onpremconfig holds the reconcilers for the AI Gateway configuration
// entities (AIGatewayModel, and its sibling kinds) that feed the on-prem AI
// Gateway control plane instances with configuration.
//
// Only entities whose aiGatewayRef resolves to an OnPremAIGateway are managed
// here; entities referencing a KonnectAIGateway are owned by the generic
// Konnect reconciler, which skips the on-prem-targeted ones in turn.
//
// Entity status reuses the Programmed condition with on-prem semantics
// ("included in the last successfully pushed configuration"): the generated
// reconcilers read the per-entity outcome of the instance's configuration
// syncs (translation and push) through the EntityStatusClient implemented by
// the instance's EntityStatusReporter, and turn it into the Programmed
// condition.
package onpremconfig

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/pkg/status"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

// EntityStatusClient is the surface the generated configuration-entity
// reconcilers use to report an entity's configuration status. It is
// implemented by the instance's EntityStatusReporter, which records the
// per-entity outcome of the configuration syncs (translation and push).
//
// It is the on-prem counterpart of KIC's controllers.DataPlaneStatusClient,
// narrowed to what the on-prem reconcilers need (KIC's DataPlane interface
// also carries the CRUD and listener surface used by the Kong reconcilers,
// which has no meaning here).
type EntityStatusClient interface {
	AreKubernetesObjectReportsEnabled() bool
	KubernetesObjectConfigurationStatus(obj client.Object) status.ConfigurationStatus
	KubernetesObjectIsConfigured(obj client.Object) bool
	KubernetesObjectConfigurationStatusMessage(obj client.Object) string
}

// Controllers wires the generated AI Gateway configuration-entity reconcilers.
// Every supported kind is registered with the manager when it's set up.
type Controllers struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	Log              logr.Logger
	CacheSyncTimeout time.Duration
	ChangeNotifier   *changenotifier.ChangeNotifier
	// DataplaneClient provides the per-entity configuration status the
	// reconcilers report on the entities' Programmed condition.
	DataplaneClient EntityStatusClient
	// StatusQueue re-triggers the reconcilers when the reported configuration
	// status of entities changes.
	StatusQueue *status.Queue
}

// commonFieldsReconciler is implemented by every generated configuration-entity
// reconciler in this package.
type commonFieldsReconciler interface {
	SetCommonFields(
		client.Client,
		*runtime.Scheme,
		logr.Logger,
		time.Duration,
		*changenotifier.ChangeNotifier,
		EntityStatusClient,
		*status.Queue,
	)
	SetupWithManager(ctrl.Manager) error
}

// SetupWithManager sets up the configuration-entity controllers with the Manager.
func (cs *Controllers) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	for _, r := range []commonFieldsReconciler{
		&AIGatewayAgentReconciler{},
		&AIGatewayAuthStrategyReconciler{},
		&AIGatewayCACertificateReconciler{},
		&AIGatewayCertificateReconciler{},
		&AIGatewayConsumerReconciler{},
		&AIGatewayConsumerGroupReconciler{},
		&AIGatewayDataPlaneCertificateReconciler{},
		&AIGatewayMCPServerReconciler{},
		&AIGatewayModelReconciler{},
		&AIGatewayModelProviderReconciler{},
		&AIGatewayPolicyReconciler{},
		&AIGatewaySNIReconciler{},
	} {
		r.SetCommonFields(cs.Client, cs.Scheme, cs.Log, cs.CacheSyncTimeout, cs.ChangeNotifier, cs.DataplaneClient, cs.StatusQueue)
		if err := r.SetupWithManager(mgr); err != nil {
			return err
		}
	}
	return nil
}
