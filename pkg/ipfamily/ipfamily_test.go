package ipfamily_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/pkg/ipfamily"
)

func TestNew(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    ipfamily.IPFamily
		wantErr bool
	}{
		{in: "auto", want: ipfamily.Auto},
		{in: "ipv4", want: ipfamily.IPv4},
		{in: "ipv6", want: ipfamily.IPv6},
		{in: "dual", want: ipfamily.Dual},
		{in: "bogus", wantErr: true},
		{in: "", wantErr: true},
	} {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ipfamily.New(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
