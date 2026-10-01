package ops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// The tests in this file cover every variant of the root unions of the
// generated entities with labels, by reflection over the SDK union types, so
// a variant added to the SDK is covered without changing them:
//
//   - create and update: the generated code sets the operator's labels on
//     every variant of the SDK request union (checked on the generated source,
//     as a request can only be built from a valid spec, which differs per
//     variant);
//   - getForUID: with each variant of the SDK list item union set, the entity
//     is found by its k8s-uid label, and not without it.

// TestUnionVariantsGetLabels checks that the generated create and update
// functions of the union entities set the operator's labels on every variant
// of the SDK request union: for each variant V of the union, the function has
//
//	if <path>.V != nil { <path>.V.Labels = WithKubernetesMetadataLabels(obj, <path>.V.Labels) }
//
// (the guard may also check the parents in <path>). The union type is found
// by resolving <path> from the type returned by the CR's To<Op>...Request
// conversion the function assigns to req.
func TestUnionVariantsGetLabels(t *testing.T) {
	testCases := []struct {
		file      string
		crType    reflect.Type
		functions []string
	}{
		{
			file:      "zz_generated_ops_aigatewayauthstrategy.go",
			crType:    reflect.TypeFor[aiconfigurationv1alpha1.AIGatewayAuthStrategy](),
			functions: []string{"createAIGatewayAuthStrategy", "updateAIGatewayAuthStrategy"},
		},
		{
			file:      "zz_generated_ops_aigatewaymodel.go",
			crType:    reflect.TypeFor[aiconfigurationv1alpha1.AIGatewayModel](),
			functions: []string{"createAIGatewayModel", "updateAIGatewayModel"},
		},
		{
			file:      "zz_generated_ops_aigatewaymodelprovider.go",
			crType:    reflect.TypeFor[aiconfigurationv1alpha1.AIGatewayModelProvider](),
			functions: []string{"createAIGatewayModelProvider", "updateAIGatewayModelProvider"},
		},
		{
			file:      "zz_generated_ops_aigatewaymcpserver.go",
			crType:    reflect.TypeFor[aiconfigurationv1alpha1.AIGatewayMCPServer](),
			functions: []string{"createAIGatewayMCPServer", "updateAIGatewayMCPServer"},
		},
		{
			file:      "zz_generated_ops_aigatewaycustompolicy.go",
			crType:    reflect.TypeFor[aiconfigurationv1alpha1.AIGatewayCustomPolicy](),
			functions: []string{"createAIGatewayCustomPolicy", "updateAIGatewayCustomPolicy"},
		},
		{
			file:      "zz_generated_ops_eventgatewaylistenerpolicy.go",
			crType:    reflect.TypeFor[configurationv1alpha1.EventGatewayListenerPolicy](),
			functions: []string{"createEventGatewayListenerPolicy", "updateEventGatewayListenerPolicy"},
		},
		{
			file:      "zz_generated_ops_eventgatewayvirtualclusterpolicy.go",
			crType:    reflect.TypeFor[configurationv1alpha1.EventGatewayVirtualClusterPolicy](),
			functions: []string{"createEventGatewayVirtualClusterPolicy", "updateEventGatewayVirtualClusterPolicy"},
		},
		{
			file:      "zz_generated_ops_eventgatewayvirtualclusterproducepolicy.go",
			crType:    reflect.TypeFor[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](),
			functions: []string{"createEventGatewayVirtualClusterProducePolicy", "updateEventGatewayVirtualClusterProducePolicy"},
		},
		{
			file:      "zz_generated_ops_eventgatewayvirtualclusterconsumepolicy.go",
			crType:    reflect.TypeFor[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](),
			functions: []string{"createEventGatewayVirtualClusterConsumePolicy", "updateEventGatewayVirtualClusterConsumePolicy"},
		},
		{
			file:      "zz_generated_ops_eventgatewayschemaregistry.go",
			crType:    reflect.TypeFor[configurationv1alpha1.EventGatewaySchemaRegistry](),
			functions: []string{"createEventGatewaySchemaRegistry", "updateEventGatewaySchemaRegistry"},
		},
	}

	for _, tc := range testCases {
		file, err := parser.ParseFile(token.NewFileSet(), tc.file, nil, 0)
		require.NoError(t, err)
		for _, name := range tc.functions {
			t.Run(name, func(t *testing.T) {
				fn := findFunc(t, file, name)
				reqType := requestTypeOf(t, fn, tc.crType)

				// The variants labels are set on, grouped by the union they belong to.
				injected := map[reflect.Type][]string{}
				for _, path := range labelInjections(t, fn) {
					union, variant := resolveVariant(t, reqType, path)
					injected[union] = append(injected[union], variant)
				}
				require.NotEmpty(t, injected, "%s sets no labels", name)
				for union, variants := range injected {
					for _, member := range sdkUnionMembers(t, union) {
						assert.Contains(t, variants, member.Name,
							"%s does not set labels on variant %s of %s", name, member.Name, union)
					}
				}
			})
		}
	}
}

