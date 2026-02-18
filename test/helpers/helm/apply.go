package helm

import (
	"path"
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"
	terratestlog "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"github.com/kong/kong-operator/v2/test/helpers/apply"
)

// ApplyTemplate applies templated resources to the cluster using the given rest.Config.
func ApplyTemplate(t *testing.T, cfg *rest.Config, chartPath string, templates []string) {
	t.Helper()

	helmArgs := []string{
		"--api-versions",
		"admissionregistration.k8s.io/v1/ValidatingAdmissionPolicy",
		"--api-versions",
		"admissionregistration.k8s.io/v1/ValidatingAdmissionPolicyBinding",
	}

	data := RenderTemplate(t, chartPath, templates, helmArgs...)
	res, err := apply.Apply(t.Context(), cfg, []byte(data))
	require.NoError(t, err)
	for _, r := range res {
		t.Logf("Result: %s", r)
	}
}

// RenderTemplate renders the selected templates in the chart and returns the result as a string.
func RenderTemplate(t *testing.T, chartPath string, templates []string, helmArgs ...string) string {
	t.Helper()
	releaseName := "ko"
	valuesFile := path.Join(chartPath, "values.yaml")

	// Discard terratest stdout logging
	terratestlog.Default = terratestlog.Discard

	res := helm.RenderTemplate(t, &helm.Options{
		ValuesFiles: []string{valuesFile},
	}, chartPath, releaseName, templates, helmArgs...)

	return res
}
