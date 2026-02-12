package ops

import (
	"errors"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestErrorIsForbiddenError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "error is ForbiddenError",
			err: &sdkkonnecterrs.ForbiddenError{
				Status:   403,
				Title:    "Quota Exceeded",
				Instance: "kong:trace:0000000000000000000",
				Detail:   "Maximum number of Active Networks exceeded. Max allowed: 0",
			},
			want: true,
		},
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
			got := ErrorIsForbiddenError(tt.err)
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

func TestErrorIsSDKError400(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "error is not SDKError",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "SDKError with non-400 status code",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 500,
				Body:       `{"code": 13, "message": "internal error"}`,
			},
			want: false,
		},
		{
			name: "SDKError with 400 status code and invalid JSON body",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       `invalid json`,
			},
			want: true,
		},
		{
			name: "SDKError with 400 status code and empty details",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body:       `{"code": 3, "message": "validation error", "details": []}`,
			},
			want: true,
		},
		{
			name: "SDKError with 400 status code and non-reference error type",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body: `{
					"code": 3,
					"message": "validation error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"type": "ERROR_TYPE_FIELD",
							"field": "name",
							"messages": ["name is required"]
						}
					]
				}`,
			},
			want: true,
		},
		{
			name: "SDKError with 400 status code and ERROR_TYPE_REFERENCE should be recoverable",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body: `{
					"code": 3,
					"message": "data constraint error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"type": "ERROR_TYPE_REFERENCE",
							"field": "service.id",
							"messages": ["service.id (type: foreign) constraint failed"]
						}
					]
				}`,
			},
			want: false,
		},
		{
			name: "SDKError with 400 status code and multiple details including ERROR_TYPE_REFERENCE",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body: `{
					"code": 3,
					"message": "data constraint error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"type": "ERROR_TYPE_FIELD",
							"field": "name",
							"messages": ["name is invalid"]
						},
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"type": "ERROR_TYPE_REFERENCE",
							"field": "service.id",
							"messages": ["service.id (type: foreign) constraint failed"]
						}
					]
				}`,
			},
			want: true,
		},
		{
			name: "SDKError with 400 status code and multiple details, all ERROR_TYPE_REFERENCE",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 400,
				Body: `{
					"code": 3,
					"message": "data constraint error",
					"details": [
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"type": "ERROR_TYPE_REFERENCE",
							"field": "service.uuid",
							"messages": ["service.uuid (type: primary) constraint failed"]
						},
						{
							"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
							"type": "ERROR_TYPE_REFERENCE",
							"field": "service.id",
							"messages": ["service.id (type: foreign) constraint failed"]
						}
					]
				}`,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorIsSDKError400(tt.err)
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
			name: "error is SDKError with code 6",
			err: &sdkkonnecterrs.SDKError{
				Body: `{
					"code": 6,
					"message": "already exists",
					"details": []
				}`,
			},
			want: true,
		},
		{
			name: "error is SDKError with code 7",
			err: &sdkkonnecterrs.SDKError{
				Body: `{
					"code": 7,
					"message": "other error",
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

func TestSDKErrorIsConflict(t *testing.T) {
	tests := []struct {
		name string
		err  *sdkkonnecterrs.SDKError
		want bool
	}{
		{
			name: "SDKError with data constraint error message and code 3",
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
			name: "SDKError with data constraint error message and code 6",
			err: &sdkkonnecterrs.SDKError{
				Body: `{
					"code": 6,
					"message": "data constraint error",
					"details": []
				}`,
			},
			want: true,
		},
		{
			name: "SDKError with unique constraint failed message",
			err: &sdkkonnecterrs.SDKError{
				Body: `{
					"code": 3,
					"message": "name (type: unique) constraint failed",
					"details": []
				}`,
			},
			want: true,
		},
		{
			name: "SDKError with non-conflict message",
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
			name: "SDKError with conflict message but different code",
			err: &sdkkonnecterrs.SDKError{
				Body: `{
					"code": 4,
					"message": "data constraint error",
					"details": []
				}`,
			},
			want: false,
		},
		{
			name: "SDKError with invalid JSON body",
			err: &sdkkonnecterrs.SDKError{
				Body: `invalid json`,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SDKErrorIsConflict(tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestErrorIsDataPlaneGroupConflictProposedConfigIsTheSame(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "not a ConflictError",
			err:  errors.New("not a conflict error"),
			want: false,
		},
		{
			name: "ConflictError with non-int code type",
			err: &sdkkonnecterrs.ConflictError{
				Status: 409.0,
				Detail: "Proposed configuration and current configuration are identical",
			},
			want: true,
		},
		{
			name: "ConflictError with wrong code",
			err: &sdkkonnecterrs.ConflictError{
				Status: 400,
				Detail: "Proposed configuration and current configuration are identical",
			},
			want: false,
		},
		{
			name: "ConflictError with non-string message",
			err: &sdkkonnecterrs.ConflictError{
				Status: 409,
				Detail: 12345,
			},
			want: false,
		},
		{
			name: "ConflictError with message missing expected substring",
			err: &sdkkonnecterrs.ConflictError{
				Status: 409,
				Detail: "Some other conflict detail",
			},
			want: false,
		},
		{
			name: "Valid ConflictError with matching float code and message",
			err: &sdkkonnecterrs.ConflictError{
				Status: 409.0,
				Detail: "Error: Proposed configuration and current configuration are identical, no changes required",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorIsDataPlaneGroupConflictProposedConfigIsTheSame(tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestErrorIsDataPlaneGroupBadRequestPreviousConfigNotFinishedProvisioning(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "error is not BadRequestError",
			err:  errors.New("not a bad request error"),
			want: false,
		},
		{
			name: "BadRequestError with empty invalid parameters",
			err:  &sdkkonnecterrs.BadRequestError{InvalidParameters: []sdkkonnectcomp.InvalidParameters{}},
			want: false,
		},
		{
			name: "BadRequestError with invalid parameter of wrong type",
			err: &sdkkonnecterrs.BadRequestError{
				InvalidParameters: []sdkkonnectcomp.InvalidParameters{
					{
						Type: "some_wrong_type",
					},
				},
			},
			want: false,
		},
		{
			name: "BadRequestError with matching type but nil InvalidParameterStandard",
			err: &sdkkonnecterrs.BadRequestError{
				InvalidParameters: []sdkkonnectcomp.InvalidParameters{
					{
						Type: sdkkonnectcomp.InvalidParametersTypeInvalidParameterStandard,
					},
				},
			},
			want: false,
		},
		{
			name: "BadRequestError with matching type, but non-matching field",
			err: &sdkkonnecterrs.BadRequestError{
				InvalidParameters: []sdkkonnectcomp.InvalidParameters{
					{
						Type: sdkkonnectcomp.InvalidParametersTypeInvalidParameterStandard,
						InvalidParameterStandard: &sdkkonnectcomp.InvalidParameterStandard{
							Field:  "wrong_field",
							Reason: "Data-plane groups in the previous configuration have not finished provisioning",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "BadRequestError with matching type and field but non-matching reason",
			err: &sdkkonnecterrs.BadRequestError{
				InvalidParameters: []sdkkonnectcomp.InvalidParameters{
					{
						Type: sdkkonnectcomp.InvalidParametersTypeInvalidParameterStandard,
						InvalidParameterStandard: &sdkkonnectcomp.InvalidParameterStandard{
							Field:  "dataplane_groups",
							Reason: "some other reason",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "BadRequestError with valid matching invalid parameter",
			err: &sdkkonnecterrs.BadRequestError{
				InvalidParameters: []sdkkonnectcomp.InvalidParameters{
					{
						Type: sdkkonnectcomp.InvalidParametersTypeInvalidParameterStandard,
						InvalidParameterStandard: &sdkkonnectcomp.InvalidParameterStandard{
							Field:  "dataplane_groups",
							Reason: "Error: Data-plane groups in the previous configuration have not finished provisioning",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "BadRequestError with multiple parameters where one matches",
			err: &sdkkonnecterrs.BadRequestError{
				InvalidParameters: []sdkkonnectcomp.InvalidParameters{
					{
						Type: sdkkonnectcomp.InvalidParametersTypeInvalidParameterStandard,
						InvalidParameterStandard: &sdkkonnectcomp.InvalidParameterStandard{
							Field:  "some_field",
							Reason: "irrelevant reason",
						},
					},
					{
						Type: sdkkonnectcomp.InvalidParametersTypeInvalidParameterStandard,
						InvalidParameterStandard: &sdkkonnectcomp.InvalidParameterStandard{
							Field:  "dataplane_groups",
							Reason: "Data-plane groups in the previous configuration have not finished provisioning fully",
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorIsDataPlaneGroupBadRequestPreviousConfigNotFinishedProvisioning(tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestErrorIsConflictError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "error is ConflictError with status 409",
			err: &sdkkonnecterrs.ConflictError{
				Status: 409.0,
				Detail: "Key (org_id, name) already exists.",
			},
			want: true,
		},
		{
			name: "error is ConflictError with non-409 status",
			err: &sdkkonnecterrs.ConflictError{
				Status: 400,
				Detail: "Some other error",
			},
			want: false,
		},
		{
			name: "error is not ConflictError",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "error is SDKError",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: 409,
				Body:       "conflict error body",
			},
			want: false,
		},
		{
			name: "error is nil",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorIsConflictError(tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestErrorIsRateLimited(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "error is RateLimited",
			err:  &sdkkonnecterrs.RateLimited{Status: lo.ToPtr(int64(429)), Title: lo.ToPtr("Too Many Requests")},
			want: true,
		},
		{
			name: "error is SDKError with 429 status code",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
			},
			want: true,
		},
		{
			name: "error is SDKError with non-429 status code",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusInternalServerError,
				Body:       `{"error": "internal server error"}`,
			},
			want: false,
		},
		{
			name: "error is not rate limit related",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorIsRateLimited(tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetRetryAfterFromRateLimitError(t *testing.T) {
	tests := []struct {
		name              string
		err               error
		wantDuration      time.Duration
		wantIsRateLimited bool
	}{
		{
			name:              "non-rate-limit error returns false",
			err:               errors.New("some other error"),
			wantDuration:      0,
			wantIsRateLimited: false,
		},
		{
			name: "SDKError with Retry-After header in seconds",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": []string{"30"},
					},
				},
			},
			wantDuration:      30 * time.Second,
			wantIsRateLimited: true,
		},
		{
			name: "SDKError with no Retry-After header returns default",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
				RawResponse: &http.Response{
					Header: http.Header{},
				},
			},
			wantDuration:      DefaultRateLimitRetryAfter,
			wantIsRateLimited: true,
		},
		{
			name: "SDKError with nil RawResponse returns default",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
			},
			wantDuration:      DefaultRateLimitRetryAfter,
			wantIsRateLimited: true,
		},
		{
			name: "SDKError with invalid Retry-After header returns default",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": []string{"invalid"},
					},
				},
			},
			wantDuration:      DefaultRateLimitRetryAfter,
			wantIsRateLimited: true,
		},
		{
			name: "SDKError with zero Retry-After header returns default",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": []string{"0"},
					},
				},
			},
			wantDuration:      DefaultRateLimitRetryAfter,
			wantIsRateLimited: true,
		},
		{
			name: "SDKError with negative Retry-After header returns default",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": []string{"-5"},
					},
				},
			},
			wantDuration:      DefaultRateLimitRetryAfter,
			wantIsRateLimited: true,
		},
		{
			name:              "RateLimited error returns default (no RawResponse)",
			err:               &sdkkonnecterrs.RateLimited{Status: lo.ToPtr(int64(429)), Title: lo.ToPtr("Too Many Requests")},
			wantDuration:      DefaultRateLimitRetryAfter,
			wantIsRateLimited: true,
		},
		{
			name: "SDKError with Retry-After header as HTTP date in the past returns default",
			err: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
				RawResponse: &http.Response{
					Header: http.Header{
						// RFC 1123 format date in the past
						"Retry-After": []string{"Wed, 21 Oct 2015 07:28:00 GMT"},
					},
				},
			},
			wantDuration:      DefaultRateLimitRetryAfter,
			wantIsRateLimited: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDuration, gotIsRateLimited := GetRetryAfterFromRateLimitError(tt.err)
			require.Equal(t, tt.wantIsRateLimited, gotIsRateLimited)
			if tt.wantIsRateLimited {
				require.Equal(t, tt.wantDuration, gotDuration)
			}
		})
	}

	// Test HTTP date format in the future separately since duration depends on current time
	t.Run("SDKError with Retry-After header as HTTP date in the future", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			futureTime := time.Now().Add(60 * time.Second)
			err := &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests,
				Body:       `{"error": "rate limited"}`,
				RawResponse: &http.Response{
					Header: http.Header{
						// RFC 1123 format (HTTP date)
						"Retry-After": []string{futureTime.UTC().Format(http.TimeFormat)},
					},
				},
			}

			gotDuration, gotIsRateLimited := GetRetryAfterFromRateLimitError(err)
			require.True(t, gotIsRateLimited)
			require.Equal(t, 60*time.Second, gotDuration)
		})
	})
}
