/*
Copyright 2024.

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

package v2alpha1

import (
	ctrl "sigs.k8s.io/controller-runtime"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	operatorv2alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2alpha1"
)

// log is for logging in this package.
//
//nolint:unused
var controlplanelog = ctrllog.Log.WithName("controlplane-resource")

// SetupControlPlaneWebhookWithManager registers the webhook for ControlPlane in the manager.
func SetupControlPlaneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&operatorv2alpha1.ControlPlane{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
