package telemetry

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/kong/kubernetes-telemetry/pkg/forwarders"
	"github.com/kong/kubernetes-telemetry/pkg/provider"
	"github.com/kong/kubernetes-telemetry/pkg/serializers"
	"github.com/kong/kubernetes-telemetry/pkg/telemetry"
	"github.com/kong/kubernetes-telemetry/pkg/types"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	mgrmetadata "github.com/kong/kong-operator/v2/modules/manager/metadata"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

const (
	splunkEndpoint  = "kong-hf.konghq.com:61833"
	telemetryPeriod = time.Hour

	SignalStart = "gateway-operator-start"
	SignalPing  = "gateway-operator-ping"
)

type Payload = types.ProviderReport

// Config holds the configuration that is sent to telemetry manager.
type Config struct {
	DataPlaneControllerEnabled          bool
	DataPlaneBlueGreenControllerEnabled bool
	ControlPlaneControllerEnabled       bool
	GatewayControllerEnabled            bool
	KonnectControllerEnabled            bool
	AIGatewayControllerEnabled          bool
	KongPluginInstallationEnabled       bool
}

// CreateManager creates telemetry manager using the provided rest.Config.
func CreateManager(signal string, restConfig *rest.Config, log logr.Logger, meta mgrmetadata.Info, cfg Config) (telemetry.Manager, error) {
	k, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create client-go kubernetes client: %w", err)
	}

	scheme := scheme.Get()
	cl, err := client.New(restConfig, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create controller-runtime's client: %w", err)
	}

	metadataClient, err := metadata.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create metadata client: %w", err)
	}

	m, err := createManager(
		types.Signal(SignalPing),
		k,
		cl,
		metadataClient,
		meta,
		cfg,
		log,
		telemetry.OptManagerPeriod(telemetryPeriod),
	)
	if err != nil {
		return nil, err
	}

	tf, err := forwarders.NewTLSForwarder(splunkEndpoint, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry TLSForwarder: %w", err)
	}
	serializer := serializers.NewSemicolonDelimited()
	consumer := telemetry.NewConsumer(serializer, tf)
	err = m.AddConsumer(consumer)
	if err != nil {
		return nil, fmt.Errorf("failed to add telemetry consumer to manager: %w", err)
	}

	return m, nil
}

