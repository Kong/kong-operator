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

package eventgateway

// +kubebuilder:rbac:groups=eventgateway.konghq.com,resources=dataplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=eventgateway.konghq.com,resources=dataplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konnect.konghq.com,resources=konnecteventgateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=create;get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
