package kongplugininstallation

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	orascreds "oras.land/oras-go/v2/registry/remote/credentials"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/controller/kongplugininstallation/image"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/secrets/ref"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
)

const kindKongPluginInstallation = gatewayv1.Kind("KongPluginInstallation")

// Reconciler reconciles a KongPluginInstallation object.
type Reconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	DevelopmentMode bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	or := reconcile.AsReconciler[*v1alpha1.KongPluginInstallation](mgr.GetClient(), r)
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.KongPluginInstallation{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(
			predicate.Funcs{
				DeleteFunc: func(e event.DeleteEvent) bool {
					return true
				},
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return true
				},
			},
		)).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.listKongPluginInstallationsForSecret),
			builder.WithPredicates(
				predicate.NewPredicateFuncs(func(obj client.Object) bool {
					secret, ok := obj.(*corev1.Secret)
					if !ok {
						return false
					}
					return secret.Type == corev1.SecretTypeDockerConfigJson
				}),
			),
		).
		Watches(
			&gatewayv1beta1.ReferenceGrant{},
			handler.EnqueueRequestsFromMapFunc(r.listReferenceGrantsForKongPluginInstallation),
			builder.WithPredicates(
				ref.ReferenceGrantForSecretFrom(
					gatewayv1.Group(v1alpha1.SchemeGroupVersion.Group), kindKongPluginInstallation,
				),
			),
		).
		Complete(or)
}

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, kpi *v1alpha1.KongPluginInstallation) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "kongplugininstallation", r.DevelopmentMode)

	log.Trace(logger, "reconciling KongPluginInstallation resource")

	if err := setStatusConditionForKongPluginInstallation(
		ctx, r.Client, kpi, metav1.ConditionFalse, v1alpha1.KongPluginInstallationReasonPending, "fetching plugin is in progress",
	); err != nil {
		return ctrl.Result{}, err
	}

	log.Trace(logger, "managing KongPluginInstallation resource")
	var credentialsStore orascreds.Store
	if kpi.Spec.ImagePullSecretRef != nil {
		log.Trace(logger, "getting secret for KongPluginInstallation resource")
		kpiNamespace := gatewayv1.Namespace(kpi.Namespace)
		imagePullSecretRef := kpi.Spec.ImagePullSecretRef
		ref.EnsureNamespaceInSecretRef(imagePullSecretRef, kpiNamespace)
		if err := ref.DoesFieldReferenceCoreV1Secret(*imagePullSecretRef, "imagePullSecretRef"); err != nil {
			return ctrl.Result{}, setStatusConditionFailedForKongPluginInstallation(ctx, r.Client, kpi, err.Error())
		}
		whyNotGrantedMsg, isReferenceGranted, refErr := ref.CheckReferenceGrantForSecret(
			ctx, r.Client, kpi, *imagePullSecretRef,
		)
		if refErr != nil {
			return ctrl.Result{}, fmt.Errorf("failed to resolve reference: %w", refErr)
		}
		if !isReferenceGranted {
			return ctrl.Result{}, setStatusConditionFailedForKongPluginInstallation(ctx, r.Client, kpi, whyNotGrantedMsg)
		}

		secretNN := client.ObjectKey{
			Namespace: string(*imagePullSecretRef.Namespace),
			Name:      string(imagePullSecretRef.Name),
		}
		var secret corev1.Secret
		if err := r.Client.Get(
			ctx,
			secretNN,
			&secret,
		); err != nil {
			if k8serrors.IsNotFound(err) {
				return ctrl.Result{}, setStatusConditionFailedForKongPluginInstallation(ctx, r.Client, kpi, fmt.Sprintf("referenced Secret %q not found", secretNN))
			}
			return ctrl.Result{}, fmt.Errorf("something unexpected during fetching secret %s: %w", secretNN, err)
		}

		const requiredKey = ".dockerconfigjson"
		secretData, ok := secret.Data[requiredKey]
		if !ok {
			return ctrl.Result{}, setStatusConditionFailedForKongPluginInstallation(
				ctx, r.Client, kpi, fmt.Sprintf("can't parse secret %q - unexpected type, it should follow 'kubernetes.io/dockerconfigjson'", secretNN),
			)
		}
		var err error
		credentialsStore, err = orascreds.NewMemoryStoreFromDockerConfig(secretData)
		if err != nil {
			return ctrl.Result{}, setStatusConditionFailedForKongPluginInstallation(ctx, r.Client, kpi, fmt.Sprintf("can't parse secret: %q data: %s", secretNN, err))
		}
	}

	log.Trace(logger, "fetch plugin for KongPluginInstallation resource")
	plugin, err := image.FetchPlugin(ctx, kpi.Spec.Image, credentialsStore)
	if err != nil {
		return ctrl.Result{}, setStatusConditionFailedForKongPluginInstallation(ctx, r.Client, kpi, fmt.Sprintf("problem with the image: %q error: %s", kpi.Spec.Image, err))
	}

	cms, err := kubernetes.ListConfigMapsForOwner(ctx, r.Client, kpi.GetUID())
	if err != nil {
		return ctrl.Result{}, err
	}
	var cm corev1.ConfigMap
	switch len(cms) {
	case 0:
		if cmName := kpi.Status.UnderlyingConfigMapName; cmName != "" {
			cm.Name = cmName
		} else {
			cm.GenerateName = kpi.Name + "-"
		}
		resources.LabelObjectAsKongPluginInstallationManaged(&cm)
		resources.AnnotateConfigMapWithKongPluginInstallation(&cm, *kpi)
		cm.Namespace = kpi.Namespace
		cm.Data = plugin
		if err := ctrl.SetControllerReference(kpi, &cm, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Client.Create(ctx, &cm); err != nil {
			return ctrl.Result{}, err
		}
		kpi.Status.UnderlyingConfigMapName = cm.Name
	case 1:
		cm = cms[0]
		cm.Data = plugin
		if err := r.Client.Update(ctx, &cm); err != nil {
			return ctrl.Result{}, err
		}
	default:
		// It should never happen.
		return ctrl.Result{}, errors.New("unexpected error happened - more than one ConfigMap found")
	}

	return ctrl.Result{}, setStatusConditionForKongPluginInstallation(
		ctx, r.Client, kpi, metav1.ConditionTrue, v1alpha1.KongPluginInstallationReasonReady, "plugin successfully saved in cluster as ConfigMap",
	)
}

