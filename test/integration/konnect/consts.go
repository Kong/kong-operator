package konnect

import "time"

const (
	waitTime = 60 * time.Second
	tickTime = 1 * time.Second

	// A Konnect-attached (hybrid) DataPlane becomes ready only after it receives
	// its configuration from the Konnect control plane. That sync is normally fast
	// (a healthy data plane is ready within ~20s), but it occasionally stalls for a
	// freshly-provisioned data plane and never completes within any reasonable
	// window. Recreating the DataPlane establishes a fresh control-plane session and
	// reliably recovers, so readiness is retried with recreation rather than waited
	// on with a single long timeout. dataPlaneReadinessAttemptTimeout is the per-
	// attempt budget (generous versus the ~20s healthy case) and
	// dataPlaneReadinessMaxAttempts caps the number of recreations.
	dataPlaneReadinessAttemptTimeout = 90 * time.Second
	dataPlaneReadinessMaxAttempts    = 3
)
