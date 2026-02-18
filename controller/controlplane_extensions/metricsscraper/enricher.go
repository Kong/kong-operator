package metricsscraper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kong/go-kong/kong"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
)

func init() {
	collectors := []prometheus.Collector{
		KongUpstreamLatencyMsHistogram,
	}

	for _, c := range collectors {
		if err := metrics.Registry.Register(c); err != nil {
			if !errors.As(err, &prometheus.AlreadyRegisteredError{}) {
				panic(err)
			}
		}
	}
}

// MetricsEnricher consumes Metrics and enriches them with Kubernetes metadata.
type MetricsEnricher interface {
	Consume(context.Context, Metrics) error
}

// metricsEnricher enriches the metrics with additional metadata.
type metricsEnricher struct {
	dataplane               *operatorv1beta1.DataPlane
	adminAPIAddressProvider AdminAPIAddressProvider
	httpClient              *http.Client
	cl                      client.Client
	logger                  logr.Logger
}

// NewEnricher creates a new MetricsEnricher.
func NewEnricher(
	logger logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	cl client.Client,
	certs certs,
	adminAPIAddressProvider AdminAPIAddressProvider,
) (metricsEnricher, error) {
	return metricsEnricher{
		dataplane:               dataplane,
		adminAPIAddressProvider: adminAPIAddressProvider,
		httpClient:              httpClientWithCerts(certs),
		cl:                      cl,
		logger:                  logger,
	}, nil
}

const (
	// KongMetricTagK8sName is the tag set on Kong Services in the Admin API
	// configuration to indicate the name of the Kubernetes Service associated
	// with the Kong Service.
	KongMetricTagK8sName = "k8s-name"
	// KongMetricTagK8sNamespace is the tag set on Kong Services in the Admin API
	// configuration to indicate the namespace of the Kubernetes Service associated
	// with the Kong Service.
	KongMetricTagK8sNamespace = "k8s-namespace"
)

// Consume consumes the metrics and enriches them with kubernetes metadata.
func (me metricsEnricher) Consume(ctx context.Context, m Metrics) error {
	// TODO: Potentially, create a watch which will get notifications on new
	// endpoints for a DataPlane.
	addrs, err := me.adminAPIAddressProvider.AdminAddressesForDP(ctx, me.dataplane)
	if err != nil {
		return fmt.Errorf("failed fetching Admin API addresses for DataPlane %s error: %w",
			client.ObjectKeyFromObject(me.dataplane), err,
		)
	}
	if len(addrs) == 0 {
		return nil
	}

	// A DataPlane has homogenous configuration so we just take the first
	// address available and use it to get the Kong services from the configuration.
	kongClient, err := kong.NewClient(&addrs[0], me.httpClient)
	if err != nil {
		return fmt.Errorf("failed creating kong.Client for DataPlane %s error: %w", client.ObjectKeyFromObject(me.dataplane), err)
	}

	services, err := kongClient.Services.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed listing Services for DataPlane %s error: %w", client.ObjectKeyFromObject(me.dataplane), err)
	}

	for dataplaneURL, metricFamily := range m.metrics {
		for name, metric := range metricFamily {
			if name != KongMetricNameKongUpstreamLatencyMs {
				continue
			}

			for _, m := range metric.GetMetric() {
				// Extract the name of the service from the metric labels.
				// This has the name of the service in the Kong configuration.
				serviceLabel, ok := lo.Find(m.GetLabel(),
					func(p *dto.LabelPair) bool {
						return *p.Name == "service"
					},
				)
				if !ok || serviceLabel.Value == nil {
					me.logger.Info("'service' label not found", "metric", name)
					continue
				}

				svc, ok := lo.Find(services, func(s *kong.Service) bool {
					return s.Name != nil && serviceLabel.GetValue() == *s.Name
				})
				if !ok {
					me.logger.Info("service not found in config", "service", *serviceLabel.Value)
					continue
				}

				tagK8sName, ok := extractAndTrimPrefix(svc.Tags, KongMetricTagK8sName)
				if !ok {
					me.logger.Info(KongMetricTagK8sName + " tag not found for service " + *svc.Name)
					continue
				}

				tagK8sNamespace, ok := extractAndTrimPrefix(svc.Tags, KongMetricTagK8sNamespace)
				if !ok {
					me.logger.Info(KongMetricTagK8sNamespace + " tag not found for service " + *svc.Name)
					continue
				}

				// Below labels have to match the ones in HistogramPassthroughMetric.Desc.
				m.Label = []*dto.LabelPair{
					{
						Name:  lo.ToPtr("namespace"),
						Value: lo.ToPtr(tagK8sNamespace),
					},
					{
						Name:  lo.ToPtr("service"),
						Value: lo.ToPtr(tagK8sName),
					},
					{
						Name:  lo.ToPtr("kubernetes_apiversion"),
						Value: lo.ToPtr("v1"),
					},
					{
						Name:  lo.ToPtr("kubernetes_kind"),
						Value: lo.ToPtr("service"),
					},
					{
						Name:  lo.ToPtr("kubernetes_name"),
						Value: lo.ToPtr(tagK8sName),
					},
					{
						Name:  lo.ToPtr("kubernetes_namespace"),
						Value: lo.ToPtr(tagK8sNamespace),
					},
					{
						Name:  lo.ToPtr("dataplane_url"),
						Value: lo.ToPtr(string(dataplaneURL)),
					},
				}

				KongUpstreamLatencyMsHistogram.Observe(m, dataplaneURL)
			}
		}
	}

	// NOTE: to consider
	// Now that we've observed the metrics, we can remove the time series about
	// dataplanes instances that do not exist anymore.
	// KongUpstreamLatencyHistogram.Prune(addrs)

	return nil
}

// extractAndTrimPrefix looks for a tag with the given prefix and returns the
// value with the prefix + ":" trimmed.
func extractAndTrimPrefix(tags []*string, prefix string) (string, bool) {
	raw, ok := lo.Find(tags, func(tag *string) bool {
		return tag != nil && strings.HasPrefix(*tag, prefix+":")
	})
	if !ok {
		return "", false
	}
	return strings.TrimPrefix(*raw, prefix+":"), true
}
