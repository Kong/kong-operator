package dataplane

import "errors"

// ErrDataPlaneBlueGreenRolloutFailedToChangeSpecDuringPromotion is an error
// which indicates that DataPlane update which changes its spec was rejected
// because it cannot be changed during a Blue Green promotion.
var ErrDataPlaneBlueGreenRolloutFailedToChangeSpecDuringPromotion = errors.New("failed to change DataPlane spec when promotion is in progress")
