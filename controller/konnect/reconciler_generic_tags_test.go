package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/metadata"
)

func TestUserTagsForEntity(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name     string
		ent      client.Object
		plugins  []configurationv1.KongPlugin
		expected []string
	}{
		{
			name: "type without tags",
			ent: &konnectv1alpha2.KonnectGatewayControlPlane{
				Name: "cp", Namespace: "ns",
			},
		},
		{
			name: "KongService with spec and annotation tags",
			ent: &configurationv1alpha1.KongService{
				Name: "svc", Namespace: "ns",
				Annotations: map[string]string{metadata.AnnotationKeyTags: "a1,k8s-name:other"},
				Spec: configurationv1alpha1.KongServiceSpec{
					KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
						Tags: []string{"s1", "k8s-uid:1234"},
					},
				},
			},
			expected: []string{"s1", "k8s-uid:1234", "a1", "k8s-name:other"},
		},
		{
			name: "KongConsumer with spec tags",
			ent: &configurationv1.KongConsumer{
				Name: "c", Namespace: "ns",
				Spec: configurationv1.KongConsumerSpec{
					Tags: []string{"s1"},
				},
			},
			expected: []string{"s1"},
		},
		{
			name: "KongPluginBinding includes tags of the referenced KongPlugin",
			ent: &configurationv1alpha1.KongPluginBinding{
				Name: "pb", Namespace: "ns",
				Annotations: map[string]string{metadata.AnnotationKeyTags: "pba1"},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{Name: "plugin"},
					Tags:            []string{"pb1"},
				},
			},
			plugins: []configurationv1.KongPlugin{
				{
					Name: "plugin", Namespace: "ns",
					Annotations: map[string]string{metadata.AnnotationKeyTags: "pa1"},
					Tags:        []string{"p1"},
				},
			},
			expected: []string{"p1", "pa1", "pb1", "pba1"},
		},
		{
			name: "KongPluginBinding with missing KongPlugin",
			ent: &configurationv1alpha1.KongPluginBinding{
				Name: "pb", Namespace: "ns",
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					PluginReference: configurationv1alpha1.PluginRef{Name: "plugin"},
					Tags:            []string{"pb1"},
				},
			},
			expected: []string{"pb1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			objs := []client.Object{tc.ent}
			for i := range tc.plugins {
				objs = append(objs, &tc.plugins[i])
			}
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(objs...).
				Build()

			tags, err := userTagsForEntity(ctx, cl, tc.ent)
			require.NoError(t, err)
			assert.ElementsMatch(t, tc.expected, tags)
		})
	}
}

func TestHandleReservedTags(t *testing.T) {
	ctx := context.Background()

	programmedCondition := func(t *testing.T, cl client.Client, obj client.Object, conditions func() []metav1.Condition) *metav1.Condition {
		t.Helper()
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(obj), obj))
		for _, c := range conditions() {
			if c.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType {
				return &c
			}
		}
		return nil
	}

	t.Run("KongService without reserved tags is not stopped", func(t *testing.T) {
		svc := &configurationv1alpha1.KongService{
			Name: "svc", Namespace: "ns",
			Annotations: map[string]string{metadata.AnnotationKeyTags: "k8s-team:foo"},
			Spec: configurationv1alpha1.KongServiceSpec{
				KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
					Tags: []string{"tag1"},
				},
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(svc).
			WithStatusSubresource(svc).
			Build()

		stop, res, err := handleReservedTags(ctx, cl, svc)
		require.NoError(t, err)
		assert.False(t, stop)
		assert.True(t, res.IsZero())
		assert.Nil(t, programmedCondition(t, cl, svc, func() []metav1.Condition { return svc.Status.Conditions }))
	})

	t.Run("KongService with reserved tags in annotation is marked not programmed", func(t *testing.T) {
		svc := &configurationv1alpha1.KongService{
			Name: "svc", Namespace: "ns",
			Annotations: map[string]string{metadata.AnnotationKeyTags: "tag1,k8s-uid:1234"},
		}
		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(svc).
			WithStatusSubresource(svc).
			Build()

		stop, res, err := handleReservedTags(ctx, cl, svc)
		require.NoError(t, err)
		assert.True(t, stop)
		assert.True(t, res.IsZero())

		cond := programmedCondition(t, cl, svc, func() []metav1.Condition { return svc.Status.Conditions })
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectEntityProgrammedReasonInvalidTags, cond.Reason)
		assert.Contains(t, cond.Message, "k8s-uid:1234")
	})

	t.Run("KongPluginBinding referencing KongPlugin with reserved tags is marked not programmed", func(t *testing.T) {
		pb := &configurationv1alpha1.KongPluginBinding{
			Name: "pb", Namespace: "ns",
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				PluginReference: configurationv1alpha1.PluginRef{Name: "plugin"},
			},
		}
		plugin := &configurationv1.KongPlugin{
			Name: "plugin", Namespace: "ns",
			Tags: []string{"managed-by:someone-else"},
		}
		cl := fake.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(pb, plugin).
			WithStatusSubresource(pb).
			Build()

		stop, res, err := handleReservedTags(ctx, cl, pb)
		require.NoError(t, err)
		assert.True(t, stop)
		assert.True(t, res.IsZero())

		cond := programmedCondition(t, cl, pb, func() []metav1.Condition { return pb.Status.Conditions })
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, konnectv1alpha1.KonnectEntityProgrammedReasonInvalidTags, cond.Reason)
		assert.Contains(t, cond.Message, "managed-by:someone-else")
	})
}
