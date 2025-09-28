package config

// DefaultClusterCAKeySize is the default size of the cluster CA key.
const DefaultClusterCAKeySize = 4096

const (
	// DefaultSecretLabelSelector is the default label selector to filter reconciled `Secret`s.
	// Value true vs internal
	DefaultSecretLabelSelector = "konghq.com/secret"
	// DefaultConfigMapLabelSelector is the default label selector to filter reconciled `ConfigMap`s.
	DefaultConfigMapLabelSelector = "konghq.com/configmap"
)

const (
	// LabelValueForSelectorTrue is the label value used to select resources managed by the operator.
	// Those resource are user facing, they will be fetched by operator and validated by validating webhook.
	LabelValueForSelectorTrue = "true"
	// LabelValueForSelectorInternal is the label value used to select resources managed by the operator.
	// Those resources are not user facing, they will be fetched by operator and by-pass the validating webhook.
	// Otherwise it leads to chicken egg problem when operator creates a Secret and validating webhook is not
	// running yet, furthermore validation for objects created by operator is pointless and sometimes locks the
	// operator reconciliation.
	LabelValueForSelectorInternal = "internal"
)
