package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/controller/konnect/constraints"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestWatchOptions(t *testing.T) {
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.KonnectGatewayControlPlane{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongService{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1.KongConsumer{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongRoute{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongCACertificate{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongCertificate{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongKey{})
}

func testReconciliationWatchOptionsForEntity[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	ent TEnt,
) {
	t.Helper()

	var tt T = *ent
	t.Run(tt.GetTypeName(), func(t *testing.T) {
		cl := fakectrlruntimeclient.NewFakeClient()
		require.NotNil(t, cl)
		watchOptions := ReconciliationWatchOptionsForEntity[T, TEnt](cl, ent)
		_ = watchOptions
	})
}
