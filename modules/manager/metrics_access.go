/*
Copyright 2025 Kong Inc.

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

import "fmt"

// MetricsAccessFilter defines the access filter function for the metrics endpoint.
type MetricsAccessFilter string

// Set implements [flag.Value].
func (mf *MetricsAccessFilter) Set(v string) error {
	switch v {
	case string(MetricsAccessFilterOff), string(MetricsAccessFilterRBAC):
		*mf = MetricsAccessFilter(v)
	default:
		return fmt.Errorf("invalid value %q for metrics access filter", v)
	}
	return nil
}

const (
	// MetricsAccessFilterOff disabled the access filter on metrics endpoint.
	MetricsAccessFilterOff MetricsAccessFilter = "off"
	// MetricsAccessFilterRBAC enables the access filter on metrics endpoint.
	// For more information consult:
	// https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/metrics/filters#WithAuthenticationAndAuthorization
	MetricsAccessFilterRBAC MetricsAccessFilter = "rbac"
)

// String returns the string representation of the MetricsFilter.
func (mf MetricsAccessFilter) String() string {
	return string(mf)
}
