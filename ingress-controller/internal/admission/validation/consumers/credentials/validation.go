// Package credentials includes validators for the credentials provided for KongConsumers.
package credentials

import (
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/ingress-controller/internal/labels"
	"github.com/kong/kong-operator/ingress-controller/internal/util"
)

// -----------------------------------------------------------------------------
//  Validation - Public Functions
// -----------------------------------------------------------------------------

// ValidateCredentials performs basic validation on a credential secret given
// the Kubernetes secret which contains credentials data.
func ValidateCredentials(secret *corev1.Secret) error {
	credentialType, err := util.ExtractKongCredentialType(secret)
	if err != nil {
		// this shouldn't occur, since we check this earlier in the admission controller's handleSecret function, but
		// checking here also in case a refactor removes that
		return fmt.Errorf("secret has no credential type, add a %s label", labels.CredentialTypeLabel)
	}

	// verify that the credential type provided is valid
	if !ValidTypes.Has(credentialType) {
		return fmt.Errorf("invalid credential type %s", credentialType)
	}

	// skip further validation if the credential type is not supported.
	if !SupportedTypes.Has(credentialType) {
		return nil
	}

	// Check if we're dealing with a JWT credential with an HMAC algorithm.
	// In this case, the rsa_public_key field is not required.
	algo, hasAlgo := secret.Data["algorithm"]
	ignoreMissingRSAPublicKey := credentialType == "jwt" && hasAlgo && algoIsHMAC(string(algo))

	ignoreMissingSecretKey := credentialType == "jwt" && hasAlgo && !algoIsHMAC(string(algo))

	// verify that all required fields are present
	var missingFields []string
	var missingDataFields []string
	for _, field := range CredTypeToFields[credentialType] {
		// Ignore missing rsa_public_key for jwt credentials with HMAC algorithm
		if field == "rsa_public_key" && ignoreMissingRSAPublicKey {
			continue
		}

		// Ignore missing secret for jwt credentials with non HMAC algorithm
		if field == "secret" && ignoreMissingSecretKey {
			continue
		}

		// verify whether the required field is missing
		requiredData, ok := secret.Data[field]
		if !ok {
			missingFields = append(missingFields, field)
			continue
		}

		// verify whether the required field is present, but missing data
		if len(requiredData) < 1 {
			missingDataFields = append(missingDataFields, field)
		}
	}

	// report on any required fields that were missing
	if len(missingFields) > 0 {
		return fmt.Errorf("missing required field(s): %s", strings.Join(missingFields, ", "))
	}

	// report on any required fields that were present, but were missing actual data
	if len(missingDataFields) > 0 {
		return fmt.Errorf("some fields were invalid due to missing data: %s", strings.Join(missingDataFields, ", "))
	}

	return nil
}

// -----------------------------------------------------------------------------
//  Validation - Credentials
// -----------------------------------------------------------------------------

// Credential is a metadata struct to help validate the contents of
// consumer credentials, particularly unique constraints on the underlying data.
type Credential struct {
	// Type indicates the credential type, which will reference one of the types
	// in the SupportedTypes set.
	Type string

	// Key is the key for the credentials data
	Key string

	// Value is the data provided for the key
	Value string
}

// -----------------------------------------------------------------------------
// Validation - Validating Index
// -----------------------------------------------------------------------------

// Index is a map of credentials types to a map of credential keys to the underlying
// values already seen for that type and key. This type is used as a history tracker
// for validation so that callers can keep track of the credentials they've seen thus
// far and validate whether new credentials they encounter are in violation of any
// constraints on their respective types.
type Index map[string]map[string]map[string]struct{}

// ValidateCredentialsForUniqueKeyConstraints will attempt to add a new Credential to the CredentialsTypeMap
// and will validate it for both normal structure validation and for
// unique key constraint violations.
func (cs Index) ValidateCredentialsForUniqueKeyConstraints(secret *corev1.Secret) error {
	credentialType, err := util.ExtractKongCredentialType(secret)
	if err != nil {
		return fmt.Errorf("secret has no credential type, add a %s label", labels.CredentialTypeLabel)
	}

	// the additional key/values are optional, but must be validated
	// for unique constraint violations. Using an index of credentials
	// validation will be checked on any Add() to the index, so errors
	// from this include the unique key constraint errors.
	for k, v := range secret.Data {
		if err := cs.add(Credential{
			Type:  credentialType,
			Key:   k,
			Value: string(v),
		}); err != nil {
			return err
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// Valdating Index - Private Methods
// -----------------------------------------------------------------------------

func (cs Index) add(newCred Credential) error {
	// retrieve all the keys which are constrained for this type
	constraints, ok := uniqueKeyConstraints[newCred.Type]
	if !ok {
		return nil // there are no constraints for this credType
	}

	// for each key which is constrained for this type check the existing list
	// to see if there are any violations of that constraint given the new credentials
	for _, constrainedKey := range constraints {
		if newCred.Key == constrainedKey { // this key has constraints on it, we need to check for violations
			if _, ok := cs[newCred.Type][newCred.Key][newCred.Value]; ok {
				return fmt.Errorf("unique key constraint violated for %s", newCred.Key)
			}
		}
	}

	// if needed, initialize the index
	if cs[newCred.Type] == nil {
		cs[newCred.Type] = map[string]map[string]struct{}{newCred.Key: {newCred.Value: {}}}
	}
	if cs[newCred.Type][newCred.Key] == nil {
		cs[newCred.Type][newCred.Key] = make(map[string]struct{})
	}

	// if we make it here there's been no constraint violation, add it to the index
	cs[newCred.Type][newCred.Key][newCred.Value] = struct{}{}

	return nil
}

func algoIsHMAC(algo string) bool {
	return slices.Contains([]string{"HS256", "HS384", "HS512"}, algo)
}
