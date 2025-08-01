package kongstate

import (
	"encoding/json"
	"strings"

	"github.com/kong/go-kong/kong"
	corev1 "k8s.io/api/core/v1"
)



// prettyPrintServiceList makes a clean printable list of Kubernetes
// services for the purpose of logging (errors, info, etc.).
func prettyPrintServiceList(services []*corev1.Service) string {
	serviceList := make([]string, 0, len(services))
	for _, svc := range services {
		serviceList = append(serviceList, svc.Namespace+"/"+svc.Name)
	}
	return strings.Join(serviceList, ", ")
}

// RawConfigToConfiguration decodes raw JSON to the format of Kong configuration.
// it is run after all patches applied to the initial config.
func RawConfigToConfiguration(raw []byte) (kong.Configuration, error) {
	if len(raw) == 0 {
		return kong.Configuration{}, nil
	}
	var kongConfig kong.Configuration
	err := json.Unmarshal(raw, &kongConfig)
	if err != nil {
		return kong.Configuration{}, err
	}
	return kongConfig, nil
}
