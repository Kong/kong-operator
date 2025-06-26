package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_extractImageNameAndTag(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		wantName string
		wantTag  string
		wantErr  bool
	}{
		{
			name:     "gcr.io/kong/kong-operator:v1.0",
			fullName: "gcr.io/kong/kong-operator:v1.0",
			wantName: "gcr.io/kong/kong-operator",
			wantTag:  "v1.0",
		},
		{
			name:     "localhost:5000/kong/kong-operator:v1.0",
			fullName: "localhost:5000/kong/kong-operator:v1.0",
			wantName: "localhost:5000/kong/kong-operator",
			wantTag:  "v1.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotTag, err := extractImageNameAndTag(tt.fullName)
			if tt.wantErr {
				require.NoError(t, err)
				return
			}

			require.Equal(t, tt.wantName, gotName)
			require.Equal(t, tt.wantTag, gotTag)
		})
	}
}
