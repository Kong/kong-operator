package helpers

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/kong/gateway-operator/pkg/consts"
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

// GetDefaultDataPlaneImagePreviousMinor returns the default data plane image with the previous version.
func GetDefaultDataPlaneImagePreviousMinor() string {
	defaultDPImage := GetDefaultDataPlaneImage()
	s := strings.Split(defaultDPImage, ".")
	if len(s) != 2 {
		panic(fmt.Sprintf("invalid default data plane image (more than one '.' in version), %s", defaultDPImage))
	}
	v, err := strconv.Atoi(s[1])
	if err != nil {
		panic(fmt.Sprintf("invalid default data plane image (after '.' not a number), %s", defaultDPImage))
	}
	// NOTICE: Kong Gateway 4.0 rather won't happen in foreseeable future, thus for now it's safe to have it hardcoded this way.
	previous := v - 1
	if previous < 0 {
		panic(fmt.Sprintf("invalid default data plane image (previous version is negative), %s", defaultDPImage))
	}
	s[1] = strconv.Itoa(previous)
	return strings.Join(s, ".")
}

// SetDefaultDataPlaneImage sets the default data plane image.
func SetDefaultDataPlaneImage(image string) {
	_defaultDataPlaneImageLock.Lock()
	defer _defaultDataPlaneImageLock.Unlock()
	_defaultDataPlaneImage = image
}
