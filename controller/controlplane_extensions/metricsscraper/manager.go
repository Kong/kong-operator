package metricsscraper

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/cloudflare/cfssl/config"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

type certs struct {
	Key  crypto.Signer
	CA   *x509.Certificate
	Cert *x509.Certificate
}

// MetricsScrapePipeline is a pipeline for scraping and enriching metrics.
type MetricsScrapePipeline interface {
	MetricsScraper
	MetricsEnricher
}

type metricsPipeline struct {
	MetricsScraper
	MetricsEnricher
}

// Manager is a manager for metrics scrapers.
type Manager struct {
	logger                   logr.Logger
	scrapeInterval           time.Duration
	client                   client.Client
	caSecretNN               types.NamespacedName
	certs                    certs
	pipelinesNotificationsCh chan scrapeUpdateNotification
	pipelinesLock            sync.RWMutex
	pipelines                map[types.UID]MetricsScrapePipeline
	cpNNToDpUID              map[types.NamespacedName]types.UID
	clusterCAKeyConfig       secrets.KeyConfig
}

// NewManager creates new MetricsScrapeManager.
func NewManager(
	logger logr.Logger,
	interval time.Duration,
	cl client.Client,
	caSecretNN types.NamespacedName,
	clusterCAKeyConfig secrets.KeyConfig,
) *Manager {
	return &Manager{
		logger:                   logger,
		scrapeInterval:           interval,
		caSecretNN:               caSecretNN,
		client:                   cl,
		pipelinesNotificationsCh: make(chan scrapeUpdateNotification),
		pipelines:                make(map[types.UID]MetricsScrapePipeline),
		cpNNToDpUID:              make(map[types.NamespacedName]types.UID),
		clusterCAKeyConfig:       clusterCAKeyConfig,
	}
}

// initMTLSCerts creates mTLS certs for the manager so that it can use them for
// secure communication with DataPlane's AdminAPI endpoints.
// When successful, it sets the certs on the manager.
func (msm *Manager) initMTLSCerts(ctx context.Context) error {
	msm.logger.Info("getting CA cluster secret to generate certs for MTLs communication with Kong Gateway", "secret", msm.caSecretNN)
	var (
		caCert *x509.Certificate
		caKey  crypto.Signer
	)
	if err := retry.Do(
		func() error {
			var err error
			caCert, caKey, err = msm.getCASecretAndKey(ctx)
			return err
		},
		retry.Context(ctx),
		retry.Attempts(0),
		retry.MaxDelay(time.Second),
		retry.MaxJitter(500*time.Millisecond),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.OnRetry(func(n uint, err error) {
			msm.logger.Info(
				"failed to get CA cluster secret to generate certs for MTLs communication with Kong Gateway, retrying...",
				"error", err,
			)
		}),
	); err != nil {
		return err
	}

	signingAlgorithm := secrets.SignatureAlgorithmForKeyType(msm.clusterCAKeyConfig.Type)
	template := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "localhost",
			Organization: []string{"Kong, Inc."},
			Country:      []string{"US"},
		},
		SignatureAlgorithm: signingAlgorithm,
		DNSNames:           []string{"localhost"},
	}

	csrKey, _, signingAlgorithm, err := secrets.CreatePrivateKey(msm.clusterCAKeyConfig)
	if err != nil {
		return err
	}

	der, err := x509.CreateCertificateRequest(rand.Reader, &template, csrKey)
	if err != nil {
		return err
	}
	// Let's make this valid for 10 years.
	expiration := int32(315400000)
	csr := certificatesv1.CertificateSigningRequestSpec{
		Request: pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE REQUEST",
			Bytes: der,
		}),
		SignerName:        "gateway-operator.konghq.com/mtls",
		ExpirationSeconds: &expiration,
		Usages: []certificatesv1.KeyUsage{
			certificatesv1.UsageDigitalSignature,
		},
	}

	signedCertPem, err := signCertificate(csr, caKey, caCert, signingAlgorithm)
	if err != nil {
		return err
	}
	pb, _ := pem.Decode(signedCertPem)
	if pb == nil {
		return fmt.Errorf("failed to decode signed certificate")
	}
	cert, err := x509.ParseCertificate(pb.Bytes)
	if err != nil {
		return err
	}

	msm.certs = certs{
		CA:   caCert,
		Cert: cert,
		Key:  csrKey,
	}
	return nil
}

