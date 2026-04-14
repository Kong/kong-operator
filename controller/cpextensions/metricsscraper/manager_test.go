package metricsscraper

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
)

type pair struct {
	controlplane *gwtypes.ControlPlane
	pipeline     MetricsScrapePipeline
}

func TestMetricsScrapeManagerAdd(t *testing.T) {
	const (
		interval = time.Second
	)

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
			msm := NewManager(logr.Discard(), interval, time.Hour, 10*time.Minute, fakeClient, types.NamespacedName{})
			for _, pipeline := range tt.pairs {
				msm.Add(pipeline.controlplane, pipeline.pipeline)
			}

			require.Equal(t, tt.expectedCpNNToDPUID, msm.cpNNToDpUID)
			require.ElementsMatch(t, tt.expectedScrapersUIDs, lo.Keys(msm.pipelines))
			for uid, scraper := range msm.pipelines {
				require.Equal(t, uid, scraper.DataPlaneUID())
			}
		})
	}
}

func TestMetricsScrapeManager_RemoveForControlPlaneNN(t *testing.T) {
	const interval = time.Second

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
			msm := NewManager(logr.Discard(), interval, time.Hour, 10*time.Minute, fakeClient, types.NamespacedName{})
			for _, pair := range tt.addPairs {
				msm.Add(pair.controlplane, pair.pipeline)
			}

			if tt.removeCpNN != nil {
				msm.RemoveForControlPlaneNN(*tt.removeCpNN)
			}

			require.Equal(t, tt.expectedCpNNToDPUID, msm.cpNNToDpUID)
			require.Equal(t, tt.expectedScrapersUIDs, lo.Keys(msm.pipelines))
			for uid, scraper := range msm.pipelines {
				require.Equal(t, uid, scraper.DataPlaneUID())
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
	addPairsCommon := func() []pair {
		return []pair{
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
		}
	}
	tests := []struct {
		name     string
		addPairs []pair
		keyType  certificate.KeyType
	}{
		{
			name:     "add 2 ControlPlanes",
			addPairs: addPairsCommon(),
			keyType:  certificate.RSA,
		},
		{
			name:     "add 2 ControlPlanes",
			addPairs: addPairsCommon(),
			keyType:  certificate.ECDSA,
		},
	}

	const (
		waitTime     = time.Second
		intervalTime = 100 * time.Microsecond
	)

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%s (CA key type %s)", tc.name, tc.keyType), func(t *testing.T) {
			cert, key := certificate.MustGenerateCertPEMFormat(certificate.WithKeyType(tc.keyType))
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
			fakeClient := fake.NewClientBuilder().WithObjects(caSecret).Build()

			msm := NewManager(logr.Discard(), intervalTime, time.Hour, 10*time.Minute, fakeClient, client.ObjectKeyFromObject(caSecret))
			for _, pair := range tc.addPairs {
				msm.Add(pair.controlplane, pair.pipeline)
			}

			for idx, pipeline := range msm.pipelines {
				mp, ok := pipeline.(metricsPipeline)
				require.True(t, ok)
				require.Zero(t, mp.MetricsScraper.(*mockScraper).CallCount(),
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

func TestMetricsScrapeManager_CertRotation(t *testing.T) {
	const (
		certTTL              = time.Hour
		certExpirationMargin = 10 * time.Minute
	)

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, operatorv1beta1.AddToScheme(scheme))

	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCATrue())
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca-secret",
			Namespace: "kong-system",
		},
		Data: map[string][]byte{
			"ca.crt":  certPEM,
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}
	dp := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dp1",
			Namespace: "ns1",
			UID:       types.UID("dp-uid1"),
		},
	}
	cp := &gwtypes.ControlPlane{
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
	}

	t.Run("initializes certs when nil", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(caSecret, dp).
			Build()
		msm := NewManager(logr.Discard(), time.Second, certTTL, certExpirationMargin, fakeClient, client.ObjectKeyFromObject(caSecret))
		require.Nil(t, msm.certs)

		require.NoError(t, msm.enableMetricsScraperForControlPlanesDataPlane(t.Context(), cp))
		require.NotNil(t, msm.certs)
		require.False(t, msm.certs.ExpirationDate.IsZero(), "expiration date should be set")
		require.WithinDuration(t, time.Now().Add(certTTL), msm.certs.ExpirationDate, certExpirationMargin)
	})

	t.Run("does not rotate certs when far from expiration", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(caSecret, dp).
			Build()
		msm := NewManager(logr.Discard(), time.Second, certTTL, certExpirationMargin, fakeClient, client.ObjectKeyFromObject(caSecret))

		// Pre-populate certs with a far-future expiration.
		msm.certs = &certs{
			ExpirationDate: time.Now().Add(24 * time.Hour),
			Cert:           &x509.Certificate{},
			CA:             &x509.Certificate{},
		}
		originalExpiration := msm.certs.ExpirationDate

		require.NoError(t, msm.enableMetricsScraperForControlPlanesDataPlane(t.Context(), cp))
		require.Equal(t, originalExpiration, msm.certs.ExpirationDate, "certs should not have been rotated")
	})

	t.Run("rotates certs when near expiration", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(caSecret, dp).
			Build()
		msm := NewManager(logr.Discard(), time.Second, certTTL, certExpirationMargin, fakeClient, client.ObjectKeyFromObject(caSecret))

		// Pre-populate certs with an expiration within the margin.
		nearExpiration := time.Now().Add(certExpirationMargin / 2)
		msm.certs = &certs{
			ExpirationDate: nearExpiration,
			Cert:           &x509.Certificate{},
			CA:             &x509.Certificate{},
		}

		require.NoError(t, msm.enableMetricsScraperForControlPlanesDataPlane(t.Context(), cp))
		require.NotEqual(t, nearExpiration, msm.certs.ExpirationDate, "certs should have been rotated")
		require.True(t, msm.certs.ExpirationDate.After(time.Now()), "new cert should expire in the future")
	})

	t.Run("rotates certs when already expired", func(t *testing.T) {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(caSecret, dp).
			Build()
		msm := NewManager(logr.Discard(), time.Second, certTTL, certExpirationMargin, fakeClient, client.ObjectKeyFromObject(caSecret))

		// Pre-populate certs with an expiration in the past.
		expired := time.Now().Add(-time.Hour)
		msm.certs = &certs{
			ExpirationDate: expired,
			Cert:           &x509.Certificate{},
			CA:             &x509.Certificate{},
		}

		require.NoError(t, msm.enableMetricsScraperForControlPlanesDataPlane(t.Context(), cp))
		require.NotEqual(t, expired, msm.certs.ExpirationDate, "certs should have been rotated")
		require.True(t, msm.certs.ExpirationDate.After(time.Now()), "new cert should expire in the future")
	})
}
