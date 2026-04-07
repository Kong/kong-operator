package mcpserver

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
)

// MCPServerCPReconciler reconciles a KonnectGatewayControlPlane object.
type MCPServerCPReconciler struct {
	client.Client

	ControllerOptions controller.Options
	LoggingMode       logging.Mode
	SignalManager     *SignalManager
	SdkFactory        sdkops.SDKFactory
}

// SetupWithManager sets up the controller with the Manager.
func (r *MCPServerCPReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.SignalManager.run(ctx)
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.ControllerOptions).
		For(&konnectv1alpha1.KonnectGatewayControlPlane{}).
		Complete(r)
}

// Reconcile reconciles the KonnectGatewayControlPlane resource.
func (r *MCPServerCPReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "mcp-cp", r.LoggingMode)

	var controlPlane konnectv1alpha1.KonnectGatewayControlPlane
	if err := r.Get(ctx, req.NamespacedName, &controlPlane); err != nil {
		if apierrors.IsNotFound(err) {
			r.SignalManager.EmitControlPlaneEvent(ctx, CPEvent{
				Type: EventTypeDeregister,
				ControlPlane: &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      req.Name,
						Namespace: req.Namespace,
					},
				},
			})
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info(logger, "reconciling KonnectGatewayControlPlane", "namespace", controlPlane.Namespace, "name", controlPlane.Name)

	// If the KonnectID is not set, we have nothing to signal, so we can skip emitting an event.
	if controlPlane.GetKonnectID() == "" {
		return ctrl.Result{}, nil
	}

	authRef := controlPlane.GetKonnectAPIAuthConfigurationRef()
	apiAuthRef := types.NamespacedName{
		Name:      authRef.Name,
		Namespace: controlPlane.Namespace,
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	err := r.Get(ctx, apiAuthRef, &apiAuth)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get APIAuth ref for %s: %w", client.ObjectKeyFromObject(&controlPlane), err)
	}

	token, err := konnect.GetTokenFromKonnectAPIAuthConfiguration(ctx, r.Client, &apiAuth)
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, r.Client, &apiAuth,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}
	srv, err := server.NewServer[konnectv1alpha1.KonnectGatewayControlPlane](apiAuth.Status.ServerURL)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to parse server URL %q: %w", apiAuth.Status.ServerURL, err)
	}
	konnectClient := r.SdkFactory.NewKonnectSDK(srv, sdkops.SDKToken(token))

	r.SignalManager.EmitControlPlaneEvent(ctx, CPEvent{
		Type:          EventTypeRegister,
		KonnectClient: konnectClient,
		ControlPlane:  &controlPlane,
	})

	return ctrl.Result{}, nil
}