// findFunc returns the declaration of the function named name in file.
func findFunc(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	require.Failf(t, "function not found", "%s", name)
	return nil
}

// requestTypeOf returns the type of req in fn, from its
//
//	req, err := obj.<fields>.<method>(...)
//
// statement: the (pointer) type returned by method, resolved on crType.
func requestTypeOf(t *testing.T, fn *ast.FuncDecl, crType reflect.Type) reflect.Type {
	t.Helper()
	var reqType reflect.Type
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || reqType != nil || len(assign.Lhs) == 0 || len(assign.Rhs) != 1 {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); !ok || id.Name != "req" {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// Resolve the receiver (obj.<fields>) from the CR type.
		recv := reflect.PointerTo(crType)
		for _, field := range selectorPath(t, sel.X)[1:] {
			f, ok := derefType(recv).FieldByName(field)
			require.True(t, ok, "%s has no field %s", derefType(recv), field)
			recv = f.Type
		}
		method, ok := recv.MethodByName(sel.Sel.Name)
		if !ok {
			method, ok = reflect.PointerTo(derefType(recv)).MethodByName(sel.Sel.Name)
		}
		require.True(t, ok, "%s has no method %s", recv, sel.Sel.Name)
		reqType = derefType(method.Type.Out(0))
		return false
	})
	require.NotNil(t, reqType, "%s does not build req from the CR", fn.Name.Name)
	return reqType
}

// labelInjections returns the paths (after req) of the fields fn sets labels
// on with <path>.Labels = WithKubernetesMetadataLabels(obj, <path>.Labels),
// inside an if statement guarding <path> != nil.
func labelInjections(t *testing.T, fn *ast.FuncDecl) [][]string {
	t.Helper()
	var paths [][]string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		guarded := nilChecks(t, ifStmt.Cond)
		for _, stmt := range ifStmt.Body.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				continue
			}
			call, ok := assign.Rhs[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "WithKubernetesMetadataLabels" {
				continue
			}
			target := selectorPath(t, assign.Lhs[0])
			require.Equal(t, "Labels", target[len(target)-1])
			path := target[1 : len(target)-1] // Drop req and Labels.
			require.Equal(t, target, selectorPath(t, call.Args[1]), "labels are read from another field than they are set on")
			require.Contains(t, guarded, strings.Join(path, "."), "labels are set on %s without a nil check", strings.Join(path, "."))
			paths = append(paths, path)
		}
		return true
	})
	return paths
}

// nilChecks returns the paths (after req) checked with "<path> != nil" in the
// && conjunction cond.
func nilChecks(t *testing.T, cond ast.Expr) []string {
	t.Helper()
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	switch bin.Op {
	case token.LAND:
		return append(nilChecks(t, bin.X), nilChecks(t, bin.Y)...)
	case token.NEQ:
		if id, ok := bin.Y.(*ast.Ident); ok && id.Name == "nil" {
			if _, ok := bin.X.(*ast.SelectorExpr); ok {
				return []string{strings.Join(selectorPath(t, bin.X)[1:], ".")}
			}
		}
	default:
	}
	return nil
}

