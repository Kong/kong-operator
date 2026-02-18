package kongstate

import (
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
)

func TestCertificate_SanitizedCopy(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   Certificate
		want Certificate
	}{
		{
			name: "fills all fields but Consumer and sanitizes key",
			in: Certificate{kong.Certificate{
				ID:        new("1"),
				Cert:      new("2"),
				Key:       new("3"),
				CreatedAt: new(int64(4)),
				SNIs:      []*string{new("5.1"), new("5.2")},
				Tags:      []*string{new("6.1"), new("6.2")},
			}},
			want: Certificate{kong.Certificate{
				ID:        new("1"),
				Cert:      new("2"),
				Key:       redactedString,
				CreatedAt: new(int64(4)),
				SNIs:      []*string{new("5.1"), new("5.2")},
				Tags:      []*string{new("6.1"), new("6.2")},
			}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.SanitizedCopy()
			assert.Equal(t, tt.want, got)
		})
	}
}
