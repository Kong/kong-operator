package helpers

import (
	"sync"

	"github.com/kong/kong-operator/pkg/consts"
)

var (
	_defaultDataPlaneImageLock sync.RWMutex
	_defaultDataPlaneImage     = consts.DefaultDataPlaneImage
	_defaultDataPlaneBaseImage = consts.DefaultDataPlaneBaseImage
)

// GetDefaultDataPlaneBaseImage returns the default data plane base image.
func GetDefaultDataPlaneBaseImage() string {
	_defaultDataPlaneImageLock.RLock()
	defer _defaultDataPlaneImageLock.RUnlock()
	return _defaultDataPlaneBaseImage
}

// SetDefaultDataPlaneBaseImage sets the default data plane base image.
func SetDefaultDataPlaneBaseImage(image string) {
	_defaultDataPlaneImageLock.Lock()
	defer _defaultDataPlaneImageLock.Unlock()
	_defaultDataPlaneBaseImage = image
}

// GetDefaultDataPlaneImage returns the default data plane image.
func GetDefaultDataPlaneImage() string {
	_defaultDataPlaneImageLock.RLock()
	defer _defaultDataPlaneImageLock.RUnlock()
	return _defaultDataPlaneImage
}

// GetDefaultDataPlaneEnterpriseImage returns the default data plane enterprise image.
func GetDefaultDataPlaneEnterpriseImage() string {
	_defaultDataPlaneImageLock.RLock()
	defer _defaultDataPlaneImageLock.RUnlock()
	return consts.DefaultDataPlaneEnterpriseImage
}

// SetDefaultDataPlaneImage sets the default data plane image.
func SetDefaultDataPlaneImage(image string) {
	_defaultDataPlaneImageLock.Lock()
	defer _defaultDataPlaneImageLock.Unlock()
	_defaultDataPlaneImage = image
}
