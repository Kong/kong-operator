package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/controllers"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/logging"
	mgrconsts "github.com/kong/kong-operator/v2/ingress-controller/internal/manager/consts"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
)

// -----------------------------------------------------------------------------
// GatewayClass Controller - Vars & Consts
// -----------------------------------------------------------------------------

// SetControllerName is an alias for mgrconsts.SetControllerName.
var SetControllerName = mgrconsts.SetControllerName

// GetControllerName is an alias for mgrconsts.GetControllerName.
var GetControllerName = mgrconsts.GetControllerName

// -----------------------------------------------------------------------------
// GatewayClass Controller - Reconciler
// -----------------------------------------------------------------------------

// GatewayClassReconciler reconciles a GatewayClass object.
type GatewayClassReconciler struct {
	client.Client

	Log             logr.Logger
	Scheme          *runtime.Scheme
	DataplaneClient controllers.DataPlane

	CacheSyncTimeout time.Duration
}

// SetupWithManager sets up the controller with the Manager.
func (r *GatewayClassReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// set the controller name
		Named(strings.ToUpper(gatewayapi.V1GroupVersion) + "GatewayClass").
		// set the controller options
		WithOptions(controller.Options{
			LogConstructor: func(_ *reconcile.Request) logr.Logger {
				return r.Log
			},
			CacheSyncTimeout: r.CacheSyncTimeout,
		}).
		// watch GatewayClass objects
		//
		// No watch predicate here: every GatewayClass must be cached (not just
		// ones we control), so that a reassignment away from this controller's
		// ControllerName is observed and the cache stops treating its Gateways
		// as owned. Reconcile still gates the Accepted status write on
		// isGatewayClassControlled below.
		For(&gatewayapi.GatewayClass{}).
		Complete(r)
}

// -----------------------------------------------------------------------------
// GatewayClass Controller - Reconciliation
// -----------------------------------------------------------------------------

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gatewayclasses/status,verbs=get;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *GatewayClassReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("GatewayV1GatewayClass", req.NamespacedName)

	log.V(logging.DebugLevel).Info("Processing gatewayclass")

	gwc := new(gatewayapi.GatewayClass)
	if err := r.Get(ctx, req.NamespacedName, gwc); err != nil {
		if apierrors.IsNotFound(err) {
			gwc.Name = req.Name
			return ctrl.Result{}, r.DataplaneClient.DeleteObject(gwc)
		}
		return ctrl.Result{}, err
	}

	// The translator needs the GatewayClass cached (ControllerName) to evaluate
	// listener-attachment ownership the same way this controller does, regardless
	// of whether it has been accepted yet.
	if err := r.DataplaneClient.UpdateObject(gwc); err != nil {
		return ctrl.Result{}, err
	}

	if isGatewayClassControlled(gwc) {
		alreadyAccepted := util.CheckCondition(
			gwc.Status.Conditions,
			util.ConditionType(gatewayapi.GatewayClassConditionStatusAccepted),
			util.ConditionReason(gatewayapi.GatewayClassReasonAccepted),
			metav1.ConditionTrue,
			gwc.Generation,
		)

		if !alreadyAccepted {
			acceptedCondtion := metav1.Condition{
				Type:               string(gatewayapi.GatewayClassConditionStatusAccepted),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: gwc.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             string(gatewayapi.GatewayClassReasonAccepted),
				Message:            "the gatewayclass has been accepted by the controller",
			}
			oldGwc := gwc.DeepCopy()
			setGatewayClassCondition(gwc, acceptedCondtion)
			gwc = pruneGatewayClassStatusConds(gwc)

			if err := r.Status().Patch(ctx, gwc, client.MergeFrom(oldGwc)); err != nil {
				if apierrors.IsConflict(err) {
					log.V(logging.DebugLevel).Info("conflict found when updating GatewayClass, retrying")
					return ctrl.Result{
						RequeueAfter: ctrlconsts.RequeueWithoutBackoff,
					}, nil
				}
				return ctrl.Result{}, fmt.Errorf("failed to update gatewayclass status to accepted: %w", err)
			}

			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetLogger sets the logger.
func (r *GatewayClassReconciler) SetLogger(l logr.Logger) {
	r.Log = l
}

// -----------------------------------------------------------------------------
// GatewayClass Controller - Private
// -----------------------------------------------------------------------------

// pruneGatewayClassStatusConds cleans out old status conditions if the
// Gatewayclass currently has more status conditions set than the 8 maximum
// allowed by the Kubernetes API.
func pruneGatewayClassStatusConds(gwc *gatewayapi.GatewayClass) *gatewayapi.GatewayClass {
	if len(gwc.Status.Conditions) > maxConds {
		gwc.Status.Conditions = gwc.Status.Conditions[len(gwc.Status.Conditions)-maxConds:]
	}
	return gwc
}

// setGatewayClassCondition sets the condition with specified type in gatewayclass status
// to expected condition in newCondition.
// if the gatewayclass status does not contain a condition with that type, add one more condition.
// if the gatewayclass status contains condition(s) with the type, then replace with the new condition.
func setGatewayClassCondition(gwc *gatewayapi.GatewayClass, newCondition metav1.Condition) {
	newConditions := []metav1.Condition{}
	for _, condition := range gwc.Status.Conditions {
		if condition.Type != newCondition.Type {
			newConditions = append(newConditions, condition)
		}
	}
	newConditions = append(newConditions, newCondition)
	gwc.Status.Conditions = newConditions
}
