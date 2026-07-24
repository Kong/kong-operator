module github.com/kong/kong-operator/v2

go 1.26.5

require (
	cloud.google.com/go/container v1.53.0
	dario.cat/mergo v1.0.2
	github.com/Kong/sdk-konnect-go v0.46.0
	github.com/avast/retry-go/v5 v5.0.0
	github.com/blang/semver/v4 v4.0.0
	github.com/cert-manager/cert-manager v1.21.0
	github.com/cloudflare/cfssl v1.6.5
	github.com/cnf/structhash v0.0.0-20250313080605-df4c6cc74a9a
	github.com/docker/go-connections v0.7.0
	github.com/dominikbraun/graph v0.23.0
	github.com/evanphx/json-patch/v5 v5.9.11
	github.com/go-logr/logr v1.4.4
	github.com/go-logr/zapr v1.3.0
	github.com/goccy/go-json v0.10.6
	github.com/gohugoio/hashstructure v0.6.0
	github.com/google/go-cmp v0.7.0
	github.com/google/go-containerregistry v0.21.7
	github.com/google/pprof v0.0.0-20260709232956-b9395ee17fa0
	github.com/google/uuid v1.6.0
	github.com/gruntwork-io/terratest v1.0.1
	github.com/hashicorp/go-cleanhttp v0.5.2
	github.com/hashicorp/go-retryablehttp v0.7.8
	github.com/jpillora/backoff v1.0.0
	github.com/kong/go-database-reconciler v1.41.1
	github.com/kong/go-kong v0.77.0
	github.com/kong/kubernetes-telemetry v0.1.14
	github.com/kong/kubernetes-testing-framework v0.51.0
	github.com/kong/semver/v4 v4.0.1
	github.com/kr/pretty v0.3.1
	github.com/mitchellh/mapstructure v1.5.0
	github.com/moby/moby/api v1.55.0
	github.com/moul/pb v0.0.0-20220425114252-bca18df4138c
	github.com/opencontainers/image-spec v1.1.1
	github.com/prometheus/client_golang v1.24.0
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/common v0.70.1
	github.com/samber/lo v1.53.0
	github.com/samber/mo v1.17.0
	github.com/stretchr/testify v1.11.1
	github.com/testcontainers/testcontainers-go v0.43.0
	github.com/testcontainers/testcontainers-go/modules/postgres v0.43.0
	github.com/tidwall/gjson v1.19.0
	github.com/tonglil/buflogr v1.1.1
	go.uber.org/goleak v1.3.0
	go.uber.org/zap v1.28.0
	golang.org/x/mod v0.38.0
	golang.org/x/sync v0.22.0
	google.golang.org/api v0.290.0
	google.golang.org/grpc v1.82.1
	k8s.io/api v0.36.3
	k8s.io/apiextensions-apiserver v0.36.3
	k8s.io/apimachinery v0.36.3
	k8s.io/cli-runtime v0.36.3
	k8s.io/client-go v0.36.3
	k8s.io/kube-openapi v0.0.0-20260721132016-d427ff9ee9ad
	k8s.io/kubectl v0.36.3
	k8s.io/kubernetes v1.36.3
	oras.land/oras-go/v2 v2.6.2
	sigs.k8s.io/controller-runtime v0.24.1
	sigs.k8s.io/controller-tools v0.21.0
	sigs.k8s.io/e2e-framework v0.7.0
	sigs.k8s.io/gateway-api v1.6.1
	sigs.k8s.io/gateway-api/conformance v1.6.1
	sigs.k8s.io/structured-merge-diff/v6 v6.4.2
	sigs.k8s.io/yaml v1.6.0
)