// createManager creates a telemetry manager with given kubernetes clientset, dynamic client and consumer.
//
//	It was separated to allow testing with mocked dependencies.
func createManager(
	signal types.Signal,
	k kubernetes.Interface,
	cl client.Client,
	metadataClient metadata.Interface,
	meta mgrmetadata.Info,
	cfg Config,
	log logr.Logger,
	opts ...telemetry.OptManager,
) (telemetry.Manager, error) {
	o := append([]telemetry.OptManager{telemetry.OptManagerLogger(log)}, opts...)
	m, err := telemetry.NewManager(signal, o...)
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry manager: %w", err)
	}

	// Add identify cluster workflow
	{
		w, err := telemetry.NewIdentifyPlatformWorkflow(k)
		if err != nil {
			return nil, fmt.Errorf("failed to create identify platform workflow: %w", err)
		}
		m.AddWorkflow(w)
	}
	// Add cluster state workflow
	{
		checker := k8sutils.CRDChecker{Client: cl}

		cpExists, err := checker.CRDExists(gwtypes.ControlPlaneGVR())
		if err != nil {
			log.Info("failed to check if controlplane CRD exists", "error", err)
		}
		aiGatewayExists, err := checker.CRDExists(operatorv1alpha1.AIGatewayGVR())
		if err != nil {
			log.Info("failed to check if aigateway CRD exists", "error", err)
		}
		dpExists, err := checker.CRDExists(operatorv1beta1.DataPlaneGVR())
		if err != nil {
			log.Info("failed to check if dataplane CRD exists", "error", err)
		}

		w, err := telemetry.NewClusterStateWorkflow(metadataClient, cl.RESTMapper())
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster state workflow: %w", err)
		}

		if dpExists {
			// Add dataplane count provider to monitor number of dataplanes in the cluster.
			p, err := NewDataPlaneCountProvider(metadataClient, cl.RESTMapper())
			if err != nil {
				log.Info("failed to create dataplane count provider", "error", err)
			} else {
				w.AddProvider(p)
			}
		}

		if cpExists {
			// Add controlplane count provider to monitor number of controlplanes in the cluster.
			p, err := NewControlPlaneCountProvider(metadataClient, cl.RESTMapper())
			if err != nil {
				log.Info("failed to create controlplane count provider", "error", err)
			} else {
				w.AddProvider(p)
			}
		}

		// AIGateway is optional so check if it exists before enabling the count provider.
		if aiGatewayExists {
			// Add aigateway count provider to monitor number of aigateways in the cluster.
			p, err := NewAIgatewayCountProvider(metadataClient, cl.RESTMapper())
			if err != nil {
				log.Info("failed to create aigateway count provider", "error", err)
			} else {
				w.AddProvider(p)
			}
		}

		if dpExists {
			// Add dataplane count not from gateway.
			p, err := NewStandaloneDataPlaneCountProvider(cl)
			if err != nil {
				log.Info("failed to create standalone dataplane count provider", "error", err)
			} else {
				w.AddProvider(p)
			}
		}

		if cpExists {
			// Add controlplane count not from gateway.
			p, err := NewStandaloneControlPlaneCountProvider(cl)
			if err != nil {
				log.Info("failed to create standalone controlplane count provider", "error", err)
			} else {
				w.AddProvider(p)
			}
		}

		if dpExists {
			// Add dataplane requested replicas count provider to monitor number of requested replicas for dataplanes.
			p, err := NewDataPlaneRequestedReplicasCountProvider(cl)
			if err != nil {
				log.Info("failed to create dataplane requested replicas count provider", "error", err)
			} else {
				w.AddProvider(p)
			}
		}

		if cfg.GatewayControllerEnabled {
			// Add gateway counter provider to monitor number of reconciled gateways, including Konnect hybrid gateways.
			p := &gatewayCountProvider{
				konnectEnabled: cfg.KonnectControllerEnabled,
				cl:             cl,
			}
			w.AddProvider(p)
		}

		if cfg.KonnectControllerEnabled {
			{
				group, version := configurationv1.GroupVersion.Group, configurationv1.GroupVersion.Version
				AddObjectCountProviderOrLog[configurationv1.KongConsumer](w, metadataClient, cl.RESTMapper(), log, group, version)
			}
			{
				group, version := configurationv1beta1.GroupVersion.Group, configurationv1beta1.GroupVersion.Version
				AddObjectCountProviderOrLog[configurationv1beta1.KongConsumerGroup](w, metadataClient, cl.RESTMapper(), log, group, version)
			}
			{
				group, version := configurationv1alpha1.GroupVersion.Group, configurationv1alpha1.GroupVersion.Version
				AddObjectCountProviderOrLog[configurationv1alpha1.KongRoute](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongService](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongCredentialACL](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongCredentialJWT](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongCredentialHMAC](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongCredentialAPIKey](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongCredentialBasicAuth](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongSNI](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongVault](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongCertificate](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongCACertificate](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongDataPlaneClientCertificate](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongKey](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongKeySet](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongPluginBinding](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongTarget](w, metadataClient, cl.RESTMapper(), log, group, version)
				AddObjectCountProviderOrLog[configurationv1alpha1.KongUpstream](w, metadataClient, cl.RESTMapper(), log, group, version)
			}
			{
				group, version := konnectv1alpha1.GroupVersion.Group, konnectv1alpha1.GroupVersion.Version
				AddObjectCountProviderOrLog[konnectv1alpha2.KonnectGatewayControlPlane](w, metadataClient, cl.RESTMapper(), log, group, version)
			}
		}

		m.AddWorkflow(w)
	}
	// Add state workflow
	{
		w, err := telemetry.NewStateWorkflow()
		if err != nil {
			return nil, fmt.Errorf("failed to create state workflow: %w", err)
		} else {
			payload := Payload{
				"v":                                         meta.Release,
				"controller_dataplane_enabled":              cfg.DataPlaneControllerEnabled,
				"controller_dataplane_bg_enabled":           cfg.DataPlaneBlueGreenControllerEnabled,
				"controller_controlplane_enabled":           cfg.ControlPlaneControllerEnabled,
				"controller_gateway_enabled":                cfg.GatewayControllerEnabled,
				"controller_konnect_enabled":                cfg.KonnectControllerEnabled,
				"controller_aigateway_enabled":              cfg.AIGatewayControllerEnabled,
				"controller_kongplugininstallation_enabled": cfg.KongPluginInstallationEnabled,
			}

			p, err := provider.NewFixedValueProvider("payload", payload)
			if err != nil {
				log.Info("failed to create fixed payload provider", "error", err)
			} else {
				w.AddProvider(p)
				m.AddWorkflow(w)
			}
		}
	}

	return m, nil
}

// AddObjectCountProviderOrLog adds a provider for counting objects of the specified
// type to the workflow.
// If this fails to create the provider, it logs the error on Info level (as this
// is not a critical operation).
func AddObjectCountProviderOrLog[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	w telemetry.Workflow,
	metadataClient metadata.Interface,
	restMapper meta.RESTMapper,
	log logr.Logger,
	group string,
	version string,
) {
	p, err := NewObjectCountProvider[T, TEnt](metadataClient, restMapper, group, version)
	if err != nil {
		log.Info("failed to create object provider", "error", err, "kind", constraints.EntityTypeName[T]())
		return
	}

	w.AddProvider(p)
}
