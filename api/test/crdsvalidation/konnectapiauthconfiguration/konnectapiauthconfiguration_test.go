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
	"github.com/kong/kubernetes-configuration/test/crdsvalidation/konnectapiauthconfiguration/testcases"
)

func TestKonnectAPIAuthConfiguration(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.GetConfig()
	require.NoError(t, err, "error loading Kubernetes config")
	cl, err := konnectv1alpha1client.NewForConfig(cfg)
	require.NoError(t, err, "error creating konnectv1alpha1client client")

	for _, tcsGroup := range testcases.TestCases {
		t.Run(tcsGroup.Name, func(t *testing.T) {
			for _, tc := range tcsGroup.TestCases {
				t.Run(tc.Name, func(t *testing.T) {
					cl := cl.KonnectAPIAuthConfigurations(tc.KonnectAPIAuthConfiguration.Namespace)
					entity, err := cl.Create(ctx, &tc.KonnectAPIAuthConfiguration, metav1.CreateOptions{})
					if err == nil {
						t.Cleanup(func() {
							assert.NoError(t, client.IgnoreNotFound(cl.Delete(ctx, entity.Name, metav1.DeleteOptions{})))
						})
						// Create doesn't set the status, so we need to update it explicitly.
						entity.Status = tc.KonnectAPIAuthConfiguration.Status
						entity, err = cl.UpdateStatus(ctx, entity, metav1.UpdateOptions{})
						assert.NoError(t, err)
					}

					if tc.ExpectedErrorMessage == nil {
						assert.NoError(t, err)

						// Update the object and check if the update is allowed.
						if tc.Update != nil {
							tc.Update(entity)
							_, err := cl.Update(ctx, entity, metav1.UpdateOptions{})
							if tc.ExpectedUpdateErrorMessage != nil {
								require.NotNil(t, err)
								assert.Contains(t, err.Error(), *tc.ExpectedUpdateErrorMessage)
							} else {
								assert.NoError(t, err)
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
