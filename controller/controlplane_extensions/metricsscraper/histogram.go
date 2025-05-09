package metricsscraper

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const (
	// KongMetricNameKongUpstreamLatencyMs is the name of the kong_upstream_latency_ms metric.
	KongMetricNameKongUpstreamLatencyMs = "kong_upstream_latency_ms"
)

// HistogramCollector is a prometheus.Collector that collects histograms.
type HistogramCollector struct {
	Name    string
	Help    string
	lock    sync.RWMutex
	Metrics map[adminAPIEndpointURL]prometheus.Metric
}

var _ prometheus.Collector = &HistogramCollector{}

// KongUpstreamLatencyMsHistogram is a prometheus.Collector that collects
// kong_upstream_latency_ms histograms.
var KongUpstreamLatencyMsHistogram = &HistogramCollector{
	Name:    KongMetricNameKongUpstreamLatencyMs,
	Help:    "Provides kong_upstream_latency_ms histogram enriched with dataplane metadata",
	Metrics: make(map[adminAPIEndpointURL]prometheus.Metric),
}

// Collect implements prometheus.Collector.
func (m *HistogramCollector) Collect(ch chan<- prometheus.Metric) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	for _, metric := range m.Metrics {
		ch <- metric
	}
}

// Describe implements prometheus.Collector.
func (m *HistogramCollector) Describe(ch chan<- *prometheus.Desc) {
	m.lock.RLock()
	defer m.lock.RUnlock()
	for _, metric := range m.Metrics {
		ch <- metric.Desc()
	}
}

// Observe observes a metric for a given dataplaneURL.
func (m *HistogramCollector) Observe(metric *dto.Metric, dataplaneURL adminAPIEndpointURL) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.Metrics[dataplaneURL] = &HistogramPassthroughMetric{
		Name:   m.Name,
		Help:   m.Help,
		Metric: metric,
	}
}

// HistogramPassthroughMetric is a prometheus.Metric that passes through a dto.Metric.
// It allows observing whole histograms and not observing individual data points
// like it's done with prometheus.HistogramVec.
type HistogramPassthroughMetric struct {
	Name   string
	Help   string
	Metric *dto.Metric
}

var _ prometheus.Metric = &HistogramPassthroughMetric{}

// Desc implements prometheus.Metric.
func (m *HistogramPassthroughMetric) Desc() *prometheus.Desc {
	return prometheus.NewDesc(
		m.Name,
		m.Help,
		[]string{
			"namespace",
			"service",
			"kubernetes_apiversion",
			"kubernetes_kind",
			"kubernetes_name",
			"kubernetes_namespace",
			"dataplane_url",
		},
		nil,
	)
}

// Write implements prometheus.Write.
// Passed parameter dm is an output for metrics (target of write).
func (m *HistogramPassthroughMetric) Write(dm *dto.Metric) error {
	dm.Histogram = m.Metric.Histogram
	dm.Label = m.Metric.Label
	dm.TimestampMs = m.Metric.TimestampMs
	dm.Untyped = m.Metric.Untyped

	return nil
}