require (
	cel.dev/expr v0.25.1 // indirect
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	filippo.io/edwards25519 v1.1.1 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/Kong/go-diff v1.2.2 // indirect
	github.com/Kong/gojsondiff v1.3.2 // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/adrg/strutil v0.3.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/aws/aws-sdk-go-v2 v1.42.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.9 // indirect
	github.com/aws/aws-sdk-go-v2/config v1.32.25 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.24 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.29 // indirect
	github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager v0.1.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.29 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.30 // indirect
	github.com/aws/aws-sdk-go-v2/service/acm v1.38.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/autoscaling v1.66.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.69.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.57.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.297.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ecr v1.57.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/ecrpublic v1.39.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/ecs v1.78.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/iam v1.53.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.14 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.11.22 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.29 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.22 // indirect
	github.com/aws/aws-sdk-go-v2/service/kms v1.50.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/lambda v1.89.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/rds v1.118.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/route53 v1.63.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/s3 v1.99.1 // indirect
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.41.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.2.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/sns v1.39.16 // indirect
	github.com/aws/aws-sdk-go-v2/service/sqs v1.42.26 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssm v1.68.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.31.3 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.36.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.43.3 // indirect
	github.com/aws/smithy-go v1.27.3 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bombsimon/logrusr/v3 v3.1.0 // indirect
	github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/chai2010/gettext-go v1.0.2 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/ettle/strcase v0.2.0 // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/fatih/camelcase v1.0.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.10.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.1 // indirect
	github.com/gammazero/deque v1.2.1 // indirect
	github.com/gammazero/workerpool v1.2.1 // indirect
	github.com/go-errors/errors v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-openapi/jsonpointer v1.0.0 // indirect
	github.com/go-openapi/jsonreference v1.0.0 // indirect
	github.com/go-openapi/swag v0.27.1 // indirect
	github.com/go-openapi/swag/cmdutils v0.27.1 // indirect
	github.com/go-openapi/swag/conv v0.27.1 // indirect
	github.com/go-openapi/swag/fileutils v0.27.1 // indirect
	github.com/go-openapi/swag/jsonutils v0.27.1 // indirect
	github.com/go-openapi/swag/loading v0.27.1 // indirect
	github.com/go-openapi/swag/mangling v0.27.1 // indirect
	github.com/go-openapi/swag/netutils v0.27.1 // indirect
	github.com/go-openapi/swag/pools v0.27.1 // indirect
	github.com/go-openapi/swag/stringutils v0.27.1 // indirect
	github.com/go-openapi/swag/typeutils v0.27.1 // indirect
	github.com/go-openapi/swag/yamlutils v0.27.1 // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/gobuffalo/flect v1.0.3 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/gonvenience/bunt v1.3.5 // indirect
	github.com/gonvenience/neat v1.3.12 // indirect
	github.com/gonvenience/term v1.0.2 // indirect
	github.com/gonvenience/text v1.0.7 // indirect
	github.com/gonvenience/wrap v1.1.2 // indirect
	github.com/gonvenience/ytbx v1.4.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/cel-go v0.26.0 // indirect
	github.com/google/certificate-transparency-go v1.3.1 // indirect
	github.com/google/gnostic-models v0.7.1 // indirect
	github.com/google/go-github/v48 v48.2.0 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.18 // indirect
	github.com/googleapis/gax-go/v2 v2.23.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.27.7 // indirect
	github.com/gruntwork-io/go-commons v0.8.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-memdb v1.3.5 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/golang-lru v1.0.2 // indirect
	github.com/hexops/gotextdiff v1.0.3 // indirect
	github.com/homeport/dyff v1.6.0 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/itchyny/gojq v0.12.17 // indirect
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.9.2 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jmoiron/sqlx v1.3.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.19.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/lucasb-eyer/go-colorful v1.4.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20230326075908-cb1d2100619a // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mattn/go-ciede2000 v0.0.0-20170301095244-782e8c62fec3 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-zglob v0.0.2-0.20190814121620-e3c945676326 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/mitchellh/go-ps v1.0.0 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/mitchellh/hashstructure v1.1.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.2.0 // indirect
	github.com/moby/moby/client v0.4.1 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/spdystream v0.5.1 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pelletier/go-toml v1.9.5 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/pquerna/otp v1.4.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/puzpuzpuz/xsync/v2 v2.5.1 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sergi/go-diff v1.4.0 // indirect
	github.com/sethvargo/go-password v0.3.1 // indirect
	github.com/shirou/gopsutil/v3 v3.24.5 // indirect
	github.com/shirou/gopsutil/v4 v4.26.5 // indirect
	github.com/shoenig/go-m1cpu v0.1.6 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/spyzhov/ajson v0.8.0 // indirect
	github.com/ssgelm/cookiejarparser v1.0.1 // indirect
	github.com/stoewer/go-strcase v1.3.1 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/texttheater/golang-levenshtein v1.0.1 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/urfave/cli v1.22.16 // indirect
	github.com/virtuald/go-ordered-json v0.0.0-20170621173500-b18e6e673d74 // indirect
	github.com/weppos/publicsuffix-go v0.30.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	github.com/yudai/golcs v0.0.0-20170316035057-ecda9a501e82 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zmap/zcrypto v0.0.0-20230310154051-c8b263fd8300 // indirect
	github.com/zmap/zlint/v3 v3.5.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.40.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.40.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	go4.org/netipx v0.0.0-20231129151722-fdeea329fbba // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/exp v0.0.0-20260312153236-7ab1446f8b90 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.47.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.5.0 // indirect
	google.golang.org/genproto v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260630182238-925bb5da69e7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.36.3 // indirect
	k8s.io/component-base v0.36.3 // indirect
	k8s.io/component-helpers v0.36.3 // indirect
	k8s.io/controller-manager v0.0.0 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/streaming v0.36.3 // indirect
	k8s.io/utils v0.0.0-20260626114624-be93311217bd // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.34.0 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/kind v0.32.0 // indirect
	sigs.k8s.io/kustomize/api v0.21.1 // indirect
	sigs.k8s.io/kustomize/kyaml v0.21.1 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
)

