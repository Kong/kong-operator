/*
Copyright 2022 Kong Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manager

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"os"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/kong/gateway-operator/internal/telemetry"
	"github.com/kong/gateway-operator/modules/manager/metadata"
	"github.com/kong/gateway-operator/pkg/vars"
)

const (
	caCertFilename  = "ca.crt"
	tlsCertFilename = "tls.crt"
	tlsKeyFilename  = "tls.key"
)

// Config represents the configuration for the manager.
type Config struct {
	MetricsAddr              string
	ProbeAddr                string
	WebhookCertDir           string
	WebhookPort              int
	LeaderElection           bool
	LeaderElectionNamespace  string
	DevelopmentMode          bool
	Out                      *os.File
	NewClientFunc            client.NewClientFunc
	ControllerName           string
	ControllerNamespace      string
	AnonymousReports         bool
	APIServerPath            string
	KubeconfigPath           string
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	LoggerOpts               *zap.Options

	// controllers for standard APIs and features
	GatewayControllerEnabled            bool
	ControlPlaneControllerEnabled       bool
	DataPlaneControllerEnabled          bool
	DataPlaneBlueGreenControllerEnabled bool

	// Controllers for specialty APIs and experimental features.
	AIGatewayControllerEnabled              bool
	KongPluginInstallationControllerEnabled bool

	// Controllers for Konnect APIs.
	KonnectControllersEnabled bool

	// webhook and validation options
	ValidatingWebhookEnabled bool
}

// DefaultConfig returns a default configuration for the manager.
func DefaultConfig() Config {
	const (
		defaultNamespace               = "kong-system"
		defaultLeaderElectionNamespace = defaultNamespace
	)

	return Config{
		MetricsAddr:                   ":8080",
		ProbeAddr:                     ":8081",
		WebhookCertDir:                defaultWebhookCertDir,
		WebhookPort:                   9443,
		DevelopmentMode:               false,
		LeaderElection:                true,
		LeaderElectionNamespace:       defaultLeaderElectionNamespace,
		ClusterCASecretName:           "kong-operator-ca",
		ClusterCASecretNamespace:      defaultNamespace,
		ControllerNamespace:           defaultNamespace,
		LoggerOpts:                    &zap.Options{},
		GatewayControllerEnabled:      true,
		ControlPlaneControllerEnabled: true,
		DataPlaneControllerEnabled:    true,
	}
}

// SetupControllersFunc represents function to setup controllers, which is called
// in Run function.
type SetupControllersFunc func(manager.Manager, *Config) ([]ControllerDef, error)

// Run runs the manager. Parameter cfg represents the configuration for the manager
// that for normal operation is derived from command-line flags. The function
// setupControllers is expected to return a list of configured ControllerDef
// that will be added to the manager. The function admissionRequestHandler is
// used to construct the admission webhook handler for the validating webhook
// that is added to the manager too. Argument startedChan can be used as a signal
// to notify the caller when the manager has been started. Specifically, this channel
// gets closed when manager.Start() is called. Pass nil if you don't need this signal.
func Run(
	cfg Config,
	scheme *runtime.Scheme,
	setupControllers SetupControllersFunc,
	admissionRequestHandler AdmissionRequestHandlerFunc,
	startedChan chan<- struct{},
	metadata metadata.Info,
) error {
	setupLog := ctrl.Log.WithName("setup")
	setupLog.Info("starting controller manager",
		"release", metadata.Release,
		"repo", metadata.Repo,
		"commit", metadata.Commit,
	)

	if cfg.ControllerName != "" {
		setupLog.Info(fmt.Sprintf("custom controller name provided: %s", cfg.ControllerName))
		vars.SetControllerName(cfg.ControllerName)
	}

	if cfg.DevelopmentMode {
		setupLog.Info("development mode enabled")
	}

	if cfg.LeaderElection {
		setupLog.Info("leader election enabled", "namespace", cfg.LeaderElectionNamespace)
	} else {
		setupLog.Info("leader election disabled")
	}

	restCfg := ctrl.GetConfigOrDie()
	restCfg.UserAgent = metadata.UserAgent()

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: cfg.MetricsAddr,
		},
		WebhookServer: webhook.NewServer(
			webhook.Options{
				Port: cfg.WebhookPort,
			},
		),
		HealthProbeBindAddress:  cfg.ProbeAddr,
		LeaderElection:          cfg.LeaderElection,
		LeaderElectionNamespace: cfg.LeaderElectionNamespace,
		LeaderElectionID:        "a7feedc84.konghq.com",
		NewClient:               cfg.NewClientFunc,
	})
	if err != nil {
		return err
	}

	caMgr := &caManager{
		logger:          ctrl.Log.WithName("ca_manager"),
		client:          mgr.GetClient(),
		secretName:      cfg.ClusterCASecretName,
		secretNamespace: cfg.ClusterCASecretNamespace,
	}
	err = mgr.Add(caMgr)
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	if err := setupIndexes(context.Background(), mgr, cfg); err != nil {
		return err
	}

	if cfg.ValidatingWebhookEnabled {
		// if the validatingWebhook is enabled, we don't need to setup the Gateway API controllers
		// here, as they will be set up by the webhook manager once all the webhook resources will be created
		// and the webhook will be in place.
		webhookMgr := &webhookManager{
			client: mgr.GetClient(),
			mgr:    mgr,
			logger: ctrl.Log.WithName("webhook_manager"),
			cfg:    &cfg,
		}
		if err := webhookMgr.PrepareWebhookServerWithControllers(context.Background(), setupControllers, admissionRequestHandler); err != nil {
			return fmt.Errorf("unable to create webhook server: %w", err)
		}

		if err := mgr.Add(webhookMgr); err != nil {
			return fmt.Errorf("unable to add webhook manager: %w", err)
		}

		defer func() {
			setupLog.Info("cleaning up webhook and certificateConfig resources")
			if err := webhookMgr.cleanup(context.Background()); err != nil {
				setupLog.Error(err, "error while performing cleanup")
			}
		}()
	} else {
		controllers, err := setupControllers(mgr, &cfg)
		if err != nil {
			setupLog.Error(err, "failed setting up controllers")
			return err
		}
		for _, c := range controllers {
			if err := c.MaybeSetupWithManager(mgr); err != nil {
				return fmt.Errorf("unable to create controller %q: %w", c.Name(), err)
			}
		}

		// Add readyz check here only if the validating webhook is disabled.
		// When the webhook is enabled we add a readyz check in PrepareWebhookServer
		// to mark the controller ready only after the webhook has started.
		if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
			return fmt.Errorf("unable to set up ready check: %w", err)
		}
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	// Enable anonnymous reporting when configured but not for development builds
	// to reduce the noise.
	if cfg.AnonymousReports && !cfg.DevelopmentMode {
		stopAnonymousReports, err := setupAnonymousReports(context.Background(), restCfg, setupLog, metadata)
		if err != nil {
			setupLog.Error(err, "failed setting up anonymous reports")
		} else {
			defer stopAnonymousReports()
		}
	}

	setupLog.Info("starting manager")
	// If started channel is set, close it to notify the caller that manager has started.
	if startedChan != nil {
		close(startedChan)
	}
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}

type caManager struct {
	logger          logr.Logger
	client          client.Client
	secretName      string
	secretNamespace string
}

// Start starts the CA manager.
func (m *caManager) Start(ctx context.Context) error {
	if m.secretName == "" {
		return fmt.Errorf("cannot use an empty secret name when creating a CA secret")
	}
	if m.secretNamespace == "" {
		return fmt.Errorf("cannot use an empty secret namespace when creating a CA secret")
	}
	return m.maybeCreateCACertificate(ctx)
}

func (m *caManager) maybeCreateCACertificate(ctx context.Context) error {
	// TODO https://github.com/Kong/gateway-operator/issues/199 this also needs to check if the CA is expired and
	// managed, and needs to reissue it (and all issued certificates) if so
	ca := &corev1.Secret{}
	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()
	err := m.client.Get(ctx, client.ObjectKey{Namespace: m.secretNamespace, Name: m.secretName}, ca)
	if k8serrors.IsNotFound(err) {
		serial, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
		if err != nil {
			return err
		}
		m.logger.Info(fmt.Sprintf("no CA certificate Secret %s found, generating CA certificate", m.secretName))
		template := x509.Certificate{
			Subject: pkix.Name{
				CommonName:   "Kong Gateway Operator CA",
				Organization: []string{"Kong, Inc."},
				Country:      []string{"US"},
			},
			SerialNumber:          serial,
			SignatureAlgorithm:    x509.ECDSAWithSHA256,
			NotBefore:             time.Now(),
			NotAfter:              time.Now().Add(time.Second * 315400000),
			KeyUsage:              x509.KeyUsageCertSign + x509.KeyUsageKeyEncipherment + x509.KeyUsageDigitalSignature,
			BasicConstraintsValid: true,
			IsCA:                  true,
		}

		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		privDer, err := x509.MarshalECPrivateKey(priv)
		if err != nil {
			return err
		}

		der, err := x509.CreateCertificate(rand.Reader, &template, &template, priv.Public(), priv)
		if err != nil {
			return err
		}

		signedSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: m.secretNamespace,
				Name:      m.secretName,
			},
			Type: corev1.SecretTypeTLS,
			StringData: map[string]string{
				"tls.crt": string(pem.EncodeToMemory(&pem.Block{
					Type:  "CERTIFICATE",
					Bytes: der,
				})),

				"tls.key": string(pem.EncodeToMemory(&pem.Block{
					Type:  "EC PRIVATE KEY",
					Bytes: privDer,
				})),
			},
		}
		err = m.client.Create(ctx, signedSecret)
		if err != nil {
			return err
		}

	} else if err != nil {
		return err
	}
	return nil
}

// setupAnonymousReports sets up and starts the anonymous reporting and returns
// a cleanup function and an error.
// The caller is responsible to call the returned function - when the returned
// error is not nil - to stop the reports sending.
func setupAnonymousReports(ctx context.Context, restCfg *rest.Config, logger logr.Logger, metadata metadata.Info) (func(), error) {
	logger.Info("starting anonymous reports")

	payload := telemetry.Payload{
		"v":      metadata.Release,
		"flavor": metadata.Flavor,
	}

	tMgr, err := telemetry.CreateManager(telemetry.SignalPing, restCfg, logger, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to create anonymous reports manager: %w", err)
	}

	if err := tMgr.Start(); err != nil {
		return nil, fmt.Errorf("anonymous reports failed to start: %w", err)
	}

	if err := tMgr.TriggerExecute(ctx, telemetry.SignalStart); err != nil {
		// We failed to send initial start signal with telemetry data.
		// Don't abort and return an error, just log an error and continue.
		logger.WithValues("error", err).
			Info("failed to send an initial telemetry start signal")
	}

	return tMgr.Stop, nil
}
