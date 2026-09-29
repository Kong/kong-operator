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

	"github.com/google/uuid"
	"github.com/kong/go-kong/kong"
	"github.com/samber/mo"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// KongLicenseCacheGetter is a LicenseGetter that picks the effective KongLicense
// directly from the manager's shared, informer-backed cache. Unlike the
// KongLicense reconciler's own cache, this is updated before watch events are
// delivered, so a reconcile triggered by a KongLicense add/update/delete never
// observes a stale license (no cross-workqueue ordering race).
type KongLicenseCacheGetter struct {
	client client.Reader
}

// NewKongLicenseCacheGetter returns a LicenseGetter backed by the given
// (informer-backed) client reader.
func NewKongLicenseCacheGetter(r client.Reader) *KongLicenseCacheGetter {
	return &KongLicenseCacheGetter{client: r}
}

// GetLicense returns the newest enabled KongLicense in the cache, in Kong
// configuration format. It does not check license validity.
func (g KongLicenseCacheGetter) GetLicense() mo.Option[kong.License] {
	list := &configurationv1alpha1.KongLicenseList{}
	// The reader is the manager's cached client, so this never hits the API
	// server; a bare context is enough. A failed List must not look like "no
	// license": that would set LicenseValid=False and strip KONG_LICENSE_DATA,
	// rolling the pods. Log it so the trigger stays visible.
	if err := g.client.List(context.Background(), list); err != nil {
		ctrl.Log.Error(err, "failed to list KongLicenses for the license getter")
		return mo.None[kong.License]()
	}

	var chosen *configurationv1alpha1.KongLicense
	for i := range list.Items {
		license := &list.Items[i]
		if !license.Enabled {
			continue
		}
		if chosen == nil || newerKongLicense(license, chosen) {
			chosen = license
		}
	}
	if chosen == nil {
		return mo.None[kong.License]()
	}
	return mo.Some(kong.License{
		ID:      new(uuid.NewSHA1(uuid.Nil, []byte("KongLicense:"+chosen.Name)).String()),
		Payload: new(chosen.RawLicenseString),
	})
}

// newerKongLicense returns true if license1 is newer than license2 (compared
// by metadata.creationTimestamp). If the creationTimestamps are equal, returns
// the one with the lexically smaller name. It mirrors the pick rule used by
// the embedded KIC's KongLicense reconciler.
func newerKongLicense(license1, license2 *configurationv1alpha1.KongLicense) bool {
	if license1.CreationTimestamp.After(license2.CreationTimestamp.Time) {
		return true
	}
	if license2.CreationTimestamp.After(license1.CreationTimestamp.Time) {
		return false
	}
	return license1.Name < license2.Name
}
