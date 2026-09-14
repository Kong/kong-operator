package test

import (
	"net"
	"strconv"

	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/test"
)

// NewHTTPBinContainer returns a container running the httpbin test image.
//
// On IPv6 clusters the command is overridden to bind the IPv6 wildcard
// address, because the image's default command binds 0.0.0.0 (see its Cmd:
// `gunicorn -b 0.0.0.0:80 httpbin:app -k gevent`), which is unreachable on
// IPv6-only clusters: nothing listens on the Pod's address and Kong answers
// 502 for any route pointing at it. The Pod still reports Ready, because the
// generated Deployment has no readiness probe.
//
// On IPv4 clusters the image's default command is used as-is.
//
// Note that the port argument is only honored in the IPv6 case: the image's
// default command always binds port 80 (which is what every current caller
// passes anyway).
func NewHTTPBinContainer(name string, port int32) corev1.Container {
	container := generators.NewContainer(name, HTTPBinImage, port)
	if test.ClusterIPFamily() == clusters.IPv6 {
		container.Command = []string{
			"gunicorn",
			"-b", net.JoinHostPort(consts.ListenAddressIPv6, strconv.Itoa(int(port))),
			"httpbin:app",
			"-k", "gevent",
		}
	}
	return container
}
