package metricsscraper

import (
	"context"
	"crypto/x509"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/test/helpers/certificate"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
)

type pair struct {
	controlplane *gwtypes.ControlPlane
	pipeline     MetricsScrapePipeline
}

func TestMetricsScrapeManagerAdd(t *testing.T) {
	const (
		interval = time.Second
	)
	clusterCAKeyType := secrets.KeyConfig{
		Type: x509.ECDSA,
		Size: 1024,
	}

	tests := []struct {
		name                 string
		pairs                []pair
		expectedCpNNToDPUID  map[types.NamespacedName]types.UID
		expectedScrapersUIDs []types.UID
	}{
		{
			name: "one ControlPlane with one scraper",
			pairs: []pair{
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp1",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp1",
								},
							},
						},
					},
					pipeline: metricsPipeline{
						MetricsScraper: NewPrometheusMetricsScraper(
							logr.Discard(),
							&operatorv1beta1.DataPlane{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "dp1",
									Namespace: "ns1",
									UID:       types.UID("dp-uid1"),
								},
							},
							http.DefaultClient,
							&metricsmocks.MockAdminAPIAddressProvider{},
						),
						MetricsEnricher: &metricsEnricher{},
					},
				},
			},
			expectedCpNNToDPUID: map[types.NamespacedName]types.UID{
				{
					Name:      "cp1",
					Namespace: "ns1",
				}: "dp-uid1",
			},
			expectedScrapersUIDs: []types.UID{
				"dp-uid1",
			},
		},
		{
			name: "one ControlPlane with one scraper which then gets overridden by another scraper for a different DataPlane",
			pairs: []pair{
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp1",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp1",
								},
							},
						},
					},
					pipeline: metricsPipeline{
						MetricsScraper: NewPrometheusMetricsScraper(
							logr.Discard(),
							&operatorv1beta1.DataPlane{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "dp1",
									Namespace: "ns1",
									UID:       types.UID("dp-uid1"),
								},
							},
							http.DefaultClient,
							&metricsmocks.MockAdminAPIAddressProvider{},
						),
						MetricsEnricher: &metricsEnricher{},
					},
				},
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp1",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp1",
								},
							},
						},
					},
					pipeline: metricsPipeline{
						MetricsScraper: NewPrometheusMetricsScraper(
							logr.Discard(),
							&operatorv1beta1.DataPlane{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "dp2",
									Namespace: "ns1",
									UID:       types.UID("dp-uid2"),
								},
							},
							http.DefaultClient,
							&metricsmocks.MockAdminAPIAddressProvider{},
						),
						MetricsEnricher: &metricsEnricher{},
					},
				},
			},
			expectedCpNNToDPUID: map[types.NamespacedName]types.UID{
				{
					Name:      "cp1",
					Namespace: "ns1",
				}: "dp-uid2",
			},
			expectedScrapersUIDs: []types.UID{
				"dp-uid2",
			},
		},
		{
			name: "2 ControlPlanes with 2 scrapers",
			pairs: []pair{
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp1",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp1",
								},
							},
						},
					},
					pipeline: metricsPipeline{
						MetricsScraper: NewPrometheusMetricsScraper(
							logr.Discard(),
							&operatorv1beta1.DataPlane{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "dp1",
									Namespace: "ns1",
									UID:       types.UID("dp-uid1"),
								},
							},
							http.DefaultClient,
							&metricsmocks.MockAdminAPIAddressProvider{},
						),
						MetricsEnricher: &metricsEnricher{},
					},
				},
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp2",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp2",
								},
							},
						},
					},
					pipeline: metricsPipeline{
						MetricsScraper: NewPrometheusMetricsScraper(
							logr.Discard(),
							&operatorv1beta1.DataPlane{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "dp2",
									Namespace: "ns1",
									UID:       types.UID("dp-uid2"),
								},
							},
							http.DefaultClient,
							&metricsmocks.MockAdminAPIAddressProvider{},
						),
						MetricsEnricher: &metricsEnricher{},
					},
				},
			},
			expectedCpNNToDPUID: map[types.NamespacedName]types.UID{
				{
					Name:      "cp1",
					Namespace: "ns1",
				}: "dp-uid1",
				{
					Name:      "cp2",
					Namespace: "ns1",
				}: "dp-uid2",
			},
			expectedScrapersUIDs: []types.UID{
				"dp-uid1",
				"dp-uid2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().Build()
			msm := NewManager(logr.Discard(), interval, fakeClient, types.NamespacedName{}, clusterCAKeyType)
			for _, pipeline := range tt.pairs {
				msm.Add(pipeline.controlplane, pipeline.pipeline)
			}

			assert.Equal(t, tt.expectedCpNNToDPUID, msm.cpNNToDpUID)
			assert.ElementsMatch(t, tt.expectedScrapersUIDs, lo.Keys(msm.pipelines))
			for uid, scraper := range msm.pipelines {
				assert.Equal(t, uid, scraper.DataPlaneUID())
			}
		})
	}
}

