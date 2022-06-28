package gateway

import "fmt"

// -----------------------------------------------------------------------------
// Gateway - Errors
// -----------------------------------------------------------------------------

// ErrUnsupportedGateway is an error which indicates that a provided Gateway
// is not supported because it's GatewayClass was not associated with this
// controller.
var ErrUnsupportedGateway = fmt.Errorf("gateway not supported")
