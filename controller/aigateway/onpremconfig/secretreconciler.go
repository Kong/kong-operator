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

package onpremconfig

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

// -----------------------------------------------------------------------------
// Secret - Reconciler
// -----------------------------------------------------------------------------

// sensitiveSecretRefGetter is implemented by the generated GetSensitiveDataSecretRefs
// accessors of the configuration entity kinds that carry secretRefs.
type sensitiveSecretRefGetter interface {
	client.Object
	GetSensitiveDataSecretRefs() []aiconfigurationv1alpha1.SensitiveDataSecretRef
}

// SecretReconciler watches the Secrets referenced by the configuration entities'
// secretRefs and feeds the ChangeNotifier when they change: the generated
// configuration-entity reconcilers only watch the entities themselves, so without
// this watcher an update to e.g. an AIGatewayAuthStrategy's oidc clientSecret Secret
// would not re-render the configuration until an unrelated entity event arrived.
//
// The reconciler runs on the instance's own manager, whose cache only holds Secrets
// matching the operator's Secret label selector in the instance's gateway namespace
// (see pkg/multiinstance/aigateway's cacheOpts), so only ingestible Secrets produce
// events and no label predicate is needed here.
//
// NOTE: cross-namespace secretRefs are rejected by the on-prem translation, so only
// Secrets in the entity's namespace are matched here. When cross-namespace secretRefs
// gain support (https://github.com/Kong/kong-operator/issues/5908), the reference
// namespace resolution must follow suit.
type SecretReconciler struct {
	client.Client

	Log            logr.Logger
	ChangeNotifier *changenotifier.ChangeNotifier
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("aiconfigurationv1alpha1Secret").
		For(&corev1.Secret{}).
		Complete(r)
}

// Reconcile notifies the ChangeNotifier for every configuration entity in the
// Secret's namespace whose secretRefs reference the Secret. The Secret itself is
// not fetched: on deletion the entity list still comes from the cache, so the
// referencing entities re-render and report the now-dangling reference.
func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.ChangeNotifier == nil {
		return ctrl.Result{}, nil
	}
	var errs []error
	for _, notify := range secretRefNotifierFuncs {
		if err := notify(ctx, r.Client, req.NamespacedName, r.ChangeNotifier, r.Log); err != nil {
			errs = append(errs, err)
		}
	}
	return ctrl.Result{}, errors.Join(errs...)
}

// secretRefNotifierFuncs holds one fan-out entry per configuration entity kind
// that both carries secretRefs and has an aiGatewayRef to notify through. Kinds
// without a generated GetSensitiveDataSecretRefs accessor have no Secrets to
// watch. AIGatewayConsumerCredential has the accessor but no aiGatewayRef — its
// credentials are rendered inside the parent consumer's document — so it is
// handled by notifyConsumersForCredentialSecrets instead.
var secretRefNotifierFuncs = []func(
	ctx context.Context,
	cl client.Client,
	secretNN types.NamespacedName,
	cn *changenotifier.ChangeNotifier,
	log logr.Logger,
) error{
	notifyEntitiesForSecret[aiconfigurationv1alpha1.AIGatewayAuthStrategyList],
	notifyEntitiesForSecret[aiconfigurationv1alpha1.AIGatewayCACertificateList],
	notifyEntitiesForSecret[aiconfigurationv1alpha1.AIGatewayCertificateList],
	notifyEntitiesForSecret[aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateList],
	notifyEntitiesForSecret[aiconfigurationv1alpha1.AIGatewayModelProviderList],
	notifyEntitiesForSecret[aiconfigurationv1alpha1.AIGatewayPolicyList],
	notifyConsumersForCredentialSecrets,
}

