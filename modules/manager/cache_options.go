package manager

import (
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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
	byObject[&apiextensionsv1.CustomResourceDefinition{}] = crdCacheByObject()

	return byObject, nil
}

// crdCacheByObject strips the OpenAPI schema - the bulk of a CRD's size - from every
// CustomResourceDefinition outside ssaCRDGroups before it enters the cache.
//
// The crdschema controller (controller/crdschema) watches every CRD in the cluster (its
// predicate filters which reconcile events fire, not what the informer caches), but only
// ever rebuilds the SSA TypeConverter for ssaCRDGroups. Without this, the cache holds full
// schemas for every other CRD in the cluster too - including large, unrelated CRDs like
// gateway-operator.konghq.com's GatewayConfiguration - for no reason.
func crdCacheByObject() cache.ByObject {
	return cache.ByObject{
		Transform: func(obj any) (any, error) {
			crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
			if !ok {
				return obj, nil
			}
			if _, relevant := ssaCRDGroups[crd.Spec.Group]; relevant {
				return crd, nil
			}
			// NOTE: We do not need to deep-copy the CRD here because the cache
			// will do that for us before storing it.
			for i := range crd.Spec.Versions {
				// NOTE: We strip the schema here as its unused.
				// Any future controller that needs the schema should add its own
				// ByObject entry for CRDs in its group.
				crd.Spec.Versions[i].Schema = nil
			}
			return crd, nil
		},
	}
}
