package config

// DefaultClusterCAKeySize is the default size of the cluster CA key.
const DefaultClusterCAKeySize = 4096

const (
	DefaultSecretLabelSelector    = "konghq.com/secret"
	DefaultConfigMapLabelSelector = "konghq.com/configmap"
)
