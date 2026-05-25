// Package poc provides a quick-and-dirty stub reconciler for
// KonnectEventGateway and KonnectEventDataPlaneCertificate that skips all
// auth/SDK calls and simply stamps the resources as Programmed. PoC use only.
package poc

import (
	"context"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/eventgateway/admin"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

const (
	stubStatusServerURL = "gw-admin.default.svc.cluster.local"
	stubReason          = "Programmed"
	stubMessage         = "Stamped by PoC stub reconciler"
)

// EventGatewayStubReconciler stamps KonnectEventGateway as Programmed without
// contacting Konnect.
type EventGatewayStubReconciler struct {
	Client      client.Client
	LoggingMode logging.Mode
	admin       *admin.Client
}

// SetupWithManager registers the reconciler with the controller manager.
func (r *EventGatewayStubReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konnectv1alpha1.KonnectEventGateway{}).
		Complete(r)
}

// Reconcile stamps Programmed=True with synthetic ID/ServerURL/OrgID.
func (r *EventGatewayStubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := &konnectv1alpha1.KonnectEventGateway{}
	if err := r.Client.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	r.admin = admin.New()
	if err := r.admin.EnsureEventGateway(ctx, admin.EventGateway{
		ID:          string(obj.UID),
		Name:        obj.Name,
		Description: "test gw",
	}); err != nil {
		return ctrl.Result{}, err
	}

	obj.Status.ID = string(obj.UID)
	obj.Status.ServerURL = stubStatusServerURL
	obj.Status.OrgID = string(obj.UID)
	apimeta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             stubReason,
		Message:            stubMessage,
		ObservedGeneration: obj.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	})
	if err := r.Client.Status().Update(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
