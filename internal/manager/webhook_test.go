/*
Copyright 2022 Kong Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/internal/consts"
)

func TestWaitForWebhookCertificate(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(testScheme))

	testCases := []struct {
		name         string
		pollTimeout  time.Duration
		pollInterval time.Duration
		createAfter  time.Duration
		namespace    string
		secret       *corev1.Secret
		err          error
	}{
		{
			name:         "secret created before the timer expires",
			pollTimeout:  5 * time.Second,
			pollInterval: 1 * time.Second,
			createAfter:  2 * time.Second,
			namespace:    "test",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      consts.WebhookCertificateConfigSecretName,
					Namespace: "test",
				},
			},
			err: nil,
		},
		{
			name:         "secret not created before the timer expires",
			pollTimeout:  3 * time.Second,
			pollInterval: 1 * time.Second,
			namespace:    "test",
			err:          fmt.Errorf("timeout for creating webhook certificate expired"),
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(testScheme).
				Build()

			webhookMgr := webhookManager{
				client: fakeClient,
				cfg: &Config{
					ControllerNamespace: "test",
				},
			}

			if tc.createAfter != 0 {
				time.AfterFunc(tc.createAfter, func() {
					require.NoError(t, fakeClient.Create(ctx, tc.secret))
				})
			}
			_, err := webhookMgr.waitForWebhookCertificate(ctx, tc.pollTimeout, tc.pollInterval)
			if tc.err != nil {
				require.EqualError(t, err, tc.err.Error(), tc.name)
			} else {
				require.NoError(t, err, tc.name)
			}
		})
	}
}