// selectorPath returns the identifiers of a selector chain, e.g. [req A B] for req.A.B.
func selectorPath(t *testing.T, expr ast.Expr) []string {
	t.Helper()
	switch e := expr.(type) {
	case *ast.Ident:
		return []string{e.Name}
	case *ast.SelectorExpr:
		return append(selectorPath(t, e.X), e.Sel.Name)
	default:
		require.Failf(t, "unexpected expression", "%T", expr)
		return nil
	}
}

// resolveVariant resolves path from reqType and returns the union type the
// last field belongs to, and the field's name.
func resolveVariant(t *testing.T, reqType reflect.Type, path []string) (reflect.Type, string) {
	t.Helper()
	current := reqType
	for i, name := range path {
		f, ok := current.FieldByName(name)
		require.True(t, ok, "%s has no field %s", current, name)
		if i == len(path)-1 {
			require.Equal(t, "member", f.Tag.Get("union"), "%s.%s is not a union member", current, name)
			return current, name
		}
		current = derefType(f.Type)
	}
	return nil, ""
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// sdkUnionMembers returns the union member fields of an SDK union type.
func sdkUnionMembers(t *testing.T, unionType reflect.Type) []reflect.StructField {
	t.Helper()
	var members []reflect.StructField
	for f := range unionType.Fields() {
		if f.Tag.Get("union") == "member" {
			members = append(members, f)
		}
	}
	require.NotEmpty(t, members, "no union members found in %s", unionType)
	return members
}

const unionVariantsTestUID types.UID = "union-variants-uid"

// newSDKUnionItem returns a value of the SDK union type T with only member set,
// with the given ID and labels.
func newSDKUnionItem[T any](t *testing.T, member reflect.StructField, id string, labels map[string]string) T {
	t.Helper()
	var item T
	v := reflect.New(member.Type.Elem())
	idField := v.Elem().FieldByName("ID")
	require.True(t, idField.IsValid(), "%s has no ID field", member.Type.Elem())
	switch idField.Kind() {
	case reflect.String:
		idField.SetString(id)
	case reflect.Pointer:
		idField.Set(reflect.ValueOf(&id))
	default:
		require.Failf(t, "unexpected ID type", "%s.ID is %s", member.Type.Elem(), idField.Type())
	}
	labelsField := v.Elem().FieldByName("Labels")
	require.True(t, labelsField.IsValid(), "%s has no Labels field", member.Type.Elem())
	labelsField.Set(reflect.ValueOf(labels))
	reflect.ValueOf(&item).Elem().FieldByName(member.Name).Set(v)
	return item
}

// unionLookupCases runs, for each member of the SDK list item union T, a
// lookup whose list returns only items with that member set, and checks the
// entity is found by its k8s-uid label, and not found without it. It also
// checks an object without a UID is not found, without listing (lookup must
// then not expect a list call).
func unionLookupCases[T any](t *testing.T, lookup func(t *testing.T, uid types.UID, items []T) (string, error)) {
	t.Helper()
	for _, member := range sdkUnionMembers(t, reflect.TypeFor[T]()) {
		t.Run(member.Name, func(t *testing.T) {
			id, err := lookup(t, unionVariantsTestUID, []T{
				newSDKUnionItem[T](t, member, "other", map[string]string{KubernetesUIDLabelKey: "other-uid"}),
				newSDKUnionItem[T](t, member, "matched", map[string]string{KubernetesUIDLabelKey: string(unionVariantsTestUID)}),
			})
			require.NoError(t, err)
			assert.Equal(t, "matched", id)

			id, err = lookup(t, unionVariantsTestUID, []T{
				newSDKUnionItem[T](t, member, "unlabeled", nil),
			})
			assert.Empty(t, id)
			var notFound EntityWithMatchingUIDNotFoundError
			require.ErrorAs(t, err, &notFound)
		})
	}
	t.Run("returns not found without listing when the object has no UID", func(t *testing.T) {
		id, err := lookup(t, "", nil)
		assert.Empty(t, id)
		var notFound EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFound)
	})
}

