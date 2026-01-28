package index

import (
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

const (
	// IndexFieldKonnectGatewayControlPlaneGroupOnMembers is the index field for KonnectGatewayControlPlane -> its members.
	IndexFieldKonnectGatewayControlPlaneGroupOnMembers = "konnectGatewayControlPlaneGroupMembers"

	// IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration is the index field for KonnectGatewayControlPlane -> APIAuthConfiguration.
	IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration = "konnectGatewayControlPlaneAPIAuthConfigurationRef"

	// IndexFieldKonnectGatewayControlPlaneOnKonnectID is the index field for KonnectGatewayControlPlane -> KonnectID.
	IndexFieldKonnectGatewayControlPlaneOnKonnectID = "konnectGatewayControlPlaneKonnectID"
)

// OptionsForKonnectGatewayControlPlane returns required Index options for KonnectGatewayControlPlane reconciler.
func OptionsForKonnectGatewayControlPlane() []Option {
	return []Option{
		{
			Object:         &konnectv1alpha2.KonnectGatewayControlPlane{},
			Field:          IndexFieldKonnectGatewayControlPlaneGroupOnMembers,
			ExtractValueFn: konnectGatewayControlPlaneGroupMembers,
		},
		{
			Object:         &konnectv1alpha2.KonnectGatewayControlPlane{},
			Field:          IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration,
			ExtractValueFn: konnectGatewayControlPlaneAPIAuthConfigurationRef,
		},
		{
			Object:         &konnectv1alpha2.KonnectGatewayControlPlane{},
			Field:          IndexFieldKonnectGatewayControlPlaneOnKonnectID,
			ExtractValueFn: konnectGatewayControlPlaneKonnectID,
		},
	}
}

func konnectGatewayControlPlaneGroupMembers(object client.Object) []string {
	cp, ok := object.(*konnectv1alpha2.KonnectGatewayControlPlane)
	if !ok {
		return nil
	}
	clusterType := cp.GetKonnectClusterType()
	if clusterType == nil {
		return nil
	}

	if string(*clusterType) != string(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup) {
		return nil
	}

	ret := make([]string, 0, len(cp.Spec.Members))
	for _, member := range cp.Spec.Members {
		ret = append(ret, member.Name)
	}

	return ret
}

func konnectGatewayControlPlaneAPIAuthConfigurationRef(object client.Object) []string {
	cp, ok := object.(*konnectv1alpha2.KonnectGatewayControlPlane)
	if !ok {
		return nil
	}

	namespace := cp.GetNamespace()
	if cp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Namespace != nil {
		namespace = *cp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Namespace
	}

	return []string{namespace + "/" + cp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name}
}

func konnectGatewayControlPlaneKonnectID(object client.Object) []string {
	cp, ok := object.(*konnectv1alpha2.KonnectGatewayControlPlane)
	if !ok {
		return nil
	}

	if konnectID := cp.GetKonnectStatus().GetKonnectID(); konnectID != "" {
		return []string{konnectID}
	}
	return nil
}
