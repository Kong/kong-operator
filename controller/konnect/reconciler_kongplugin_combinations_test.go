package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1beta1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestGetCombinations(t *testing.T) {
	type args struct {
		relations ForeignRelations
	}
	tests := []struct {
		name string
		args args
		want []Rel
	}{
		{
			name: "empty",
			args: args{
				relations: ForeignRelations{},
			},
			want: nil,
		},
		{
			name: "plugins on consumer only",
			args: args{
				relations: ForeignRelations{
					Consumer: []configurationv1.KongConsumer{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "foo",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bar",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					Consumer: "foo",
				},
				{
					Consumer: "bar",
				},
			},
		},
		{
			name: "plugins on consumer group only",
			args: args{
				relations: ForeignRelations{
					ConsumerGroup: []configurationv1beta1.KongConsumerGroup{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "foo",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "bar",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					ConsumerGroup: "foo",
				},
				{
					ConsumerGroup: "bar",
				},
			},
		},
		{
			name: "plugins on service only",
			args: args{
				relations: ForeignRelations{
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					Service: "s1",
				},
			},
		},
		{
			name: "plugins on services only",
			args: args{
				relations: ForeignRelations{
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s2",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					Service: "s1",
				},
				{
					Service: "s2",
				},
			},
		},
		{
			name: "plugins on combination of service and route",
			args: args{
				relations: ForeignRelations{
					Route: []configurationv1alpha1.KongRoute{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "r1",
							},
							Spec: configurationv1alpha1.KongRouteSpec{
								ServiceRef: &configurationv1alpha1.ServiceRef{
									Type: configurationv1alpha1.ServiceRefNamespacedRef,
									NamespacedRef: &commonv1alpha1.NameRef{
										Name: "s1",
									},
								},
							},
						},
					},
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					Route: "r1",
				},
				{
					Service: "s1",
				},
			},
		},
		{
			name: "plugins on combination of service and consumer",
			args: args{
				relations: ForeignRelations{
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
					},
					Consumer: []configurationv1.KongConsumer{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "c1",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					Consumer: "c1",
					Service:  "s1",
				},
				// NOTE: https://github.com/kong/kong-operator/issues/660
				// is related to the following combination not being present.
				// Currently we do not generate combination for Service only
				// when Service **and** Consumers have the annotation present.
				// {
				// 	Service: "s1",
				// },
			},
		},
		{
			name: "plugins on combination of service and consumers",
			args: args{
				relations: ForeignRelations{
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
					},
					Consumer: []configurationv1.KongConsumer{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "c1",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "c2",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					Consumer: "c1",
					Service:  "s1",
				},
				{
					Consumer: "c2",
					Service:  "s1",
				},
				// NOTE: https://github.com/kong/kong-operator/issues/660
				// is related to the following combination not being present.
				// Currently we do not generate combination for Service only
				// when Service **and** Consumers have the annotation present.
				// {
				// 	Service: "s1",
				// },
			},
		},
		{
			name: "plugins on combination of service and consumer groups",
			args: args{
				relations: ForeignRelations{
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
					},
					ConsumerGroup: []configurationv1beta1.KongConsumerGroup{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "cg1",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "cg2",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					ConsumerGroup: "cg1",
					Service:       "s1",
				},
				{
					ConsumerGroup: "cg2",
					Service:       "s1",
				},
				// NOTE: https://github.com/kong/kong-operator/issues/660
				// is related to the following combination not being present.
				// Currently we do not generate combination for Service only
				// when Service **and** ConsumerGroups have the annotation present.
				// {
				// 	Service: "s1",
				// },
			},
		},
		{
			name: "plugins on combination of service, route and consumer groups",
			args: args{
				relations: ForeignRelations{
					Route: []configurationv1alpha1.KongRoute{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "r1",
							},
							Spec: configurationv1alpha1.KongRouteSpec{
								ServiceRef: &configurationv1alpha1.ServiceRef{
									Type: configurationv1alpha1.ServiceRefNamespacedRef,
									NamespacedRef: &commonv1alpha1.NameRef{
										Name: "s1",
									},
								},
							},
						},
					},
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
					},
					ConsumerGroup: []configurationv1beta1.KongConsumerGroup{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "cg1",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "cg2",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					ConsumerGroup: "cg1",
					Route:         "r1",
				},
				{
					ConsumerGroup: "cg1",
					Service:       "s1",
				},
				{
					ConsumerGroup: "cg2",
					Route:         "r1",
				},
				{
					ConsumerGroup: "cg2",
					Service:       "s1",
				},
				// NOTE: https://github.com/kong/kong-operator/issues/660
				// is related to the following combination not being present.
				// Currently we do not generate combination for Service and Route
				// on their own.
				// {
				// 	Service: "s1",
				// },
				// {
				//	Route: "r1",
				// },
			},
		},
		{
			name: "plugins on combination of service, route and consumer",
			args: args{
				relations: ForeignRelations{
					Route: []configurationv1alpha1.KongRoute{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "r1",
							},
							Spec: configurationv1alpha1.KongRouteSpec{
								ServiceRef: &configurationv1alpha1.ServiceRef{
									Type: configurationv1alpha1.ServiceRefNamespacedRef,
									NamespacedRef: &commonv1alpha1.NameRef{
										Name: "s1",
									},
								},
							},
						},
					},
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
					},
					Consumer: []configurationv1.KongConsumer{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "c1",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					Consumer: "c1",
					Route:    "r1",
				},
				{
					Consumer: "c1",
					Service:  "s1",
				},
			},
		},
		{
			name: "plugins on combination of service, route and consumers",
			args: args{
				relations: ForeignRelations{
					Route: []configurationv1alpha1.KongRoute{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "r1",
							},
							Spec: configurationv1alpha1.KongRouteSpec{
								ServiceRef: &configurationv1alpha1.ServiceRef{
									Type: configurationv1alpha1.ServiceRefNamespacedRef,
									NamespacedRef: &commonv1alpha1.NameRef{
										Name: "s1",
									},
								},
							},
						},
					},
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "s1",
							},
						},
					},
					Consumer: []configurationv1.KongConsumer{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "c1",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "c2",
							},
						},
					},
				},
			},
			want: []Rel{
				{
					Consumer: "c1",
					Route:    "r1",
				},
				{
					Consumer: "c1",
					Service:  "s1",
				},
				{
					Consumer: "c2",
					Route:    "r1",
				},
				{
					Consumer: "c2",
					Service:  "s1",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.args.relations.GetCombinations())
		})
	}
}

