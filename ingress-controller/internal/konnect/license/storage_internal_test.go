package license

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/license"
)

func TestKonnectLicenseFromSecret(t *testing.T) {
	timeNowUnix := time.Now().Unix()

	testCases := []struct {
		name            string
		secret          *corev1.Secret
		expectedLicense license.KonnectLicense
		expectError     bool
		expectedErrMsg  string
	}{
		{
			name: "nil secret Data returns error",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
			},
			expectError:    true,
			expectedErrMsg: "secret test-secret doesn't contain data",
		},
		{
			name: "empty Data map returns error with all missing keys",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: map[string][]byte{},
			},
			expectError:    true,
			expectedErrMsg: "missing required key(s): payload, id, updated_at in secret test-secret",
		},
		{
			name: "missing payload key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: map[string][]byte{
					"id":         []byte("some-id"),
					"updated_at": []byte(strconv.FormatInt(timeNowUnix, 10)),
				},
			},
			expectError:    true,
			expectedErrMsg: "missing required key(s): payload in secret test-secret",
		},
		{
			name: "missing id key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: map[string][]byte{
					"payload":    []byte("some-payload"),
					"updated_at": []byte(strconv.FormatInt(timeNowUnix, 10)),
				},
			},
			expectError:    true,
			expectedErrMsg: "missing required key(s): id in secret test-secret",
		},
		{
			name: "missing updated_at key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: map[string][]byte{
					"payload": []byte("some-payload"),
					"id":      []byte("some-id"),
				},
			},
			expectError:    true,
			expectedErrMsg: "missing required key(s): updated_at in secret test-secret",
		},
		{
			name: "missing multiple keys",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: map[string][]byte{
					"payload": []byte("some-payload"),
				},
			},
			expectError:    true,
			expectedErrMsg: "missing required key(s): id, updated_at in secret test-secret",
		},
		{
			name: "invalid updated_at value",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: map[string][]byte{
					"payload":    []byte("some-payload"),
					"id":         []byte("some-id"),
					"updated_at": []byte("not-a-number"),
				},
			},
			expectError:    true,
			expectedErrMsg: "failed to parse updated_at as timestamp of license stored in secret test-secret",
		},
		{
			name: "valid secret returns license",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: map[string][]byte{
					"payload":    []byte("some-license-payload"),
					"id":         []byte("some-license-id"),
					"updated_at": []byte(strconv.FormatInt(timeNowUnix, 10)),
				},
			},
			expectedLicense: license.KonnectLicense{
				Payload:   "some-license-payload",
				ID:        "some-license-id",
				UpdatedAt: time.Unix(timeNowUnix, 0),
			},
		},
		{
			name: "secret with empty payload is invalid",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
				Data: map[string][]byte{
					"payload":    []byte(""),
					"id":         []byte(""),
					"updated_at": []byte("0"),
				},
			},
			expectError:    true,
			expectedErrMsg: "missing required key(s): payload, id in secret test-secret",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := konnectLicenseFromSecret(tc.secret)
			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectedLicense, l)
		})
	}
}
