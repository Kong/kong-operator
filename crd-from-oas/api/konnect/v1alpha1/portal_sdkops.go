package v1alpha1

import (
	"encoding/json"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
)

// ToCreatePortal converts the PortalAPISpec to the SDK type
// sdkkonnectcomp.CreatePortal using JSON marshal/unmarshal.
// Fields that exist in the CRD spec but not in the SDK type (e.g., Kubernetes
// object references) are naturally excluded because they have different JSON names.
func (s *PortalAPISpec) ToCreatePortal() (*sdkkonnectcomp.CreatePortal, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PortalAPISpec: %w", err)
	}
	var target sdkkonnectcomp.CreatePortal
	if err := json.Unmarshal(data, &target); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into CreatePortal: %w", err)
	}
	return &target, nil
}

// ToUpdatePortal converts the PortalAPISpec to the SDK type
// sdkkonnectcomp.UpdatePortal using JSON marshal/unmarshal.
// Fields that exist in the CRD spec but not in the SDK type (e.g., Kubernetes
// object references) are naturally excluded because they have different JSON names.
func (s *PortalAPISpec) ToUpdatePortal() (*sdkkonnectcomp.UpdatePortal, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PortalAPISpec: %w", err)
	}
	var target sdkkonnectcomp.UpdatePortal
	if err := json.Unmarshal(data, &target); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into UpdatePortal: %w", err)
	}
	return &target, nil
}
