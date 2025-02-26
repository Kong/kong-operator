module github.com/kong/gateway-operator

go 1.24.0

// 1.2.2 was released on main branch with a breaking change that was not
// intended to be released in 1.2.x:
// https://github.com/Kong/gateway-operator/commit/3876430e09e61edce58bd8464989e33236bd1872
// This retraction is to prevent it from being used and from breaking builds of dependent projects.
retract v1.2.2

require (
	github.com/Kong/sdk-konnect-go v0.2.14
	github.com/Masterminds/semver v1.5.0
	github.com/cloudflare/cfssl v1.6.5
	github.com/go-logr/logr v1.4.2
	github.com/go-logr/zapr v1.3.0
	github.com/goccy/go-json v0.10.5
	github.com/google/go-cmp v0.7.0
	github.com/google/go-containerregistry v0.20.3
	github.com/google/uuid v1.6.0
	github.com/gruntwork-io/terratest v0.48.2
	github.com/kong/kubernetes-configuration v1.2.0-rc.1.0.20250225175646-4662c505b8e7
	github.com/kong/kubernetes-telemetry v0.1.8
	github.com/kong/kubernetes-testing-framework v0.47.2
	github.com/kong/semver/v4 v4.0.1
	github.com/kr/pretty v0.3.1
	github.com/opencontainers/image-spec v1.1.0
	github.com/prometheus/client_golang v1.21.0
	github.com/samber/lo v1.49.1
	github.com/samber/mo v1.13.0
	github.com/sourcegraph/conc v0.3.0
	github.com/stretchr/testify v1.10.0
	github.com/tidwall/pretty v1.2.1
	go.uber.org/zap v1.27.0
	golang.org/x/mod v0.23.0
	k8s.io/api v0.32.2
	k8s.io/apiextensions-apiserver v0.32.2
	k8s.io/apimachinery v0.32.2
	k8s.io/client-go v0.32.2
	k8s.io/kubernetes v1.32.2
	// TODO: Use official release when
	// https://github.com/oras-project/oras-go/pull/850 becomes part of it.
	oras.land/oras-go/v2 v2.5.1-0.20250115103459-a15da41d19be
	sigs.k8s.io/controller-runtime v0.20.2
	sigs.k8s.io/gateway-api v1.2.1
	sigs.k8s.io/kustomize/api v0.19.0
)

require (
	cel.dev/expr v0.20.0 // indirect
	cloud.google.com/go/auth v0.10.2 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.5 // indirect
	cloud.google.com/go/compute/metadata v0.6.0 // indirect
	cloud.google.com/go/container v1.38.1 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/asaskevich/govalidator v0.0.0-20230301143203-a9d515a09cc2 // indirect
	github.com/avast/retry-go/v4 v4.6.0 // indirect
	github.com/aws/aws-sdk-go-v2 v1.32.5 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.6.7 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.28.5 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.17.46 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.20 // indirect
	github.com/aws/aws-sdk-go-v2/feature/s3/manager v1.17.41 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.3.24 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.6.24 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.3.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/acm v1.30.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/autoscaling v1.51.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.44.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.37.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.193.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/ecr v1.36.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/ecs v1.52.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/iam v1.38.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.12.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.4.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.10.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.12.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.18.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/kms v1.37.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/lambda v1.69.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/rds v1.91.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/route53 v1.46.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.69.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.34.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sns v1.33.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sqs v1.37.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssm v1.56.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.24.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.28.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.33.1 // indirect
	github.com/aws/smithy-go v1.22.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/bombsimon/logrusr/v3 v3.1.0 // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.6 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/docker v27.5.0+incompatible // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/emicklei/go-restful/v3 v3.12.1 // indirect
	github.com/ericlagergren/decimal v0.0.0-20240411145413-00de7ca16731 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/gammazero/deque v0.2.0 // indirect
	github.com/gammazero/workerpool v1.1.3 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-errors/errors v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/gonvenience/bunt v1.3.5 // indirect
	github.com/gonvenience/neat v1.3.12 // indirect
	github.com/gonvenience/term v1.0.2 // indirect
	github.com/gonvenience/text v1.0.7 // indirect
	github.com/gonvenience/wrap v1.1.2 // indirect
	github.com/gonvenience/ytbx v1.4.4 // indirect
	github.com/google/cel-go v0.22.1 // indirect
	github.com/google/certificate-transparency-go v1.1.7 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/go-github/v48 v48.2.0 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/s2a-go v0.1.8 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.4 // indirect
	github.com/googleapis/gax-go/v2 v2.14.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.1 // indirect
	github.com/gruntwork-io/go-commons v0.8.0 // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/homeport/dyff v1.6.0 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.7.1 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/jmoiron/sqlx v1.3.5 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kong/go-kong v0.63.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/mattn/go-ciede2000 v0.0.0-20170301095244-782e8c62fec3 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-zglob v0.0.2-0.20190814121620-e3c945676326 // indirect
	github.com/miekg/dns v1.1.62 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/go-ps v1.0.0 // indirect
	github.com/mitchellh/hashstructure v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/pquerna/otp v1.4.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/puzpuzpuz/xsync/v2 v2.5.1 // indirect
	github.com/rogpeppe/go-internal v1.13.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sergi/go-diff v1.3.1 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/cobra v1.8.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/texttheater/golang-levenshtein v1.0.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/urfave/cli v1.22.16 // indirect
	github.com/virtuald/go-ordered-json v0.0.0-20170621173500-b18e6e673d74 // indirect
	github.com/weppos/publicsuffix-go v0.30.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	github.com/zmap/zcrypto v0.0.0-20230310154051-c8b263fd8300 // indirect
	github.com/zmap/zlint/v3 v3.5.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.59.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.59.0 // indirect
	go.opentelemetry.io/otel v1.34.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.34.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.34.0 // indirect
	go.opentelemetry.io/otel/sdk v1.34.0 // indirect
	go.opentelemetry.io/otel/trace v1.34.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/crypto v0.35.0 // indirect
	golang.org/x/exp v0.0.0-20250210185358-939b2ce775ac // indirect
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/tools v0.30.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250212204824-5a70512c5d8b // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250212204824-5a70512c5d8b // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	k8s.io/apiserver v0.32.2 // indirect
	k8s.io/component-helpers v0.0.0 // indirect
	k8s.io/controller-manager v0.0.0 // indirect
	k8s.io/utils v0.0.0-20241210054802-24370beab758
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.31.2 // indirect
)

