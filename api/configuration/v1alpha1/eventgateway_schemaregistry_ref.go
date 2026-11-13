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

package v1alpha1

import (
	"encoding/json"
	"fmt"
)

// UnmarshalJSON implements [json.Unmarshaler]. A schema registry reference by
// Konnect name, removed, held the name in an object ({type: name, name: {name:
// x}}): the API server serves such a stored reference pruned to an empty
// object in name, where the reference to the object holds a string. That name
// is decoded as unset rather than failing the decoding of the whole object,
// which would fail listing every policy of its kind: the policy alone then
// reports its reference has no name.
func (r *EventGatewaySchemaRegistryRef) UnmarshalJSON(data []byte) error {
	if r == nil {
		return fmt.Errorf("unmarshaling EventGatewaySchemaRegistryRef: nil receiver")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("unmarshaling EventGatewaySchemaRegistryRef: %w", err)
	}
	if name, ok := fields["name"]; ok {
		var s string
		if json.Unmarshal(name, &s) != nil {
			delete(fields, "name")
		}
	}
	cleaned, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("unmarshaling EventGatewaySchemaRegistryRef: %w", err)
	}
	type alias EventGatewaySchemaRegistryRef
	var a alias
	if err := json.Unmarshal(cleaned, &a); err != nil {
		return fmt.Errorf("unmarshaling EventGatewaySchemaRegistryRef: %w", err)
	}
	*r = EventGatewaySchemaRegistryRef(a)
	return nil
}
