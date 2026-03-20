module github.com/kong/kong-operator/v2/crd-from-oas

go 1.26.1

require (
	github.com/caarlos0/env/v11 v11.3.1
	github.com/getkin/kin-openapi v0.134.0
	github.com/kong/kong-operator/v2 v2.0.0-00000000000000-000000000000
	github.com/stretchr/testify v1.11.1
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/apimachinery v0.35.2
	k8s.io/client-go v0.35.2
)

require (
	github.com/Kong/sdk-konnect-go v0.27.1 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-openapi/jsonpointer v0.22.1 // indirect
	github.com/go-openapi/swag/jsonname v0.25.1 // indirect
	github.com/itchyny/gojq v0.12.17 // indirect
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.9.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/oasdiff/yaml v0.0.0-20260313112342-a3ea61cb4d4c // indirect
	github.com/oasdiff/yaml3 v0.0.0-20260224194419-61cd415a242b // indirect
	github.com/onsi/ginkgo/v2 v2.28.0 // indirect
	github.com/onsi/gomega v1.39.1 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/woodsbury/decimal128 v1.3.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/api v0.35.2 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250910181357-589584f1c912 // indirect
	k8s.io/utils v0.0.0-20260108192941-914a6e750570 // indirect
	sigs.k8s.io/controller-runtime v0.23.3 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.2 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace (
	github.com/kong/kong-operator/v2 => ../
	k8s.io/api => k8s.io/api v0.35.0
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.35.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.35.0
	k8s.io/apiserver => k8s.io/apiserver v0.35.0
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.35.0
	k8s.io/client-go => k8s.io/client-go v0.35.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.35.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.35.0
	k8s.io/code-generator => k8s.io/code-generator v0.35.0
	k8s.io/component-base => k8s.io/component-base v0.35.0
	k8s.io/component-helpers => k8s.io/component-helpers v0.35.0
	k8s.io/controller-manager => k8s.io/controller-manager v0.35.0
	k8s.io/cri-api => k8s.io/cri-api v0.35.0
	k8s.io/cri-client => k8s.io/cri-client v0.35.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.35.0
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.35.0
	k8s.io/endpointslice => k8s.io/endpointslice v0.35.0
	k8s.io/externaljwt => k8s.io/externaljwt v0.35.0
	k8s.io/kms => k8s.io/kms v0.35.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.35.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.35.0
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.35.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.35.0
	k8s.io/kubectl => k8s.io/kubectl v0.35.0
	k8s.io/kubelet => k8s.io/kubelet v0.35.0
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.30.3
	k8s.io/metrics => k8s.io/metrics v0.35.0
	k8s.io/mount-utils => k8s.io/mount-utils v0.35.0
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.35.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.35.0
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.35.0
	k8s.io/sample-controller => k8s.io/sample-controller v0.35.0
)
