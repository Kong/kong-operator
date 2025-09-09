package controlplane_extensions

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/api/gateway-operator/v1alpha1"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

func prometheusPluginForSvc(svc *corev1.Service, cp *gwtypes.ControlPlane, ext *operatorv1alpha1.DataPlaneMetricsExtension) (*configurationv1.KongPlugin, error) {
	var b []byte
	if ext != nil {
		var err error
		pluginConfig := convertDataPlanePluginMetricsExtensionConfigToPrometheusPluginConfig(ext)
		b, err = json.Marshal(pluginConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal Prometheus plugin config for Service %s: %w",
				client.ObjectKeyFromObject(svc), err,
			)
		}
	}

	plugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      prometheusPluginNameForSvc(svc),
			Namespace: svc.Namespace,
			Labels: map[string]string{
				consts.GatewayOperatorManagedByLabel:          "controlplane",
				consts.GatewayOperatorManagedByNameLabel:      cp.GetName(),
				consts.GatewayOperatorManagedByNamespaceLabel: cp.GetNamespace(),
				consts.GatewayOperatorKongPluginTypeLabel:     consts.KongPluginNamePrometheus,
			},
		},
		PluginName: consts.KongPluginNamePrometheus,
		Config: apiextensionsv1.JSON{
			Raw: b,
		},
	}

	return plugin, nil
}

func prometheusPluginNameForSvc(svc *corev1.Service) string {
	return svc.GetName() + "-metrics-prometheus"
}

func convertDataPlanePluginMetricsExtensionConfigToPrometheusPluginConfig(
	ext *operatorv1alpha1.DataPlaneMetricsExtension,
) PrometheusPluginConfig {
	return PrometheusPluginConfig{
		Latency:        ext.Spec.Config.Latency,
		Bandwidth:      ext.Spec.Config.Bandwidth,
		UpstreamHealth: ext.Spec.Config.UpstreamHealth,
		StatusCode:     ext.Spec.Config.StatusCode,
	}
}

// PrometheusPluginConfig holds the configuration for the Prometheus plugin.
//
// Ref: https://docs.konghq.com/hub/kong-inc/prometheus/configuration/.
type PrometheusPluginConfig struct {
	Latency        bool `json:"latency_metrics"`
	Bandwidth      bool `json:"bandwidth_metrics"`
	UpstreamHealth bool `json:"upstream_health_metrics"`
	StatusCode     bool `json:"status_code_metrics"`
}
