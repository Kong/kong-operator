package ops

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// konnectPolicyUser is a Konnect Event Gateway policy using an entity whose
// deletion is guarded (e.g. a TLS trust bundle or a static key).
type konnectPolicyUser struct {
	id   string
	name string
	// parentName is the name of the Konnect entity the policy belongs to
	// (e.g. its listener or virtual cluster).
	parentName string
}

// policyUsers are the users of an entity whose deletion is blocked, split
// between the policy objects in the entity's namespace, those in other
// namespaces and the Konnect policies managed from outside the cluster.
type policyUsers struct {
	// users are the policy objects in the entity's namespace (namespace/name,
	// sorted).
	users []string
	// otherNamespaces is the number of policy objects in other namespaces,
	// counted rather than named so as not to disclose them.
	otherNamespaces int
	// unmanaged are the Konnect policies (parent/policy, sorted) with no
	// policy object in the cluster behind them.
	unmanaged []string
}

// classifyPolicyUsers maps the Konnect policies using an entity in namespace
// to the cluster's policy objects, keyed by their Konnect ID in managed.
func classifyPolicyUsers(namespace string, konnectPolicies []konnectPolicyUser, managed map[string]client.ObjectKey) policyUsers {
	var u policyUsers
	for _, p := range konnectPolicies {
		user, ok := managed[p.id]
		switch {
		case !ok:
			u.unmanaged = append(u.unmanaged, p.parentName+"/"+p.name)
		case user.Namespace == namespace:
			u.users = append(u.users, user.String())
		default:
			u.otherNamespaces++
		}
	}
	slices.Sort(u.users)
	u.users = slices.Compact(u.users)
	slices.Sort(u.unmanaged)
	return u
}

// describePolicyUsers describes what uses an entity, kind being the policy
// objects' kind and konnectKind the Konnect policies' description.
func describePolicyUsers(kind, konnectKind string, users []string, otherNamespaces int, unmanaged []string) string {
	var parts []string
	if len(users) > 0 {
		parts = append(parts, kind+" "+strings.Join(users, ", "))
	}
	if otherNamespaces > 0 {
		parts = append(parts, fmt.Sprintf("%d %s in other namespaces", otherNamespaces, kind))
	}
	if len(unmanaged) > 0 {
		parts = append(parts, konnectKind+" "+strings.Join(unmanaged, ", ")+", which are not managed from this cluster")
	}
	return strings.Join(parts, ", and by ")
}

// decodeRawResponseBody decodes the JSON body of an SDK response into v. The
// SDK models Event Gateway policies' config as an empty struct, dropping it
// when decoding, but restores the raw response body afterwards, so the
// config can be read from it instead.
func decodeRawResponseBody(raw *http.Response, v any) error {
	if raw == nil || raw.Body == nil || raw.Body == http.NoBody {
		return ErrNilResponse
	}
	body, err := io.ReadAll(raw.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decoding response body: %w", err)
	}
	return nil
}
