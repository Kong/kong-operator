package metricsscraper

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr"
	prometheus "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
)

func kongMetricsServer(t *testing.T) *httptest.Server {
	const metricsBody = `` +
		`# HELP kong_upstream_latency_ms Latency added by upstream response for each service/route in Kong` + "\n" +
		`# TYPE kong_upstream_latency_ms histogram` + "\n" +
		`kong_upstream_latency_ms_bucket{service="httproute.default.httproute-echo.0",route="httproute.default.httproute-echo.0.0",le="25"} 550` + "\n" +
		`kong_upstream_latency_ms_bucket{service="httproute.default.httproute-echo.0",route="httproute.default.httproute-echo.0.0",le="50"} 550` + "\n" +
		`kong_upstream_latency_ms_bucket{service="httproute.default.httproute-echo.0",route="httproute.default.httproute-echo.0.0",le="80"} 550` + "\n" +
		`kong_upstream_latency_ms_bucket{service="httproute.default.httproute-echo.0",route="httproute.default.httproute-echo.0.0",le="60000"} 610` + "\n" +
		`kong_upstream_latency_ms_bucket{service="httproute.default.httproute-echo.0",route="httproute.default.httproute-echo.0.0",le="+Inf"} 610` + "\n" +
		`kong_upstream_latency_ms_count{service="httproute.default.httproute-echo.0",route="httproute.default.httproute-echo.0.0"} 610` + "\n" +
		`kong_upstream_latency_ms_sum{service="httproute.default.httproute-echo.0",route="httproute.default.httproute-echo.0.0"} 12232` + "\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(metricsBody)); err != nil {
			t.Logf("failed to write response: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(func() { srv.Close() })
	return srv
}

func TestPrometheusMetricsScraper_Scrape(t *testing.T) {
	tests := []struct {
		name        string
		dataplane   *operatorv1beta1.DataPlane
		expected    func(adminAPIMetricsServerAddress string) Metrics
		expectedErr error
	}{
		{
			name: "scraping from a valid Admin API endpoint works",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-1",
					Namespace: "default",
				},
			},
			expected: func(serverAddr string) Metrics {
				return Metrics{
					metrics: metricsMap{
						adminAPIEndpointURL(serverAddr): {
							"kong_upstream_latency_ms": {
								Name: proto.String("kong_upstream_latency_ms"),
								Help: proto.String("Latency added by upstream response for each service/route in Kong"),
								Type: prometheus.MetricType_HISTOGRAM.Enum(),
								Metric: []*prometheus.Metric{
									{
										Histogram: &prometheus.Histogram{
											SampleCount: proto.Uint64(610),
											SampleSum:   proto.Float64(12232),
											Bucket: []*prometheus.Bucket{
												{
													CumulativeCount: proto.Uint64(550),
													UpperBound:      proto.Float64(25),
												},
												{
													CumulativeCount: proto.Uint64(550),
													UpperBound:      proto.Float64(50),
												},
												{
													CumulativeCount: proto.Uint64(550),
													UpperBound:      proto.Float64(80),
												},
												{
													CumulativeCount: proto.Uint64(610),
													UpperBound:      proto.Float64(60000),
												},
												{
													CumulativeCount: proto.Uint64(610),
													UpperBound:      proto.Float64(math.Inf(1)),
												},
											},
										},
										Label: []*prometheus.LabelPair{
											{
												Name:  proto.String("service"),
												Value: proto.String("httproute.default.httproute-echo.0"),
											},
											{
												Name:  proto.String("route"),
												Value: proto.String("httproute.default.httproute-echo.0.0"),
											},
										},
									},
								},
							},
						},
					},
				}
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adminAPIMetricsServer := kongMetricsServer(t)
			addressProvider := &metricsmocks.MockAdminAPIAddressProvider{
				Addresses: []string{adminAPIMetricsServer.URL},
			}

			httpClient := http.DefaultClient

			scraper := NewPrometheusMetricsScraper(logr.Discard(), tt.dataplane, httpClient, addressProvider)

			metrics, err := scraper.Scrape(t.Context())
			require.NoError(t, err)

			expected := tt.expected(adminAPIMetricsServer.URL)
			require.Equal(t, expected, metrics)
		})
	}
}
