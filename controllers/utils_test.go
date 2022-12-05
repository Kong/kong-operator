package controllers

import (
	"bytes"
	"testing"

	"github.com/bombsimon/logrusr/v3"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/pointer"

	gwtypes "github.com/kong/gateway-operator/internal/types"
)

func Test_ensureContainerImageUpdated(t *testing.T) {
	for _, tt := range []struct {
		name          string
		originalImage string
		newImage      *string
		newVersion    *string
		expectedImage string
		updated       bool
		wantErr       string
	}{
		{
			name:          "invalid images produce an error",
			originalImage: "fake:invalid:image",
			wantErr:       "invalid container image found: fake:invalid:image",
		},
		{
			name:          "setting new image when existing is local with port is allowed",
			originalImage: "localhost:5000/kic:2.7.0",
			newImage:      pointer.String("kong/kong"),
			newVersion:    pointer.String("2.7.0"),
			expectedImage: "kong/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "setting new local image is allowed",
			originalImage: "kong/kong:2.7.0",
			newImage:      pointer.String("localhost:5000/kong"),
			newVersion:    pointer.String("2.7.0"),
			expectedImage: "localhost:5000/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "empty image and version makes no changes",
			originalImage: "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       false,
		},
		{
			name:          "same image and version makes no changes",
			originalImage: "kong/kong:2.7.0",
			newImage:      pointer.String("kong/kong"),
			newVersion:    pointer.String("2.7.0"),
			expectedImage: "kong/kong:2.7.0",
			updated:       false,
		},
		{
			name:          "version added when not originally present",
			originalImage: "kong/kong",
			newImage:      pointer.String("kong/kong"),
			newVersion:    pointer.String("2.7.0"),
			expectedImage: "kong/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "version is changed when a new one is provided",
			originalImage: "kong/kong:2.7.0",
			newImage:      pointer.String("kong/kong"),
			newVersion:    pointer.String("3.0.0"),
			expectedImage: "kong/kong:3.0.0",
			updated:       true,
		},
		{
			name:          "image is added when not originally present",
			originalImage: "",
			newImage:      pointer.String("kong/kong"),
			expectedImage: "kong/kong",
			updated:       true,
		},
		{
			name:          "image is changed when a new one is provided",
			originalImage: "kong/kong",
			newImage:      pointer.String("kong/kong-gateway"),
			expectedImage: "kong/kong-gateway",
			updated:       true,
		},
		{
			name:          "image and version are added when not originally present",
			originalImage: "",
			newImage:      pointer.String("kong/kong-gateway"),
			newVersion:    pointer.String("3.0.0"),
			expectedImage: "kong/kong-gateway:3.0.0",
			updated:       true,
		},
		{
			name:          "image and version are changed when new ones are provided",
			originalImage: "kong/kong:2.7.0",
			newImage:      pointer.String("kong/kong-gateway"),
			newVersion:    pointer.String("3.0.0"),
			expectedImage: "kong/kong-gateway:3.0.0",
			updated:       true,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			container := generators.NewContainer("test", tt.originalImage, 80)
			updated, err := ensureContainerImageUpdated(&container, tt.newImage, tt.newVersion)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err.Error())
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.updated, updated)
			if updated {
				assert.NotEqual(t, tt.originalImage, container.Image)
			} else {
				assert.Equal(t, tt.originalImage, container.Image)
			}

			if tt.expectedImage != "" {
				assert.Equal(t, tt.expectedImage, container.Image)
			}
		})
	}
}

func TestLog(t *testing.T) {
	var buf bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&buf)
	log := logrusr.New(logger)

	gw := gwtypes.Gateway{}
	t.Run("info logging works both for values and pointers to objects", func(t *testing.T) {
		info(log, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		info(log, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
	})

	t.Run("debug logging works both for values and pointers to objects", func(t *testing.T) {
		debug(log, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		debug(log, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
	})

	t.Run("trace logging works both for values and pointers to objects", func(t *testing.T) {
		trace(log, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		trace(log, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
	})
}
