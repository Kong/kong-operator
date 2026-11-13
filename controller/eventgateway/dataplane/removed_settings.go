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
	"fmt"

	"github.com/kong/semver/v4"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/versions"
)

// kegRemovedSettingsVersion is the keg version that removed the settings of
// spec.config.configPollIntervalSeconds and spec.config.enableDebugEndpoints:
// keg refuses to start with them.
var kegRemovedSettingsVersion = semver.MustParse("1.3.0")

// kegReadsRemovedSettings tells whether the keg of the image reads the
// settings keg 1.3.0 removed: only one whose version, from the tag of the
// image, is older than 1.3.0. versions.FromImage drops a suffix of the tag,
// e.g. -rc.1 or -debian, so a pre-release of 1.3.0 counts as 1.3.0, which has
// its changes. One whose version can't be read, e.g. of a digest only or of a
// tag like main, doesn't either: the default keg doesn't, and passing them
// would stop keg from starting.
func kegReadsRemovedSettings(image string) bool {
	v, err := versions.FromImage(image)
	return err == nil && v.LT(kegRemovedSettingsVersion)
}

// ignoredKEGSettings returns, for each setting of the KegDataPlane the keg of
// the image no longer reads, why it is ignored.
func ignoredKEGSettings(egdp *eventgatewayv1alpha1.KegDataPlane, image string) []string {
	cfg := egdp.Spec.Config
	if cfg == nil || kegReadsRemovedSettings(image) {
		return nil
	}
	keg := fmt.Sprintf("the keg of the image %s is %s or later, which", image, kegRemovedSettingsVersion)
	if _, err := versions.FromImage(image); err != nil {
		keg = fmt.Sprintf("the image %s has no version tag, so its keg is taken for %s or later, which", image, kegRemovedSettingsVersion)
	}
	var ignored []string
	if cfg.ConfigPollIntervalSeconds != nil {
		ignored = append(ignored, "spec.config.configPollIntervalSeconds is ignored: "+keg+" polls its configuration on its own")
	}
	if cfg.EnableDebugEndpoints != nil {
		ignored = append(ignored, "spec.config.enableDebugEndpoints is ignored: "+keg+" serves its debug endpoints on a listener of its own")
	}
	return ignored
}
