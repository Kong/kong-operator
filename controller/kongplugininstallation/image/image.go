package image

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/types"
	ociv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/samber/lo"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"

	"github.com/kong/kong-operator/v2/modules/manager/metadata"
)

// The target files' names expected in an image with a custom Kong plugin.
const (
	kongPluginHandler = "handler.lua"
	kongPluginSchema  = "schema.lua"
)

// PluginFiles maps a plugin's file names to their content.
// It's expected that each plugin consists of `schema.lua` and `handler.lua` files.
type PluginFiles map[string]string

// newPluginFilesFromMap creates PluginFiles from a map of files with content.
// It ensures that the required files handler.lua and schema.lua are only present
// in the map.
func newPluginFilesFromMap(pluginFiles map[string]string) (PluginFiles, error) {
	var missingFiles []string
	for _, f := range []string{kongPluginHandler, kongPluginSchema} {
		if _, ok := pluginFiles[f]; !ok {
			missingFiles = append(missingFiles, f)
		}
	}
	if len(missingFiles) > 0 {
		return nil, fmt.Errorf("required files not found in the image: %s", strings.Join(missingFiles, ", "))
	}
	if len(pluginFiles) != 2 {
		return nil, fmt.Errorf("expected exactly 2 files, got %d: %s", len(pluginFiles), strings.Join(lo.Keys(pluginFiles), ","))
	}
	return PluginFiles(pluginFiles), nil
}

// FetchPlugin fetches the content of the plugin from the image URL. When authentication is not needed pass nil.
func FetchPlugin(ctx context.Context, imageURL string, credentialsStore credentials.Store) (PluginFiles, error) {
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
	registry.Client = &auth.Client{
		Client:     auth.DefaultClient.Client,
		Header:     map[string][]string{"User-Agent": {metadata.Metadata().UserAgent()}},
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
	// Image with plugin should have exactly one layer that contains a plugin with the name plugin.lua.
	// This is requirement described in details in the documentation. Any mismatch is treated as invalid image.
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

type sizeLimitBytes int64

func (sl sizeLimitBytes) int64() int64 {
	return int64(sl)
}

func (sl sizeLimitBytes) String() string {
	return fmt.Sprintf("%.2f MiB", float64(sl)/(1024*1024))
}

func extractKongPluginFromLayer(r io.Reader) (PluginFiles, error) {
	// Search for the files walking through the archive.
	// The size of a plugin is limited to the size of a ConfigMap in Kubernetes.
	const sizeLimit1MiB sizeLimitBytes = 1024 * 1024

	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to parse layer as tar.gz: %w", err)
	}
	pluginFiles := make(map[string]string)
	for tr := tar.NewReader(io.LimitReader(gr, sizeLimit1MiB.int64())); ; {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("unexpected error during looking for plugin: %w", err)
		}

		switch fileName := filepath.Base(h.Name); fileName {
		case kongPluginHandler, kongPluginSchema:
			file := make([]byte, h.Size)
			if _, err := io.ReadFull(tr, file); err != nil {
				if errors.Is(err, io.ErrUnexpectedEOF) {
					return nil, fmt.Errorf("plugin size limit of %s exceeded", sizeLimit1MiB)
				}
				return nil, fmt.Errorf("failed to read %s from image: %w", fileName, err)
			}
			pluginFiles[fileName] = string(file)
		default:
			return nil, fmt.Errorf(
				"file %q is unexpected, required files are %s and %s", fileName, kongPluginHandler, kongPluginSchema,
			)
		}
	}

	return newPluginFilesFromMap(pluginFiles)
}
