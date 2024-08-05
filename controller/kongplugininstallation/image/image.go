package image

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/types"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"

	"github.com/kong/gateway-operator/modules/manager/metadata"
)

// FetchPluginContent fetches the content of the plugin from the image URL. When authentication is not needed pass nil.
func FetchPluginContent(ctx context.Context, imageURL string, credentialsStore credentials.Store) ([]byte, error) {
	ref, err := name.ParseReference(imageURL)
	if err != nil {
		return nil, fmt.Errorf("unexpected format of image url: %w", err)
	}
	registryName, repositoryName, imageTag := ref.Context().RegistryStr(), ref.Context().RepositoryStr(), ref.Identifier()
	// Errors for NewRegistry(..) and Repository(..) should never happen because the image URL has been already validated above.
	registry, err := remote.NewRegistry(registryName)
	if err != nil {
		return nil, fmt.Errorf("for image: %s unexpected registry: %s, because: %w", imageURL, registryName, err)
	}
	var credentialFunc auth.CredentialFunc
	if credentialsStore != nil {
		credentialFunc = credentials.Credential(credentialsStore)
	}
	httpClient := auth.DefaultClient
	httpClient.Header = map[string][]string{"User-Agent": {metadata.Metadata().UserAgent()}}
	registry.Client = &auth.Client{
		Client:     httpClient.Client,
		Cache:      auth.NewCache(),
		Credential: credentialFunc,
	}

	repository, err := registry.Repository(ctx, repositoryName)
	if err != nil {
		return nil, fmt.Errorf("for image: %s unexpected repository: %s, because: %w", imageURL, registryName, err)
	}

	var (
		mut                        sync.Mutex
		layersThatMayContainPlugin []ociv1.Descriptor
	)
	inMemoryStore := memory.New()
	if _, err := oras.Copy(ctx, repository, imageTag, inMemoryStore, imageTag, oras.CopyOptions{
		CopyGraphOptions: oras.CopyGraphOptions{
			PostCopy: func(ctx context.Context, desc ociv1.Descriptor) error {
				// Look for OCI or Docker layer media type (they are fully compatible, see:
				// https://github.com/opencontainers/image-spec/blob/39ab2d54cfa8fe1bee1ff20001264986d92ab85a/media-types.md?plain=1#L60-L64)
				// Such object in the graph represents an actual layer that contains a plugin.
				if mediaType := types.MediaType(desc.MediaType); mediaType == types.OCILayer || mediaType == types.DockerLayer {
					mut.Lock()
					layersThatMayContainPlugin = append(layersThatMayContainPlugin, desc)
					mut.Unlock()
				}
				return nil
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("can't fetch image: %s, because: %w", imageURL, err)
	}
	// How to build such image is described in details,
	// following manual results in the image with exactly one layer that contains a plugin.
	if numOfLayers := len(layersThatMayContainPlugin); numOfLayers != 1 {
		return nil, fmt.Errorf("expected exactly one layer with plugin, found %d layers", numOfLayers)
	}
	layerWithPlugin := layersThatMayContainPlugin[0]
	contentOfLayerWithPlugin, err := inMemoryStore.Fetch(ctx, layerWithPlugin)
	if err != nil {
		return nil, fmt.Errorf("can't get layer of image: %w", err)
	}

	return extractKongPluginFromLayer(contentOfLayerWithPlugin)
}

// CredentialsStoreFromString expects content of typical configuration as a string, described
// in https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry
// and returns credentials.Store.
// This is typical way how private registries are used with Docker and Kubernetes.
func CredentialsStoreFromString(s string) (credentials.Store, error) {
	// TODO: Now we create temporary file, which is not great and should be changed,
	// but it's the only way to use credentials.NewFileStore(...) which robustly
	// parses config.json (format used by Docker and Kubernetes).
	tmpFile, err := os.CreateTemp("", "credentials")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer tmpFile.Close()
	defer os.Remove(tmpFile.Name())
	if _, err = tmpFile.WriteString(s); err != nil {
		return nil, fmt.Errorf("failed to write credentials to file: %w", err)
	}
	return credentials.NewFileStore(tmpFile.Name())
}

func extractKongPluginFromLayer(r io.Reader) ([]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layer as tar.gz: %w", err)
	}

	// The target file name for custom Kong plugin.
	const kongPluginName = "plugin.lua"

	// Search for the file walking through the archive.
	// Limit plugin to 1MB the same as ConfigMap in Kubernetes.
	const sizeLimit_1MiB = 1024 * 1024

	for tr := tar.NewReader(io.LimitReader(gr, sizeLimit_1MiB)); ; {
		switch h, err := tr.Next(); {
		case err == nil:
			if filepath.Base(h.Name) == kongPluginName {
				plugin := make([]byte, h.Size)
				if _, err := io.ReadFull(tr, plugin); err != nil {
					return nil, fmt.Errorf("failed to read %s from image: %w", kongPluginName, err)
				}
				return plugin, nil
			}
		case errors.Is(err, io.EOF):
			return nil, fmt.Errorf("file %q not found in the image", kongPluginName)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return nil, fmt.Errorf("plugin size exceed %d bytes", sizeLimit_1MiB)
		default:
			return nil, fmt.Errorf("unexpected error during looking for plugin: %w", err)
		}
	}
}
