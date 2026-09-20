package controllers

import (
	internalcontrollers "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers"
)

// DataPlane re-exports internalcontrollers.DataPlane so that reconcilers
// living outside the ingress-controller module tree can consume it.
type DataPlane = internalcontrollers.DataPlane

// Reconciler re-exports internalcontrollers.Reconciler so that reconcilers
// living outside the ingress-controller module tree can implement it.
type Reconciler = internalcontrollers.Reconciler
