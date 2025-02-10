package konnect

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/pkg/log"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	k8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// KongCredentialTypeBasicAuth is the type of basic-auth credential, it's used
	// as the value for konghq.com/credential label.
	KongCredentialTypeBasicAuth = "basic-auth"
)

// KongCredentialSecretReconciler reconciles a KongPlugin object.
type KongCredentialSecretReconciler struct {
	developmentMode bool
	client          client.Client
	scheme          *runtime.Scheme
}

// NewKongCredentialSecretReconciler creates a new KongCredentialSecretReconciler.
func NewKongCredentialSecretReconciler(
	developmentMode bool,
	client client.Client,
	scheme *runtime.Scheme,
) *KongCredentialSecretReconciler {
	return &KongCredentialSecretReconciler{
		developmentMode: developmentMode,
		client:          client,
		scheme:          scheme,
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
			handler.EnqueueRequestsFromMapFunc(enqueueSecretsForKongConsumer),
			builder.WithPredicates(
				predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1.KongConsumer]),
			),
		).
		// NOTE: We use MatchEveryOwner because we set both the KongConsumer and
		// the Secret holding the credentials as owners.
		// The KongConsumer is set for obvious reasons, when that's deleted we want
		// to delete the managed credential to be deleted as well.
		// The Secret is set as owner because we want to trigger the reconciliation
		// of the secret (this controller) so that the managed credential is enforced
		// (e.g. recreated).
		Owns(&configurationv1alpha1.KongCredentialBasicAuth{}, builder.MatchEveryOwner).
		// TODO: add more credentials types support.
		Complete(r)
}

func enqueueSecretsForKongConsumer(ctx context.Context, obj client.Object) []reconcile.Request {
	consumer, ok := obj.(*configurationv1.KongConsumer)
	if !ok {
		return nil
	}

	ret := make([]ctrl.Request, 0, len(consumer.Credentials))
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
			if cpRef != nil &&
				cpRef.Type == configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
				return true
			}
		}
		return false
	}
}

// Reconcile reconciles a Secrets that are used as Consumers credentials.
func (r *KongCredentialSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var secret corev1.Secret
	if err := r.client.Get(ctx, req.NamespacedName, &secret); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger := log.GetLogger(ctx, "Secret", r.developmentMode)
	log.Debug(logger, "reconciling")

	if !secret.GetDeletionTimestamp().IsZero() {
		log.Debug(logger, "secret is being deleted")
		return ctrl.Result{}, nil
	}

	cl := client.NewNamespacedClient(r.client, secret.Namespace)

	credType, err := extractKongCredentialType(&secret)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Validate the Secret using the credential type label.
	if err := validateSecret(&secret, credType); err != nil {
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

	// If there are no Consumers that use the Secret then remove all the Credentials that use it.
	if len(kongConsumerList.Items) == 0 {
		if err := cl.DeleteAllOf(ctx, &corev1.Secret{},
			client.MatchingFields{
				IndexFieldKongConsumerReferencesSecrets: secret.Name,
			},
		); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed deleting Secret: %w", err)
		}
		return ctrl.Result{}, nil
	}

	for _, kongConsumer := range kongConsumerList.Items {
		if res, err := r.handleConsumerUsingCredentialSecret(ctx, &secret, credType, &kongConsumer); err != nil || !res.IsZero() {
			return ctrl.Result{}, err
		} else if !res.IsZero() {
			return res, nil
		}
	}

	return ctrl.Result{}, nil
}

const (
	// CredentialTypeLabel is the label key for the credential type.
	CredentialTypeLabel = "konghq.com/credential" //nolint:gosec
)

