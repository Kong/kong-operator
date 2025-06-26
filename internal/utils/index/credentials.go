package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/konnect/constraints"
)

func kongCredentialReferencesSecret[
	T constraints.SupportedCredentialType,
	TEnt constraints.KongCredential[T],
](obj client.Object) []string {
	cred, ok := obj.(TEnt)
	if !ok {
		return nil
	}

	var ret []string
	for _, or := range cred.GetOwnerReferences() {
		if or.Kind == "Secret" {
			ret = append(ret, or.Name)
		}
	}
	return ret
}
