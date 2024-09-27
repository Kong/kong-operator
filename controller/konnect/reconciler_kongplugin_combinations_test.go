package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
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
					Consumer: []string{"foo", "bar"},
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
					ConsumerGroup: []string{"foo", "bar"},
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
									NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
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
					Consumer: []string{"c1"},
				},
			},
			want: []Rel{
				{
					Consumer: "c1",
					Service:  "s1",
				},
				// NOTE: https://github.com/Kong/gateway-operator/issues/660
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
					Consumer: []string{"c1", "c2"},
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
				// NOTE: https://github.com/Kong/gateway-operator/issues/660
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
					ConsumerGroup: []string{"cg1", "cg2"},
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
				// NOTE: https://github.com/Kong/gateway-operator/issues/660
				// is related to the following combination not being present.
				// Currently we do not generate combination for Service only
				// when Service **and** ConsumerGroups have the annotation present.
				// {
				// 	Service: "s1",
				// },
			},
		},
		{
			name: "plugins on combination of service,route and consumer",
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
									NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
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
					Consumer: []string{"c1"},
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
			name: "plugins on combination of service,route and consumers",
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
									NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
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
					Consumer: []string{"c1", "c2"},
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
