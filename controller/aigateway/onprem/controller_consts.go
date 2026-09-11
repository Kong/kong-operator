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

package onprem

import "time"

// -----------------------------------------------------------------------------
// OnPremAIGateway - Finalizers
// -----------------------------------------------------------------------------

// OnPremAIGatewayFinalizer defines finalizers added by the onpremaigateway controller.
type OnPremAIGatewayFinalizer string

const (
	// OnPremAIGatewayFinalizerInstanceTeardown is the finalizer to tear down the on-prem AI Gateway control
	// plane instance that this resource runs.
	OnPremAIGatewayFinalizerInstanceTeardown OnPremAIGatewayFinalizer = "gateway-operator.konghq.com/teardown-onprem-aigateway-instance"
)

// requeueAfterBoot is the delay after which an OnPremAIGateway is requeued while its control plane
// instance is booting.
const requeueAfterBoot = time.Second
