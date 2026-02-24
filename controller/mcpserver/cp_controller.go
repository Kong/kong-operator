package mcpserver

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
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
				Type: EventTypeUnset,
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

	konnectClient, err := r.buildKonnectClient(ctx, &controlPlane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build Konnect client for %s/%s: %w", controlPlane.Namespace, controlPlane.Name, err)
	}

	r.SignalManager.EmitControlPlaneEvent(ctx, CPEvent{
		Type:          EventTypeSet,
		KonnectClient: konnectClient,
		ControlPlane:  &controlPlane,
	})

	return ctrl.Result{}, nil
}

// buildKonnectClient constructs a Konnect SDK client for the given KonnectGatewayControlPlane
// by resolving its KonnectAPIAuthConfiguration reference.
func (r *MCPServerCPReconciler) buildKonnectClient(
	ctx context.Context,
	cp *konnectv1alpha1.KonnectGatewayControlPlane,
) (sdkops.SDKWrapper, error) {
	authRef := cp.GetKonnectAPIAuthConfigurationRef()

	nn := types.NamespacedName{
		Name:      authRef.Name,
		Namespace: cp.Namespace,
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	if err := r.Get(ctx, nn, &apiAuth); err != nil {
		return nil, fmt.Errorf("failed to get KonnectAPIAuthConfiguration %s: %w", nn, err)
	}

	token, err := tokenFromKonnectAPIAuth(ctx, r.Client, &apiAuth)
	if err != nil {
		return nil, fmt.Errorf("failed to get token from KonnectAPIAuthConfiguration %s: %w", nn, err)
	}

	srv, err := server.NewServer[konnectv1alpha1.KonnectGatewayControlPlane](apiAuth.Spec.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server URL %q: %w", apiAuth.Spec.ServerURL, err)
	}

	return r.SdkFactory.NewKonnectSDK(srv, sdkops.SDKToken(token)), nil
}

// tokenFromKonnectAPIAuth extracts the bearer token from a KonnectAPIAuthConfiguration,
// fetching the referenced Secret when the auth type is secretRef.
func tokenFromKonnectAPIAuth(
	ctx context.Context,
	cl client.Client,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
) (string, error) {
	switch apiAuth.Spec.Type {
	case konnectv1alpha1.KonnectAPIAuthTypeToken:
		return apiAuth.Spec.Token, nil
	case konnectv1alpha1.KonnectAPIAuthTypeSecretRef:
		nn := types.NamespacedName{
			Name:      apiAuth.Spec.SecretRef.Name,
			Namespace: apiAuth.Spec.SecretRef.Namespace,
		}
		if nn.Namespace == "" {
			nn.Namespace = apiAuth.Namespace
		}
		var secret corev1.Secret
		if err := cl.Get(ctx, nn, &secret); err != nil {
			return "", fmt.Errorf("failed to get Secret %s: %w", nn, err)
		}
		token, ok := secret.Data["token"]
		if !ok {
			return "", fmt.Errorf("secret %s does not have key 'token'", nn)
		}
		return string(token), nil
	default:
		return "", fmt.Errorf("unsupported KonnectAPIAuthConfiguration type %q", apiAuth.Spec.Type)
	}
}
