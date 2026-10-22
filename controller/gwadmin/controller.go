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

// Package gwadmin contains the controller deploying gw-admin, the on-prem
// control plane of the data planes that do not use Konnect, and managing its
// certificates.
package gwadmin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
	"github.com/kong/kong-operator/v2/controller/pkg/hpa"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ControllerName is the name of the GWAdmin controller.
const ControllerName = "gwadmin"

// Reconciler reconciles a GWAdmin object.
type Reconciler struct {
	client.Client

	// LoggingMode controls the format of log output.
	LoggingMode logging.Mode

	// ClusterDomain is the cluster DNS domain, added to the names the server
	// certificates are issued for.
	ClusterDomain string

	// SecretLabelSelector is the label the operator's Secret cache selects
	// on; the Secrets of the GWAdmin carry it so the operator sees them.
	SecretLabelSelector string

	// OperatorNamespace is the namespace Kong Operator runs in. It holds the
	// Secrets of the GWAdmins only Kong Operator uses: the keys of their
	// certificate authorities and the operator client certificate.
	OperatorNamespace string

	// CATTL is the validity of the certificate authorities Kong Operator
	// creates. Defaults to consts.GWAdminCATTL.
	CATTL time.Duration

	// CertTTL is the validity of the certificates Kong Operator issues.
	// Defaults to consts.GWAdminLeafTTL.
	CertTTL time.Duration

	// TypeConverter is injected via the TypeConverterProvider at controller
	// registration time. It is used for both diff-before-apply and
	// structured-merge-diff based PodTemplateSpec merging.
	TypeConverter managedfields.TypeConverter

	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time

	eventRecorder events.EventRecorder
}

func (r *Reconciler) clock() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reconciler) caTTL() time.Duration {
	if r.CATTL > 0 {
		return r.CATTL
	}
	return consts.GWAdminCATTL
}

func (r *Reconciler) certTTL() time.Duration {
	if r.CertTTL > 0 {
		return r.CertTTL
	}
	return consts.GWAdminLeafTTL
}

// event records an event on the GWAdmin.
func (r *Reconciler) event(gw *operatorv1alpha1.GWAdmin, eventType, reason, action, note string, args ...any) {
	if r.eventRecorder != nil {
		r.eventRecorder.Eventf(gw, nil, eventType, reason, action, note, args...)
	}
}

// IndexFieldGWAdminOnSecret is the index field for GWAdmin -> Secret: the
// Secrets a GWAdmin references, and those it needs for itself.
const IndexFieldGWAdminOnSecret = "gwAdminSecret" //nolint:gosec

// IndexOptions returns the index options the GWAdmin controller needs.
func IndexOptions() []index.Option {
	return []index.Option{{
		Object:         &operatorv1alpha1.GWAdmin{},
		Field:          IndexFieldGWAdminOnSecret,
		ExtractValueFn: secretIndexValues,
	}}
}

// secretIndexValues returns the Secrets the GWAdmin references or owns, as
// namespace/name. Owned names are included so that a GWAdmin blocked by
// someone else's Secret under one of them is reconciled once it's gone.
func secretIndexValues(obj client.Object) []string {
	gw, ok := obj.(*operatorv1alpha1.GWAdmin)
	if !ok {
		return nil
	}
	names := ownedSecretNames(gw)
	if isManualCA(gw) {
		names = append(names, gw.Spec.TLS.ClientCA.SecretRef.Name)
	}
	if isManualCP(gw) {
		names = append(names, gw.Spec.TLS.CP.SecretRef.Name)
	}
	values := make([]string, 0, len(names))
	for _, n := range names {
		values = append(values, gw.Namespace+"/"+n)
	}
	return values
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	if r.OperatorNamespace == "" {
		return errors.New("the GWAdmin controller needs the operator namespace")
	}
	r.eventRecorder = mgr.GetEventRecorder(ControllerName)
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.GWAdmin{}).
		Owns(&appsv1.Deployment{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		// The user's CA and cp certificate Secrets, and Secrets taken by
		// someone else under a name the GWAdmin needs.
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(shareddataplane.EnqueueDataPlanesByIndex(
			mgr.GetClient(),
			func() client.ObjectList { return &operatorv1alpha1.GWAdminList{} },
			IndexFieldGWAdminOnSecret,
			operatorv1alpha1.GWAdminKind,
			"Secret",
		))).
		// The Secrets of a GWAdmin in the operator namespace, which no owner
		// reference can point to.
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(enqueueForPrivateSecret)).
		// The data plane Deployments, in any namespace: their rollouts gate
		// the rotations.
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(enqueueForDataPlane)).
		// The grants deciding which namespaces data planes count in.
		Watches(&configurationv1alpha1.KongReferenceGrant{}, handler.EnqueueRequestsFromMapFunc(r.enqueueForGrant)).
		Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile moves the current state of a GWAdmin toward the desired state.
