package controllerutils

import (
	internalutils "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers/utils"
)

// EnsureProgrammedCondition re-exports internalutils.EnsureProgrammedCondition
// for use by reconcilers living outside the ingress-controller module tree.
var EnsureProgrammedCondition = internalutils.EnsureProgrammedCondition

// WithUnknownMessage re-exports internalutils.WithUnknownMessage for use by
// reconcilers living outside the ingress-controller module tree.
var WithUnknownMessage = internalutils.WithUnknownMessage