// TestUnionVariantsGetForUID checks the getForUID lookups of the entities
// whose list items are unions (their ID and labels are read from whichever
// variant is set) for every variant.
func TestUnionVariantsGetForUID(t *testing.T) {
	const gatewayID = "gateway-1"
	gatewayRef := func() *aiconfigurationv1alpha1.KonnectEntityRef {
		return &aiconfigurationv1alpha1.KonnectEntityRef{ID: gatewayID}
	}

	t.Run("AIGatewayAuthStrategy", func(t *testing.T) {
		unionLookupCases(t, func(t *testing.T, uid types.UID, items []sdkkonnectcomp.AIGatewayAuthStrategy) (string, error) {
			sdk := sdkmocks.NewMockAIGatewayAuthStrategiesSDK(t)
			if uid != "" {
				sdk.EXPECT().
					ListAiGatewayAuthStrategies(mock.Anything, sdkkonnectops.ListAiGatewayAuthStrategiesRequest{GatewayID: gatewayID}).
					Return(&sdkkonnectops.ListAiGatewayAuthStrategiesResponse{
						ListAIGatewayAuthStrategiesResponse: &sdkkonnectcomp.ListAIGatewayAuthStrategiesResponse{Data: items},
					}, nil).Once()
			}
			return getAIGatewayAuthStrategyForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
				UID:    uid,
				Status: aiconfigurationv1alpha1.AIGatewayAuthStrategyStatus{GatewayID: gatewayRef()},
			})
		})
	})
	t.Run("AIGatewayModel", func(t *testing.T) {
		unionLookupCases(t, func(t *testing.T, uid types.UID, items []sdkkonnectcomp.AIGatewayModel) (string, error) {
			sdk := sdkmocks.NewMockAIGatewayModelsSDK(t)
			if uid != "" {
				sdk.EXPECT().
					ListAiGatewayModels(mock.Anything, sdkkonnectops.ListAiGatewayModelsRequest{GatewayID: gatewayID}).
					Return(&sdkkonnectops.ListAiGatewayModelsResponse{
						ListAIGatewayModelsResponse: &sdkkonnectcomp.ListAIGatewayModelsResponse{Data: items},
					}, nil).Once()
			}
			return getAIGatewayModelForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayModel{
				UID:    uid,
				Status: aiconfigurationv1alpha1.AIGatewayModelStatus{GatewayID: gatewayRef()},
			})
		})
	})
	t.Run("AIGatewayModelProvider", func(t *testing.T) {
		unionLookupCases(t, func(t *testing.T, uid types.UID, items []sdkkonnectcomp.AIGatewayModelProvider) (string, error) {
			sdk := sdkmocks.NewMockAIGatewayModelProvidersSDK(t)
			if uid != "" {
				sdk.EXPECT().
					ListAiGatewayModelProviders(mock.Anything, sdkkonnectops.ListAiGatewayModelProvidersRequest{GatewayID: gatewayID}).
					Return(&sdkkonnectops.ListAiGatewayModelProvidersResponse{
						ListAIGatewayModelProvidersResponse: &sdkkonnectcomp.ListAIGatewayModelProvidersResponse{Data: items},
					}, nil).Once()
			}
			return getAIGatewayModelProviderForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayModelProvider{
				UID:    uid,
				Status: aiconfigurationv1alpha1.AIGatewayModelProviderStatus{GatewayID: gatewayRef()},
			})
		})
	})
	t.Run("AIGatewayMCPServer", func(t *testing.T) {
		unionLookupCases(t, func(t *testing.T, uid types.UID, items []sdkkonnectcomp.AIGatewayMCPServer) (string, error) {
			sdk := sdkmocks.NewMockAIGatewayMCPServersSDK(t)
			if uid != "" {
				sdk.EXPECT().
					ListAiGatewayMcpServers(mock.Anything, sdkkonnectops.ListAiGatewayMcpServersRequest{GatewayID: gatewayID}).
					Return(&sdkkonnectops.ListAiGatewayMcpServersResponse{
						ListAIGatewayMCPServersResponse: &sdkkonnectcomp.ListAIGatewayMCPServersResponse{Data: items},
					}, nil).Once()
			}
			return getAIGatewayMCPServerForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayMCPServer{
				UID:    uid,
				Status: aiconfigurationv1alpha1.AIGatewayMCPServerStatus{GatewayID: gatewayRef()},
			})
		})
	})
	t.Run("AIGatewayCustomPolicy", func(t *testing.T) {
		unionLookupCases(t, func(t *testing.T, uid types.UID, items []sdkkonnectcomp.AIGatewayCustomPolicy) (string, error) {
			sdk := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
			if uid != "" {
				sdk.EXPECT().
					ListAiGatewayCustomPolicies(mock.Anything, sdkkonnectops.ListAiGatewayCustomPoliciesRequest{GatewayID: gatewayID}).
					Return(&sdkkonnectops.ListAiGatewayCustomPoliciesResponse{
						ListAIGatewayCustomPoliciesResponse: &sdkkonnectcomp.ListAIGatewayCustomPoliciesResponse{Data: items},
					}, nil).Once()
			}
			return getAIGatewayCustomPolicyForUID(t.Context(), sdk, &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
				UID:    uid,
				Status: aiconfigurationv1alpha1.AIGatewayCustomPolicyStatus{GatewayID: gatewayRef()},
			})
		})
	})
}