func (r *Reconciler) listKongPluginInstallationsForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	name, namespace := obj.GetName(), obj.GetNamespace()

	var kpiList v1alpha1.KongPluginInstallationList
	if err := r.List(ctx, &kpiList); err != nil {
		ctrllog.FromContext(ctx).Error(
			err,
			"failed to run map funcs for secrets",
		)
		return nil
	}

	var recs []reconcile.Request
	for _, kpi := range kpiList.Items {
		if kpi.Spec.ImagePullSecretRef == nil {
			continue
		}
		ref.EnsureNamespaceInSecretRef(kpi.Spec.ImagePullSecretRef, gatewayv1.Namespace(kpi.Namespace))
		if err := ref.DoesFieldReferenceCoreV1Secret(*kpi.Spec.ImagePullSecretRef, "imagePullSecretRef"); err != nil {
			continue
		}
		if string(*kpi.Spec.ImagePullSecretRef.Namespace) == namespace && string(kpi.Spec.ImagePullSecretRef.Name) == name {
			recs = append(recs, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&kpi),
			})
		}
	}
	return recs
}

func (r *Reconciler) listReferenceGrantsForKongPluginInstallation(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx)

	grant, ok := obj.(*gatewayv1beta1.ReferenceGrant)
	if !ok {
		logger.Error(
			fmt.Errorf("unexpected object type"),
			"ReferenceGrant watch predicate received unexpected object type",
			"expected", "*gatewayapi.ReferenceGrant", "found", reflect.TypeOf(obj),
		)
		return nil
	}
	var kpiList v1alpha1.KongPluginInstallationList
	if err := r.Client.List(ctx, &kpiList); err != nil {
		logger.Error(err, "Failed to list KongPluginInstallations in watch", "referencegrant", grant.Name)
		return nil
	}
	var recs []reconcile.Request
	for _, kpi := range kpiList.Items {
		if ref.IsReferenceGrantForObj(grant, &kpi) {
			recs = append(recs, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&kpi),
			})
		}
	}
	return recs
}

func setStatusConditionFailedForKongPluginInstallation(
	ctx context.Context, client client.Client, kpi *v1alpha1.KongPluginInstallation, msg string,
) error {
	return setStatusConditionForKongPluginInstallation(ctx, client, kpi, metav1.ConditionFalse, v1alpha1.KongPluginInstallationReasonFailed, msg)
}

func setStatusConditionForKongPluginInstallation(
	ctx context.Context, client client.Client, kpi *v1alpha1.KongPluginInstallation, conditionStatus metav1.ConditionStatus, reason v1alpha1.KongPluginInstallationConditionReason, msg string,
) error {
	status := metav1.Condition{
		Type:               string(v1alpha1.KongPluginInstallationConditionStatusAccepted),
		Status:             conditionStatus,
		ObservedGeneration: kpi.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             string(reason),
		Message:            msg,
	}
	_, index, found := lo.FindIndexOf(kpi.Status.Conditions, func(c metav1.Condition) bool {
		return c.Type == string(v1alpha1.KongPluginInstallationConditionStatusAccepted)
	})
	if found {
		// Nothing changed, condition doesn't need to be updated.
		if c := kpi.Status.Conditions[index]; c.Status == status.Status && c.Reason == status.Reason && c.Message == status.Message {
			return nil
		}
		kpi.Status.Conditions[index] = status
	} else {
		kpi.Status.Conditions = append(kpi.Status.Conditions, status)
	}
	return client.Status().Update(ctx, kpi)
}
