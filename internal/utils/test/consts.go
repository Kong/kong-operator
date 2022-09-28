package test

import (
	"time"
)

// -----------------------------------------------------------------------------
// Public consts - Gateway
// -----------------------------------------------------------------------------

const (
	// GatewayClassAcceptanceTimeLimit is the amount of time that the operator
	// will wait for a GatewayClass to be accepted.
	GatewayClassAcceptanceTimeLimit = time.Second * 7

	// GatewaySchedulingTimeLimit is the maximum amount of time to wait for
	// a supported Gateway to be marked as Scheduled by the gateway controller.
	GatewaySchedulingTimeLimit = time.Second * 7

	// GatewayReadyTimeLimit is the maximum amount of time to wait for a
	// supported Gateway to be fully provisioned and marked as Ready by the
	// gateway controller.
	GatewayReadyTimeLimit = time.Minute * 3
)

// -----------------------------------------------------------------------------
// Public consts - Controlplane
// -----------------------------------------------------------------------------

const (
	// ControlPlaneCondDeadline is the default timeout for checking on controlplane resources.
	ControlPlaneCondDeadline = time.Minute
	// ControlPlaneCondTick is the default tick for checking on controlplane resources.
	ControlPlaneCondTick = time.Second
	// ControlPlaneSchedulingTimeLimit is the maximum amount of time to wait for
	// a supported ControlPlane to be created after a Gateway resource is
	// created
	ControlPlaneSchedulingTimeLimit = time.Minute * 3
)

// -----------------------------------------------------------------------------
// Public consts - Ingress
// -----------------------------------------------------------------------------

const (
	// WaitIngressTick is the default timeout tick interval for checking on ingress resources.
	WaitIngressTick = time.Second * 1
	// DefaultIngressWait is the default timeout for checking on ingress resources.
	DefaultIngressWait = time.Minute * 3
)

// -----------------------------------------------------------------------------
// Public consts - Container images
// -----------------------------------------------------------------------------

const (
	// httpBinImage is the container image name we use for deploying the "httpbin" HTTP testing tool.
	// if you need a simple HTTP server for tests you're writing, use this and check the documentation.
	// See: https://github.com/kong/httpbin
	HTTPBinImage = "kong/httpbin:0.1.0"

	// tcpEchoImage echoes TCP text sent to it after printing out basic information about its environment, e.g.
	// Welcome, you are connected to node kind-control-plane.
	// Running on Pod tcp-echo-58ccd6b78d-hn9t8.
	// In namespace foo.
	// With IP address 10.244.0.13.
	TCPEchoImage = "kong/go-echo:0.1.0"
)

// -----------------------------------------------------------------------------
// Global Testing Vars & Consts
// -----------------------------------------------------------------------------

const (
	// defaultKongResponseBody is the default response body that will be returned
	// from the Kong Gateway when it is first provisioned and when no default
	// routes are configured.
	DefaultKongResponseBody = `{"message":"no Route matched with those values"}`

	// ObjectUpdateTimeout is the amount of time that will be allowed for
	// conflicts to be resolved before an object update will be considered failed.
	ObjectUpdateTimeout = time.Second * 30

	// SubresourceReadinessWait is the maximum amount of time allowed for
	// sub-resources to become "Ready" after being created on behalf of a
	// parent resource.
	SubresourceReadinessWait = time.Second * 30

	DefaultHTTPPort = 80

	// GatewayCRDsKustomizeURL is a URL that provides Gateway CRDs.
	GatewayCRDsKustomizeURL = "github.com/kubernetes-sigs/gateway-api/config/crd/experimental?ref=v0.5.0"

	// GatewayRawRepoURL is the base URL to the raw Gateway API repository.
	GatewayRawRepoURL = "https://raw.githubusercontent.com/kubernetes-sigs/gateway-api/v0.5.0"
)
