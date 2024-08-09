package kongpluginbindings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	konnectv1alpha1client "github.com/kong/kubernetes-configuration/pkg/clientset/typed/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/konnectcontrolplane/testcases"
)

func TestKonnectControlPlane(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.GetConfig()
	require.NoError(t, err, "error loading Kubernetes config")
	cl, err := konnectv1alpha1client.NewForConfig(cfg)
	require.NoError(t, err, "error creating konnectv1alpha1client client")

	for _, tcsGroup := range testcases.TestCases {
		tcsGroup := tcsGroup
		t.Run(tcsGroup.Name, func(t *testing.T) {
			for _, tc := range tcsGroup.TestCases {
				tc := tc
				t.Run(tc.Name, func(t *testing.T) {
					cl := cl.KonnectControlPlanes(tc.KonnectControlPlane.Namespace)
					kcp, err := cl.Create(ctx, &tc.KonnectControlPlane, metav1.CreateOptions{})
					if tc.ExpectedErrorMessage == nil {
						assert.NoError(t, err)
						t.Cleanup(func() {
							assert.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, kcp.Name, metav1.DeleteOptions{})))
						})

						// Update the object and check if the update is allowed.
						if tc.Update != nil {
							tc.Update(kcp)

							_, err := cl.Update(ctx, kcp, metav1.UpdateOptions{})
							if tc.ExpectedUpdateErrorMessage != nil {
								require.NotNil(t, err)
								assert.Contains(t, err.Error(), *tc.ExpectedUpdateErrorMessage)
							}
						}
					} else {
						require.NotNil(t, err)
						assert.Contains(t, err.Error(), *tc.ExpectedErrorMessage)
					}
				})
			}
		})
	}
}
