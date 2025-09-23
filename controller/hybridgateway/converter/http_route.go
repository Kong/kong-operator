package converter

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/intermediate"
	"github.com/kong/kong-operator/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

var _ APIConverter[gwtypes.HTTPRoute] = &httpRouteConverter{}

// httpRouteConverter is a concrete implementation of the APIConverter interface for HTTPRoute.
type httpRouteConverter struct {
	client.Client

	route           *gwtypes.HTTPRoute
	outputStore     []client.Object
	sharedStatusMap *route.SharedRouteStatusMap
	ir              *intermediate.HTTPRouteRepresentation
}

// NewHTTPRouteConverter returns a new instance of httpRouteConverter.
func newHTTPRouteConverter(httpRoute *gwtypes.HTTPRoute, cl client.Client, sharedStatusMap *route.SharedRouteStatusMap) APIConverter[gwtypes.HTTPRoute] {
	return &httpRouteConverter{
		Client:          cl,
		outputStore:     []client.Object{},
		sharedStatusMap: sharedStatusMap,
		route:           httpRoute,
		ir:              intermediate.NewHTTPRouteRepresentation(httpRoute),
	}
}

// GetRootObject implements APIConverter.
func (c *httpRouteConverter) GetRootObject() gwtypes.HTTPRoute {
	return *c.route
}

// Translate implements APIConverter.
func (c *httpRouteConverter) Translate() error {
	return c.translate(context.TODO())
}

// GetOutputStore implements APIConverter.
func (c *httpRouteConverter) GetOutputStore(ctx context.Context) []unstructured.Unstructured {
	objects := make([]unstructured.Unstructured, 0, len(c.outputStore))
	for _, obj := range c.outputStore {
		unstr, err := utils.ToUnstructured(obj, c.Scheme())
		if err != nil {
			continue
		}
		objects = append(objects, unstr)
	}
	return objects
}

// Reduce implements APIConverter.
func (c *httpRouteConverter) Reduce(obj unstructured.Unstructured) []utils.ReduceFunc {
	switch obj.GetKind() {
	case "KongRoute":
		return []utils.ReduceFunc{
			utils.KeepProgrammed,
			utils.KeepYoungest,
		}
	default:
		return nil
	}
}

// ListExistingObjects implements APIConverter.
func (c *httpRouteConverter) ListExistingObjects(ctx context.Context) ([]unstructured.Unstructured, error) {
	if c.route == nil {
		return nil, nil
	}

	list := &configurationv1alpha1.KongRouteList{}
	labels := map[string]string{
		// TODO: Add appropriate labels for KongRoute objects managed by HTTPRoute
	}
	opts := []client.ListOption{
		client.InNamespace(c.route.Namespace),
		client.MatchingLabels(labels),
	}
	if err := c.List(ctx, list, opts...); err != nil {
		return nil, err
	}

	unstructuredItems := make([]unstructured.Unstructured, 0, len(list.Items))
	for _, item := range list.Items {
		unstr, err := utils.ToUnstructured(&item, c.Scheme())
		if err != nil {
			return nil, err
		}
		unstructuredItems = append(unstructuredItems, unstr)
	}

	return unstructuredItems, nil
}

// UpdateSharedRouteStatus implements APIConverter.
func (c *httpRouteConverter) UpdateSharedRouteStatus(objs []unstructured.Unstructured) error {
	// TODO: Implement status update logic for HTTPRoute
	return nil
}

// translate converts the HTTPRoute to KongRoute(s) and stores them in outputStore.
func (c *httpRouteConverter) translate(ctx context.Context) error {
	// Generate translation data.
	if err := c.addControlPlaneRefs(ctx); err != nil {
		return err
	}

	// Generate kong services, upstream and targets.
	for _, val := range c.ir.Rules {
		// Get the controlPlaneRef for the given Rule.
		cpr := c.ir.GetControlPlaneRefByName(val.Name)
		if cpr == nil {
			continue
		}
		name := val.String()

		// Build the upstream resource.
		upstream, err := builder.NewKongUpstream().
			WithName(name).
			WithNamespace(c.route.Namespace).
			WithLabels(c.route).
			WithAnnotations(c.route, c.ir.GetParentRefByName(val.Name)).
			WithSpecName(name).
			WithControlPlaneRef(*cpr).
			WithOwner(c.route).Build()
		if err != nil {
			// TODO: decide how to handle build errors in converter
			// For now, skip this resource
			continue
		}
		c.outputStore = append(c.outputStore, &upstream)

		// Build the service resource.
		service, err := builder.NewKongService().
			WithName(name).
			WithNamespace(c.route.Namespace).
			WithLabels(c.route).
			WithAnnotations(c.route, c.ir.GetParentRefByName(val.Name)).
			WithSpecName(name).
			WithSpecHost(name).
			WithControlPlaneRef(*cpr).
			WithOwner(c.route).Build()
		if err != nil {
			// TODO: decide how to handle build errors in converter
			// For now, skip this resource
			continue
		}
		c.outputStore = append(c.outputStore, &service)

		// Build the target resources.
		for _, bRef := range val.BackendRefs {
			targetName := bRef.String()

			target, err := builder.NewKongTarget().
				WithName(targetName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route).
				WithAnnotations(c.route, c.ir.GetParentRefByName(bRef.Name)).
				WithUpstreamRef(name).
				WithBackendRef(c.route, &bRef.BackendRef).
				WithOwner(c.route).Build()
			if err != nil {
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &target)
		}

		// Build the kong route resource.
		for _, match := range val.Matches {
			routeName := match.String()
			serviceName := val.String()

			route, err := builder.NewKongRoute().
				WithName(routeName).
				WithNamespace(c.route.Namespace).
				WithLabels(c.route).
				WithAnnotations(c.route, c.ir.GetParentRefByName(match.Name)).
				WithSpecName(routeName).
				WithStripPath(c.ir.StripPath).
				WithKongService(serviceName).
				WithHTTPRouteMatch(match.Match).
				WithOwner(c.route).Build()
			if err != nil {
				// TODO: decide how to handle build errors in converter
				// For now, skip this resource
				continue
			}
			c.outputStore = append(c.outputStore, &route)
		}

	}

	return nil
}

func (c *httpRouteConverter) addControlPlaneRefs(ctx context.Context) error {
	for i, pRef := range c.route.Spec.ParentRefs {
		pRefName := intermediate.NameFromHTTPRoute(c.route, "", i)
		cpRef, err := refs.GetControlPlaneRefByParentRef(ctx, c.Client, c.route, pRef)
		if err != nil {
			return err
		}
		c.ir.AddControlPlaneRef(intermediate.ControlPlaneRef{
			Name:            pRefName,
			ControlPlaneRef: cpRef,
		})
	}
	return nil
}
