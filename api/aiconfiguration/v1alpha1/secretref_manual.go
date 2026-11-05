package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
)

// crossNamespaceSecretRefChecker is the generated accessor set the on-prem translations
// gate secretRefs through.
type crossNamespaceSecretRefChecker interface {
	GetNamespace() string
	GetTypeName() string
	GetSensitiveDataSecretRefs() []SensitiveDataSecretRef
}

// checkCrossNamespaceSecretRefsGranted errors when a KongReferenceGrant in the Secret's
// namespace does not allow one of the entity's cross-namespace secretRefs. This is the same
// rule as the Konnect path (controller/konnect reconciler_secretref.go handleSecretRef).
// The kind comes from GetTypeName, because objects read from the cache have no TypeMeta.
func checkCrossNamespaceSecretRefsGranted(ctx context.Context, cl client.Client, entity crossNamespaceSecretRefChecker) error {
	for _, ref := range entity.GetSensitiveDataSecretRefs() {
		if ref.Namespace == nil || *ref.Namespace == "" || *ref.Namespace == entity.GetNamespace() {
			continue
		}
		if err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx, cl,
			entity.GetNamespace(),
			*ref.Namespace,
			ref.Name,
			metav1.GroupVersionKind(GroupVersion.WithKind(entity.GetTypeName())),
			metav1.GroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret")),
		); err != nil {
			return err
		}
	}
	return nil
}

// noteSecretLabelRequirement annotates a not-found Secret error with the operator's Secret
// label requirement: the operator's caches only hold Secrets carrying the configured Secret
// label selector (konghq.com/secret=true by default, see the --secret-label-selector flag),
// so an existing but unlabeled Secret reads as not found.
func noteSecretLabelRequirement(err error) error {
	if !apierrors.IsNotFound(err) {
		return err
	}
	return fmt.Errorf(
		"%w (note: the operator only reads Secrets carrying its Secret label selector, konghq.com/secret=true by default)",
		err,
	)
}

// valueFromSecretRef converts a SecretRef to a JSON value by fetching the referenced Secret and extracting the specified key.
func (src *AIGatewayPolicyConfigDataSource) valueFromSecretRef(ctx context.Context, cl client.Client, namespace string) (apiextensionsv1.JSON, error) {
	if src.SecretRef == nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("secretRef is nil for AIGatewayPolicyConfigDataSource")
	}
	var secret corev1.Secret
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: src.SecretRef.Name}, &secret); err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to fetch Secret %s/%s: %w", namespace, src.SecretRef.Name, err)
	}
	secretBytes, ok := secret.Data[src.SecretRef.Key]
	if !ok {
		return apiextensionsv1.JSON{}, fmt.Errorf("secret %s/%s is missing key %q", namespace, src.SecretRef.Name, src.SecretRef.Key)
	}
	var config map[string]any
	// Try to unmarshal the secret bytes into a map to check if it's valid JSON. If it is, return it as a JSON object.
	err := json.Unmarshal(secretBytes, &config)
	if err == nil {
		return apiextensionsv1.JSON{Raw: secretBytes}, nil
	}
	// If the secret bytes are not valid JSON, assume that they are in YAML format and convert them to JSON.
	jsonBytes, err := yaml.YAMLToJSON(secretBytes)
	if err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("failed to convert YAML to JSON for secret %s/%s: %w",
			namespace, src.SecretRef.Name, err)
	}
	return apiextensionsv1.JSON{Raw: jsonBytes}, nil
}
