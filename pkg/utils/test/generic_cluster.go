package test

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// genericCluster is a clusters.Cluster implementation backed by an already
// running cluster reachable through the ambient kubeconfig (the same one
// kubectl uses: the KUBECONFIG env var or ~/.kube/config, honoring the current
// context). It performs no provisioning or teardown, which makes it suitable
// for running tests against pre-existing clusters such as minikube where the
// provider specific tooling (kind, gcloud, ...) is not available.
type genericCluster struct {
	name        string
	clusterType clusters.Type
	client      *kubernetes.Clientset
	cfg         *rest.Config
	addons      clusters.Addons
}

// NewGenericClusterFromKubeconfig builds a clusters.Cluster from the ambient
// kubeconfig. It does not create or delete any infrastructure: the cluster is
// expected to already exist and be reachable through the current kubeconfig
// context.
//
// Authentication support is limited by how KTF drives kubectl. KTF does not use
// the kubeconfig file directly; it regenerates a throwaway one from this
// rest.Config and only carries a fixed subset of fields (CA data, server,
// client cert/key data, username/password, bearer token, legacy auth provider).
// Consequently the supported credential forms are:
//   - client certificates, embedded or referenced as on-disk file paths (the
//     latter are inlined below) - e.g. minikube, kubeadm admin.conf, k3s,
//   - a static bearer token,
//   - HTTP basic auth.
//
// It does NOT support kubeconfigs that authenticate via an exec credential
// plugin (e.g. EKS "aws eks get-token", GKE "gke-gcloud-auth-plugin", OIDC
// helpers): rest.Config.ExecProvider is not carried over, so the regenerated
// kubeconfig ends up without credentials. It also does not propagate
// insecure-skip-tls-verify or tls-server-name. Managed-cluster kubeconfigs that
// rely on those will not work here.
func NewGenericClusterFromKubeconfig(name string, clusterType clusters.Type) (clusters.Cluster, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig for existing %s cluster %q: %w", clusterType, name, err)
	}

	// KTF regenerates a throwaway kubeconfig from this rest.Config (to drive
	// kubectl), but it only copies the embedded CertData/KeyData/CAData byte
	// fields. Kubeconfigs like minikube's reference the client cert, key and CA
	// as on-disk file paths instead, which would be dropped on that round-trip
	// and leave kubectl without credentials. Inline the file contents into the
	// *Data fields so the generated kubeconfig stays authenticated.
	if err := rest.LoadTLSFiles(cfg); err != nil {
		return nil, fmt.Errorf("failed to inline TLS credentials for existing %s cluster %q: %w", clusterType, name, err)
	}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build clientset for existing %s cluster %q: %w", clusterType, name, err)
	}

	return &genericCluster{
		name:        name,
		clusterType: clusterType,
		client:      client,
		cfg:         cfg,
		addons:      make(clusters.Addons),
	}, nil
}

func (c *genericCluster) Name() string                  { return c.name }
func (c *genericCluster) Type() clusters.Type           { return c.clusterType }
func (c *genericCluster) Client() *kubernetes.Clientset { return c.client }
func (c *genericCluster) Config() *rest.Config          { return c.cfg }

func (c *genericCluster) Version() (semver.Version, error) {
	versionInfo, err := c.Client().ServerVersion()
	if err != nil {
		return semver.Version{}, err
	}
	return semver.Parse(strings.TrimPrefix(versionInfo.String(), "v"))
}

// Cleanup is a no-op: the generic cluster is not owned by the test suite, so it
// must not be torn down.
func (c *genericCluster) Cleanup(context.Context) error { return nil }

func (c *genericCluster) GetAddon(name clusters.AddonName) (clusters.Addon, error) {
	if addon, ok := c.addons[name]; ok {
		return addon, nil
	}
	return nil, fmt.Errorf("addon %s not found", name)
}

func (c *genericCluster) ListAddons() []clusters.Addon {
	addonList := make([]clusters.Addon, 0, len(c.addons))
	for _, v := range c.addons {
		addonList = append(addonList, v)
	}
	return addonList
}

func (c *genericCluster) DeployAddon(ctx context.Context, addon clusters.Addon) error {
	if _, ok := c.addons[addon.Name()]; ok {
		return fmt.Errorf("addon component %s is already loaded into cluster %s", addon.Name(), c.Name())
	}
	c.addons[addon.Name()] = addon
	return addon.Deploy(ctx, c)
}

func (c *genericCluster) DeleteAddon(ctx context.Context, addon clusters.Addon) error {
	if _, ok := c.addons[addon.Name()]; !ok {
		return nil
	}
	if err := addon.Delete(ctx, c); err != nil {
		return err
	}
	delete(c.addons, addon.Name())
	return nil
}

// DumpDiagnostics dumps the generic (non provider specific) diagnostics for the
// cluster to a temporary directory.
func (c *genericCluster) DumpDiagnostics(ctx context.Context, meta string) (string, error) {
	outDir, err := os.MkdirTemp(os.TempDir(), clusters.DiagnosticOutDirectoryPrefix)
	if err != nil {
		return "", err
	}
	err = clusters.DumpDiagnostics(ctx, c, meta, outDir)
	return outDir, err
}

// IPFamily reports IPv4: minikube and most local clusters default to IPv4-only
// networking.
func (c *genericCluster) IPFamily() clusters.IPFamily { return clusters.IPv4 }
