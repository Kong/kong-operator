package helpers

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/test/helpers/certificate"
)

// SetupControllerOperatorResources sets up the controller namespace and CA secret that are required
// for the operator to run. It returns a cleanup function that should be called when done.
func SetupControllerOperatorResources(
	ctx context.Context,
	namespace string,
	cl client.Client,
) (func(), error) {
	fmt.Printf("INFO: creating controller namespace %s\n", namespace)
	controllerNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if err := client.IgnoreAlreadyExists(cl.Create(ctx, controllerNs)); err != nil {
		return nil, err
	}

	// Create CA Secret before environment setup so that
	// the controller has it available on startup.
	cert, key := certificate.MustGenerateCertPEMFormat(
		certificate.WithCommonName("Kong Operator CA"),
		certificate.WithCATrue(),
	)
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kong-operator-ca",
			Namespace: namespace,
			Labels: map[string]string{
				"konghq.com/secret": "internal",
			},
		},
		Data: map[string][]byte{
			"ca.crt":  cert,
			"tls.crt": cert,
			"tls.key": key,
		},
	}
	if err := client.IgnoreNotFound(cl.Delete(ctx, caSecret)); err != nil {
		return nil, err
	}
	if err := cl.Create(ctx, caSecret); err != nil {
		return nil, err
	}

	if err := os.Setenv("POD_NAMESPACE", namespace); err != nil {
		return nil, err
	}
	if err := os.Setenv("POD_NAME", "kong-operator-controller-manager"); err != nil {
		return nil, err
	}

	return func() {
		os.Unsetenv("POD_NAMESPACE")
		os.Unsetenv("POD_NAME")
		_ = client.IgnoreNotFound(cl.Delete(ctx, caSecret))
		_ = client.IgnoreNotFound(cl.Delete(ctx, controllerNs))
	}, nil
}