func TestGroupByControlPlane(t *testing.T) {
	cpWithName := func(name string) *konnectv1alpha2.KonnectGatewayControlPlane {
		return &konnectv1alpha2.KonnectGatewayControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      name,
			},
		}
	}
	type args struct {
		relations ForeignRelations
	}
	tests := []struct {
		name    string
		args    args
		objects []client.Object
		want    ForeignRelationsGroupedByControlPlane
		wantErr bool
	}{
		{
			name: "empty",
			args: args{
				relations: ForeignRelations{},
			},
			want: ForeignRelationsGroupedByControlPlane{},
		},
		{
			name: "single service with control plane ref",
			args: args{
				relations: ForeignRelations{
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "s1",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongServiceSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
			},
			want: ForeignRelationsGroupedByControlPlane{
				types.NamespacedName{Namespace: "default", Name: "cp1"}: {
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "s1",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongServiceSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "multiple services with different control plane refs",
			args: args{
				relations: ForeignRelations{
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "s1",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongServiceSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "s2",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongServiceSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp2",
									},
								},
							},
						},
					},
				},
			},
			want: ForeignRelationsGroupedByControlPlane{
				types.NamespacedName{Namespace: "default", Name: "cp1"}: {
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "s1",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongServiceSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
				types.NamespacedName{Namespace: "default", Name: "cp2"}: {
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "s2",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongServiceSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp2",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "service and route with same control plane ref",
			objects: []client.Object{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "s1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
								Name: "cp1",
							},
						},
					},
				},
			},
			args: args{
				relations: ForeignRelations{
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "s1",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongServiceSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
					Route: []configurationv1alpha1.KongRoute{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "r1",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongRouteSpec{
								ServiceRef: &configurationv1alpha1.ServiceRef{
									Type: configurationv1alpha1.ServiceRefNamespacedRef,
									NamespacedRef: &commonv1alpha1.NameRef{
										Name: "s1",
									},
								},
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
			},
			want: ForeignRelationsGroupedByControlPlane{
				types.NamespacedName{Namespace: "default", Name: "cp1"}: {
					Service: []configurationv1alpha1.KongService{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "s1",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongServiceSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
					Route: []configurationv1alpha1.KongRoute{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "r1",
								Namespace: "default",
							},
							Spec: configurationv1alpha1.KongRouteSpec{
								ServiceRef: &configurationv1alpha1.ServiceRef{
									Type: configurationv1alpha1.ServiceRefNamespacedRef,
									NamespacedRef: &commonv1alpha1.NameRef{
										Name: "s1",
									},
								},
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "consumer with control plane ref",
			args: args{
				relations: ForeignRelations{
					Consumer: []configurationv1.KongConsumer{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "c1",
								Namespace: "default",
							},
							Spec: configurationv1.KongConsumerSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
			},
			want: ForeignRelationsGroupedByControlPlane{
				types.NamespacedName{Namespace: "default", Name: "cp1"}: {
					Consumer: []configurationv1.KongConsumer{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "c1",
								Namespace: "default",
							},
							Spec: configurationv1.KongConsumerSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "consumer group with control plane ref",
			args: args{
				relations: ForeignRelations{
					ConsumerGroup: []configurationv1beta1.KongConsumerGroup{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "cg1",
								Namespace: "default",
							},
							Spec: configurationv1beta1.KongConsumerGroupSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
			},
			want: ForeignRelationsGroupedByControlPlane{
				types.NamespacedName{Namespace: "default", Name: "cp1"}: {
					ConsumerGroup: []configurationv1beta1.KongConsumerGroup{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "cg1",
								Namespace: "default",
							},
							Spec: configurationv1beta1.KongConsumerGroupSpec{
								ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
									Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
										Name: "cp1",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := tt.objects
			objects = append(objects, cpWithName("cp1"), cpWithName("cp2"))
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(objects...).
				Build()
			got, err := tt.args.relations.GroupByControlPlane(t.Context(), cl)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
