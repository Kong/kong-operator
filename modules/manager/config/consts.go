package config

// DefaultClusterCAKeySize is the default size of the cluster CA key.
const DefaultClusterCAKeySize = 4096

const (
	// DefaultSecretLabelSelector is the deafult label selector to filter reconciled `Secret`s.
	DefaultSecretLabelSelector = "konghq.com/secret"
	// DefaultConfigMapLabelSelector is the default label selector to filter reconciled `ConfigMap`s.
	DefaultConfigMapLabelSelector = "konghq.com/configmap"
)