// notifyConsumersForCredentialSecrets notifies the parent AIGatewayConsumer of every
// AIGatewayConsumerCredential in the Secret's namespace whose secretRefs reference the
// Secret. The credential itself has no aiGatewayRef and no reconciler: its rendered
// form is embedded in the parent consumer's document, so the consumer is what must
// re-render and report the resolved (or dangling) credential Secret on its status.
//
// Like the SecretReconciler itself, cross-namespace secretRefs are not resolved here
// beyond the refNamespace matching rule: they are rejected by the on-prem translation,
// and when they gain support (https://github.com/Kong/kong-operator/issues/5908) the
// reference namespace resolution must follow suit.
func notifyConsumersForCredentialSecrets(
	ctx context.Context,
	cl client.Client,
	secretNN types.NamespacedName,
	cn *changenotifier.ChangeNotifier,
	log logr.Logger,
) error {
	var list aiconfigurationv1alpha1.AIGatewayConsumerCredentialList
	if err := cl.List(ctx, &list, client.InNamespace(secretNN.Namespace)); err != nil {
		log.Error(err, "Failed to list AIGatewayConsumerCredentials referencing a Secret",
			"secretNamespace", secretNN.Namespace, "secretName", secretNN.Name)
		return err
	}
	for i := range list.Items {
		cred := &list.Items[i]
		consumerRef := cred.Spec.AIGatewayConsumerRef
		if consumerRef.NamespacedRef == nil {
			continue
		}
		// The translation only renders credentials living in the consumer's namespace
		// (aigwCredentials lists via the index in the consumer's namespace), so a
		// credential pointing at a consumer in another namespace never becomes part of
		// that consumer's document: notifying it would only waste a fetch and a
		// notification.
		if refNS := consumerRef.NamespacedRef.Namespace; refNS != nil && *refNS != "" && *refNS != cred.GetNamespace() {
			continue
		}
		for _, secretRef := range cred.GetSensitiveDataSecretRefs() {
			// Same refNamespace resolution as notifyEntitiesForSecret: explicit
			// secretRef.Namespace wins, else the credential's namespace.
			refNamespace := cred.GetNamespace()
			if secretRef.Namespace != nil && *secretRef.Namespace != "" {
				refNamespace = *secretRef.Namespace
			}
			if secretRef.Name != secretNN.Name || refNamespace != secretNN.Namespace {
				continue
			}
			consumerNN := types.NamespacedName{
				Namespace: cred.GetNamespace(),
				Name:      consumerRef.NamespacedRef.Name,
			}
			if consumerRef.NamespacedRef.Namespace != nil && *consumerRef.NamespacedRef.Namespace != "" {
				consumerNN.Namespace = *consumerRef.NamespacedRef.Namespace
			}
			var consumer aiconfigurationv1alpha1.AIGatewayConsumer
			if err := cl.Get(ctx, consumerNN, &consumer); err != nil {
				// The consumer is gone: nothing to notify.
				if apierrors.IsNotFound(err) {
					break
				}
				log.Error(err, "Failed to fetch AIGatewayConsumer referenced by an AIGatewayConsumerCredential",
					"consumerNamespace", consumerNN.Namespace, "consumerName", consumerNN.Name)
				return err
			}
			ref := consumer.GetAIGatewayRef()
			if ref.NamespacedRef == nil || !ref.TargetsOnPremAIGateway() {
				break
			}
			parent := types.NamespacedName{
				Namespace: consumer.GetNamespace(),
				Name:      ref.NamespacedRef.Name,
			}
			if ref.NamespacedRef.Namespace != nil && *ref.NamespacedRef.Namespace != "" {
				parent.Namespace = *ref.NamespacedRef.Namespace
			}
			cn.NotifyChange(ctx, &parent, &consumer)
			break
		}
	}
	return nil
}

// notifyEntitiesForSecret lists the entities of one kind in the Secret's namespace and
// notifies the ChangeNotifier for every entity referencing the Secret, addressed to the
// OnPremAIGateway the entity targets. It returns the List error so that Reconcile can
// propagate it and the queue retries the Secret event.
func notifyEntitiesForSecret[
	TList interface {
		GetItems() []T
	},
	TListPtr interface {
		*TList
		client.ObjectList
		GetItems() []T
	},
	T any,
	TT interface {
		*T
		sensitiveSecretRefGetter
		aiGatewayRefGetter
	},
](
	ctx context.Context,
	cl client.Client,
	secretNN types.NamespacedName,
	cn *changenotifier.ChangeNotifier,
	log logr.Logger,
) error {
	var (
		l    TList
		lPtr TListPtr = &l
	)
	if err := cl.List(ctx, lPtr, client.InNamespace(secretNN.Namespace)); err != nil {
		log.Error(err, "Failed to list configuration entities referencing a Secret",
			"secretNamespace", secretNN.Namespace, "secretName", secretNN.Name)
		return err
	}
	items := lPtr.GetItems()
	for i := range items {
		ent := TT(&items[i])
		ref := ent.GetAIGatewayRef()
		if ref.NamespacedRef == nil || !ref.TargetsOnPremAIGateway() {
			continue
		}
		for _, secretRef := range ent.GetSensitiveDataSecretRefs() {
			// Match on the secretRef's resolved namespace rather than the entity's: the List
			// above is already scoped to the Secret's namespace, so comparing the entity's
			// namespace here would never filter anything.
			refNamespace := ent.GetNamespace()
			if secretRef.Namespace != nil && *secretRef.Namespace != "" {
				refNamespace = *secretRef.Namespace
			}
			if secretRef.Name != secretNN.Name || refNamespace != secretNN.Namespace {
				continue
			}
			parent := types.NamespacedName{
				Namespace: ent.GetNamespace(),
				Name:      ref.NamespacedRef.Name,
			}
			if ref.NamespacedRef.Namespace != nil && *ref.NamespacedRef.Namespace != "" {
				parent.Namespace = *ref.NamespacedRef.Namespace
			}
			cn.NotifyChange(ctx, &parent, ent)
			break
		}
	}
	return nil
}
