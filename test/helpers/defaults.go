package helpers

import (
	"sync"

	"github.com/kong/gateway-operator/pkg/consts"
)

var (
	_defaultDataPlaneImageLock sync.RWMutex
	_defaultDataPlaneBaseImage = consts.DefaultDataPlaneBaseEnterpriseImage
	_defaultDataPlaneImage     = consts.DefaultDataPlaneEnterpriseImage
)

// GetDefaultDataPlaneBaseImage returns the default data plane base image.
//
// NOTE: This is not necessary anymore as Kong 3.10 OSS images are not readily
// available anymore but we keep this for now to prevent breaking changes.
// This can be removed in next major version.
func GetDefaultDataPlaneBaseImage() string {
	_defaultDataPlaneImageLock.RLock()
	defer _defaultDataPlaneImageLock.RUnlock()
	return _defaultDataPlaneBaseImage
}

// SetDefaultDataPlaneBaseImage sets the default data plane base image.
//
// NOTE: This is not necessary anymore as Kong 3.10 OSS images are not readily
// available anymore but we keep this for now to prevent breaking changes.
// This can be removed in next major version.
func SetDefaultDataPlaneBaseImage(image string) {
	_defaultDataPlaneImageLock.Lock()
	defer _defaultDataPlaneImageLock.Unlock()
	_defaultDataPlaneBaseImage = image
}

// GetDefaultDataPlaneImage returns the default data plane image.
//
// NOTE: This currently works on a shared image for Enterprise and non-Enterprise.
// This is because Kong 3.10 OSS images are not readily available anymore.
func GetDefaultDataPlaneImage() string {
	_defaultDataPlaneImageLock.RLock()
	defer _defaultDataPlaneImageLock.RUnlock()
	return _defaultDataPlaneImage
}

// GetDefaultDataPlaneEnterpriseImage returns the default data plane enterprise image.
//
// NOTE: This can be removed in next major version and replaced with
// GetDefaultDataPlaneImage() which will return the same value.
func GetDefaultDataPlaneEnterpriseImage() string {
	_defaultDataPlaneImageLock.RLock()
	defer _defaultDataPlaneImageLock.RUnlock()
	return _defaultDataPlaneImage
}

// SetDefaultDataPlaneImage sets the default data plane image.
func SetDefaultDataPlaneImage(image string) {
	_defaultDataPlaneImageLock.Lock()
	defer _defaultDataPlaneImageLock.Unlock()
	_defaultDataPlaneImage = image
}
