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
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/internal/admission"
	"github.com/kong/gateway-operator/internal/consts"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

const (
	defaultWebhookCertDir = "/tmp/k8s-webhook-server/serving-certs"

	defaultsecretPollInterval = 2 * time.Second
	defaultsecretPollTimeout  = 60 * time.Second
)

type webhookManager struct {
	client client.Client
	mgr    ctrl.Manager
	cfg    *Config
}

func (m *webhookManager) Start(ctx context.Context) error {
	if m.cfg.ControllerNamespace == "" {
		return errors.New("controllerNamespace must be set")
	}
	if m.cfg.WebhookCertDir == "" {
		return errors.New("webhookCertDir must be set")
	}
	if m.cfg.WebhookPort == 0 {
		return errors.New("webhookPort must be set")
	}

	// create the webhook resources (if they already exist, it is no-op)
	if err := m.createWebhookResources(ctx); err != nil {
		return err
	}

	certSecret := &corev1.Secret{}
	// check if the certificate secret already exists
	if err := m.client.Get(ctx, types.NamespacedName{Namespace: m.cfg.ControllerNamespace, Name: consts.WebhookCertificateConfigSecretName}, certSecret); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		// no certificate secret found, create all the resources needed to produce it (if they already exist, it is no-op)
		if err := m.createCertificateConfigResources(ctx); err != nil {
			return err
		}

		// wait for the certificate to be created
		certSecret, err = m.waitForWebhookCertificate(ctx, defaultsecretPollTimeout, defaultsecretPollInterval)
		if err != nil {
			return err
		}
	}

	// write the webhook certificate files on the filesystem
	if err := os.WriteFile(path.Join(m.cfg.WebhookCertDir, caCertFilename), certSecret.Data["ca"], os.ModePerm); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(m.cfg.WebhookCertDir, tlsCertFilename), certSecret.Data["cert"], os.ModePerm); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(m.cfg.WebhookCertDir, tlsKeyFilename), certSecret.Data["key"], os.ModePerm); err != nil {
		return err
	}

	// create and start a new webhook server
	if err := admission.AddNewWebhookServerToManager(m.mgr, ctrl.Log, m.cfg.WebhookPort, m.cfg.WebhookCertDir); err != nil {
		return err
	}

	// load the Gateway API controllers and start them only after the webhook is in place
	controllers := setupControllers(m.mgr, m.cfg)
	for _, c := range controllers {
		if err := c.MaybeSetupWithManager(m.mgr); err != nil {
			return fmt.Errorf("unable to create controller %q: %w", c.Name(), err)
		}
	}
	return nil
}

// createCertificateConfigResources create all the resources needed by the CertificateConfig jobs
func (m *webhookManager) createCertificateConfigResources(ctx context.Context) error {
	// create the certificateConfig ServiceAccount
	serviceAccount := k8sresources.GenerateNewServiceAccountForCertificateConfig(m.cfg.ControllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, serviceAccount); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig ClusterRole
	clusterRole := k8sresources.GenerateNewClusterRoleForCertificateConfig(m.cfg.ControllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, clusterRole); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig ClusterRoleBinding
	clusterRoleBinding := k8sresources.GenerateNewClusterRoleBindingForCertificateConfig(m.cfg.ControllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, clusterRoleBinding); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig Role
	role := k8sresources.GenerateNewRoleForCertificateConfig(m.cfg.ControllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, role); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig RoleBinding
	roleBinding := k8sresources.GenerateNewRoleBindingForCertificateConfig(m.cfg.ControllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, roleBinding); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig jobs
	if err := m.createCertificateConfigJobs(ctx); err != nil {
		return err
	}

	return nil
}

func (m *webhookManager) createWebhookResources(ctx context.Context) error {
	// create the operator ValidatinWebhookConfiguration
	validatingWebhookConfiguration := k8sresources.GenerateNewValidatingWebhookConfiguration(m.cfg.ControllerNamespace, consts.WebhookServiceName, consts.WebhookName)
	if err := m.client.Create(ctx, validatingWebhookConfiguration); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the Service needed to expose the operator Webhook
	webhookService := k8sresources.GenerateNewServiceForCertificateConfig(m.cfg.ControllerNamespace, consts.WebhookServiceName)
	if err := m.client.Create(ctx, webhookService); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	return nil
}

func (m *webhookManager) createCertificateConfigJobs(ctx context.Context) error {
	jobCertificateConfigImage := consts.WebhookCertificateConfigBaseImage
	if relatedJobImage := os.Getenv("RELATED_IMAGE_CERTIFICATE_CONFIG"); relatedJobImage != "" {
		// RELATED_IMAGE_CERTIFICATE_CONFIG is set by the operator-sdk when building the operator bundle.
		// https://github.com/Kong/gateway-operator/issues/261
		jobCertificateConfigImage = relatedJobImage
	}
	createJob, patchJob := k8sresources.GenerateNewWebhookCertificateConfigJobs(m.cfg.ControllerNamespace,
		consts.WebhookCertificateConfigName,
		jobCertificateConfigImage,
		consts.WebhookCertificateConfigSecretName,
		consts.WebhookName)

	if err := m.client.Create(ctx, createJob); err != nil {
		return err
	}

	if err := m.client.Create(ctx, patchJob); err != nil {
		return err
	}
	return nil
}

// waitForWebhookCertificate polls the API server at a specific interval until the webhook certificate
// secret is created. If the timer expires, it returns an error. Otherwise, the Secret is returned.
func (m *webhookManager) waitForWebhookCertificate(ctx context.Context, pollTimeout time.Duration, pollInterval time.Duration) (*corev1.Secret, error) {
	ticker := time.NewTicker(pollInterval)
	quit := make(chan struct{})
	errChan := make(chan error, 1)
	certificateSecret := &corev1.Secret{}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ticker.C:
				err := m.client.Get(ctx, types.NamespacedName{Namespace: m.cfg.ControllerNamespace, Name: consts.WebhookCertificateConfigSecretName}, certificateSecret)
				if err != nil {
					if !k8serrors.IsNotFound(err) {
						errChan <- err
						return
					}
					continue
				}
				return
			case <-quit:
				ticker.Stop()
				errChan <- fmt.Errorf("timeout for creating webhook certificate expired")
				return
			}
		}
	}()
	time.AfterFunc(pollTimeout, func() {
		close(quit)
	})
	wg.Wait()
	ticker.Stop()
	select {
	case err := <-errChan:
		return nil, err
	default:
		return certificateSecret, nil
	}
}
