package main

import "github.com/samber/lo"

var supportedKonnectTypesWithControlPlaneRef = []supportedTypesT{
	{
		PackageVersion: "v1alpha1",
		AdditionalImports: []string{
			`commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"`,
			`konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"`,
		},
		Types: []templateDataT{
			{
				Type:                       "KonnectCloudGatewayDataPlaneGroupConfiguration",
				KonnectStatusType:          "KonnectEntityStatusWithControlPlaneRef",
				KonnectStatusEmbedded:      true,
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
				ControlPlaneRefRequired:    true,
			},
		},
	},
	{
		PackageVersion: "v1alpha2",
		AdditionalImports: []string{
			`commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"`,
		},
		Types: []templateDataT{
			{
				Type:                     "KonnectExtension",
				ControlPlaneRefType:      "commonv1alpha1.KonnectExtensionControlPlaneRef",
				ControlPlaneRefRequired:  true,
				ControlPlaneRefFieldPath: "Spec.Konnect.ControlPlane.Ref",
			},
		},
	},
}

var supportedKonnectTypesWithControlPlaneConfig = []supportedTypesT{
	{
		PackageVersion: "v1",
		AdditionalImports: []string{
			`commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"`,
			`konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"`,
		},
		Types: []templateDataT{
			{
				Type:                       "KongConsumer",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
			{
				Type: "KongPlugin",
			},
		},
	},
	{
		PackageVersion: "v1beta1",
		AdditionalImports: []string{
			`commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"`,
			`konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"`,
		},
		Types: []templateDataT{
			{
				Type:                       "KongConsumerGroup",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
		},
	},
	{
		PackageVersion: "v1alpha1",
		AdditionalImports: []string{
			`commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"`,
			`konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"`,
		},
		Types: []templateDataT{
			{
				Type:                       "KongKey",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
			{
				Type:                       "KongKeySet",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
			{
				Type:                       "KongCredentialBasicAuth",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
			{
				Type:                       "KongCredentialAPIKey",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
			{
				Type:                       "KongCredentialJWT",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
			{
				Type:                       "KongCredentialACL",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
			{
				Type:                       "KongCredentialHMAC",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
			{
				Type:                       "KongCACertificate",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
			{
				Type:                       "KongCertificate",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
			{
				Type:                       "KongPluginBinding",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
				ControlPlaneRefRequired:    true,
			},
			{
				Type:                       "KongService",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
			{
				Type:                       "KongRoute",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
				ServiceRefType:             "ServiceRef",
			},
			{
				Type:                       "KongUpstream",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
			{
				Type:                       "KongTarget",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndUpstreamRefs",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
			{
				Type:                       "KongVault",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
			{
				Type:                       "KongSNI",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
			{
				Type:                       "KongDataPlaneClientCertificate",
				KonnectStatusType:          "*konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef",
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
				ControlPlaneRefType:        "commonv1alpha1.ControlPlaneRef",
			},
		},
	},
}

var supportedKonnectTypesStandalone = []supportedTypesT{
	{
		PackageVersion: "v1alpha1",
		AdditionalImports: []string{
			`konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"`,
		},
		Types: []templateDataT{
			{
				Type:                       "KonnectGatewayControlPlane",
				KonnectStatusType:          "KonnectEntityStatus",
				KonnectStatusEmbedded:      true,
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
			{
				Type: "KonnectAPIAuthConfiguration",
			},
			{
				Type:                       "KonnectCloudGatewayNetwork",
				KonnectStatusType:          "KonnectEntityStatus",
				KonnectStatusEmbedded:      true,
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
		},
	},
	{
		PackageVersion: "v1alpha2",
		Types: []templateDataT{
			{
				Type:                       "KonnectGatewayControlPlane",
				KonnectStatusType:          "KonnectEntityStatus",
				KonnectStatusEmbedded:      true,
				GetKonnectStatusReturnType: "*KonnectEntityStatus",
			},
		},
	},
}

var supportedKonnectV1Alpha1TypesWithNetworkRef = []supportedTypesT{
	{
		PackageVersion: "v1alpha1",
		AdditionalImports: []string{
			`konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"`,
		},
		Types: []templateDataT{
			{
				Type:                       "KonnectCloudGatewayTransitGateway",
				KonnectStatusType:          "KonnectEntityStatusWithNetworkRef",
				KonnectStatusEmbedded:      true,
				GetKonnectStatusReturnType: "*konnectv1alpha2.KonnectEntityStatus",
			},
		},
	},
}

var supportedGatewayOperatorTypes = []supportedTypesT{
	{
		PackageVersion: "v1alpha1",
		Types: []templateDataT{
			{
				Type: "AIGateway",
			},
			{
				Type: "KongPluginInstallation",
			},
		},
	},
	{
		PackageVersion: "v1beta1",
		Types: []templateDataT{
			{
				Type: "DataPlane",
			},
			{
				Type: "ControlPlane",
			},
		},
	},
	{
		PackageVersion: "v2beta1",
		Types: []templateDataT{
			{
				Type: "GatewayConfiguration",
			},
		},
	},
}

var supportedConfigurationPackageTypesWithList = supportedKonnectTypesWithControlPlaneConfig

var supportedKonnectPackageTypesWithList = func() []supportedTypesT {
	// Make sure that each template is generated once per package version.
	base := append(
		supportedKonnectTypesStandalone,
		supportedKonnectTypesWithControlPlaneRef...,
	)
	base = append(base, supportedKonnectV1Alpha1TypesWithNetworkRef...)

	m := make(map[string]supportedTypesT)
	for _, t := range base {
		v, ok := m[t.PackageVersion]
		if !ok {
			m[t.PackageVersion] = t
			continue
		}
		v.Types = append(m[t.PackageVersion].Types, t.Types...)
		m[t.PackageVersion] = v
	}

	return lo.Values(m)
}()

var supportedGatewayOperatorPackageTypesWithList = supportedGatewayOperatorTypes