func ensureExistingCredential[
	T constraints.SupportedCredentialType,
	TPtr constraints.KongCredential[T],
](
	ctx context.Context,
	cl client.Client,
	cred TPtr,
	secret *corev1.Secret,
	consumer *configurationv1.KongConsumer,
) (ctrl.Result, error) {
	var update bool

	switch cred := any(cred).(type) {
	case *configurationv1alpha1.KongCredentialBasicAuth:
		if cred.Spec.ConsumerRef.Name != consumer.Name ||
			cred.Spec.Password != string(secret.Data[corev1.BasicAuthPasswordKey]) ||
			cred.Spec.Username != string(secret.Data[corev1.BasicAuthUsernameKey]) {

			cred.Spec.ConsumerRef.Name = consumer.Name
			setKongCredentialBasicAuthSpec(&cred.Spec, secret)
			update = true
		}

	default:
		// NOTE: Shouldn't happen.
		panic(fmt.Sprintf("unsupported credential type %T", cred))
	}

	if update {
		if err := cl.Update(ctx, cred); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{
					Requeue: true,
				}, nil
			}
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// ensureCredentialExists creates the credential based on the provided secret
// and credType.
func (r *KongCredentialSecretReconciler) ensureCredentialExists(
	ctx context.Context,
	secret *corev1.Secret,
	credType string,
	kongConsumer *configurationv1.KongConsumer,
) error {
	nn := types.NamespacedName{
		Namespace: secret.Namespace,
		Name:      secret.Name,
	}

	var cred client.Object
	switch credType {
	case KongCredentialTypeBasicAuth:
		cred = secretToKongCredentialBasicAuth(secret, kongConsumer)

	default:
		return fmt.Errorf("Secret %s used as credential, but has unsupported type %s",
			nn, credType,
		)
	}

	err := controllerutil.SetControllerReference(kongConsumer, cred, r.scheme)
	if err != nil {
		return err
	}
	// Set the secret as owner too so that deletion (or changes) of the credential
	// triggers the reconciliation of the secret in this controller.
	err = controllerutil.SetOwnerReference(secret, cred, r.scheme)
	if err != nil {
		return err
	}
	if err := r.client.Create(ctx, cred); err != nil {
		return err
	}

	return nil
}

// extractKongCredentialType returns the credential type of a Secret extracted from
// the konghq.com/credential label.
// If the label is not present it returns an error.
func extractKongCredentialType(secret *corev1.Secret) (string, error) {
	credType, ok := secret.Labels[CredentialTypeLabel]
	if !ok {
		return "", fmt.Errorf("Secret %s used as credential, but lacks %s label",
			client.ObjectKeyFromObject(secret), CredentialTypeLabel,
		)
	}
	return credType, nil
}

func validateSecret(
	s *corev1.Secret,
	credType string,
) error {
	nn := client.ObjectKeyFromObject(s)

	switch credType {
	case KongCredentialTypeBasicAuth:
		if err := validateSecretForKongCredentialBasicAuth(s); err != nil {
			return err
		}
	default:
		return fmt.Errorf("Secret %s used as credential, but has unsupported type %s",
			nn, credType,
		)
	}
	return nil
}

func validateSecretForKongCredentialBasicAuth(s *corev1.Secret) error {
	if _, ok := s.Data[corev1.BasicAuthUsernameKey]; !ok {
		return fmt.Errorf("Secret %s used as basic-auth credential, but lacks %s key",
			client.ObjectKeyFromObject(s), corev1.BasicAuthUsernameKey,
		)
	}
	if _, ok := s.Data[corev1.BasicAuthPasswordKey]; !ok {
		return fmt.Errorf("Secret %s used as basic-auth credential, but lacks %s key",
			client.ObjectKeyFromObject(s), corev1.BasicAuthPasswordKey,
		)
	}
	return nil
}

func secretToKongCredentialBasicAuth(
	s *corev1.Secret, k *configurationv1.KongConsumer,
) *configurationv1alpha1.KongCredentialBasicAuth {
	cred := &configurationv1alpha1.KongCredentialBasicAuth{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: k.Name + "-",
			Namespace:    k.Namespace,
		},
		Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: k.Name,
			},
		},
	}
	setKongCredentialBasicAuthSpec(&cred.Spec, s)
	return cred
}

func setKongCredentialBasicAuthSpec(
	spec *configurationv1alpha1.KongCredentialBasicAuthSpec, s *corev1.Secret,
) {
	spec.Username = string(s.Data[corev1.BasicAuthUsernameKey])
	spec.Password = string(s.Data[corev1.BasicAuthPasswordKey])
}

func (r KongCredentialSecretReconciler) handleConsumerUsingCredentialSecret(
	ctx context.Context,
	s *corev1.Secret,
	credType string,
	consumer *configurationv1.KongConsumer,
) (ctrl.Result, error) {
	// TODO: add more credentials types support.
	switch credType { //nolint:gocritic
	case KongCredentialTypeBasicAuth:
		var kongCredentialsBasicAuthList configurationv1alpha1.KongCredentialBasicAuthList
		err := r.client.List(
			ctx,
			&kongCredentialsBasicAuthList,
			client.MatchingFields{
				IndexFieldKongCredentialBasicAuthReferencesKongConsumer: consumer.Name,
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed listing KongCredentialBasicAuth: %w", err)
		}

		switch len(kongCredentialsBasicAuthList.Items) {
		case 0:
			if err := r.ensureCredentialExists(ctx, s, credType, consumer); err != nil {
				return ctrl.Result{}, err
			}

		case 1:
			credentialBasicAuth := kongCredentialsBasicAuthList.Items[0]
			if !credentialBasicAuth.DeletionTimestamp.IsZero() {
				return ctrl.Result{}, nil
			}

			res, err := ensureExistingCredential(ctx, r.client, &credentialBasicAuth, s, consumer)
			if err != nil || !res.IsZero() {
				return res, err
			}

		default:
			if err := k8sreduce.ReduceKongCredentials(ctx, r.client, kongCredentialsBasicAuthList.Items); err != nil {
				return ctrl.Result{}, err
			}
			// NOTE: requeue just in case we didn't perform any deletes.
			// Even if we'd check it here, we could still be running against stale cache.
			return ctrl.Result{
				Requeue: true,
			}, nil

		}
	}

	return ctrl.Result{}, nil
}
