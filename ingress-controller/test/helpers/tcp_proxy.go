package helpers

import internal "github.com/kong/kong-operator/v2/ingress-controller/test/internal/helpers"

type TCPProxy = internal.TCPProxy

func NewTCPProxy(destination string) (*TCPProxy, error) {
	return internal.NewTCPProxy(destination)
}
