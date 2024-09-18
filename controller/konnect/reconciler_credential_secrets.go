package konnect

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/controller/pkg/log"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// KongCredentialTypeBasicAuth is the type of basic-auth credential.
	KongCredentialTypeBasicAuth = "basic-auth"
)

// KongCredentialSecretReconciler reconciles a KongPlugin object.
type KongCredentialSecretReconciler struct {
	developmentMode bool
	client          client.Client
}

// NewKongCredentialSecretReconciler creates a new KongCredentialSecretReconciler.
func NewKongCredentialSecretReconciler(
	developmentMode bool,
	client client.Client,
) *KongCredentialSecretReconciler {
	return &KongCredentialSecretReconciler{
		developmentMode: developmentMode,
		client:          client,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KongCredentialSecretReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("KongCredentialSecret").
		For(
			&corev1.Secret{},
			builder.WithPredicates(
				predicate.NewPredicateFuncs(
					secretIsUsedByConsumerAttachedToKonnectControlPlane(mgr.GetClient()),
				),
			),
		).
		Watches(&configurationv1.KongConsumer{},
			handler.EnqueueRequestsFromMapFunc(enqueueSecretForKongConsumer),
			builder.WithPredicates(
				predicate.NewPredicateFuncs(
					kongConsumerRefersToKonnectGatewayControlPlane,
				),
			),
		).
		Owns(&configurationv1alpha1.CredentialBasicAuth{}).
		// TODO: add more credentials types support.
		// https://github.com/Kong/gateway-operator/issues/619
		// https://github.com/Kong/gateway-operator/issues/620
		// https://github.com/Kong/gateway-operator/issues/621
		// https://github.com/Kong/gateway-operator/issues/622
		Complete(r)
}

func enqueueSecretForKongConsumer(ctx context.Context, obj client.Object) []reconcile.Request {
	consumer, ok := obj.(*configurationv1.KongConsumer)
	if !ok {
		return nil
	}

	var ret []ctrl.Request
	for _, secretName := range consumer.Credentials {
		ret = append(ret, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      secretName,
				Namespace: consumer.Namespace,
			},
		})
	}
	return ret
}

// secretIsUsedByConsumerAttachedToKonnectControlPlane returns true if the Secret
// is used by KongConsumer which refers to a KonnectGatewayControlPlane.
func secretIsUsedByConsumerAttachedToKonnectControlPlane(cl client.Client) func(obj client.Object) bool {
	return func(obj client.Object) bool {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			ctrllog.FromContext(context.Background()).Error(
				operatorerrors.ErrUnexpectedObject,
				"failed to run predicate function",
				"expected", "Secret", "found", reflect.TypeOf(obj),
			)
			return false
		}

		// List consumers using this Secret as credential.
		kongConsumerList := configurationv1.KongConsumerList{}
		err := cl.List(
			context.Background(),
			&kongConsumerList,
			client.MatchingFields{
				IndexFieldKongConsumerReferencesSecrets: secret.GetName(),
			},
		)
		if err != nil {
			return false
		}

		for _, kongConsumer := range kongConsumerList.Items {
			cpRef := kongConsumer.Spec.ControlPlaneRef
			if cpRef != nil && cpRef.Type == configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
				return true
			}
		}
		return false
	}
}

