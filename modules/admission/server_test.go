package admission

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
)

func TestHandleDataPlaneValidation(t *testing.T) {
	b := fakeclient.NewClientBuilder()
	b.WithObjects(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-cm"},
			Data: map[string]string{
				"off": "off",
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-secret"},
			// fake client does not encode fields in StringData to Data,
			// so here we should usebase64 encoded value in Data.
			Data: map[string][]byte{
				"postgres": []byte(base64.StdEncoding.EncodeToString([]byte("postgres"))),
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-cm-2"},
			// fake client does not encode fields in StringData to Data,
			// so here we should usebase64 encoded value in Data.
			Data: map[string]string{
				"KONG_DATABASE": "xxx",
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-secret-2"},
			// fake client does not encode fields in StringData to Data,
			// so here we should usebase64 encoded value in Data.
			Data: map[string][]byte{
				"DATABASE": []byte(base64.StdEncoding.EncodeToString([]byte("xxx"))),
			},
		},
	)
	c := b.Build()

	handler := NewRequestHandler(c, logr.Discard())
	server := httptest.NewServer(handler)

	testCases := []struct {
		name      string
		dataplane *operatorv1beta1.DataPlane
		hasError  bool
		errMsg    string
	}{
		// NOTE: Scaffolding for admission server tests left intentionally to allow adding test cases
		// for curreently untested scenarios.
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			review := &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "",
					Kind: metav1.GroupVersionKind{
						Group:   operatorv1beta1.SchemeGroupVersion.Group,
						Version: operatorv1beta1.SchemeGroupVersion.Version,
						Kind:    "dataplanes",
					},
					Resource:  dataPlaneGVResource,
					Name:      tc.dataplane.Name,
					Namespace: tc.dataplane.Namespace,
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Object: tc.dataplane,
					},
				},
			}

			buf, err := json.Marshal(review)
			require.NoErrorf(t, err, "there should be error in marshaling into JSON")
			req, err := http.NewRequest("POST", server.URL, bytes.NewReader(buf))
			require.NoError(t, err, "there should be no error in making HTTP request")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err, "there should be no error in getting response")
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err, "there should be no error in reading body")
			resp.Body.Close()
			respReview := &admissionv1.AdmissionReview{}
			err = json.Unmarshal(body, respReview)
			require.NoError(t, err, "there should be no error in unmarshalling body")
			validationResp := respReview.Response

			if !tc.hasError {
				// code in http package is in type int, but int32 in Result.Code
				// so EqualValues used instead of Equal
				require.EqualValues(t, http.StatusOK, validationResp.Result.Code, "response code should be 200 OK")
			} else {
				require.EqualValues(t, http.StatusBadRequest, validationResp.Result.Code, "response code should be 400 Bad Request")
				require.Equal(t, validationResp.Result.Message, tc.errMsg, "result message should contain expected content")
			}
		})
	}
}
