/*
Copyright 2022 Kong Inc.

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

package manager

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/modules/admission"
)

// AdmissionRequestHandlerFunc is a function that returns an implementation of admission.RequestHandler,
// (validation webhook) it's passed to Run function and called later.
type AdmissionRequestHandlerFunc func(c client.Client, l logr.Logger) *admission.RequestHandler
