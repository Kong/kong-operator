package metricsscraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/go-logr/logr"
	prometheus "github.com/prometheus/client_model/go"
	prometheusexpfmt "github.com/prometheus/common/expfmt"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/pkg/log"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// MetricsConsumer is an interface for consumers of metrics scraped by a MetricsScraper.
type MetricsConsumer interface {
	Consume(context.Context, Metrics) error
}

// MetricsScraper is an interface for a scraper that scrapes metrics from a DataPlane.
type MetricsScraper interface {
	Scrape(ctx context.Context) (Metrics, error)
	DataPlaneUID() types.UID
}

// adminAPIEndpointURL is a strong type for the URL of an Admin API endpoint for metrics map indexing.
type adminAPIEndpointURL string

// metricName is a strong type for the name of a metric for metrics map indexing.
type metricName string

// metricsMap groups metrics by the Admin API endpoint URL they were scraped from,
// and then by the metric name.
type metricsMap map[adminAPIEndpointURL]map[metricName]*prometheus.MetricFamily

// Metrics represents a collection of metrics scraped from a DataPlane.
type Metrics struct {
	metrics metricsMap
}

// PrometheusMetricsScraper is a MetricsScraper that scrapes Prometheus metrics
// from the Admin API endpoint of provided DataPlane.
type PrometheusMetricsScraper struct {
	logger                  logr.Logger
	httpClient              *http.Client
	dp                      *operatorv1beta1.DataPlane
	adminAPIAddressProvider AdminAPIAddressProvider

	subscribersLock    sync.RWMutex
	metricsSubscribers []MetricsConsumer
}

// NewPrometheusMetricsScraper creates a new PrometheusMetricsScraper that scrapes
// metrics from the Admin API endpoints of the provided DataPlane.
func NewPrometheusMetricsScraper(
	logger logr.Logger, dp *operatorv1beta1.DataPlane, httpClient *http.Client, adminAPIAddrProvider AdminAPIAddressProvider,
) MetricsScraper {
	return &PrometheusMetricsScraper{
		logger:                  logger,
		httpClient:              httpClient,
		dp:                      dp,
		metricsSubscribers:      make([]MetricsConsumer, 0),
		adminAPIAddressProvider: adminAPIAddrProvider,
	}
}

// Scrape scrapes metrics from the Admin API endpoints of the configured DataPlane.
func (p *PrometheusMetricsScraper) Scrape(ctx context.Context) (Metrics, error) {
	// TODO: watch for changes in admin API addresses, do not query every scrape.
	urls, err := p.adminAPIAddressProvider.AdminAddressesForDP(ctx, p.dp)
	if err != nil {
		return Metrics{}, err
	}
	if len(urls) == 0 {
		return Metrics{}, nil
	}

	log.Debug(p.logger, "scraping DataPlane metrics", "DataPlane", p.dp, "urls", urls)

	metrics := Metrics{
		metrics: make(metricsMap),
	}

	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"/metrics", nil)
		if err != nil {
			return Metrics{}, err
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return Metrics{}, fmt.Errorf("failed to scrape metrics from %s: %w", u, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			return Metrics{}, fmt.Errorf("failed to scrape metrics from %s: %s: %s", u, resp.Status, string(b))
		}

		var parser prometheusexpfmt.TextParser
		metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
		if err != nil {
			b, errBody := io.ReadAll(resp.Body)
			if errBody != nil {
				return Metrics{}, fmt.Errorf("failed to parse metrics (failed reading response body: %w) from %s: %s: %s", errBody, u, resp.Status, string(b))
			}
			return Metrics{}, fmt.Errorf("failed to parse metrics from %s: %s: %s", u, resp.Status, string(b))
		}

		adminAPIURL := adminAPIEndpointURL(u)
		m := metrics.metrics[adminAPIURL]
		for name, metricFamily := range metricFamilies {
			if m == nil {
				m = make(map[metricName]*prometheus.MetricFamily)
			}
			m[metricName(name)] = metricFamily
		}
		metrics.metrics[adminAPIURL] = m
	}

	p.subscribersLock.RLock()
	for _, subscriber := range p.metricsSubscribers {
		if err := subscriber.Consume(ctx, metrics); err != nil {
			p.logger.Error(err, "failed to consume metrics", "dataplane", client.ObjectKeyFromObject(p.dp))
		}
	}
	p.subscribersLock.RUnlock()

	return metrics, nil
}

// DataPlaneUID returns the UID of the DataPlane this scraper is scraping metrics for.
func (p *PrometheusMetricsScraper) DataPlaneUID() types.UID {
	if p == nil || p.dp == nil {
		return ""
	}

	return p.dp.UID
}
