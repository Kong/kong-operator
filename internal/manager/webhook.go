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
	client              client.Client
	mgr                 ctrl.Manager
	controllerNamespace string
	webhookCertDir      string
	webhookPort         int
}

func (m *webhookManager) Start(ctx context.Context) error {
	if m.controllerNamespace == "" {
		return errors.New("controllerNamespace must be set")
	}
	if m.webhookCertDir == "" {
		return errors.New("webhookCertDir must be set")
	}
	if m.webhookPort == 0 {
		return errors.New("webhookPort must be set")
	}

	// create the webhook resources (if they already exist, it is no-op)
	if err := m.createWebhookResources(ctx); err != nil {
		return err
	}

	certSecret := &corev1.Secret{}
	// check if the certificate secret already exists
	if err := m.client.Get(ctx, types.NamespacedName{Namespace: m.controllerNamespace, Name: consts.WebhookCertificateConfigSecretName}, certSecret); err != nil {
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
	if err := os.WriteFile(path.Join(m.webhookCertDir, caCertFilename), certSecret.Data["ca"], os.ModePerm); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(m.webhookCertDir, tlsCertFilename), certSecret.Data["cert"], os.ModePerm); err != nil {
		return err
	}
	if err := os.WriteFile(path.Join(m.webhookCertDir, tlsKeyFilename), certSecret.Data["key"], os.ModePerm); err != nil {
		return err
	}

	// create and start a new webhook server
	return admission.AddNewWebhookServerToManager(m.mgr, ctrl.Log, m.webhookPort, m.webhookCertDir)
}

// createCertificateConfigResources create all the resources needed by the CertificateConfig jobs
func (m *webhookManager) createCertificateConfigResources(ctx context.Context) error {
	// create the certificateConfig ServiceAccount
	serviceAccount := k8sresources.GenerateNewServiceAccountForCertificateConfig(m.controllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, serviceAccount); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig ClusterRole
	clusterRole := k8sresources.GenerateNewClusterRoleForCertificateConfig(m.controllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, clusterRole); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig ClusterRoleBinding
	clusterRoleBinding := k8sresources.GenerateNewClusterRoleBindingForCertificateConfig(m.controllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, clusterRoleBinding); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig Role
	role := k8sresources.GenerateNewRoleForCertificateConfig(m.controllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
	if err := m.client.Create(ctx, role); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the certificateConfig RoleBinding
	roleBinding := k8sresources.GenerateNewRoleBindingForCertificateConfig(m.controllerNamespace, consts.WebhookCertificateConfigName, consts.WebhookCertificateConfigLabelvalue)
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
	validatingWebhookConfiguration := k8sresources.GenerateNewValidatingWebhookConfiguration(m.controllerNamespace, consts.WebhookServiceName, consts.WebhookName)
	if err := m.client.Create(ctx, validatingWebhookConfiguration); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return err
		}
	}

	// create the Service needed to expose the operator Webhook
	webhookService := k8sresources.GenerateNewServiceForCertificateConfig(m.controllerNamespace, consts.WebhookServiceName)
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
	createJob, patchJob := k8sresources.GenerateNewWebhookCertificateConfigJobs(m.controllerNamespace,
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
				err := m.client.Get(ctx, types.NamespacedName{Namespace: m.controllerNamespace, Name: consts.WebhookCertificateConfigSecretName}, certificateSecret)
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
