/*
Copyright 2022 Kong, Inc.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// GroupName is the group name used in this package.
	GroupName = "configuration.konghq.com"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}

	// SchemeGroupVersion is a convenience var for generated clientsets.
	SchemeGroupVersion = GroupVersion

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// Resource takes an unqualified resource and returns a Group qualified GroupResource.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&IngressClassParameters{},
		&IngressClassParametersList{},
		&KongCACertificate{},
		&KongCACertificateList{},
		&KongCertificate{},
		&KongCertificateList{},
		&KongCredentialACL{},
		&KongCredentialACLList{},
		&KongCredentialAPIKey{},
		&KongCredentialAPIKeyList{},
		&KongCredentialBasicAuth{},
		&KongCredentialBasicAuthList{},
		&KongCredentialHMAC{},
		&KongCredentialHMACList{},
		&KongCredentialJWT{},
		&KongCredentialJWTList{},
		&KongCustomEntity{},
		&KongCustomEntityList{},
		&KongDataPlaneClientCertificate{},
		&KongDataPlaneClientCertificateList{},
		&KongKey{},
		&KongKeyList{},
		&KongKeySet{},
		&KongKeySetList{},
		&KongLicense{},
		&KongLicenseList{},
		&KongPluginBinding{},
		&KongPluginBindingList{},
		&KongReferenceGrant{},
		&KongReferenceGrantList{},
		&KongRoute{},
		&KongRouteList{},
		&KongService{},
		&KongServiceList{},
		&KongSNI{},
		&KongSNIList{},
		&KongTarget{},
		&KongTargetList{},
		&KongUpstream{},
		&KongUpstreamList{},
		&KongVault{},
		&KongVaultList{},
	)

	if err := addKnownTypesGenerated(scheme); err != nil {
		return err
	}

	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