func (msm *Manager) getCASecretAndKey(ctx context.Context) (*x509.Certificate, crypto.Signer, error) {
	var caSecret corev1.Secret
	err := msm.client.Get(ctx, msm.caSecretNN, &caSecret)
	if err != nil {
		return nil, nil, err
	}

	ca, ok := caSecret.Data[consts.TLSCRT]
	if !ok {
		return nil, nil, fmt.Errorf(consts.TLSCRT + " field not found")
	}
	caCertBlock, _ := pem.Decode(ca)
	if caCertBlock == nil {
		return nil, nil, fmt.Errorf("failed decoding %q data from secret %s", consts.TLSCRT, caSecret.Name)
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	key, ok := caSecret.Data[consts.TLSKey]
	if !ok {
		return nil, nil, fmt.Errorf(consts.TLSKey + " field not found")
	}
	caKeyBlock, _ := pem.Decode(key)
	if caKeyBlock == nil {
		return nil, nil, fmt.Errorf("failed decoding %q data from secret %s", consts.TLSKey, caSecret.Name)
	}

	caKey, err := secrets.ParseKey(msm.clusterCAKeyConfig.Type, caKeyBlock)
	return caCert, caKey, err
}

// Start starts the metrics scraping loop.
// It spawns a goroutine that periodically scrapes metrics from the enabled scrapers.
// This satisfies the Manager interface and can be used with controller-runtime Manager.
func (msm *Manager) Start(ctx context.Context) error {
	if err := msm.initMTLSCerts(ctx); err != nil {
		return fmt.Errorf("failed to create mTLS certs: %w", err)
	}

	go func(ctx context.Context) {
		ticker := time.NewTicker(msm.scrapeInterval)
		defer ticker.Stop()
		defer close(msm.pipelinesNotificationsCh)

		for {
			select {
			case <-ctx.Done():
				return

			case sun := <-msm.pipelinesNotificationsCh:
				switch sun.Action {
				case add:
					if err := msm.enableMetricsScraperForControlPlanesDataPlane(ctx, sun.ControlPlane); err != nil {
						log.Error(msm.logger, err, "failed to enable metrics scraper for ControlPlane", sun.ControlPlane)
					}
				case remove:
					msm.RemoveForControlPlaneNN(sun.ControlPlaneNN)
				}

			case <-ticker.C:
				msm.pipelinesLock.RLock()
				pipeline := lo.Values(msm.pipelines)
				msm.pipelinesLock.RUnlock()

				for _, p := range pipeline {
					go func(p MetricsScrapePipeline) {
						metrics, err := p.Scrape(ctx)
						if err != nil {
							msm.logger.Error(err, "failed to scrape metrics")
							return
						}
						if err := p.Consume(ctx, metrics); err != nil {
							msm.logger.Error(err, "failed to consume metrics")
							return
						}
					}(p)
				}

			}
		}
	}(ctx)

	return nil
}

type scrapeUpdateAction uint8

const (
	add scrapeUpdateAction = iota
	remove
)

type scrapeUpdateNotification struct {
	ControlPlane   *gwtypes.ControlPlane
	ControlPlaneNN types.NamespacedName
	Action         scrapeUpdateAction
}

// NotifyAdd notifies the manager that a new ControlPlane has been added.
func (msm *Manager) NotifyAdd(ctx context.Context, cp *gwtypes.ControlPlane) {
	select {
	case <-ctx.Done():
	case msm.pipelinesNotificationsCh <- scrapeUpdateNotification{ControlPlane: cp, Action: add}:
	}
}

// NotifyRemove notifies the manager that a ControlPlane has been removed.
// It uses the ControlPlane's NamespacedName to identify the ControlPlane because
// the ControlPlane object is already deleted when this method is called.
func (msm *Manager) NotifyRemove(ctx context.Context, cp types.NamespacedName) {
	select {
	case <-ctx.Done():
	case msm.pipelinesNotificationsCh <- scrapeUpdateNotification{ControlPlaneNN: cp, Action: remove}:
	}
}

// Add adds a scraper to the manager for the given ControlPlane.
// If a scraper already exists for the given DataPlane UID, it is replaced.
// User is responsible for ensuring that the scraper is configured for DataPlane
// that is associated with the given ControlPlane.
// It returns true if the scraper was added.
func (msm *Manager) Add(cp *gwtypes.ControlPlane, pipeline MetricsScrapePipeline) bool {
	if cp == nil {
		return false
	}

	switch dp := cp.Spec.DataPlane; dp.Type {
	case gwtypes.ControlPlaneDataPlaneTargetRefType:
		if dp.Ref == nil {
			return false
		}
	case gwtypes.ControlPlaneDataPlaneTargetManagedByType:
	default:
		return false
	}

	dpUID := pipeline.DataPlaneUID()
	cpNN := client.ObjectKeyFromObject(cp)

	msm.pipelinesLock.Lock()
	defer msm.pipelinesLock.Unlock()
	if _, ok := msm.pipelines[dpUID]; ok {
		// If we already have a scraper for this DataPlane, we don't need to add
		// it again. We just need to update the mapping of the ControlPlane NN
		// to the DataPlane UID.
		msm.cpNNToDpUID[cpNN] = dpUID
		return false
	}

	msm.pipelines[dpUID] = pipeline
	if oldDpDUID, ok := msm.cpNNToDpUID[cpNN]; ok {
		// If we already have a scraper for this ControlPlane, we need to check
		// if it's the same DataPlane. If it's not, we need to remove the old
		// scraper.
		if oldDpDUID != dpUID {
			delete(msm.pipelines, oldDpDUID)
		}
	}
	msm.cpNNToDpUID[cpNN] = dpUID
	return true
}

// RemoveForControlPlaneNN removes a scraper from the manager for the given ControlPlane.
func (msm *Manager) RemoveForControlPlaneNN(cpNN types.NamespacedName) {
	msm.pipelinesLock.Lock()
	defer msm.pipelinesLock.Unlock()

	dpUID, ok := msm.cpNNToDpUID[cpNN]
	if !ok {
		return
	}

	delete(msm.pipelines, dpUID)
	delete(msm.cpNNToDpUID, cpNN)
	log.Debug(msm.logger, "removed metrics scraper for ControlPlane", cpNN, "dataplane_uid", dpUID)
}

func (msm *Manager) enableMetricsScraperForControlPlanesDataPlane(
	ctx context.Context,
	controlplane *gwtypes.ControlPlane,
) error {
	if controlplane == nil ||
		((controlplane.Spec.DataPlane.Type != gwtypes.ControlPlaneDataPlaneTargetRefType || controlplane.Spec.DataPlane.Ref == nil) &&
			(controlplane.Spec.DataPlane.Type != gwtypes.ControlPlaneDataPlaneTargetManagedByType)) {
		return fmt.Errorf("ControlPlane does not have a valid, supported DataPlane target")
	}

	var dpNN types.NamespacedName
	switch controlplane.Spec.DataPlane.Type {
	case gwtypes.ControlPlaneDataPlaneTargetRefType:
		dpNN = types.NamespacedName{
			Name:      controlplane.Spec.DataPlane.Ref.Name,
			Namespace: controlplane.Namespace,
		}
	case gwtypes.ControlPlaneDataPlaneTargetManagedByType:
		if controlplane.Status.DataPlane == nil {
			return fmt.Errorf("ControlPlane's DataPlane is managed but it's not set in the status")
		}
		dpNN = types.NamespacedName{
			Name:      controlplane.Status.DataPlane.Name,
			Namespace: controlplane.Namespace,
		}
	}

	var dp operatorv1beta1.DataPlane
	if err := msm.client.Get(ctx, dpNN, &dp); err != nil {
		return fmt.Errorf("failed to get DataPlane %s: %w", dpNN, err)
	}

	adminAPIAddressProvider := NewAdminAPIAddressProvider(msm.client)
	httpClient := httpClientWithCerts(msm.certs)

	enricher, err := NewEnricher(msm.logger, &dp, msm.client, msm.certs, adminAPIAddressProvider)
	if err != nil {
		return fmt.Errorf("failed to create metrics enricher: %w", err)
	}

	pipeline := &metricsPipeline{
		MetricsScraper:  NewPrometheusMetricsScraper(msm.logger, &dp, httpClient, adminAPIAddressProvider),
		MetricsEnricher: enricher,
	}

	if msm.Add(controlplane, pipeline) {
		log.Debug(msm.logger, "enabled metrics scraper for ControlPlane", "controlplane", controlplane, "DataPlane", controlplane.Spec.DataPlane)
	}
	return nil
}

func signCertificate(
	csr certificatesv1.CertificateSigningRequestSpec,
	key crypto.Signer,
	caCert *x509.Certificate,
	signingAlgorithm x509.SignatureAlgorithm,
) ([]byte, error) {
	usages := make([]string, 0, len(csr.Usages))
	for _, usage := range csr.Usages {
		usages = append(usages, string(usage))
	}

	certExpiryDuration := time.Second * time.Duration(*csr.ExpirationSeconds)
	durationUntilExpiry := time.Until(caCert.NotAfter)
	if durationUntilExpiry <= 0 {
		return nil, fmt.Errorf("the signer has expired: %v", caCert.NotAfter)
	}
	if durationUntilExpiry < certExpiryDuration {
		certExpiryDuration = durationUntilExpiry
	}

	policy := &config.Signing{
		Default: &config.SigningProfile{
			Usage:        usages,
			Expiry:       certExpiryDuration,
			ExpiryString: certExpiryDuration.String(),
		},
	}
	cfs, err := local.NewSigner(key, caCert, signingAlgorithm, policy)
	if err != nil {
		return nil, err
	}

	certBytes, err := cfs.Sign(signer.SignRequest{Request: string(csr.Request)})
	if err != nil {
		return nil, err
	}
	return certBytes, nil
}
