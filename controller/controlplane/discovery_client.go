package controlplane

import (
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// CachedDiscoveryClient is a wrapper around the discovery client that caches the API resources
// for a period of time.
type CachedDiscoveryClient struct {
	cl *discovery.DiscoveryClient

	lastLookupTime time.Time
	period         time.Duration
	lock           sync.RWMutex
	apiResourceMap map[schema.GroupVersion]*metav1.APIResourceList
}

// NewDiscoveryClient creates a new CachedDiscoveryClient.
func NewDiscoveryClient(cfg *rest.Config, period time.Duration) *CachedDiscoveryClient {
	return &CachedDiscoveryClient{
		period: period,
		cl:     discovery.NewDiscoveryClientForConfigOrDie(cfg),
	}
}

// GetAPIResourceListMapping returns the GroupVersion to API resources map.
func (c *CachedDiscoveryClient) GetAPIResourceListMapping() (map[schema.GroupVersion]*metav1.APIResourceList, error) {
	c.lock.RLock()
	isNil := c.apiResourceMap == nil
	c.lock.RUnlock()

	if isNil || time.Since(c.lastLookupTime) > c.period {
		if err := c.refresh(); err != nil {
			return nil, err
		}
	}
	return c.apiResourceMap, nil
}

func (c *CachedDiscoveryClient) refresh() error {
	_, s, _, err := c.cl.GroupsAndMaybeResources()
	if err != nil {
		return err
	}
	c.lock.Lock()
	c.apiResourceMap = s
	c.lock.Unlock()
	c.lastLookupTime = time.Now()
	return nil
}
