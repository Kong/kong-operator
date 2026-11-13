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

package util

import (
	"github.com/samber/lo"
)

// InheritedTags holds the konghq.com/tags an entity inherits from Gateways and their
// GatewayClasses. The two groups are kept apart so that an aggregation over several sources
// (e.g. several Gateway listeners or routes) can emit every Gateway's tags before any
// GatewayClass's, with the least specific tags last.
type InheritedTags struct {
	Gateway      []string
	GatewayClass []string
}

// IsEmpty reports whether no tags are inherited.
func (it InheritedTags) IsEmpty() bool {
	return len(it.Gateway) == 0 && len(it.GatewayClass) == 0
}

// All returns the Gateway tags followed by the GatewayClass tags, deduplicated keeping the first
// occurrence. It returns nil when nothing is inherited.
func (it InheritedTags) All() []string {
	return lo.FromSlicePtr(MergeTags(nil, it.Gateway, it.GatewayClass))
}

// Append appends the tags of another InheritedTags to it, keeping every Gateway tag of it before
// every GatewayClass tag. Tags are deduplicated within each group, so aggregating several sources
// keeps every tag at the position of its most specific source; tags shared between the two groups
// are kept in both and deduplicated when the tags are flattened.
func (it *InheritedTags) Append(other InheritedTags) {
	appendTags := func(dst []string, tags []string) []string {
		seen := make(map[string]struct{}, len(dst)+len(tags))
		for _, tag := range dst {
			seen[tag] = struct{}{}
		}
		for _, tag := range tags {
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			dst = append(dst, tag)
		}
		return dst
	}
	it.Gateway = appendTags(it.Gateway, other.Gateway)
	it.GatewayClass = appendTags(it.GatewayClass, other.GatewayClass)
}

// TagPointers returns the tags as a slice of string pointers, deduplicated keeping the first
// occurrence, or nil when nothing is inherited.
func (it InheritedTags) TagPointers() []*string {
	if it.IsEmpty() {
		return nil
	}
	return MergeTags(nil, it.Gateway, it.GatewayClass)
}
