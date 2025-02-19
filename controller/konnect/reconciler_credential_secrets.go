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
	// KongCredentialTypeAPIKey is the type of api-key credential, it's used
	// as the value for konghq.com/credential label.
	KongCredentialTypeAPIKey = "key-auth"
)

const (
	// CredentialSecretKeyNameAPIKeyKey is the credential secret key name for API key type.
	CredentialSecretKeyNameAPIKeyKey = "key"
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
	ls := metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      SecretCredentialLabel,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}
	labelSelectorPredicate, err := predicate.LabelSelectorPredicate(ls)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("KongCredentialSecret").
		For(
			&corev1.Secret{},
			builder.WithPredicates(
				labelSelectorPredicate,
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
		Owns(&configurationv1alpha1.KongCredentialAPIKey{}, builder.MatchEveryOwner).
		// TODO: Add more credential types support.
		// TODO: https://github.com/Kong/gateway-operator/issues/1124
		// TODO: https://github.com/Kong/gateway-operator/issues/1125
		// TODO: https://github.com/Kong/gateway-operator/issues/1126
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
			if objHasControlPlaneRef(&kongConsumer) {
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

	switch len(kongConsumerList.Items) {
	case 0:
		// If there are no Consumers that use the Secret then remove all the managed
		// Credentials that use this Secret.
		if err := deleteAllCredentialsUsingSecret(ctx, cl, &secret, credType); err != nil {
			return ctrl.Result{}, err
		}

	default:
		for _, kongConsumer := range kongConsumerList.Items {
			if res, err := r.handleConsumerUsingCredentialSecret(ctx, &secret, credType, &kongConsumer); err != nil || !res.IsZero() {
				return ctrl.Result{}, err
			} else if !res.IsZero() {
				return res, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

const (
	// CredentialTypeLabel is the label key for the credential type.
	CredentialTypeLabel = "konghq.com/credential" //nolint:gosec
)

func deleteAllCredentialsUsingSecret(
	ctx context.Context,
	cl client.Client,
	secret *corev1.Secret,
	credType string,
) error {
	// TODO: Add more credential types support.
	// TODO: https://github.com/Kong/gateway-operator/issues/1124
	// TODO: https://github.com/Kong/gateway-operator/issues/1125
	// TODO: https://github.com/Kong/gateway-operator/issues/1126
	switch credType {

	// NOTE: To use DeleteAllOf() we need a selectable field added to the CRD.

	case KongCredentialTypeBasicAuth:
		var l configurationv1alpha1.KongCredentialBasicAuthList
		err := cl.List(ctx, &l,
			client.MatchingFields{
				IndexFieldKongCredentialBasicAuthReferencesSecret: secret.Name,
			},
		)
		if err != nil {
			return fmt.Errorf("failed listing unused KongCredentialBasicAuths: %w", err)
		}

		for _, cred := range l.Items {
			if err := cl.Delete(ctx, &cred); err != nil {
				return fmt.Errorf("failed deleting unused KongCredentialBasicAuth %s: %w",
					client.ObjectKeyFromObject(&cred), err,
				)
			}
		}

	case KongCredentialTypeAPIKey:
		var l configurationv1alpha1.KongCredentialAPIKeyList
		err := cl.List(ctx, &l,
			client.MatchingFields{
				IndexFieldKongCredentialAPIKeyReferencesSecret: secret.Name,
			},
		)
		if err != nil {
			return fmt.Errorf("failed listing unused KongCredentialAPIKeys: %w", err)
		}

		for _, cred := range l.Items {
			if err := cl.Delete(ctx, &cred); err != nil {
				return fmt.Errorf("failed deleting unused KongCredentialAPIKey %s: %w",
					client.ObjectKeyFromObject(&cred), err,
				)
			}
		}
	}

	return nil
}

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

	case *configurationv1alpha1.KongCredentialAPIKey:
		if cred.Spec.ConsumerRef.Name != consumer.Name ||
			cred.Spec.Key != string(secret.Data[CredentialSecretKeyNameAPIKeyKey]) {

			cred.Spec.ConsumerRef.Name = consumer.Name
			setKongCredentialAPIKeySpec(&cred.Spec, secret)
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
func ensureCredentialExists(
	ctx context.Context,
	cl client.Client,
	secret *corev1.Secret,
	credType string,
	kongConsumer *configurationv1.KongConsumer,
	scheme *runtime.Scheme,
) error {
	nn := types.NamespacedName{
		Namespace: secret.Namespace,
		Name:      secret.Name,
	}

	var cred client.Object
	switch credType {

	case KongCredentialTypeBasicAuth:
		cred = secretToKongCredentialBasicAuth(secret, kongConsumer)

	case KongCredentialTypeAPIKey:
		cred = secretToKongCredentialAPIKey(secret, kongConsumer)

	default:
		return fmt.Errorf("Secret %s used as credential, but has unsupported type %s",
			nn, credType,
		)
	}

	err := controllerutil.SetControllerReference(kongConsumer, cred, scheme)
	if err != nil {
		return err
	}
	// Set the secret as owner too so that deletion (or changes) of the credential
	// triggers the reconciliation of the secret in this controller.
	err = controllerutil.SetOwnerReference(secret, cred, scheme)
	if err != nil {
		return err
	}
	if err := cl.Create(ctx, cred); err != nil {
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

	// TODO: Add more credential types support.
	// TODO: https://github.com/Kong/gateway-operator/issues/1124
	// TODO: https://github.com/Kong/gateway-operator/issues/1125
	// TODO: https://github.com/Kong/gateway-operator/issues/1126
	switch credType {
	case KongCredentialTypeBasicAuth:
		if err := validateSecretForKongCredentialBasicAuth(s); err != nil {
			return err
		}
	case KongCredentialTypeAPIKey:
		if err := validateSecretForKongCredentialAPIKey(s); err != nil {
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

func validateSecretForKongCredentialAPIKey(s *corev1.Secret) error {
	if _, ok := s.Data[CredentialSecretKeyNameAPIKeyKey]; !ok {
		return fmt.Errorf("Secret %s used as key-auth credential, but lacks %s key",
			client.ObjectKeyFromObject(s), CredentialSecretKeyNameAPIKeyKey,
		)
	}
	return nil
}

func secretObjectMetaForConsumer(consumer *configurationv1.KongConsumer) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		GenerateName: consumer.Name + "-",
		Namespace:    consumer.Namespace,
	}
}

func secretToKongCredentialBasicAuth(
	s *corev1.Secret, c *configurationv1.KongConsumer,
) *configurationv1alpha1.KongCredentialBasicAuth {
	cred := &configurationv1alpha1.KongCredentialBasicAuth{
		ObjectMeta: secretObjectMetaForConsumer(c),
		Spec: configurationv1alpha1.KongCredentialBasicAuthSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: c.Name,
			},
		},
	}
	setKongCredentialBasicAuthSpec(&cred.Spec, s)
	return cred
}

func secretToKongCredentialAPIKey(
	s *corev1.Secret, k *configurationv1.KongConsumer,
) *configurationv1alpha1.KongCredentialAPIKey {
	cred := &configurationv1alpha1.KongCredentialAPIKey{
		ObjectMeta: secretObjectMetaForConsumer(k),
		Spec: configurationv1alpha1.KongCredentialAPIKeySpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: k.Name,
			},
		},
	}
	setKongCredentialAPIKeySpec(&cred.Spec, s)
	return cred
}

func setKongCredentialBasicAuthSpec(
	spec *configurationv1alpha1.KongCredentialBasicAuthSpec, s *corev1.Secret,
) {
	spec.Username = string(s.Data[corev1.BasicAuthUsernameKey])
	spec.Password = string(s.Data[corev1.BasicAuthPasswordKey])
}

func setKongCredentialAPIKeySpec(
	spec *configurationv1alpha1.KongCredentialAPIKeySpec, s *corev1.Secret,
) {
	spec.Key = string(s.Data[CredentialSecretKeyNameAPIKeyKey])
}

func (r KongCredentialSecretReconciler) handleConsumerUsingCredentialSecret(
	ctx context.Context,
	s *corev1.Secret,
	credType string,
	consumer *configurationv1.KongConsumer,
) (ctrl.Result, error) {
	// TODO: add more credentials types support.
	// TODO: https://github.com/Kong/gateway-operator/issues/1124
	// TODO: https://github.com/Kong/gateway-operator/issues/1125
	// TODO: https://github.com/Kong/gateway-operator/issues/1126

	switch credType {
	case KongCredentialTypeBasicAuth:
		var l configurationv1alpha1.KongCredentialBasicAuthList
		err := r.client.List(
			ctx,
			&l,
			client.MatchingFields{
				IndexFieldKongCredentialBasicAuthReferencesKongConsumer: consumer.Name,
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed listing KongCredentialBasicAuth: %w", err)
		}

		if res, err := handleCreds(ctx, r.client, s, credType, consumer, l.Items, r.scheme); err != nil || !res.IsZero() {
			return res, err
		}

	case KongCredentialTypeAPIKey:
		var l configurationv1alpha1.KongCredentialAPIKeyList
		err := r.client.List(
			ctx,
			&l,
			client.MatchingFields{
				IndexFieldKongCredentialAPIKeyReferencesKongConsumer: consumer.Name,
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed listing KongCredentialAPIKey: %w", err)
		}

		if res, err := handleCreds(ctx, r.client, s, credType, consumer, l.Items, r.scheme); err != nil || !res.IsZero() {
			return res, err
		}

	default:
		return ctrl.Result{}, fmt.Errorf("Secret %s used as credential, but has unsupported type %s",
			client.ObjectKeyFromObject(s), credType,
		)
	}

	return ctrl.Result{}, nil
}

func handleCreds[
	T constraints.SupportedCredentialType,
	TPtr constraints.KongCredential[T],
](
	ctx context.Context,
	cl client.Client,
	s *corev1.Secret,
	credType string,
	consumer *configurationv1.KongConsumer,
	creds []T,
	scheme *runtime.Scheme,
) (ctrl.Result, error) {
	switch len(creds) {
	case 0:
		if err := ensureCredentialExists(ctx, cl, s, credType, consumer, scheme); err != nil {
			return ctrl.Result{}, err
		}

	case 1:
		cred, ok := any(&creds[0]).(TPtr)
		if !ok {
			return ctrl.Result{}, fmt.Errorf("failed to cast Kong credential %T", creds[0])
		}

		if !cred.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{}, nil
		}

		res, err := ensureExistingCredential(ctx, cl, cred, s, consumer)
		if err != nil || !res.IsZero() {
			return res, err
		}

	default:
		if err := k8sreduce.ReduceKongCredentials[T, TPtr](ctx, cl, creds); err != nil {
			return ctrl.Result{}, err
		}
		// NOTE: requeue just in case we didn't perform any deletes.
		// Even if we'd check it here, we could still be running against stale cache.
		return ctrl.Result{
			Requeue: true,
		}, nil

	}

	return ctrl.Result{}, nil
}
