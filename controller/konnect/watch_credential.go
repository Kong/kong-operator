package konnect

import (
	"context"
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	operatorerrors "github.com/kong/kong-operator/internal/errors"
)

// kongCredentialRefersToKonnectGatewayControlPlane returns a predicate function that
// returns true if the KongCredential refers to a KongConsumer which uses
// KonnectGatewayControlPlane reference.
func kongCredentialRefersToKonnectGatewayControlPlane[
	T interface {
		*configurationv1alpha1.KongCredentialACL |
			*configurationv1alpha1.KongCredentialAPIKey |
			*configurationv1alpha1.KongCredentialBasicAuth |
			*configurationv1alpha1.KongCredentialJWT |
			*configurationv1alpha1.KongCredentialHMAC

		GetConsumerRefName() string
		GetTypeName() string
		GetNamespace() string
	},
](cl client.Client) func(obj client.Object) bool {
	return func(obj client.Object) bool {
		credential, ok := obj.(T)
		if !ok {
			ctrllog.FromContext(context.Background()).Error(
				operatorerrors.ErrUnexpectedObject,
				"failed to run predicate function",
				"expected", constraints.EntityTypeName[T](), "found", reflect.TypeOf(obj),
			)
			return false
		}

		nn := types.NamespacedName{
			Namespace: credential.GetNamespace(),
			Name:      credential.GetConsumerRefName(),
		}
		var consumer configurationv1.KongConsumer
		if err := cl.Get(context.Background(), nn, &consumer); err != nil {
			return false
		}

		return objHasControlPlaneRef(&consumer)
	}
}