require (
	golang.org/x/sync v0.11.0 // indirect
	google.golang.org/api v0.206.0 // indirect
	google.golang.org/grpc v1.70.0 // indirect
	sigs.k8s.io/kind v0.24.0 // indirect
	sigs.k8s.io/kustomize/kyaml v0.19.0
)

require (
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/klauspost/compress v1.17.11 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	golang.org/x/oauth2 v0.26.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/term v0.29.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	golang.org/x/time v0.10.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/component-base v0.32.2 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20241212222426-2c72e554b1e7 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.5.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

// The replace directives for `k8s.io/*` are required for making it possible to
// use `k8s.io/kubernetes` as a library.
// They can be updated with `./hack/update-k8sio-gomod-replace.sh` script.
// This is a workaround for https://github.com/Kong/gateway-operator/issues/1384.
replace (
	k8s.io/api => k8s.io/api v0.32.2
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.32.2
	k8s.io/apimachinery => k8s.io/apimachinery v0.32.2
	k8s.io/apiserver => k8s.io/apiserver v0.32.2
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.32.2
	k8s.io/client-go => k8s.io/client-go v0.32.2
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.32.2
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.32.2
	k8s.io/code-generator => k8s.io/code-generator v0.32.2
	k8s.io/component-base => k8s.io/component-base v0.32.2
	k8s.io/component-helpers => k8s.io/component-helpers v0.32.2
	k8s.io/controller-manager => k8s.io/controller-manager v0.32.2
	k8s.io/cri-api => k8s.io/cri-api v0.32.2
	k8s.io/cri-client => k8s.io/cri-client v0.32.2
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.32.2
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.32.2
	k8s.io/endpointslice => k8s.io/endpointslice v0.32.2
	k8s.io/externaljwt => k8s.io/externaljwt v0.32.2
	k8s.io/kms => k8s.io/kms v0.32.2
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.32.2
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.32.2
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.32.2
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.32.2
	k8s.io/kubectl => k8s.io/kubectl v0.32.2
	k8s.io/kubelet => k8s.io/kubelet v0.32.2
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.30.3
	k8s.io/metrics => k8s.io/metrics v0.32.2
	k8s.io/mount-utils => k8s.io/mount-utils v0.32.2
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.32.2
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.32.2
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.32.2
	k8s.io/sample-controller => k8s.io/sample-controller v0.32.2
)