// TestUnionVariantsGetLabelsCoversAllUnionEntities makes sure the list in
// TestUnionVariantsGetLabels is kept up to date: every generated ops file
// whose create or update function sets labels on more than one union variant
// (several WithKubernetesMetadataLabels calls, each in an if statement) is
// covered.
func TestUnionVariantsGetLabelsCoversAllUnionEntities(t *testing.T) {
	covered := []string{
		"zz_generated_ops_aigatewayauthstrategy.go",
		"zz_generated_ops_aigatewaymodel.go",
		"zz_generated_ops_aigatewaymodelprovider.go",
		"zz_generated_ops_aigatewaymcpserver.go",
		"zz_generated_ops_aigatewaycustompolicy.go",
		"zz_generated_ops_eventgatewaylistenerpolicy.go",
		"zz_generated_ops_eventgatewayvirtualclusterpolicy.go",
		"zz_generated_ops_eventgatewayvirtualclusterproducepolicy.go",
		"zz_generated_ops_eventgatewayvirtualclusterconsumepolicy.go",
		"zz_generated_ops_eventgatewayschemaregistry.go",
	}
	names, err := filepath.Glob("zz_generated_ops_*.go")
	require.NoError(t, err)
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || (!strings.HasPrefix(fn.Name.Name, "create") && !strings.HasPrefix(fn.Name.Name, "update")) {
				continue
			}
			if guardedInjections(fn) > 1 {
				assert.True(t, slices.Contains(covered, name),
					"%s sets labels on union variants but is not covered by TestUnionVariantsGetLabels", name)
			}
		}
	}
}

// guardedInjections returns the number of if statements in fn that set labels
// with WithKubernetesMetadataLabels.
func guardedInjections(fn *ast.FuncDecl) int {
	count := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		found := false
		ast.Inspect(ifStmt.Body, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "WithKubernetesMetadataLabels" {
				found = true
			}
			return !found
		})
		if found {
			count++
		}
		return true
	})
	return count
}
