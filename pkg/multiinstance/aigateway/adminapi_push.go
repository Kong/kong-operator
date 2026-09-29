/*
Copyright 2026 Kong, Inc.

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

package aigateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kong/go-kong/kong"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/yaml"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	adminapi "github.com/kong/kong-operator/v2/internal/adminapi"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// pusher is the subset of the Kong Admin API client used to push declarative
// configuration. It is implemented by *kong.Client.
type pusher interface {
	ReloadDeclarativeRawConfig(ctx context.Context, config io.Reader, checkHash, flattenErrors bool) error
}

// newPushClientFunc builds the Admin API client used to push configuration to a
// single discovered endpoint. It is a field of Instance so that tests can stub it.
type newPushClientFunc func(address, serverName string, certPEM, keyPEM, caPEM []byte) (pusher, error)

// newMTLSClientAdapter adapts adminapi.NewMTLSClient to newPushClientFunc.
func newMTLSClientAdapter(address, serverName string, certPEM, keyPEM, caPEM []byte) (pusher, error) {
	return adminapi.NewMTLSClient(address, serverName, certPEM, keyPEM, caPEM)
}

// instanceFieldManager is the SSA field manager used by the instance to patch the
// OnPremAIGateway status. It is distinct from controllerpkgssa.FieldManager (used by
// the OnPremAIGateway controller) so that the instance only owns the condition it
// sets and never drops the conditions owned by the controller.
const instanceFieldManager = "gateway-operator-aigateway-instance"

// pushClientCacheKey keys the Instance's push client cache. It includes the
// certificate material so that a renewed Secret (even one replaced in place,
// under the same reference) invalidates the cached clients.
func pushClientCacheKey(
	endpoint adminapi.DiscoveredAdminAPI,
	certPEM, keyPEM, caPEM []byte,
) string {
	return strings.Join([]string{
		endpoint.Address, endpoint.TLSServerName,
		string(certPEM), string(keyPEM), string(caPEM),
	}, "\x00")
}

// sendConfigToDataPlanes pushes the rendered dbless YAML payload to the Admin API of
// every data plane endpoint discovered for the gateway (see Instance.AdminAPIs).
// A failure on any endpoint does not prevent the remaining endpoints from being
// pushed: the returned error only reports the failures, and the sync loop retries
// them on the next tick (already-up-to-date endpoints no-op thanks to check_hash).
func (i *Instance) sendConfigToDataPlanes(
	ctx context.Context,
	gwNN types.NamespacedName,
	yamlPayload []byte,
) error {
	endpoints := i.AdminAPIs()
	if endpoints.Len() == 0 {
		log.Info(i.logger, "no Admin API endpoints discovered for the gateway, skipping configuration push",
			"namespace", gwNN.Namespace, "name", gwNN.Name)
		return nil
	}

	payload, err := yaml.YAMLToJSON(yamlPayload)
	if err != nil {
		return i.failPush(ctx, gwNN, endpoints.Len(), fmt.Errorf("converting rendered configuration to JSON: %w", err))
	}

	certPEM, keyPEM, caPEM, err := i.adminMTLSCertMaterial(ctx)
	if err != nil {
		return i.failPush(ctx, gwNN, endpoints.Len(), fmt.Errorf("loading Admin API mTLS client certificate: %w", err))
	}

	var failures []string
	currentKeys := sets.New[string]()
	for endpoint := range endpoints {
		desc := endpointDesc(endpoint)
		key := pushClientCacheKey(endpoint, certPEM, keyPEM, caPEM)
		currentKeys.Insert(key)
		pushClient, ok := i.pushClients[key]
		if !ok {
			pushClient, err = i.newPushClient(endpoint.Address, endpoint.TLSServerName, certPEM, keyPEM, caPEM)
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %s", desc, err))
				continue
			}
			if i.pushClients == nil {
				i.pushClients = map[string]pusher{}
			}
			i.pushClients[key] = pushClient
		}
		if err := pushClient.ReloadDeclarativeRawConfig(ctx, bytes.NewReader(payload), true, true); err != nil {
			errKong, ok := errors.AsType[*kong.APIError](err)
			if ok {
				// TODO: surface the detailed configuration error information
				// to the user through events and status conditions.
				log.Error(i.logger, errKong,
					"failed to push configuration to Admin API endpoint",
					"endpoint", desc,
					"address", endpoint.Address,
					"details", errKong.Details(),
					"raw", string(errKong.Raw()),
				)
			}
			failures = append(failures, fmt.Sprintf("%s: %s", desc, err))
			continue
		}
		log.Debug(i.logger, "pushed configuration to Admin API endpoint",
			"endpoint", desc, "address", endpoint.Address)
	}

	// Prune the clients of endpoints that are no longer discovered (e.g. pod
	// IPs that churn over time), so the cache does not grow without bound.
	for key := range i.pushClients {
		if !currentKeys.Has(key) {
			delete(i.pushClients, key)
		}
	}

	if err := i.reportPushStatus(ctx, gwNN, endpoints.Len(), failures); err != nil {
		// The push itself is what matters: a status patch failure must not fail
		// the sync (it would be retried together with the next push anyway), so
		// only log it.
		log.Error(i.logger, err, "failed to report the configuration push result on the OnPremAIGateway status")
	}

	if len(failures) > 0 {
		return fmt.Errorf("failed to push configuration to %d of %d Admin API endpoints: %s",
			len(failures), endpoints.Len(), strings.Join(failures, "; "))
	}
	return nil
}

// failPush reports a push that failed before any endpoint could be contacted
// (e.g. missing certificate material) on the gateway status and returns the error
// for the sync loop to retry.
func (i *Instance) failPush(ctx context.Context, gwNN types.NamespacedName, total int, err error) error {
	if reportErr := i.reportPushStatus(ctx, gwNN, total, []string{err.Error()}); reportErr != nil {
		log.Error(i.logger, reportErr, "failed to report the configuration push result on the OnPremAIGateway status")
	}
	return err
}

// adminMTLSCertMaterial reads the mTLS client certificate material used to push
// configuration to the data planes' Admin API from the certificate Secret provisioned
// by the OnPremAIGateway controller.
func (i *Instance) adminMTLSCertMaterial(ctx context.Context) (certPEM, keyPEM, caPEM []byte, err error) {
	secret := &corev1.Secret{}
	if err := i.client.Get(ctx, i.env.AdminClientCertSecretNN, secret); err != nil {
		return nil, nil, nil, fmt.Errorf("getting Secret %s: %w", i.env.AdminClientCertSecretNN, err)
	}
	certPEM, keyPEM, caPEM = secret.Data["tls.crt"], secret.Data["tls.key"], secret.Data["ca.crt"]
	if len(certPEM) == 0 || len(keyPEM) == 0 || len(caPEM) == 0 {
		return nil, nil, nil, fmt.Errorf(
			"Secret %s is missing required certificate material (tls.crt, tls.key and ca.crt must be set)",
			i.env.AdminClientCertSecretNN,
		)
	}
	return certPEM, keyPEM, caPEM, nil
}

// endpointDesc returns a human-readable description of a discovered Admin API endpoint,
// used in push failure events and errors.
func endpointDesc(endpoint adminapi.DiscoveredAdminAPI) string {
	if ref := endpoint.PodRef; ref.Namespace != "" || ref.Name != "" {
		return ref.String()
	}
	return endpoint.Address
}

// reportPushStatus patches the DataPlanesConfigured condition of the OnPremAIGateway
// with the outcome of the configuration push and, when the condition changed to a
// failure, emits a Warning event listing the failing endpoints.
func (i *Instance) reportPushStatus(
	ctx context.Context,
	gwNN types.NamespacedName,
	total int,
	failures []string,
) error {
	var condition metav1.Condition
	if len(failures) == 0 {
		condition = k8sutils.NewCondition(
			aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType,
			metav1.ConditionTrue,
			aigatewayv1alpha1.OnPremAIGatewayConfigurationPushSucceededReason,
			fmt.Sprintf("Configuration pushed to %d Admin API endpoints", total),
		)
	} else {
		condition = k8sutils.NewCondition(
			aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType,
			metav1.ConditionFalse,
			aigatewayv1alpha1.OnPremAIGatewayConfigurationPushFailedReason,
			fmt.Sprintf("Failed to push configuration to %d of %d Admin API endpoints", len(failures), total),
		)
	}

	// NewCondition stamps a fresh LastTransitionTime on every call. Reuse the
	// stored one when the condition state is unchanged, so that recurring
	// failures on the retry loop are not seen as changes by ApplyStatusIfChanged
	// (which would rewrite the status on every attempt). Whether the condition
	// actually changed is decided from the stored state, not from the apply
	// result: the comparison behind ApplyStatusIfChanged can report a change
	// even when the applied content is identical.
	var changed bool
	existing := &aigatewayv1alpha1.OnPremAIGateway{}
	if err := i.client.Get(ctx, gwNN, existing); err == nil {
		condition, changed = k8sutils.UpdatedConditionPreservingLastTransitionTime(existing.Status.Conditions, condition)
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("getting OnPremAIGateway %s: %w", gwNN, err)
	}

	gw := &aigatewayv1alpha1.OnPremAIGateway{
		Namespace: gwNN.Namespace,
		Name:      gwNN.Name,
	}
	// The GVK is required for the SSA apply: a freshly constructed object has an
	// empty TypeMeta.
	gw.SetGroupVersionKind(aigatewayv1alpha1.GroupVersion.WithKind("OnPremAIGateway"))
	gw.Status.Conditions = []metav1.Condition{condition}

	if _, err := controllerpkgssa.ApplyStatusIfChanged(
		ctx, i.logger, i.client, i.env.TypeConverter, gw, instanceFieldManager,
	); err != nil {
		return fmt.Errorf("patching OnPremAIGateway status with the configuration push result: %w", err)
	}
	// Only emit events when the condition actually changed, so that recurring
	// failures on the retry loop do not spam the event recorder.
	if changed && len(failures) > 0 && i.eventRecorder != nil {
		i.eventRecorder.Eventf(
			gw, nil, corev1.EventTypeWarning,
			string(aigatewayv1alpha1.OnPremAIGatewayConfigurationPushFailedReason),
			string(aigatewayv1alpha1.OnPremAIGatewayConfigurationPushFailedReason),
			strings.Join(failures, "; "),
		)
	}
	return nil
}