// Reconcile reconciles a Secrets that are used as Consumers credentials.
func (r *KongCredentialSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var (
		entityTypeName = "Secret"
		logger         = log.GetLogger(ctx, entityTypeName, r.developmentMode)
	)

	var secret corev1.Secret
	if err := r.client.Get(ctx, req.NamespacedName, &secret); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Debug(logger, "reconciling", secret)
	cl := client.NewNamespacedClient(r.client, secret.Namespace)

	credType, err := extractKongCredentialType(&secret)
	if err != nil {
		return ctrl.Result{}, err
	}

	// List consumers using this secret as credential.
	kongConsumerList := configurationv1.KongConsumerList{}
	err = cl.List(
		ctx,
		&kongConsumerList,
		client.MatchingFields{
			IndexFieldKongConsumerReferencesSecrets: secret.GetName(),
		},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed listing KongConsumers for Secret: %w", err)
	}

	switch len(kongConsumerList.Items) {
	case 0:
		// If there are no Consumers that use the Secret then remove all the Credentials that use it.

		// TODO: add more credentials types support.
		// https://github.com/Kong/gateway-operator/issues/619
		// https://github.com/Kong/gateway-operator/issues/620
		// https://github.com/Kong/gateway-operator/issues/621
		// https://github.com/Kong/gateway-operator/issues/622
		list := configurationv1alpha1.CredentialBasicAuthList{}
		err := cl.List(ctx, &list,
			client.MatchingFields{
				IndexFieldCredentialReferencesKongSecret: secret.Name,
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed deleting CredentialBasicAuths: %w", err)
		}
		for _, credential := range list.Items {
			if err = cl.Delete(ctx, &credential); err != nil {
				return ctrl.Result{}, err
			}
		}

	default:
		for _, kongConsumer := range kongConsumerList.Items {
			// TODO: add more credentials types support.
			// https://github.com/Kong/gateway-operator/issues/619
			// https://github.com/Kong/gateway-operator/issues/620
			// https://github.com/Kong/gateway-operator/issues/621
			// https://github.com/Kong/gateway-operator/issues/622
			switch credType { //nolint:gocritic
			case KongCredentialTypeBasicAuth:
				kongCredentialsBasicAuthList := configurationv1alpha1.CredentialBasicAuthList{}
				err := cl.List(
					ctx,
					&kongCredentialsBasicAuthList,
					client.MatchingFields{
						IndexFieldCredentialReferencesKongConsumer: kongConsumer.Name,
						IndexFieldCredentialReferencesKongSecret:   secret.Name,
					},
				)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed listing CredentialBasicAuth: %w", err)
				}

				switch len(kongCredentialsBasicAuthList.Items) {
				case 0:
					credentialBasicAuth := configurationv1alpha1.CredentialBasicAuth{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: kongConsumer.Name + "-",
							Namespace:    kongConsumer.Namespace,
						},
						Spec: configurationv1alpha1.CredentialBasicAuthSpec{
							ConsumerRef: corev1.LocalObjectReference{
								Name: kongConsumer.Name,
							},
							SecretRef: corev1.LocalObjectReference{
								Name: secret.Name,
							},
							// TODO: fill in the config
						},
					}
					err := controllerutil.SetControllerReference(&kongConsumer, &credentialBasicAuth, scheme.Get())
					if err != nil {
						return ctrl.Result{}, err
					}
					if err = cl.Create(ctx, &credentialBasicAuth); err != nil {
						return ctrl.Result{}, err
					}

				default:
					credentialBasicAuth := kongCredentialsBasicAuthList.Items[0]
					if !credentialBasicAuth.DeletionTimestamp.IsZero() {
						continue
					}

					err := controllerutil.SetControllerReference(&kongConsumer, &credentialBasicAuth, scheme.Get())
					if err != nil {
						return ctrl.Result{}, err
					}
					if credentialBasicAuth.Spec.ConsumerRef.Name != kongConsumer.Name ||
						credentialBasicAuth.Spec.SecretRef.Name != secret.Name {
						if err = cl.Update(ctx, &credentialBasicAuth); err != nil {
							if k8serrors.IsConflict(err) {
								return ctrl.Result{
									Requeue: true,
								}, nil
							}
							return ctrl.Result{}, err
						}
					}
				}
			}
		}
	}

	log.Debug(logger, "reconciliation completed", secret)
	return ctrl.Result{}, nil
}

const (
	// CredentialTypeLabel is the label key for the credential type.
	CredentialTypeLabel = "konghq.com/credential" //nolint:gosec
)

// extractKongCredentialType returns the credential type of a Secret or an error if no credential type is present.
// TODO(pmalek): consider migrating this into kubernetes-configuration
func extractKongCredentialType(secret *corev1.Secret) (string, error) {
	credType, ok := secret.Labels[CredentialTypeLabel]
	if !ok {
		return "", fmt.Errorf("Secret %s/%s used as credential, but lacks %s label",
			secret.Namespace, secret.Name, CredentialTypeLabel)
	}
	return credType, nil
}