func TestMetricsScrapeManager_RemoveForControlPlaneNN(t *testing.T) {
	const interval = time.Second

	clusterCAKeyType := secrets.KeyConfig{
		Type: x509.ECDSA,
		Size: 1024,
	}

	tests := []struct {
		name                 string
		addPairs             []pair
		removeCpNN           *types.NamespacedName
		expectedCpNNToDPUID  map[types.NamespacedName]types.UID
		expectedScrapersUIDs []types.UID
	}{
		{
			name: "add 2 ControlPlanes and then remove 1",
			addPairs: []pair{
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp1",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp1",
								},
							},
						},
					},
					pipeline: metricsPipeline{
						MetricsScraper: NewPrometheusMetricsScraper(
							logr.Discard(),
							&operatorv1beta1.DataPlane{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "dp1",
									Namespace: "ns1",
									UID:       types.UID("dp-uid1"),
								},
							},
							http.DefaultClient,
							&metricsmocks.MockAdminAPIAddressProvider{},
						),
						MetricsEnricher: &metricsEnricher{},
					},
				},
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp2",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp2",
								},
							},
						},
					},
					pipeline: metricsPipeline{
						MetricsScraper: NewPrometheusMetricsScraper(
							logr.Discard(),
							&operatorv1beta1.DataPlane{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "dp2",
									Namespace: "ns1",
									UID:       types.UID("dp-uid2"),
								},
							},
							http.DefaultClient,
							&metricsmocks.MockAdminAPIAddressProvider{},
						),
						MetricsEnricher: &metricsEnricher{},
					},
				},
			},
			removeCpNN: &types.NamespacedName{
				Name:      "cp2",
				Namespace: "ns1",
			},
			expectedCpNNToDPUID: map[types.NamespacedName]types.UID{
				{
					Name:      "cp1",
					Namespace: "ns1",
				}: "dp-uid1",
			},
			expectedScrapersUIDs: []types.UID{
				"dp-uid1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().Build()
			msm := NewManager(logr.Discard(), interval, fakeClient, types.NamespacedName{}, clusterCAKeyType)
			for _, pair := range tt.addPairs {
				msm.Add(pair.controlplane, pair.pipeline)
			}

			if tt.removeCpNN != nil {
				msm.RemoveForControlPlaneNN(*tt.removeCpNN)
			}

			assert.Equal(t, tt.expectedCpNNToDPUID, msm.cpNNToDpUID)
			assert.Equal(t, tt.expectedScrapersUIDs, lo.Keys(msm.pipelines))
			for uid, scraper := range msm.pipelines {
				assert.Equal(t, uid, scraper.DataPlaneUID())
			}
		})
	}
}

type mockScraper struct {
	uid       types.UID
	err       error
	callCount atomic.Int32
}

func (m *mockScraper) DataPlaneUID() types.UID {
	return m.uid
}

func (m *mockScraper) Scrape(_ context.Context) (Metrics, error) {
	m.callCount.Add(1)
	return Metrics{}, m.err
}

func (m *mockScraper) CallCount() int {
	return int(m.callCount.Load())
}

func (m *mockScraper) AddSubscriber(_ MetricsConsumer) {
}

type mockConsumer struct{}

func (mc *mockConsumer) Consume(_ context.Context, _ Metrics) error { return nil }

func TestMetricsScrapeManager_Start(t *testing.T) {
	cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(certificate.ECDSA))
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca-secret",
			Namespace: "kong-system",
		},
		Data: map[string][]byte{
			"ca.crt":  cert,
			"tls.crt": cert,
			"tls.key": key,
		},
	}

	tests := []struct {
		name                 string
		addPairs             []pair
		expectedCpNNToDPUID  map[types.NamespacedName]types.UID
		expectedScrapersUIDs []types.UID
	}{
		{
			name: "add 2 ControlPlanes",
			addPairs: []pair{
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp1",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp1",
								},
							},
						},
					},

					pipeline: metricsPipeline{
						MetricsScraper: &mockScraper{
							uid: "dp-uid1",
						},
						MetricsEnricher: &mockConsumer{},
					},
				},
				{
					controlplane: &gwtypes.ControlPlane{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cp2",
							Namespace: "ns1",
						},
						Spec: gwtypes.ControlPlaneSpec{
							DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
								Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
								Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
									Name: "dp2",
								},
							},
						},
					},
					pipeline: metricsPipeline{
						MetricsScraper: &mockScraper{
							uid: "dp-uid2",
						},
						MetricsEnricher: &mockConsumer{},
					},
				},
			},
		},
	}

	const (
		waitTime     = time.Second
		intervalTime = 100 * time.Microsecond
	)
	clusterCAKeyType := secrets.KeyConfig{
		Type: x509.ECDSA,
		Size: 1024,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithObjects(caSecret).Build()

			msm := NewManager(logr.Discard(), intervalTime, fakeClient, client.ObjectKeyFromObject(caSecret), clusterCAKeyType)
			for _, pair := range tt.addPairs {
				msm.Add(pair.controlplane, pair.pipeline)
			}

			for idx, pipeline := range msm.pipelines {
				mp, ok := pipeline.(metricsPipeline)
				require.True(t, ok)
				assert.Zero(t, mp.MetricsScraper.(*mockScraper).CallCount(),
					"scraper %d should not have been called yet", idx,
				)
			}
			require.NoError(t, msm.Start(t.Context()))

			require.Eventually(t,
				func() bool {
					for _, pipeline := range msm.pipelines {
						mp, ok := pipeline.(metricsPipeline)
						require.True(t, ok)

						if mp.MetricsScraper.(*mockScraper).CallCount() == 0 {
							return false
						}
					}
					return true
				},
				waitTime, intervalTime,
				"all scrapers should have been called at least once",
			)
		})
	}
}
