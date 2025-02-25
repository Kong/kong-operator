package konnect

import (
	"context"
	"fmt"
	"reflect"

	"github.com/samber/lo"
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
	"github.com/kong/gateway-operator/pkg/clientops"
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
	// KongCredentialTypeJWT is the type of jwt-auth credential.
	// It's used as the values of konghq.com/credential label.
	KongCredentialTypeJWT = "jwt"
	// KongCredentialTypeACL is the type of Access Control Lists(ACLs) managed similar as credentials.
	// It's used as the value for konghq.com/credential label.
	KongCredentialTypeACL = "acl"
	// KongCredentialTypeHMAC is the type of HMAC credential, it's used
	// as the value for konghq.com/credential label.
	KongCredentialTypeHMAC = "hmac"
)

const (
	// CredentialSecretKeyNameAPIKeyKey is the credential secret key name for API key type.
	CredentialSecretKeyNameAPIKeyKey = "key"
	// CredentialSecretKeyNameACLGroupKey is the credential secret key name for ACL group type.
	CredentialSecretKeyNameACLGroupKey = "group"
	// CredentialSecretKeyNameJwtAlgorithmKey is the credential secret key name for JWT algorithm.
	CredentialSecretKeyNameJwtAlgorithmKey = "algorithm"
	// CredentualSecretKeyNameJwtKeyKey is the credential secret key name for JWT key.
	CredentialSecretKeyNameJwtKeyKey = "key"
	// CredentialSecretKeyNameJwtRSAPublicKeyKey is the credential secret key name for JWT RSA public key.
	CredentialSecretKeyNameJwtRSAPublicKeyKey = "rsa_public_key" //nolint:gosec
	// CredentialSecretKeyNameJwtSecretKey is the credentail secret ley name for JWT secret.
	CredentialSecretKeyNameJwtSecretKey = "secret"
	// CredentialSecretKeyNameHMACUsername is the credential secret key name for HMAC username type.
	CredentialSecretKeyNameHMACUsername = "username"
	// CredentialSecretKeyNameHMACSecret is the credential secret key name for HMAC secret type.
	CredentialSecretKeyNameHMACSecret = "secret"
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
		Owns(&configurationv1alpha1.KongCredentialACL{}, builder.MatchEveryOwner).
		Owns(&configurationv1alpha1.KongCredentialJWT{}, builder.MatchEveryOwner).
		Owns(&configurationv1alpha1.KongCredentialHMAC{}, builder.MatchEveryOwner).
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
		if err := clientops.DeleteAllFromList(ctx, cl, &l); err != nil {
			return fmt.Errorf("failed deleting unused KongCredentialBasicAuths: %w", err)
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
		if err := clientops.DeleteAllFromList(ctx, cl, &l); err != nil {
			return fmt.Errorf("failed deleting unused KongCredentialAPIKeys: %w", err)
		}

	case KongCredentialTypeACL:
		var l configurationv1alpha1.KongCredentialACLList
		err := cl.List(
			ctx, &l,
			client.MatchingFields{
				IndexFieldKongCredentialACLReferencesKongSecret: secret.Name,
			},
		)
		if err != nil {
			return fmt.Errorf("failed listing unused KongCredentialACLs: %w", err)
		}
		if err := clientops.DeleteAllFromList(ctx, cl, &l); err != nil {
			return fmt.Errorf("failed deleting unused KongCredentialACLs: %w", err)
		}

	case KongCredentialTypeJWT:
		var l configurationv1alpha1.KongCredentialJWTList
		err := cl.List(
			ctx, &l,
			client.MatchingFields{
				IndexFieldKongCredentialJWTReferencesSecret: secret.Name,
			},
		)
		if err != nil {
			return fmt.Errorf("failed listing unused KongCredentialJWTs: %w", err)
		}
		if err := clientops.DeleteAllFromList(ctx, cl, &l); err != nil {
			return fmt.Errorf("failed deleting unused KongCredentialJWTs: %w", err)
		}

	case KongCredentialTypeHMAC:
		var l configurationv1alpha1.KongCredentialHMACList
		err := cl.List(
			ctx, &l,
			client.MatchingFields{
				IndexFieldKongCredentialHMACReferencesSecret: secret.Name,
			},
		)
		if err != nil {
			return fmt.Errorf("failed listing unused KongCredentialHMACs: %w", err)
		}
		if err := clientops.DeleteAllFromList(ctx, cl, &l); err != nil {
			return fmt.Errorf("failed deleting unused KongCredentialHMACs: %w", err)
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

	case *configurationv1alpha1.KongCredentialACL:
		if cred.Spec.ConsumerRef.Name != consumer.Name ||
			cred.Spec.Group != string(secret.Data[CredentialSecretKeyNameACLGroupKey]) {

			cred.Spec.ConsumerRef.Name = consumer.Name
			setKongCredentialACLSpec(&cred.Spec, secret)
			update = true
		}

	case *configurationv1alpha1.KongCredentialJWT:
		if cred.Spec.ConsumerRef.Name != consumer.Name ||
			(cred.Spec.Key != nil && *cred.Spec.Key != string(secret.Data[CredentialSecretKeyNameJwtKeyKey])) {

			cred.Spec.ConsumerRef.Name = consumer.Name
			setKongCredentialJWTSpec(&cred.Spec, secret)
			update = true
		}
	case *configurationv1alpha1.KongCredentialHMAC:
		if cred.Spec.ConsumerRef.Name != consumer.Name ||
			*cred.Spec.Username != string(secret.Data[CredentialSecretKeyNameHMACUsername]) ||
			*cred.Spec.Secret != string(secret.Data[CredentialSecretKeyNameHMACSecret]) {
			cred.Spec.ConsumerRef.Name = consumer.Name
			setKongCredentialHMACSpec(&cred.Spec, secret)
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

	case KongCredentialTypeACL:
		cred = secretToKongCredentialACL(secret, kongConsumer)

	case KongCredentialTypeJWT:
		cred = secretToKongCredentialJWT(secret, kongConsumer)

	case KongCredentialTypeHMAC:
		cred = secretToKongCredentialHMAC(secret, kongConsumer)

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

	switch credType {
	case KongCredentialTypeBasicAuth:
		if err := validateSecretForKongCredentialBasicAuth(s); err != nil {
			return err
		}
	case KongCredentialTypeAPIKey:
		if err := validateSecretForKongCredentialAPIKey(s); err != nil {
			return err
		}
	case KongCredentialTypeACL:
		if err := validateSecretForKongCredentialACL(s); err != nil {
			return err
		}
	case KongCredentialTypeJWT:
		if err := validateSecretForKongCredentialJWT(s); err != nil {
			return err
		}
	case KongCredentialTypeHMAC:
		if err := validateSecretForKongCredentialHMAC(s); err != nil {
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

func validateSecretForKongCredentialACL(s *corev1.Secret) error {
	if _, ok := s.Data[CredentialSecretKeyNameACLGroupKey]; !ok {
		return fmt.Errorf(
			"Secret %s used as ACL credential, but lacks %s key",
			client.ObjectKeyFromObject(s), CredentialSecretKeyNameACLGroupKey,
		)
	}
	return nil
}

// credentialJWTAllowedAlgorithms is the list of Allowed algorithms for JWT.
var credentialJWTAllowedAlgorithms = lo.SliceToMap(
	[]string{
		"HS256",
		"HS384",
		"HS512",
		"RS256",
		"RS384",
		"RS512",
		"ES256",
		"ES384",
		"ES512",
		"PS256",
		"PS384",
		"PS512",
		"EdDSA",
	},
	func(s string) (string, struct{}) { return s, struct{}{} },
)

var credentialJWTAlgorithmsRequiringRSAPublicKey = lo.SliceToMap(
	[]string{
		"RS256",
		"RS384",
		"RS512",
		"PS256",
		"PS384",
		"PS512",
		"EdDSA",
	},
	func(s string) (string, struct{}) { return s, struct{}{} },
)

func validateSecretForKongCredentialJWT(s *corev1.Secret) error {
	// check if 'key' exists.
	if _, ok := s.Data[CredentialSecretKeyNameJwtKeyKey]; !ok {
		return fmt.Errorf(
			"Secret %s used as JWT credential, but lacks %s key",
			client.ObjectKeyFromObject(s), CredentialSecretKeyNameJwtKeyKey,
		)
	}
	// validate algorithm.
	algorithm, ok := s.Data[CredentialSecretKeyNameJwtAlgorithmKey]
	if !ok {
		return fmt.Errorf(
			"Secret %s used as JWT credential, but lacks %s key",
			client.ObjectKeyFromObject(s), CredentialSecretKeyNameJwtAlgorithmKey,
		)
	}
	// check if algorithm is supported.
	if _, ok := credentialJWTAllowedAlgorithms[string(algorithm)]; !ok {
		return fmt.Errorf(
			"Secret %s used as JWT credential, but algorithm '%s' is invalid or not supported",
			client.ObjectKeyFromObject(s), algorithm,
		)
	}
	// check rsa_public_key if the algorithm requires.
	if _, ok := credentialJWTAlgorithmsRequiringRSAPublicKey[string(algorithm)]; ok {
		if _, hasRSAPublicKey := s.Data[CredentialSecretKeyNameJwtRSAPublicKeyKey]; !hasRSAPublicKey {
			return fmt.Errorf("Secret %s used as JWT credential, but lacks %s key which is required when algorithm is %s",
				client.ObjectKeyFromObject(s),
				CredentialSecretKeyNameJwtRSAPublicKeyKey, algorithm,
			)
		}
	}
	return nil
}

func validateSecretForKongCredentialHMAC(s *corev1.Secret) error {
	if _, ok := s.Data[CredentialSecretKeyNameHMACUsername]; !ok {
		return fmt.Errorf("Secret %s used as HMAC credential, but lacks %s key",
			client.ObjectKeyFromObject(s), CredentialSecretKeyNameHMACUsername,
		)
	}
	if _, ok := s.Data[CredentialSecretKeyNameHMACSecret]; !ok {
		return fmt.Errorf("Secret %s used as HMAC credential, but lacks %s key",
			client.ObjectKeyFromObject(s), CredentialSecretKeyNameHMACSecret,
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

func secretToKongCredentialACL(
	s *corev1.Secret, c *configurationv1.KongConsumer,
) *configurationv1alpha1.KongCredentialACL {
	cred := &configurationv1alpha1.KongCredentialACL{
		ObjectMeta: secretObjectMetaForConsumer(c),
		Spec: configurationv1alpha1.KongCredentialACLSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: c.Name,
			},
		},
	}

	setKongCredentialACLSpec(&cred.Spec, s)
	return cred
}

func secretToKongCredentialJWT(
	s *corev1.Secret, c *configurationv1.KongConsumer,
) *configurationv1alpha1.KongCredentialJWT {
	cred := &configurationv1alpha1.KongCredentialJWT{
		ObjectMeta: secretObjectMetaForConsumer(c),
		Spec: configurationv1alpha1.KongCredentialJWTSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: c.Name,
			},
		},
	}

	setKongCredentialJWTSpec(&cred.Spec, s)
	return cred
}

func secretToKongCredentialHMAC(
	s *corev1.Secret, c *configurationv1.KongConsumer,
) *configurationv1alpha1.KongCredentialHMAC {
	cred := &configurationv1alpha1.KongCredentialHMAC{
		ObjectMeta: secretObjectMetaForConsumer(c),
		Spec: configurationv1alpha1.KongCredentialHMACSpec{
			ConsumerRef: corev1.LocalObjectReference{
				Name: c.Name,
			},
		},
	}

	setKongCredentialHMACSpec(&cred.Spec, s)
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

func setKongCredentialACLSpec(
	spec *configurationv1alpha1.KongCredentialACLSpec, s *corev1.Secret,
) {
	spec.Group = string(s.Data[CredentialSecretKeyNameACLGroupKey])
}

func setKongCredentialJWTSpec(
	spec *configurationv1alpha1.KongCredentialJWTSpec, s *corev1.Secret,
) {
	spec.Algorithm = string(s.Data[CredentialSecretKeyNameJwtAlgorithmKey])
	spec.Key = lo.ToPtr(string(s.Data[CredentialSecretKeyNameJwtKeyKey]))

	if rsaPublicKey, ok := s.Data[CredentialSecretKeyNameJwtRSAPublicKeyKey]; ok {
		spec.RSAPublicKey = lo.ToPtr(string(rsaPublicKey))
	}

	if jwtSecret, ok := s.Data[CredentialSecretKeyNameJwtSecretKey]; ok {
		spec.Secret = lo.ToPtr(string(jwtSecret))
	}
}

func setKongCredentialHMACSpec(
	spec *configurationv1alpha1.KongCredentialHMACSpec, s *corev1.Secret,
) {
	spec.Username = lo.ToPtr(string(s.Data[CredentialSecretKeyNameHMACUsername]))
	spec.Secret = lo.ToPtr(string(s.Data[CredentialSecretKeyNameHMACSecret]))
}

func (r KongCredentialSecretReconciler) handleConsumerUsingCredentialSecret(
	ctx context.Context,
	s *corev1.Secret,
	credType string,
	consumer *configurationv1.KongConsumer,
) (ctrl.Result, error) {
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

	case KongCredentialTypeACL:
		var l configurationv1alpha1.KongCredentialACLList
		err := r.client.List(
			ctx,
			&l,
			client.MatchingFields{
				IndexFieldKongCredentialACLReferencesKongConsumer: consumer.Name,
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed listing KongCredentialACL: %w", err)
		}
		if res, err := handleCreds(ctx, r.client, s, credType, consumer, l.Items, r.scheme); err != nil || !res.IsZero() {
			return res, err
		}

	case KongCredentialTypeJWT:
		var l configurationv1alpha1.KongCredentialJWTList
		err := r.client.List(
			ctx,
			&l,
			client.MatchingFields{
				IndexFieldKongCredentialJWTReferencesKongConsumer: consumer.Name,
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed listing KongCrenentialJWT: %w", err)
		}
		if res, err := handleCreds(ctx, r.client, s, credType, consumer, l.Items, r.scheme); err != nil || !res.IsZero() {
			return res, err
		}

	case KongCredentialTypeHMAC:
		var l configurationv1alpha1.KongCredentialHMACList
		err := r.client.List(
			ctx,
			&l,
			client.MatchingFields{
				IndexFieldKongCredentialHMACReferencesKongConsumer: consumer.Name,
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed listing KongCredentialHMAC: %w", err)
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
		var cred TPtr = &creds[0]

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
