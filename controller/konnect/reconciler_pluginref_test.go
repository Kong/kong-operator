package konnect

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestHandlePluginRef(t *testing.T) {
	ctx := context.Background()
	scheme := scheme.Get()

	testCases := []struct {
		name string
		// ent is the object passed to handlePluginRef — may be any client.Object.
		ent                 client.Object
		plugins             []configurationv1.KongPlugin
		grants              []configurationv1alpha1.KongReferenceGrant
		interceptorFuncs    *interceptor.Funcs
		expectStop          bool
		expectError         bool
		expectConditionType consts.ConditionType
		expectCondition     metav1.ConditionStatus
		expectReason        string
	}{
		{
			name: "non-KongPluginBinding object is a no-op",
			ent: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{Name: "plugin", Namespace: "ns"},
			},
			expectStop:  false,
			expectError: false,
		},
		{
			name: "same-namespace pluginRef with existing plugin sets PluginRefValid=True",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "ns",
					},
				},
			},
			plugins: []configurationv1.KongPlugin{
				{ObjectMeta: metav1.ObjectMeta{Name: "my-plugin", Namespace: "ns"}},
			},
			expectStop:          false,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionTrue,
			expectReason:        konnectv1alpha1.KongPluginRefReasonValid,
		},
		{
			name: "same-namespace pluginRef with missing plugin sets PluginRefValid=False",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "ns",
					},
				},
			},
			expectStop:          true,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        konnectv1alpha1.KongPluginRefReasonInvalid,
		},
		{
			name: "same-namespace pluginRef missing plugin during deletion is a no-op",
			ent: func() client.Object {
				now := metav1.Now()
				return &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "pb",
						Namespace:         "ns",
						DeletionTimestamp: &now,
						Finalizers:        []string{KonnectCleanupFinalizer},
					},
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongPluginBinding",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name:      "my-plugin",
							Namespace: "ns",
						},
					},
				}
			}(),
			expectStop:  false,
			expectError: false,
		},
		{
			name: "empty pluginRef namespace with existing plugin sets PluginRefValid=True",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "",
					},
				},
			},
			plugins: []configurationv1.KongPlugin{
				{ObjectMeta: metav1.ObjectMeta{Name: "my-plugin", Namespace: "ns"}},
			},
			expectStop:          false,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionTrue,
			expectReason:        konnectv1alpha1.KongPluginRefReasonValid,
		},
		{
			name: "cross-namespace pluginRef with valid grant sets PluginRefValid=True",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "binding-ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "plugin-ns",
					},
				},
			},
			plugins: []configurationv1.KongPlugin{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "my-plugin", Namespace: "plugin-ns"},
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-pb-to-plugin",
						Namespace: "plugin-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongPluginBinding",
								Namespace: "binding-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongPlugin",
							},
						},
					},
				},
			},
			expectStop:          false,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionTrue,
			expectReason:        konnectv1alpha1.KongPluginRefReasonValid,
		},
		{
			name: "cross-namespace pluginRef without grant sets PluginRefValid=False",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "binding-ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "plugin-ns",
					},
				},
			},
			grants:              []configurationv1alpha1.KongReferenceGrant{},
			expectStop:          true,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        konnectv1alpha1.KongPluginRefReasonRefNotPermitted,
		},
		{
			name: "cross-namespace pluginRef with grant for wrong namespace sets PluginRefValid=False",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "binding-ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "plugin-ns",
					},
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-pb-to-plugin",
						Namespace: "plugin-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongPluginBinding",
								Namespace: "other-ns", // wrong source namespace
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongPlugin",
							},
						},
					},
				},
			},
			expectStop:          true,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        konnectv1alpha1.KongPluginRefReasonRefNotPermitted,
		},
		{
			name: "cross-namespace pluginRef with grant for wrong kind sets PluginRefValid=False",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "binding-ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "plugin-ns",
					},
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-pb-to-plugin",
						Namespace: "plugin-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongService", // wrong from kind
								Namespace: "binding-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongPlugin",
							},
						},
					},
				},
			},
			expectStop:          true,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        konnectv1alpha1.KongPluginRefReasonRefNotPermitted,
		},
		{
			name: "cross-namespace pluginRef without grant during deletion is a no-op",
			ent: func() client.Object {
				now := metav1.Now()
				return &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "pb",
						Namespace:         "binding-ns",
						DeletionTimestamp: &now,
						Finalizers:        []string{KonnectCleanupFinalizer},
					},
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongPluginBinding",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name:      "my-plugin",
							Namespace: "plugin-ns",
						},
					},
				}
			}(),
			grants:      []configurationv1alpha1.KongReferenceGrant{},
			expectStop:  false,
			expectError: false,
		},
		{
			name: "cross-namespace pluginRef with valid grant but missing plugin sets PluginRefValid=False",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "binding-ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "plugin-ns",
					},
				},
			},
			// plugins intentionally empty — plugin does not exist
			plugins: []configurationv1.KongPlugin{},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-pb-to-plugin",
						Namespace: "plugin-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongPluginBinding",
								Namespace: "binding-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongPlugin",
							},
						},
					},
				},
			},
			expectStop:          true,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        konnectv1alpha1.KongPluginRefReasonInvalid,
		},
		{
			name: "cross-namespace pluginRef missing plugin during deletion is a no-op",
			ent: func() client.Object {
				now := metav1.Now()
				return &configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "pb",
						Namespace:         "binding-ns",
						DeletionTimestamp: &now,
						Finalizers:        []string{KonnectCleanupFinalizer},
					},
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongPluginBinding",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name:      "my-plugin",
							Namespace: "plugin-ns",
						},
					},
				}
			}(),
			plugins: []configurationv1.KongPlugin{},
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-pb-to-plugin",
						Namespace: "plugin-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongPluginBinding",
								Namespace: "binding-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "configuration.konghq.com",
								Kind:  "KongPlugin",
							},
						},
					},
				},
			},
			expectStop:  false,
			expectError: false,
		},
		{
			// covers: return ctrl.Result{}, true, err (non-ReferenceNotGranted grant check error)
			name: "cross-namespace grant check returns unexpected error stops reconciliation",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "binding-ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "plugin-ns",
					},
				},
			},
			interceptorFuncs: &interceptor.Funcs{
				List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*configurationv1alpha1.KongReferenceGrantList); ok {
						return fmt.Errorf("unexpected list error")
					}
					return c.List(ctx, list, opts...)
				},
			},
			expectStop:  true,
			expectError: true,
		},
		{
			// covers: StatusWithCondition error path for RefNotPermitted
			name: "cross-namespace RefNotPermitted patch error stops reconciliation with error",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "binding-ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "plugin-ns",
					},
				},
			},
			grants: []configurationv1alpha1.KongReferenceGrant{}, // no grant → RefNotPermitted
			interceptorFuncs: &interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
					return fmt.Errorf("status patch failed")
				},
			},
			expectStop:  true,
			expectError: true,
		},
		{
			// covers: !apierrors.IsNotFound(err) branch when cl.Get returns a generic error
			name: "cl.Get returns non-NotFound error sets PluginRefValid=False/Invalid",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "ns",
					},
				},
			},
			interceptorFuncs: &interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1.KongPlugin); ok {
						return fmt.Errorf("internal server error")
					}
					return c.Get(ctx, key, obj, opts...)
				},
			},
			expectStop:          true,
			expectError:         false,
			expectConditionType: consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			expectCondition:     metav1.ConditionFalse,
			expectReason:        konnectv1alpha1.KongPluginRefReasonInvalid,
		},
		{
			// covers: StatusWithCondition error path for Invalid (plugin not found)
			name: "plugin not found patch error stops reconciliation with error",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "ns",
					},
				},
			},
			// no plugins → not found → patch attempted → patch fails
			interceptorFuncs: &interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
					return fmt.Errorf("status patch failed")
				},
			},
			expectStop:  true,
			expectError: true,
		},
		{
			// covers: StatusWithCondition error path for Valid (plugin exists)
			name: "plugin exists patch error stops reconciliation with error",
			ent: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "pb", Namespace: "ns"},
				TypeMeta: metav1.TypeMeta{
					APIVersion: configurationv1alpha1.GroupVersion.String(),
					Kind:       "KongPluginBinding",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{
						Name:      "my-plugin",
						Namespace: "ns",
					},
				},
			},
			plugins: []configurationv1.KongPlugin{
				{ObjectMeta: metav1.ObjectMeta{Name: "my-plugin", Namespace: "ns"}},
			},
			interceptorFuncs: &interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
					return fmt.Errorf("status patch failed")
				},
			},
			expectStop:  true,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objs := []client.Object{tc.ent}
			for i := range tc.plugins {
				objs = append(objs, &tc.plugins[i])
			}
			for i := range tc.grants {
				objs = append(objs, &tc.grants[i])
			}

			builder := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(tc.ent)
			if tc.interceptorFuncs != nil {
				builder = builder.WithInterceptorFuncs(*tc.interceptorFuncs)
			}
			cl := builder.Build()

			res, stop, err := handlePluginRef(ctx, cl, tc.ent)

			assert.Equal(t, tc.expectStop, stop, "unexpected stop value")
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tc.expectConditionType != "" {
				pb, ok := tc.ent.(*configurationv1alpha1.KongPluginBinding)
				require.True(t, ok, "expected ent to be KongPluginBinding when checking condition")

				updated := &configurationv1alpha1.KongPluginBinding{}
				require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(pb), updated))

				var found bool
				for _, cond := range updated.Status.Conditions {
					if cond.Type == string(tc.expectConditionType) {
						found = true
						assert.Equal(t, tc.expectCondition, cond.Status, "unexpected condition status")
						assert.Equal(t, tc.expectReason, cond.Reason, "unexpected condition reason")
						break
					}
				}
				assert.True(t, found, "expected condition type %q not found in status", tc.expectConditionType)
			}

			if !tc.expectError && tc.expectStop {
				assert.True(t, res.IsZero(), "expected zero result when stopping without error")
			}
		})
	}
}