// The replace directives for `k8s.io/*` are required for making it possible to
// use `k8s.io/kubernetes` as a library.
// They can be updated with `./hack/update-k8sio-gomod-replace.sh` script.
// This is a workaround for https://github.com/kong/kong-operator/issues/1384.
replace (
	k8s.io/api => k8s.io/api v0.36.3
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.36.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.36.3
	k8s.io/apiserver => k8s.io/apiserver v0.36.3
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.36.3
	k8s.io/client-go => k8s.io/client-go v0.36.3
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.36.3
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.36.3
	k8s.io/code-generator => k8s.io/code-generator v0.36.3
	k8s.io/component-base => k8s.io/component-base v0.36.3
	k8s.io/component-helpers => k8s.io/component-helpers v0.36.3
	k8s.io/controller-manager => k8s.io/controller-manager v0.36.3
	k8s.io/cri-api => k8s.io/cri-api v0.36.3
	k8s.io/cri-client => k8s.io/cri-client v0.36.3
	k8s.io/cri-streaming => k8s.io/cri-streaming v0.36.3
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.36.3
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.36.3
	k8s.io/endpointslice => k8s.io/endpointslice v0.36.3
	k8s.io/externaljwt => k8s.io/externaljwt v0.36.3
	k8s.io/kms => k8s.io/kms v0.36.3
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.36.3
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.36.3
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.36.3
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.36.3
	k8s.io/kubectl => k8s.io/kubectl v0.36.3
	k8s.io/kubelet => k8s.io/kubelet v0.36.3
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.30.14
	k8s.io/metrics => k8s.io/metrics v0.36.3
	k8s.io/mount-utils => k8s.io/mount-utils v0.36.3
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.36.3
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.36.3
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.36.3
	k8s.io/sample-controller => k8s.io/sample-controller v0.36.3
	k8s.io/streaming => k8s.io/streaming v0.36.3
)
