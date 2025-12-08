package manager

import (
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createCacheOptions(l logr.Logger, cfg Config) (cache.Options, error) {
	var cacheOptions cache.Options
	if cfg.CacheSyncPeriod > 0 {
		l.Info("cache sync period set", "period", cfg.CacheSyncPeriod)
		cacheOptions.SyncPeriod = &cfg.CacheSyncPeriod
	}

	// If there are no configured watch namespaces, then we're watching ALL namespaces,
	// and we don't have to bother individually caching any particular namespaces.
	// This is the default behavior of the controller-runtime manager.
	// If there are configured watch namespaces, then we're watching only those namespaces.
	if len(cfg.WatchNamespaces) > 0 {
		l.Info("Manager set up with multiple namespaces", "namespaces", cfg.WatchNamespaces)
		watched := make(map[string]cache.Config)
		for _, ns := range cfg.WatchNamespaces {
			watched[ns] = cache.Config{}
		}
		cacheOptions.DefaultNamespaces = watched
	}

	cacheByObject, err := createCacheByObject(cfg)
	if err != nil {
		return cacheOptions, fmt.Errorf("failed to create cache options: %w", err)
	}
	cacheOptions.ByObject = cacheByObject

	return cacheOptions, nil
}

func createCacheByObject(cfg Config) (map[client.Object]cache.ByObject, error) {
	if cfg.ConfigMapLabelSelector == "" && cfg.SecretLabelSelector == "" {
		return nil, nil
	}
	byObject := map[client.Object]cache.ByObject{}
	if cfg.SecretLabelSelector != "" {
		if err := setByObjectFor[corev1.Secret](cfg.SecretLabelSelector, byObject); err != nil {
			return nil, fmt.Errorf("failed to set byObject for Secrets: %w", err)
		}
	}
	if cfg.ConfigMapLabelSelector != "" {
		if err := setByObjectFor[corev1.ConfigMap](cfg.ConfigMapLabelSelector, byObject); err != nil {
			return nil, fmt.Errorf("failed to set byObject for ConfigMaps: %w", err)
		}
	}

	return byObject, nil
}
