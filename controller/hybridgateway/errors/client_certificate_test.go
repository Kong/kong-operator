package errors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnlyClientCertificateRefErrors(t *testing.T) {
	invalid := &ClientCertificateRefError{Reason: "InvalidCertificateRef", Message: "missing certificate"}
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"invalid", invalid, true},
		{"wrapped", fmt.Errorf("translate service: %w", invalid), true},
		{"multiple invalid rules", fmt.Errorf("translation: %w", errors.Join(invalid, invalid)), true},
		{"transient API failure", errors.New("API unavailable"), false},
		{"mixed errors must preserve old state", errors.Join(invalid, errors.New("API unavailable")), false},
		{"mixed errors through converter wrapper", fmt.Errorf("translation failed with 2 errors: %w", errors.Join(invalid, errors.New("API unavailable"))), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, OnlyClientCertificateRefErrors(tt.err))
		})
	}
}
