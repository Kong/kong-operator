package metricsscraper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	prometheus "github.com/prometheus/client_model/go"
	prometheusexpfmt "github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	adminapidiscovery "github.com/kong/kong-operator/v2/internal/adminapi"
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

// dataplaneLabelURL returns the dataplane_url metric label value for an endpoint.
// The value keeps the pod DNS name form that earlier releases used, for example
// https://10-0-0-1.dataplane-admin-kong-abc.default.svc:8443, so that existing
// dashboards and alerts keep working.
func dataplaneLabelURL(endpoint adminapidiscovery.DiscoveredAdminAPI) adminAPIEndpointURL {
	u, err := url.Parse(endpoint.Address)
	if err != nil || u.Port() == "" {
		return adminAPIEndpointURL(endpoint.Address)
	}
	// The first DNS label of the TLS server name is a placeholder ("pod"). The
	// wildcard certificate matches any value, and earlier releases used the
	// dashed pod IP there.
	host := strings.ReplaceAll(u.Hostname(), ".", "-") + strings.TrimPrefix(endpoint.TLSServerName, "pod")
	return adminAPIEndpointURL(fmt.Sprintf("%s://%s:%s", u.Scheme, host, u.Port()))
}

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
	endpointClients         endpointHTTPClients
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
	endpoints, err := p.adminAPIAddressProvider.AdminAddressesForDP(ctx, p.dp)
	if err != nil {
		return Metrics{}, err
	}
	if len(endpoints) == 0 {
		return Metrics{}, nil
	}

	log.Debug(p.logger, "scraping DataPlane metrics", "DataPlane", p.dp, "endpoints", endpoints)

	metrics := Metrics{
		metrics: make(metricsMap),
	}

	for _, endpoint := range endpoints {
		httpClient, err := p.endpointClients.ForEndpoint(p.httpClient, endpoint.TLSServerName)
		if err != nil {
			return Metrics{}, err
		}
		metricFamilies, err := p.scrapeURL(ctx, httpClient, endpoint.Address)
		if err != nil {
			return Metrics{}, err
		}

		adminAPIURL := dataplaneLabelURL(endpoint)
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

func (p *PrometheusMetricsScraper) scrapeURL(
	ctx context.Context,
	httpClient *http.Client,
	u string,
) (map[string]*prometheus.MetricFamily, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"/metrics", nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to scrape metrics from %s: %w", u, err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read metrics response body from %s: %w", u, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to scrape metrics from %s: %s: %s", u, resp.Status, string(b))
	}

	parser := prometheusexpfmt.NewTextParser(model.LegacyValidation)
	metricFamilies, err := parser.TextToMetricFamilies(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("failed to parse metrics from %s: %s: %s", u, resp.Status, string(b))
	}

	return metricFamilies, nil
}

// DataPlaneUID returns the UID of the DataPlane this scraper is scraping metrics for.
func (p *PrometheusMetricsScraper) DataPlaneUID() types.UID {
	if p == nil || p.dp == nil {
		return ""
	}

	return p.dp.UID
}
