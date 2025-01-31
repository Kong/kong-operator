package ops

import (
	"errors"
	"testing"

	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/require"
)

func TestErrorIsSDKError403(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "error is SDKError with 403 status code",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 403,
				Body: `{
					"code": 7,
					"message": "usage constraint error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"messages": [
								"operation not permitted on KIC cluster"
							]
						}
					]
				}`,
			},
			want: true,
		},
		{
			name: "error is SDKError with non-403 status code",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 404,
				Body: `{
					"code": 7,
					"message": "usage constraint error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"messages": [
								"operation not permitted on KIC cluster"
							]
						}
					]
				}`,
			},
			want: false,
		},
		{
			name: "error is not SDKError",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorIsSDKError403(tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestErrorIsSDKBadRequestError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "error is BadRequestError",
			err:  &sdkkonnecterrs.BadRequestError{},
			want: true,
		},
		{
			name: "error is not BadRequestError",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorIsSDKBadRequestError(tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestErrorIsCreateConflict(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "error is ConflictError",
			err:  &sdkkonnecterrs.ConflictError{},
			want: true,
		},
		{
			name: "error is SDKError with conflict message",
			err: &sdkkonnecterrs.SDKError{
				Body: `{
					"code": 3,
					"message": "data constraint error",
					"details": []
				}`,
			},
			want: true,
		},
		{
			name: "error is SDKError with non-conflict message",
			err: &sdkkonnecterrs.SDKError{
				Body: `{
					"code": 3,
					"message": "some other error",
					"details": []
				}`,
			},
			want: false,
		},
		{
			name: "error is not ConflictError or SDKError",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorIsCreateConflict(tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}
