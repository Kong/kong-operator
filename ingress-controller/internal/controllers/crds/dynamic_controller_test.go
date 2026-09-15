package crds

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// stubManager satisfies ctrl.Manager for the fields DynamicCRDController.Reconcile
// actually touches. Every other method is inherited from the nil embedded interface
// and panics if called, which is deliberate: the test wants to know if Reconcile
// reaches further than it should.
type stubManager struct {
	ctrl.Manager

	client client.Client
}

func (m stubManager) GetClient() client.Client { return m.client }

// recordingController records whether the wrapped controller was set up.
type recordingController struct {
	setUp bool
}

func (c *recordingController) SetupWithManager(ctrl.Manager) error {
	c.setUp = true
	return nil
}

func managerServing(gvrs ...schema.GroupVersionResource) stubManager {
	gvs := make([]schema.GroupVersion, 0, len(gvrs))
	for _, gvr := range gvrs {
		gvs = append(gvs, gvr.GroupVersion())
	}
	mapper := meta.NewDefaultRESTMapper(gvs)
	for _, gvr := range gvrs {
		// DefaultRESTMapper derives the plural from the kind, so name the kind after
		// the resource to keep KindFor(gvr) resolvable.
		mapper.AddSpecific(
			gvr.GroupVersion().WithKind(gvr.Resource),
			gvr,
			gvr.GroupVersion().WithResource(gvr.Resource+"-singular"),
			meta.RESTScopeNamespace,
		)
	}
	return stubManager{client: fake.NewClientBuilder().WithRESTMapper(mapper).Build()}
}

func gvr(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "example.com", Version: "v1", Resource: resource}
}

// The watch predicate matches any *one* of the required CRDs, so Reconcile can fire
// while the set is still incomplete. It must not start the wrapped controller then:
// startControllerOnce consumes the single setup attempt, so starting early against a
// type the apiserver does not serve is unrecoverable.
func TestReconcileDoesNotStartControllerUntilAllRequiredCRDsExist(t *testing.T) {
	required := []schema.GroupVersionResource{gvr("gateways"), gvr("tlsroutes"), gvr("referencegrants")}

	t.Run("partial set does not start the controller", func(t *testing.T) {
		wrapped := &recordingController{}
		// gateways and referencegrants are served, tlsroutes is not - the standard
		// channel install that prompted this test.
		r := &DynamicCRDController{
			Log:          logr.Discard(),
			Manager:      managerServing(gvr("gateways"), gvr("referencegrants")),
			Controller:   wrapped,
			RequiredCRDs: required,
		}

		_, err := r.Reconcile(t.Context(), &apiextensionsv1.CustomResourceDefinition{})
		require.NoError(t, err)
		require.False(t, wrapped.setUp,
			"controller must not be set up while a required CRD is still missing")
	})

	t.Run("complete set starts the controller", func(t *testing.T) {
		wrapped := &recordingController{}
		r := &DynamicCRDController{
			Log:          logr.Discard(),
			Manager:      managerServing(required...),
			Controller:   wrapped,
			RequiredCRDs: required,
		}

		_, err := r.Reconcile(t.Context(), &apiextensionsv1.CustomResourceDefinition{})
		require.NoError(t, err)
		require.True(t, wrapped.setUp, "controller must be set up once every required CRD exists")
	})
}