func (r *Reconciler) Reconcile(ctx context.Context, gw *operatorv1alpha1.GWAdmin) (res ctrl.Result, err error) {
	// Owner references garbage collect everything the GWAdmin owns in its
	// namespace, the finalizer what it keeps in the operator namespace.
	if !gw.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.cleanup(ctx, gw)
	}
	logger := log.GetLogger(ctx, ControllerName, r.LoggingMode)
	log.Trace(logger, "reconciling GWAdmin resource")

	if !controllerutil.ContainsFinalizer(gw, consts.GWAdminCleanupFinalizer) {
		base := gw.DeepCopy()
		controllerutil.AddFinalizer(gw, consts.GWAdminCleanupFinalizer)
		if err := r.Patch(ctx, gw, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to add the finalizer of GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
		}
	}

	oldStatus := gw.Status.DeepCopy()
	defer func() {
		r.warnOnFailedConditions(gw, oldStatus)
		err = errors.Join(err, r.patchStatus(ctx, gw, oldStatus))
	}()

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: gw.Namespace, Name: gw.Name}, deployment); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to get Deployment for GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
		}
		deployment = nil
	}

	// Only the GWAdmin's own Deployment runs its pods: a foreign one is
	// reported below, and is no peer to wait for.
	ownDeployment := deployment
	if deployment != nil && !metav1.IsControlledBy(deployment, gw) {
		ownDeployment = nil
	}
	material, tlsErr := r.ensureTLS(ctx, gw, ownDeployment)
	switch {
	case tlsErr == nil:
	case errors.Is(tlsErr, errCacheStale):
		log.Debug(logger, "waiting for the cache to catch up with a Secret")
		return ctrl.Result{RequeueAfter: time.Second}, nil
	case errors.Is(tlsErr, errStop):
		log.Debug(logger, "TLS material can't be provisioned", "reason", certificateMessageOf(gw))
		// The pods keep running the material in place while the user fixes
		// the reason: keep applying the rest of the spec, unless the pods
		// couldn't mount it.
		if !material.mountable() {
			setReadyCondition(gw, false, operatorv1alpha1.GWAdminReasonDependenciesNotReady, certificateMessageOf(gw))
			return ctrl.Result{RequeueAfter: stopRequeue(tlsErr)}, nil
		}
	default:
		// A transient failure: the conditions keep describing the last
		// known state while the reconcile is retried.
		return ctrl.Result{}, tlsErr
	}
	setStatus(gw, r.ClusterDomain)

	// A Deployment, Service or HorizontalPodAutoscaler taken by someone else
	// under a name the GWAdmin needs would be taken over by the forced apply
	// below, and garbage collected along with the GWAdmin.
	if conflict, err := r.findForeignObject(ctx, gw, deployment); err != nil {
		return ctrl.Result{}, err
	} else if conflict != "" {
		setReadyCondition(gw, false, operatorv1alpha1.GWAdminReasonResourceConflict, conflict)
		return ctrl.Result{RequeueAfter: conflictRequeue}, nil
	}

	// failedToApply reports a failure to apply an owned object in the Ready
	// condition, so that it doesn't keep a stale value while the reconcile
	// retries.
	failedToApply := func(err error) (ctrl.Result, error) {
		setReadyCondition(gw, false, operatorv1alpha1.GWAdminReasonFailedToApply, err.Error())
		return ctrl.Result{}, err
	}

	apiSvc := generateAPIService(gw)
	if _, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, apiSvc, controllerpkgssa.FieldManager); err != nil {
		return failedToApply(fmt.Errorf("failed to apply Service %s: %w", apiSvc.GetName(), err))
	}
	cpSvc, err := buildCPService(r.TypeConverter, gw)
	if err != nil {
		return failedToApply(err)
	}
	if _, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, cpSvc, controllerpkgssa.FieldManager); err != nil {
		return failedToApply(fmt.Errorf("failed to apply Service %s: %w", cpSvc.GetName(), err))
	}

	desired, err := buildDeployment(logger, r.TypeConverter, gw, material)
	if err != nil {
		return failedToApply(err)
	}
	// With horizontal scaling the HPA owns the replica count: keep the current
	// value instead of applying minReplicas, which would revert every scale-up,
	// and once the HPA has scaled the Deployment, do not apply it at all.
	desiredReplicas, err := hpa.DesiredDeploymentReplicas(ctx, r.Client, gw, client.ObjectKeyFromObject(desired))
	if err != nil {
		return failedToApply(fmt.Errorf("failed to resolve Deployment replicas for GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err))
	}
	if desiredReplicas == nil {
		unstructured.RemoveNestedField(desired.Object, "spec", "replicas")
	} else if err := unstructured.SetNestedField(desired.Object, int64(*desiredReplicas), "spec", "replicas"); err != nil {
		return failedToApply(fmt.Errorf("failed to set Deployment replicas for GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err))
	}
	applied, err := controllerpkgssa.ApplyIfChanged(ctx, logger, r.Client, r.TypeConverter, desired, controllerpkgssa.FieldManager)
	if err != nil {
		return failedToApply(fmt.Errorf("failed to apply Deployment for GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err))
	}

	if _, err := hpa.Ensure(ctx, r.Client, logger, gw, gw.Name); err != nil {
		return failedToApply(err)
	}

	// The pods only turn Ready once Kong Operator provisions them, so Ready
	// only waits for the Deployment to observe the applied spec.
	certificates, _ := k8sutils.GetCondition(kcfgconsts.ConditionType(operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned), gw)
	switch {
	case tlsErr != nil:
		setReadyCondition(gw, false, operatorv1alpha1.GWAdminReasonDependenciesNotReady, certificates.Message)
		return ctrl.Result{RequeueAfter: stopRequeue(tlsErr)}, nil
	case certificates.Status == metav1.ConditionFalse:
		setReadyCondition(gw, false, operatorv1alpha1.GWAdminReasonDependenciesNotReady, certificates.Message)
	case deployment == nil || applied != op.Noop || deployment.Status.ObservedGeneration < deployment.Generation:
		setReadyCondition(gw, false, operatorv1alpha1.GWAdminReasonWaitingToBecomeReady, "Waiting for the Deployment to observe its spec")
	default:
		setReadyCondition(gw, true, operatorv1alpha1.GWAdminReasonReady, "")
	}

	log.Debug(logger, "reconciliation complete for GWAdmin resource")
	return ctrl.Result{RequeueAfter: material.requeueAfter}, nil
}

// certificateMessageOf returns the message of the CertificateProvisioned
// condition of the GWAdmin.
func certificateMessageOf(gw *operatorv1alpha1.GWAdmin) string {
	c, _ := k8sutils.GetCondition(kcfgconsts.ConditionType(operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned), gw)
	return c.Message
}

// warnOnFailedConditions records a warning event when a condition turns
// False, or turns False for another reason. Conditions only restating
// another one, or reporting progress, are left out.
func (r *Reconciler) warnOnFailedConditions(gw *operatorv1alpha1.GWAdmin, old *operatorv1alpha1.GWAdminStatus) {
	quiet := []string{
		operatorv1alpha1.GWAdminReasonDependenciesNotReady,
		operatorv1alpha1.GWAdminReasonWaitingToBecomeReady,
		operatorv1alpha1.GWAdminReasonPending,
	}
	for _, t := range []string{operatorv1alpha1.GWAdminConditionTypeCertificateProvisioned, operatorv1alpha1.GWAdminConditionTypeReady} {
		c, ok := k8sutils.GetCondition(kcfgconsts.ConditionType(t), gw)
		if !ok || c.Status != metav1.ConditionFalse || slices.Contains(quiet, c.Reason) {
			continue
		}
		if i := slices.IndexFunc(old.Conditions, func(o metav1.Condition) bool { return o.Type == t }); i >= 0 &&
			old.Conditions[i].Status == metav1.ConditionFalse && old.Conditions[i].Reason == c.Reason {
			continue
		}
		r.event(gw, corev1.EventTypeWarning, c.Reason, "Reconcile", "%s", c.Message)
	}
}

// conflictRequeue is the delay until a GWAdmin blocked by a foreign object
// is reconciled again: the objects of others are not all watched, so their
// removal doesn't always trigger a reconcile.
const conflictRequeue = time.Minute

// findForeignObject returns a message describing the first Deployment,
// Service or HorizontalPodAutoscaler the GWAdmin needs that exists and is not
// controlled by it, or "" when there is none. deployment is the current
// Deployment named after the GWAdmin, or nil.
func (r *Reconciler) findForeignObject(
	ctx context.Context,
	gw *operatorv1alpha1.GWAdmin,
	deployment *appsv1.Deployment,
) (string, error) {
	if deployment != nil && !metav1.IsControlledBy(deployment, gw) {
		return foreignObjectMessage(gw, "Deployment", deployment), nil
	}
	type named struct {
		kind string
		obj  client.Object
		name string
	}
	others := []named{
		{"Service", &corev1.Service{}, apiServiceName(gw)},
		{"Service", &corev1.Service{}, cpServiceName(gw)},
	}
	if gw.GetHorizontalScaling() != nil {
		others = append(others, named{"HorizontalPodAutoscaler", &autoscalingv2.HorizontalPodAutoscaler{}, gw.Name})
	}
	for _, o := range others {
		if err := r.Get(ctx, types.NamespacedName{Namespace: gw.Namespace, Name: o.name}, o.obj); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", fmt.Errorf("failed to get %s %s/%s: %w", o.kind, gw.Namespace, o.name, err)
		}
		if !metav1.IsControlledBy(o.obj, gw) {
			return foreignObjectMessage(gw, o.kind, o.obj), nil
		}
	}
	return "", nil
}

// foreignObjectMessage describes an object the GWAdmin needs that it doesn't
// control.
func foreignObjectMessage(gw *operatorv1alpha1.GWAdmin, kind string, obj client.Object) string {
	if ownedByPredecessor(obj, gw) {
		return fmt.Sprintf("%s %q is still owned by a deleted GWAdmin named %q: waiting for the garbage collector to delete it",
			kind, obj.GetName(), gw.Name)
	}
	return fmt.Sprintf("%s %q already exists and is not managed by this GWAdmin", kind, obj.GetName())
}

// ownedByPredecessor reports whether obj is controlled by a GWAdmin with the
// name of gw but another UID: one deleted, whose objects the garbage
// collector didn't delete yet, e.g. when the GWAdmin was recreated.
func ownedByPredecessor(obj client.Object, gw *operatorv1alpha1.GWAdmin) bool {
	ref := metav1.GetControllerOf(obj)
	return ref != nil && ref.Kind == operatorv1alpha1.GWAdminKind && ref.Name == gw.Name && ref.UID != gw.UID
}

// enqueueForPrivateSecret maps a Secret of a GWAdmin in the operator
// namespace to the GWAdmin.
func enqueueForPrivateSecret(_ context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	if labels[consts.GWAdminUIDLabel] == "" || labels[consts.GatewayOperatorManagedByNamespaceLabel] == "" {
		return nil
	}
	return []reconcile.Request{{
		Namespace: labels[consts.GatewayOperatorManagedByNamespaceLabel],
		Name:      labels[consts.GatewayOperatorManagedByNameLabel],
	}}
}

// enqueueForDataPlane maps a data plane Deployment to the GWAdmin its
// labels name.
func enqueueForDataPlane(_ context.Context, obj client.Object) []reconcile.Request {
	labels := obj.GetLabels()
	if labels[consts.GWAdminDataPlaneNamespaceLabel] == "" || labels[consts.GWAdminDataPlaneNameLabel] == "" {
		return nil
	}
	return []reconcile.Request{{
		Namespace: labels[consts.GWAdminDataPlaneNamespaceLabel],
		Name:      labels[consts.GWAdminDataPlaneNameLabel],
	}}
}

// enqueueForGrant maps a KongReferenceGrant to the GWAdmins of its
// namespace: it decides which namespaces their data planes count in.
func (r *Reconciler) enqueueForGrant(ctx context.Context, obj client.Object) []reconcile.Request {
	keys := gwadminref.OfGrant(ctx, r.Client, obj)
	reqs := make([]reconcile.Request, 0, len(keys))
	for _, key := range keys {
		reqs = append(reqs, reconcile.Request{NamespacedName: key})
	}
	return reqs
}

// cleanup deletes the Secrets the GWAdmin keeps in the operator namespace,
// then lets it go.
func (r *Reconciler) cleanup(ctx context.Context, gw *operatorv1alpha1.GWAdmin) error {
	if !controllerutil.ContainsFinalizer(gw, consts.GWAdminCleanupFinalizer) {
		return nil
	}
	// Named after the UID of the GWAdmin, they are the GWAdmin's: they are
	// deleted without reading them, so that a copy the cache doesn't see,
	// e.g. one that lost its labels, is not left behind with its key.
	for _, name := range privateSecretNames(gw) {
		err := r.Delete(ctx, &corev1.Secret{Namespace: r.OperatorNamespace, Name: name})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete Secret %s/%s: %w", r.OperatorNamespace, name, err)
		}
	}
	base := gw.DeepCopy()
	controllerutil.RemoveFinalizer(gw, consts.GWAdminCleanupFinalizer)
	if err := r.Patch(ctx, gw, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil &&
		!apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to remove the finalizer of GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
	}
	return nil
}

// patchStatus patches the GWAdmin status when it changed. Unlike the
// DataPlane controllers, which apply their status, it merge patches it: the
// GWAdmin controller is the only writer of the status, and the
// gateway-operator.konghq.com group isn't served by the shared SSA type
// converter.
func (r *Reconciler) patchStatus(ctx context.Context, gw *operatorv1alpha1.GWAdmin, old *operatorv1alpha1.GWAdminStatus) error {
	if equality.Semantic.DeepEqual(*old, gw.Status) {
		return nil
	}
	base := gw.DeepCopy()
	base.Status = *old
	if err := r.Status().Patch(ctx, gw, client.MergeFrom(base)); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to patch status of GWAdmin %s/%s: %w", gw.Namespace, gw.Name, err)
	}
	return nil
}
