package test

import (
	"net"
	"strconv"

	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/pkg/consts"
)

// NewHTTPBinContainer returns a container running the httpbin test image, bound
// to the IPv6 wildcard address so that it is reachable on IPv6-only clusters.
//
// The image's own command binds 0.0.0.0 (see its Cmd:
// `gunicorn -b 0.0.0.0:80 httpbin:app -k gevent`), so on an IPv6-only cluster
// nothing listens on the Pod's address and Kong answers 502 for any route
// pointing at it. The Pod still reports Ready, because the generated Deployment
// has no readiness probe.
//
// Binding the IPv6 wildcard keeps IPv4 working too: Linux defaults
// net.ipv6.bindv6only to 0, so the socket accepts both families.
func NewHTTPBinContainer(name string, port int32) corev1.Container {
	container := generators.NewContainer(name, HTTPBinImage, port)
	container.Command = []string{
		"gunicorn",
		"-b", net.JoinHostPort(consts.ListenAddressIPv6, strconv.Itoa(int(port))),
		"httpbin:app",
		"-k", "gevent",
	}
	return container
}
