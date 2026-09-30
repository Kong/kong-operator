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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestKongLicenseCacheGetter(t *testing.T) {
	ts := func(offset time.Duration) metav1.Time {
		return metav1.NewTime(time.Now().Add(offset).Truncate(time.Second))
	}

	license := func(name string, ts metav1.Time, enabled bool) *configurationv1alpha1.KongLicense {
		return &configurationv1alpha1.KongLicense{
			Name: name, CreationTimestamp: ts,
			Enabled:          enabled,
			RawLicenseString: `{"license": {"payload": "` + name + `"}}`,
		}
	}

	testCases := []struct {
		name     string
		licenses []*configurationv1alpha1.KongLicense
		wantNone bool
		wantName string
	}{
		{
			name:     "no licenses",
			wantNone: true,
		},
		{
			name:     "only disabled licenses",
			licenses: []*configurationv1alpha1.KongLicense{license("a", ts(0), false)},
			wantNone: true,
		},
		{
			name: "newest enabled license wins",
			licenses: []*configurationv1alpha1.KongLicense{
				license("old", ts(-2*time.Hour), true),
				license("new", ts(-1*time.Hour), true),
				license("disabled-newest", ts(0), false),
			},
			wantName: "new",
		},
		{
			name: "equal timestamps fall back to lexicographically smaller name",
			licenses: []*configurationv1alpha1.KongLicense{
				license("b", ts(0), true),
				license("a", ts(0), true),
			},
			wantName: "a",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(managerscheme.Get())
			for _, l := range tc.licenses {
				builder = builder.WithObjects(l)
			}
			getter := NewKongLicenseCacheGetter(builder.Build())

			got := getter.GetLicense()
			if tc.wantNone {
				_, ok := got.Get()
				assert.False(t, ok)
				return
			}
			lic, ok := got.Get()
			require.True(t, ok)
			assert.Contains(t, *lic.Payload, tc.wantName)
		})
	}
}
